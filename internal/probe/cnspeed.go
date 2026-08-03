package probe

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs/internal/model"
)

// 中国三网就近测速。
//
// 对应 oneclickvirt 的 -speed 与 spiritLHLS 的 ecsspeed：海外 VPS 到中国电信、
// 联通、移动的实际带宽，是中文 VPS 圈最常看的一项，而 iperf3 的国际节点测不出来。
//
// 两个必须说清楚的现实：
//
//  1. **Ookla 已经拿不到全球节点列表**。speedtest.net 的 servers API 与
//     speedtest-servers-static.php 实测均返回 403，c.speedtest.net 虽然可用但只
//     返回基于出口 IP 的就近 10 个节点——海外机器拿到的全是当地节点。开源的
//     sivel/speedtest-cli 也因此只能列出就近节点（这大概也是它 2024 年后停更的原因）。
//     因此中国节点只能依赖社区维护的清单。
//  2. **清单必然会过期**，所以实时抓取而不内置快照；抓不到就跳过本模块，
//     绝不用一份陈旧的内置清单去测已经下线的节点。

type cnSpeedProbe struct{}

func (cnSpeedProbe) ID() string         { return "cnspeed" }
func (cnSpeedProbe) Title() string      { return "中国三网测速" }
func (cnSpeedProbe) NeedsNetwork() bool { return true }

// cnNodeListURL 是社区维护的中国测速节点清单。
//
// 来源 spiritLHLS/speedtest.cn-CN-ID（MIT），每日自动更新，带运营商标注。
// ecs 只读取清单，不复制其内容入库——内置快照会过期，而过期清单比没有更糟。
const cnNodeListURL = "https://raw.githubusercontent.com/spiritLHLS/speedtest.cn-CN-ID/main/CN.csv"

const (
	cnSpeedDuration       = 8 * time.Second
	cnSpeedMaxBytes int64 = 100 * 1024 * 1024
)

func cnSpeedBudget() (time.Duration, int64) {
	// Profiles only choose the module preset; cnspeed always keeps the full
	// download depth when explicitly selected.
	return cnSpeedDuration, cnSpeedMaxBytes
}

// cnNodeListURLForTest 让测试把清单指向本地服务器，生产路径始终用上面的常量。
var cnNodeListURLForTest = cnNodeListURL

// cnCarriers 是要覆盖的三大运营商。
var cnCarriers = []string{"电信", "联通", "移动"}

// cnNode 是清单里的一个测速节点。
type cnNode struct {
	ID          string
	Operator    string
	Province    string
	City        string
	Host        string
	PingURL     string
	DownloadURL string
}

// cnNodeResult 是一个运营商的测速结果。
type cnNodeResult struct {
	Carrier   string
	Node      cnNode
	LatencyMS float64
	Mbps      float64
	Bytes     int64
	Err       string
	Tried     int
}

// fetchCNNodes 抓取并解析节点清单。
func fetchCNNodes(ctx context.Context, client *http.Client, userAgent string) ([]cnNode, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cnNodeListURLForTest, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	reader := csv.NewReader(io.LimitReader(response.Body, 2*1024*1024))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, fmt.Errorf("清单解析失败")
	}
	index := make(map[string]int, len(records[0]))
	for position, name := range records[0] {
		index[strings.TrimSpace(name)] = position
	}
	get := func(row []string, name string) string {
		position, ok := index[name]
		if !ok || position >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[position])
	}
	var nodes []cnNode
	for _, row := range records[1:] {
		// active=0 的节点清单方自己已标记为不可用。
		if get(row, "active") == "0" {
			continue
		}
		node := cnNode{
			ID:          get(row, "id"),
			Operator:    get(row, "operator"),
			Province:    get(row, "province"),
			City:        get(row, "city"),
			Host:        get(row, "host"),
			PingURL:     get(row, "pingUrl"),
			DownloadURL: get(row, "downloadUrl"),
		}
		if node.PingURL == "" || node.DownloadURL == "" || node.Operator == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("清单里没有可用节点")
	}
	return nodes, nil
}

// cnPingNode 测一个节点的 HTTP 往返延迟。
func cnPingNode(ctx context.Context, client *http.Client, userAgent string, node cnNode) (float64, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(pingCtx, http.MethodGet, node.PingURL, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", userAgent)
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return float64(time.Since(start)) / float64(time.Millisecond), nil
}

// cnDownload 对节点做限时下载测速。
//
// 同时限时长与限字节：只限时长会在高带宽链路上拉走几百 MB，
// 只限字节会在慢链路上耗到超时。
func cnDownload(ctx context.Context, client *http.Client, userAgent string, node cnNode,
	duration time.Duration, maxBytes int64) (float64, int64, error) {
	downloadCtx, cancel := context.WithTimeout(ctx, duration+5*time.Second)
	defer cancel()
	url := node.DownloadURL
	if !strings.Contains(url, "?") {
		url += "?size=200000000"
	}
	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	request.Header.Set("User-Agent", userAgent)
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	buffer := make([]byte, 64*1024)
	var total int64
	deadline := start.Add(duration)
	for {
		if time.Now().After(deadline) || total >= maxBytes {
			break
		}
		count, readErr := response.Body.Read(buffer)
		total += int64(count)
		if readErr != nil {
			break
		}
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 || total == 0 {
		return 0, total, fmt.Errorf("未读到数据")
	}
	return float64(total) * 8 / elapsed / 1e6, total, nil
}

func (cnSpeedProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("cnspeed", "中国三网测速")
	result.Description = "从社区清单选取电信/联通/移动就近节点，测 HTTP 下载带宽"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "HTTP download against speedtest.cn nodes",
		Profile:         "per-carrier nearest node by HTTP latency",
		ComparisonScope: "当次到该节点的 HTTP 下载带宽；节点、时段与运营商策略都会影响结果",
	}
	client, closeClient := httpClientForMode(env)
	defer closeClient()
	env.HTTPClient = client

	nodes, err := fetchCNNodes(ctx, env.HTTPClient, env.UserAgent)
	if err != nil {
		result.Skip("无法获取中国测速节点清单")
		result.Notes = append(result.Notes, fmt.Sprintf(
			"抓取节点清单失败（%s）。清单实时获取而不内置快照：节点会下线，"+
				"用过期清单去测已经消失的节点比不测更糟。", compactError(err)))
		result.Finish(start)
		return result
	}

	byCarrier := make(map[string][]cnNode)
	for _, node := range nodes {
		byCarrier[node.Operator] = append(byCarrier[node.Operator], node)
	}

	// 每个运营商最多试这么多节点来选最快的，避免把清单里几十个节点全 ping 一遍。
	const probeCandidates = 6
	duration, maxBytes := cnSpeedBudget()

	results := make([]cnNodeResult, len(cnCarriers))
	var wg sync.WaitGroup
	for index, carrier := range cnCarriers {
		wg.Add(1)
		go func(index int, carrier string) {
			defer wg.Done()
			results[index] = measureCarrier(ctx, env, byCarrier[carrier], carrier, probeCandidates)
		}(index, carrier)
	}
	wg.Wait()

	// 下载串行执行：三个运营商同时拉流会互相抢占带宽，测出来的是瓜分后的结果。
	for index := range results {
		item := &results[index]
		if item.Err != "" || item.Node.DownloadURL == "" || ctx.Err() != nil {
			continue
		}
		mbps, bytes, err := cnDownload(ctx, env.HTTPClient, env.UserAgent, item.Node, duration, maxBytes)
		if err != nil {
			item.Err = "下载失败：" + compactError(err)
			continue
		}
		item.Mbps = mbps
		item.Bytes = bytes
	}

	table := model.Table{
		Title:   "三网就近节点下载带宽",
		Columns: []string{"运营商", "节点", "位置", "HTTP 延迟", "下载", "已传输", "状态"},
	}
	succeeded := 0
	var totalBytes int64
	for _, item := range results {
		status := "完成"
		if item.Err != "" {
			status = item.Err
		}
		location, nodeName, latency, download, transferred := "—", "—", "—", "—", "—"
		if item.Node.ID != "" {
			nodeName = item.Node.ID
			location = strings.TrimSpace(item.Node.Province + " " + item.Node.City)
			latency = fmt.Sprintf("%.1f ms", item.LatencyMS)
		}
		if item.Mbps > 0 {
			succeeded++
			totalBytes += item.Bytes
			download = model.FormatRate(item.Mbps, "Mbps")
			transferred = model.FormatBytes(uint64(item.Bytes))
			result.Measurements = append(result.Measurements, model.Measurement{
				Key:   "cnspeed_" + carrierKey(item.Carrier) + "_download_mbps",
				Label: item.Carrier + " 下载带宽",
				Value: item.Mbps, Unit: "Mbps", Display: model.FormatRate(item.Mbps, "Mbps"),
				Method:         fmt.Sprintf("http-download-%ds-cap%dMiB-v1", int(duration.Seconds()), maxBytes/1024/1024),
				HigherIsBetter: model.BoolPtr(true),
			})
		}
		table.Rows = append(table.Rows, []string{
			item.Carrier, nodeName, location, latency, download, transferred, status,
		})
	}
	result.Tables = []model.Table{table}
	result.Fields = []model.Field{
		{Key: "node_list", Label: "节点清单来源", Value: "spiritLHLS/speedtest.cn-CN-ID（MIT，每日更新）"},
		{Key: "nodes_available", Label: "清单可用节点", Value: fmt.Sprintf("%d", len(nodes))},
		{Key: "download_budget", Label: "单运营商上限", Value: fmt.Sprintf("%s 或 %d MiB", duration, maxBytes/1024/1024)},
	}
	result.Sources = []model.Source{
		{Name: "speedtest.cn-CN-ID", URL: "https://github.com/spiritLHLS/speedtest.cn-CN-ID",
			Purpose: "中国测速节点清单与运营商标注（MIT，每日更新）"},
	}
	result.Notes = append(result.Notes,
		"节点清单实时抓取而不内置快照：节点会下线，用过期清单去测已经消失的节点比不测更糟。",
		"每个运营商先按 HTTP 延迟从候选节点里选最快的一个，再对它做限时下载；"+
			"三个运营商的下载串行执行，同时拉流会互相抢带宽，测出来是瓜分后的结果。",
		"这是到某个具体测速节点的 HTTP 带宽，不是“到该运营商全网”的带宽；"+
			"节点负载、时段、运营商对跨境流量的策略都会显著影响数值。",
		"Ookla 已不再提供全球节点列表（speedtest.net 的相关接口实测返回 403，"+
			"c.speedtest.net 只回基于出口 IP 的就近节点），因此中国节点只能依赖社区维护的清单。",
	)
	if succeeded == 0 {
		result.Status = model.StatusWarning
		result.Summary = "三网节点均未测得带宽"
	} else {
		if succeeded < len(cnCarriers) {
			result.Status = model.StatusWarning
		}
		var parts []string
		for _, item := range results {
			if item.Mbps > 0 {
				parts = append(parts, fmt.Sprintf("%s %s", item.Carrier, model.FormatRate(item.Mbps, "Mbps")))
			}
		}
		result.Summary = strings.Join(parts, " · ") + fmt.Sprintf(" · 共传输 %s", model.FormatBytes(uint64(totalBytes)))
	}
	result.Finish(start)
	return result
}

// measureCarrier 在一个运营商的候选节点里按延迟挑最快的。
func measureCarrier(ctx context.Context, env Environment, candidates []cnNode, carrier string, limit int) cnNodeResult {
	item := cnNodeResult{Carrier: carrier}
	if len(candidates) == 0 {
		item.Err = "清单中无该运营商节点"
		return item
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	type latencySample struct {
		node cnNode
		ms   float64
	}
	var samples []latencySample
	var mutex sync.Mutex
	var wg sync.WaitGroup
	for _, node := range candidates {
		wg.Add(1)
		go func(node cnNode) {
			defer wg.Done()
			ms, err := cnPingNode(ctx, env.HTTPClient, env.UserAgent, node)
			if err != nil {
				return
			}
			mutex.Lock()
			samples = append(samples, latencySample{node: node, ms: ms})
			mutex.Unlock()
		}(node)
	}
	wg.Wait()
	if len(samples) == 0 {
		item.Tried = len(candidates)
		item.Err = fmt.Sprintf("%d 个候选节点均不可达", len(candidates))
		return item
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].ms < samples[j].ms })
	item.Node = samples[0].node
	item.LatencyMS = samples[0].ms
	item.Tried = len(candidates)
	return item
}

// carrierKey 把运营商名转成指标键用的拉丁标识。
func carrierKey(carrier string) string {
	switch carrier {
	case "电信":
		return "telecom"
	case "联通":
		return "unicom"
	case "移动":
		return "mobile"
	default:
		return "other"
	}
}

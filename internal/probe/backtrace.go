package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

// 三网回程线路识别。
//
// 原理与 backtrace 一致：从 VPS 向三大运营商的参考 IP 做路由追踪，观察路径上
// 出现的中国骨干网段。国内运营商的进出向路由通常走同一条线路，因此这条路径上
// 的骨干特征就反映了"中国用户访问该 VPS 时数据回来走哪条线"。
//
// 需要强调的是：这是从 VPS 主动发出的探测，不是真正意义上的反向抓包。路径不对
// 称、运营商临时调度、目标侧过滤都会让结论失真，因此每条判定都保留命中的跳号、
// IP 和原始输出，没有命中已知特征时一律返回"未识别"，绝不硬猜。

type backtraceProbe struct{}

func (backtraceProbe) ID() string         { return "backtrace" }
func (backtraceProbe) Title() string      { return "三网回程" }
func (backtraceProbe) NeedsNetwork() bool { return true }

// routeSignature 是一条中国骨干网线路的识别特征。
type routeSignature struct {
	// Prefix 是该线路骨干网段的地址前缀。
	Prefix string
	// ASN 是承载该线路的自治域号。
	ASN int
	// Code 是线路简称，用于表格。
	Code string
	// Label 是完整线路名。
	Label string
	// Carrier 是所属运营商。
	Carrier string
	// Quality 越大代表线路越优质，用于在多个命中之间挑选结论。
	Quality int
}

// chinaRouteSignatures 覆盖三大运营商的主要骨干线路。
//
// 网段与 AS 对应关系来自各运营商公开的骨干网编号，属于长期稳定的公开事实；
// 但运营商会调整网段用途，因此这里只做前缀匹配并保留证据，供人工复核。
var chinaRouteSignatures = []routeSignature{
	// 中国电信：CN2 优于 163。
	{Prefix: "59.43.", ASN: 4809, Code: "CN2", Label: "电信 CN2（AS4809）", Carrier: "电信", Quality: 30},
	{Prefix: "202.97.", ASN: 4134, Code: "163", Label: "电信 163 骨干（AS4134）", Carrier: "电信", Quality: 10},

	// 中国联通：CUII/A 网优于 169。
	{Prefix: "218.105.", ASN: 9929, Code: "CUII", Label: "联通 CUII / A 网（AS9929）", Carrier: "联通", Quality: 30},
	{Prefix: "210.51.", ASN: 9929, Code: "CUII", Label: "联通 CUII / A 网（AS9929）", Carrier: "联通", Quality: 30},
	{Prefix: "219.158.", ASN: 4837, Code: "169", Label: "联通 169 骨干（AS4837）", Carrier: "联通", Quality: 10},

	// 中国移动：CMI 国际优于普通 CMNET。
	{Prefix: "223.120.", ASN: 58807, Code: "CMI", Label: "移动 CMI 国际（AS58807）", Carrier: "移动", Quality: 30},
	{Prefix: "221.183.", ASN: 9808, Code: "CMNET", Label: "移动 CMNET 骨干（AS9808）", Carrier: "移动", Quality: 10},
}

// backtraceMaxHops 是回程识别的跳数上限。
//
// 从海外 VPS 到中国骨干通常在 10-15 跳之间进入 202.97 / 59.43 / 219.158 这类
// 特征段，之后往往被目标侧过滤成连续的 `*`。20 跳能覆盖特征段又不会把时间浪费
// 在必然无响应的尾部；12 跳的路径快照上限则会让特征来不及出现。
const backtraceMaxHops = 20

// backtraceConcurrency 限制同时进行的追踪数量。
//
// 运营商与中间设备普遍对 ICMP/UDP 探测限速：实测中并发 6 个追踪会让关键跳全部
// 变成 `*`，同一目标单独跑却能稳定命中骨干段。宁可慢一点也不能把限速造成的丢包
// 误判成"未识别"。
const backtraceConcurrency = 2

// backtraceHit 是一次特征命中。
type backtraceHit struct {
	Signature routeSignature
	Hop       int
	IP        string
}

// backtraceRow 是一个参考目标的追踪结论。
type backtraceRow struct {
	Target config.Endpoint
	Hits   []backtraceHit
	Hops   []string
	Raw    string
	Err    error
}

// hopPrefixPattern 匹配 traceroute 文本输出的跳号前缀。
var (
	hopLinePattern = regexp.MustCompile(`^\s*(\d{1,2})\s+(.*)$`)
	ipv4Pattern    = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)
)

func (backtraceProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("backtrace", "三网回程")
	result.Description = "向三大运营商参考 IP 追踪路径，识别路径上的中国骨干线路"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "启发式判断",
		Engine:          "NextTrace/traceroute + 骨干网段特征表",
		Profile:         "china backbone signatures v1, max 30 hops",
		ComparisonScope: "当次探测的路径特征；不是性能基准，也不等同于反向抓包",
	}

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Skip("未发现 nexttrace、traceroute 或 tracepath")
		result.Notes = append(result.Notes, "安装 traceroute 或 NextTrace 后重跑即可识别三网回程线路。")
		result.Finish(start)
		return result
	}

	targets := env.Config.BacktraceTargets
	if len(targets) == 0 {
		result.Skip("未配置三网回程参考目标")
		result.Finish(start)
		return result
	}

	rows := make([]backtraceRow, len(targets))
	semaphore := make(chan struct{}, backtraceConcurrency)
	var wg sync.WaitGroup
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target config.Endpoint) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			rows[index] = runBacktraceTarget(ctx, engine, target)
		}(index, target)
	}
	wg.Wait()

	table := model.Table{
		Title:   "三网回程线路",
		Columns: []string{"运营商", "参考目标", "线路", "命中跳", "命中 IP", "状态"},
		// 命中 IP 会暴露机房出口位置，默认按段遮盖。
		SensitiveColumns: []int{4},
	}
	identified := 0
	var summaries []string
	for _, row := range rows {
		// 原始路径无论识别成功与否都要保留：未识别时它恰恰是判断"线路确实没走
		// 已知骨干"还是"探测被限速打断"的唯一依据。
		if row.Raw != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:     fmt.Sprintf("%s (%s) 原始路径", row.Target.Name, row.Target.Address),
				Language:  "text",
				Content:   row.Raw,
				Sensitive: true,
			})
		}
		if row.Err != nil {
			table.Rows = append(table.Rows, []string{
				row.Target.Kind, row.Target.Name, "—", "—", "—", "追踪失败",
			})
			result.Notes = append(result.Notes, fmt.Sprintf("%s 追踪失败：%s", row.Target.Name, compactError(row.Err)))
			continue
		}
		best, ok := bestBacktraceHit(row.Hits)
		if !ok {
			responded := 0
			for _, hop := range row.Hops {
				if hop != "" {
					responded++
				}
			}
			status := fmt.Sprintf("%d 跳无已知特征", len(row.Hops))
			// 绝大多数跳都没响应时，更可能是探测被限速或过滤，而不是线路真的陌生。
			if len(row.Hops) > 0 && responded*2 < len(row.Hops) {
				status = fmt.Sprintf("%d/%d 跳无响应，可能被限速", len(row.Hops)-responded, len(row.Hops))
			}
			table.Rows = append(table.Rows, []string{
				row.Target.Kind, row.Target.Name, "未识别", "—", "—", status,
			})
			continue
		}
		identified++
		table.Rows = append(table.Rows, []string{
			row.Target.Kind,
			row.Target.Name,
			describeBacktraceLine(best, row.Hits),
			fmt.Sprintf("%d", best.Hop),
			best.IP,
			"已识别",
		})
		summaries = append(summaries, row.Target.Kind+" "+best.Signature.Code)
	}
	result.Tables = []model.Table{table}
	result.Measurements = []model.Measurement{
		{
			Key: "backtrace_identified", Label: "已识别线路",
			Value: float64(identified), Unit: "项",
			Display: fmt.Sprintf("%d/%d", identified, len(targets)),
			Method:  "china-backbone-signature-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	if identified == 0 {
		result.Status = model.StatusWarning
		result.Summary = "未识别到任何已知中国骨干线路"
	} else {
		result.Summary = strings.Join(summaries, " · ")
	}
	result.Sources = []model.Source{
		{Name: "backtrace", URL: "https://github.com/zhanghanyun/backtrace", Purpose: "三网回程识别思路与参考目标选择"},
	}
	result.Notes = append(result.Notes,
		"这是从 VPS 主动发出的路径探测，不是反向抓包；路由不对称或运营商调度会让结论失真。",
		"线路判定只依据路径上出现的骨干网段前缀，报告保留命中跳号、IP 与完整原始输出供复核。",
		"CN2 的 GIA 与 GT 依据 59.43 段出现位置推断，带“推测”标注；精确区分需要更细的入口段表。",
		"未命中任何已知特征时返回“未识别”，不会猜测线路类型。",
	)
	result.Finish(start)
	return result
}

// runBacktraceTarget 追踪单个参考目标并匹配线路特征。
func runBacktraceTarget(ctx context.Context, engine routeEngine, target config.Endpoint) backtraceRow {
	row := backtraceRow{Target: target}
	traceCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	output, err := runRouteCommand(traceCtx, engine, target.Address, backtraceMaxHops)
	row.Raw = sanitizeCommandOutput(output)
	row.Hops = extractTraceHops(engine.Name, row.Raw)
	// traceroute 到国内 IP 时末段被丢弃是常态，只要拿到跳就继续分析；
	// 一跳都没有才算失败。
	if len(row.Hops) == 0 {
		if err != nil {
			row.Err = err
		} else {
			row.Err = fmt.Errorf("未解析到任何跳")
		}
		return row
	}
	row.Hits = matchRouteSignatures(row.Hops)
	return row
}

// extractTraceHops 按顺序取出每一跳的 IP，无响应的跳用空串占位。
func extractTraceHops(engineName, output string) []string {
	if engineName == "nexttrace" {
		if hops, ok := extractNextTraceHops(output); ok {
			return hops
		}
	}
	var hops []string
	for _, line := range strings.Split(output, "\n") {
		match := hopLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		address := ""
		if found := ipv4Pattern.FindString(match[2]); found != "" {
			if parsed := net.ParseIP(found); parsed != nil {
				address = found
			}
		}
		hops = append(hops, address)
	}
	return hops
}

// extractNextTraceHops 解析 NextTrace 的 JSON 输出。
func extractNextTraceHops(output string) ([]string, bool) {
	var payload struct {
		Hops [][]struct {
			Address string `json:"Address"`
		} `json:"Hops"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, false
	}
	hops := make([]string, 0, len(payload.Hops))
	for _, probes := range payload.Hops {
		address := ""
		for _, probe := range probes {
			if probe.Address != "" {
				address = probe.Address
				break
			}
		}
		hops = append(hops, address)
	}
	return hops, len(hops) > 0
}

// matchRouteSignatures 按跳序匹配所有已知骨干特征。
func matchRouteSignatures(hops []string) []backtraceHit {
	var hits []backtraceHit
	for index, address := range hops {
		if address == "" {
			continue
		}
		for _, signature := range chinaRouteSignatures {
			if strings.HasPrefix(address, signature.Prefix) {
				hits = append(hits, backtraceHit{Signature: signature, Hop: index + 1, IP: address})
				break
			}
		}
	}
	return hits
}

// bestBacktraceHit 在多个命中里挑出代表本次线路的那一条。
//
// 同一路径上同时出现 163 与 CN2 是常见情况（先经骨干再进 CN2），此时应以更优质
// 的线路作为结论，质量相同则取更靠前的跳。
func bestBacktraceHit(hits []backtraceHit) (backtraceHit, bool) {
	if len(hits) == 0 {
		return backtraceHit{}, false
	}
	sorted := append([]backtraceHit(nil), hits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Signature.Quality != sorted[j].Signature.Quality {
			return sorted[i].Signature.Quality > sorted[j].Signature.Quality
		}
		return sorted[i].Hop < sorted[j].Hop
	})
	return sorted[0], true
}

// describeBacktraceLine 给出线路名称，并在可判断时补充 CN2 的 GIA/GT 推测。
//
// 业界常用的启发式：路径先进 59.43 说明从海外直接接入 CN2，多为 GIA；先经过
// 202.97 再进 59.43 则多为 GT。这个规律有例外，因此结论一律带“推测”。
func describeBacktraceLine(best backtraceHit, hits []backtraceHit) string {
	label := best.Signature.Label
	if best.Signature.Code != "CN2" {
		return label
	}
	firstCN2, first163 := -1, -1
	for _, hit := range hits {
		if hit.Signature.Code == "CN2" && (firstCN2 < 0 || hit.Hop < firstCN2) {
			firstCN2 = hit.Hop
		}
		if hit.Signature.Code == "163" && (first163 < 0 || hit.Hop < first163) {
			first163 = hit.Hop
		}
	}
	switch {
	case first163 < 0 || firstCN2 < first163:
		return label + " · GIA（推测）"
	default:
		return label + " · GT（推测）"
	}
}

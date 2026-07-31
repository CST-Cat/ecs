package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type latencyProbe struct{}

func (latencyProbe) ID() string         { return "latency" }
func (latencyProbe) Title() string      { return "网络延迟" }
func (latencyProbe) NeedsNetwork() bool { return true }

type latencyResult struct {
	Endpoint config.Endpoint
	// DialAddress 是解析后固定使用的 IP:port。
	DialAddress string
	// ResolveTime 是一次性 DNS 解析耗时，单独记录而不混进握手延迟。
	ResolveTime time.Duration
	ResolveErr  error
	Values      []time.Duration
	Failures    int
}

// resolveEndpoint 把 host:port 解析成可直接拨号的 ip:port。
//
// Go 的 net.Dialer 不缓存 DNS，对域名反复拨号会让每次采样都带上一次完整解析，
// 几十毫秒的解析耗时直接污染 P50/P95。这里只解析一次，之后固定对 IP 拨号，
// 让延迟真正只反映 TCP 握手。
func resolveEndpoint(ctx context.Context, address string) (string, time.Duration, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	if net.ParseIP(host) != nil {
		return address, 0, nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	addresses, err := net.DefaultResolver.LookupNetIP(resolveCtx, "ip", host)
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, err
	}
	if len(addresses) == 0 {
		return "", elapsed, fmt.Errorf("解析 %s 未返回地址", host)
	}
	return net.JoinHostPort(addresses[0].Unmap().String(), port), elapsed, nil
}

func (latencyProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("latency", "网络延迟")
	result.Description = "面向全球与中国大陆服务的 TCP 建连延迟和可达率"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "native TCP connect",
		Profile:         "TCP handshake to pre-resolved IP, port 443",
		ComparisonScope: "相同目标、样本数、IP 协议和网络路径；不是 ICMP 标准 ping",
	}

	attempts := env.Config.LatencyAttempts
	results := make(chan latencyResult, len(env.Config.LatencyTargets))
	var wg sync.WaitGroup
	for _, endpoint := range env.Config.LatencyTargets {
		wg.Add(1)
		go func(endpoint config.Endpoint) {
			defer wg.Done()
			item := latencyResult{Endpoint: endpoint}
			dialAddress, resolveTime, err := resolveEndpoint(ctx, endpoint.Address)
			item.ResolveTime = resolveTime
			if err != nil {
				item.ResolveErr = err
				item.Failures = attempts
				results <- item
				return
			}
			item.DialAddress = dialAddress
			for i := 0; i < attempts; i++ {
				dialer := net.Dialer{Timeout: 3 * time.Second}
				begin := time.Now()
				connection, dialErr := dialer.DialContext(ctx, "tcp", dialAddress)
				elapsed := time.Since(begin)
				if dialErr != nil {
					item.Failures++
					continue
				}
				_ = connection.Close()
				item.Values = append(item.Values, elapsed)
			}
			results <- item
		}(endpoint)
	}
	wg.Wait()
	close(results)
	collected := make([]latencyResult, 0, len(env.Config.LatencyTargets))
	for item := range results {
		collected = append(collected, item)
	}
	order := make(map[string]int, len(env.Config.LatencyTargets))
	for index, endpoint := range env.Config.LatencyTargets {
		order[endpoint.Address] = index
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return order[collected[i].Endpoint.Address] < order[collected[j].Endpoint.Address]
	})

	table := model.Table{
		Title:   "TCP 建连",
		Columns: []string{"目标", "区域", "成功", "P50", "P95", "标准差", "DNS 解析"},
	}
	var best time.Duration
	var bestName string
	allFailed := true
	for _, item := range collected {
		median := medianDuration(item.Values)
		p95 := percentileDuration(item.Values, 0.95)
		floatValues := make([]float64, len(item.Values))
		for i, value := range item.Values {
			floatValues[i] = float64(value) / float64(time.Millisecond)
		}
		if len(item.Values) > 0 {
			allFailed = false
			if best == 0 || median < best {
				best = median
				bestName = item.Endpoint.Name
			}
		}
		resolveText := formatMilliseconds(item.ResolveTime)
		if item.ResolveErr != nil {
			resolveText = "解析失败"
		} else if item.ResolveTime == 0 {
			resolveText = "无需解析"
		}
		table.Rows = append(table.Rows, []string{
			item.Endpoint.Name,
			item.Endpoint.Kind,
			fmt.Sprintf("%d/%d", len(item.Values), attempts),
			formatMilliseconds(median),
			formatMilliseconds(p95),
			fmt.Sprintf("%.2f ms", stddevFloat(floatValues)),
			resolveText,
		})
		if item.ResolveErr != nil {
			result.Notes = append(result.Notes, fmt.Sprintf("%s 解析失败：%s", item.Endpoint.Name, compactError(item.ResolveErr)))
		}
	}
	result.Tables = []model.Table{table}
	if allFailed {
		result.Status = model.StatusWarning
		result.Summary = "全部 TCP 延迟目标不可达"
	} else {
		result.Measurements = []model.Measurement{
			{
				Key: "best_tcp_median_ms", Label: "最佳 TCP P50",
				Value: float64(best) / float64(time.Millisecond), Unit: "ms", Display: formatMilliseconds(best),
				Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
			},
		}
		result.Summary = fmt.Sprintf("%s 最快 · P50 %s", bestName, formatMilliseconds(best))
	}
	result.Notes = append(result.Notes,
		"每个目标只解析一次 DNS，之后固定对该 IP 建连；表中的延迟只包含 TCP 三次握手，解析耗时单列。",
		"这不是 ICMP ping；目标服务的 Anycast/CDN 调度会影响结果。",
		"区域标签说明服务归属，不保证本次连接实际落在该地区。",
	)
	result.Finish(start)
	return result
}

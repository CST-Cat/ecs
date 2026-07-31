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
	Values   []time.Duration
	Failures int
}

func (latencyProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("latency", "网络延迟")
	result.Description = "面向全球与中国大陆服务的 TCP 建连延迟和可达率"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "native TCP connect",
		Profile:         "DNS + TCP handshake port 443",
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
			for i := 0; i < attempts; i++ {
				dialer := net.Dialer{Timeout: 3 * time.Second}
				begin := time.Now()
				connection, err := dialer.DialContext(ctx, "tcp", endpoint.Address)
				elapsed := time.Since(begin)
				if err != nil {
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
		Columns: []string{"目标", "区域", "成功", "P50", "P95", "标准差"},
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
		table.Rows = append(table.Rows, []string{
			item.Endpoint.Name,
			item.Endpoint.Kind,
			fmt.Sprintf("%d/%d", len(item.Values), attempts),
			formatMilliseconds(median),
			formatMilliseconds(p95),
			fmt.Sprintf("%.2f ms", stddevFloat(floatValues)),
		})
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
				Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(false),
			},
		}
		result.Summary = fmt.Sprintf("%s 最快 · P50 %s", bestName, formatMilliseconds(best))
	}
	result.Notes = append(result.Notes,
		"计时包含 DNS 解析和 TCP 三次握手，不是 ICMP ping；目标服务的 Anycast/CDN 调度会影响结果。",
		"区域标签说明服务归属，不保证本次连接实际落在该地区。",
	)
	result.Finish(start)
	return result
}

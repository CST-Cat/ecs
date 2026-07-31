package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type dnsProbe struct{}

func (dnsProbe) ID() string         { return "dns" }
func (dnsProbe) Title() string      { return "DNS 质量" }
func (dnsProbe) NeedsNetwork() bool { return true }

// dnsQueryName 是固定的探测域名，各解析器都能递归到且 TTL 稳定。
const dnsQueryName = "www.cloudflare.com"

type dnsResult struct {
	Endpoint config.Endpoint
	Values   []time.Duration
	Failures int
	LastErr  error
	// WarmupErr 记录预热查询的失败原因，仅用于诊断，不计入成功率。
	WarmupErr error
}

func (dnsProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("dns", "DNS 质量")
	result.Description = "向多个公共递归解析器发送原生 UDP DNS 查询"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "native DNS/UDP",
		Profile:         "A query " + dnsQueryName + " (warmed up)",
		ComparisonScope: "相同解析器、域名、缓存状态、样本数与网络路径；不是标准基准分数",
	}

	attempts := env.Config.DNSAttempts
	results := make(chan dnsResult, len(env.Config.DNSResolvers))
	var wg sync.WaitGroup
	for _, endpoint := range env.Config.DNSResolvers {
		wg.Add(1)
		go func(endpoint config.Endpoint) {
			defer wg.Done()
			item := dnsResult{Endpoint: endpoint}
			// 预热一次且不计入统计：首次查询大概率是递归 miss，后续是缓存命中，
			// 两者能差一个数量级，混在一起会让 P50/P95 和抖动全部失真。
			if _, err := dnsQuery(ctx, endpoint.Address, dnsQueryName, 2*time.Second); err != nil {
				item.WarmupErr = err
			}
			for i := 0; i < attempts; i++ {
				elapsed, err := dnsQuery(ctx, endpoint.Address, dnsQueryName, 2*time.Second)
				if err != nil {
					item.Failures++
					item.LastErr = err
				} else {
					item.Values = append(item.Values, elapsed)
				}
			}
			results <- item
		}(endpoint)
	}
	wg.Wait()
	close(results)
	collected := make([]dnsResult, 0, len(env.Config.DNSResolvers))
	for item := range results {
		collected = append(collected, item)
	}
	order := make(map[string]int, len(env.Config.DNSResolvers))
	for index, endpoint := range env.Config.DNSResolvers {
		order[endpoint.Address] = index
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return order[collected[i].Endpoint.Address] < order[collected[j].Endpoint.Address]
	})

	table := model.Table{
		Title:   "递归解析器",
		Columns: []string{"解析器", "地址", "成功", "P50", "P95", "抖动", "状态"},
	}
	var bestName string
	var best time.Duration
	allFailed := true
	for _, item := range collected {
		median := medianDuration(item.Values)
		p95 := percentileDuration(item.Values, 0.95)
		floatValues := make([]float64, len(item.Values))
		for i, value := range item.Values {
			floatValues[i] = float64(value) / float64(time.Millisecond)
		}
		jitter := stddevFloat(floatValues)
		status := "正常"
		if len(item.Values) == 0 {
			status = "失败"
		} else {
			allFailed = false
			if best == 0 || median < best {
				best = median
				bestName = item.Endpoint.Name
			}
			if item.Failures > 0 {
				status = "部分失败"
			}
		}
		table.Rows = append(table.Rows, []string{
			item.Endpoint.Name,
			item.Endpoint.Address,
			fmt.Sprintf("%d/%d", len(item.Values), attempts),
			formatMilliseconds(median),
			formatMilliseconds(p95),
			fmt.Sprintf("%.2f ms", jitter),
			status,
		})
	}
	result.Tables = []model.Table{table}
	if allFailed {
		result.Status = model.StatusWarning
		result.Summary = "所有公共 DNS 查询均失败"
		result.Notes = append(result.Notes, "UDP/53 可能被防火墙拦截；系统 DNS 仍可能通过其他协议工作。")
	} else {
		result.Measurements = []model.Measurement{
			{
				Key: "best_dns_median_ms", Label: "最佳 DNS P50",
				Value: float64(best) / float64(time.Millisecond), Unit: "ms", Display: formatMilliseconds(best),
				Method: "udp-a-query-v1", HigherIsBetter: model.BoolPtr(false),
			},
		}
		result.Summary = fmt.Sprintf("%s 最快 · P50 %s", bestName, formatMilliseconds(best))
	}
	result.Notes = append(result.Notes, "查询域名固定为 "+dnsQueryName+"；每个解析器先发一次不计入统计的预热查询，随后的样本反映缓存命中后的稳态 UDP/53 往返，不等同于系统解析器体验。")
	result.Finish(start)
	return result
}

func dnsQuery(ctx context.Context, address, name string, timeout time.Duration) (time.Duration, error) {
	packet, id, err := buildDNSQuery(name)
	if err != nil {
		return 0, err
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	deadline := time.Now().Add(timeout)
	_ = connection.SetDeadline(deadline)
	start := time.Now()
	if _, err := connection.Write(packet); err != nil {
		return 0, err
	}
	buffer := make([]byte, 1500)
	n, err := connection.Read(buffer)
	elapsed := time.Since(start)
	if err != nil {
		return elapsed, err
	}
	if n < 12 {
		return elapsed, fmt.Errorf("DNS 响应过短")
	}
	if binary.BigEndian.Uint16(buffer[:2]) != id {
		return elapsed, fmt.Errorf("DNS 事务 ID 不匹配")
	}
	flags := binary.BigEndian.Uint16(buffer[2:4])
	if flags&0x8000 == 0 {
		return elapsed, fmt.Errorf("不是 DNS 响应")
	}
	if rcode := flags & 0x000f; rcode != 0 {
		return elapsed, fmt.Errorf("DNS RCODE %d", rcode)
	}
	if binary.BigEndian.Uint16(buffer[6:8]) == 0 {
		return elapsed, fmt.Errorf("DNS 无应答记录")
	}
	return elapsed, nil
}

func buildDNSQuery(name string) ([]byte, uint16, error) {
	random := [2]byte{}
	if _, err := rand.Read(random[:]); err != nil {
		return nil, 0, err
	}
	id := binary.BigEndian.Uint16(random[:])
	packet := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(packet[0:2], id)
	binary.BigEndian.PutUint16(packet[2:4], 0x0100)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	for _, label := range splitDNSName(name) {
		if len(label) == 0 || len(label) > 63 {
			return nil, 0, fmt.Errorf("无效 DNS 名称")
		}
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
	}
	packet = append(packet, 0)
	packet = append(packet, 0, 1) // QTYPE A
	packet = append(packet, 0, 1) // QCLASS IN
	return packet, id, nil
}

func splitDNSName(name string) []string {
	var labels []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			labels = append(labels, name[start:i])
			start = i + 1
		}
	}
	return labels
}

func formatMilliseconds(duration time.Duration) string {
	if duration <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f ms", float64(duration)/float64(time.Millisecond))
}

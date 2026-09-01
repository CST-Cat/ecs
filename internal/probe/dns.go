package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type dnsProbe struct{}

func (dnsProbe) ID() string { return "dns" }

// dnsQueryName 是固定的探测域名，各解析器都能递归到且 TTL 稳定。
const dnsQueryName = "www.cloudflare.com"

const (
	dnsStatusOK      = "ok"
	dnsStatusFailed  = "failed"
	dnsStatusPartial = "partial"
)

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
	result := newDNSResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameter(result.Methodology.Parameters, "query_name", dnsQueryName)
	addComparisonParameter(result.Methodology.Parameters, "attempts", strconv.Itoa(env.Config.DNSAttempts))
	addComparisonParameterJSON(result.Methodology.Parameters, "resolvers", env.Config.DNSResolvers)
	result.Notes = []string{"probe.dns.note.warmup", "probe.dns.note.udp_scope"}

	attempts := env.Config.DNSAttempts
	resolvers := endpointsForIPVersion(env.Config.DNSResolvers, env.Config.IPVersion)
	if len(resolvers) == 0 {
		result.Skip(model.NewMessage("probe.dns.summary.skipped"))
		result.Evidence = model.NewEvidence(0, 0, "query")
		result.Finish(start)
		return result
	}
	results := make(chan dnsResult, len(resolvers))
	var wg sync.WaitGroup
	for _, endpoint := range resolvers {
		wg.Add(1)
		go func(endpoint config.Endpoint) {
			defer wg.Done()
			item := dnsResult{Endpoint: endpoint}
			family := endpointFamily(endpoint, env.Config.IPVersion)
			// 预热一次且不计入统计：首次查询大概率是递归 miss，后续是缓存命中，
			// 两者能差一个数量级，混在一起会让 P50/P95 和抖动全部失真。
			if _, err := dnsQueryForMode(ctx, endpoint.Address, dnsQueryName, 2*time.Second, family); err != nil {
				item.WarmupErr = err
			}
			for i := 0; i < attempts; i++ {
				elapsed, err := dnsQueryForMode(ctx, endpoint.Address, dnsQueryName, 2*time.Second, family)
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
	collected := make([]dnsResult, 0, len(resolvers))
	for item := range results {
		collected = append(collected, item)
	}
	order := make(map[string]int, len(resolvers))
	for index, endpoint := range resolvers {
		order[endpoint.Address] = index
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return order[collected[i].Endpoint.Address] < order[collected[j].Endpoint.Address]
	})

	table := model.Table{
		Key:   "network.dns.resolvers",
		Title: "probe.dns.table.resolvers",
		Columns: []model.TableColumn{
			{Key: "resolver", Label: "probe.dns.column.resolver"},
			{Key: "address", Label: "probe.dns.column.address"},
			{Key: "success_count", Label: "probe.dns.column.success"},
			{Key: "p50_ms", Label: "probe.dns.column.p50", Numeric: true},
			{Key: "p95_ms", Label: "probe.dns.column.p95", Numeric: true},
			{Key: "jitter_ms", Label: "probe.dns.column.jitter", Numeric: true},
			{Key: "status", Label: "probe.dns.column.status"},
		},
		RowIdentity: "address",
	}
	var best time.Duration
	allFailed := true
	validQueries := 0
	for itemIndex, item := range collected {
		median := medianDuration(item.Values)
		p95 := percentileDuration(item.Values, 0.95)
		floatValues := make([]float64, len(item.Values))
		for i, value := range item.Values {
			floatValues[i] = float64(value) / float64(time.Millisecond)
		}
		jitter := stddevFloat(floatValues)
		validQueries += len(item.Values)
		status := dnsStatusOK
		if len(item.Values) == 0 {
			status = dnsStatusFailed
		} else {
			allFailed = false
			if best == 0 || median < best {
				best = median
			}
			if item.Failures > 0 {
				status = dnsStatusPartial
			}
		}
		if item.Failures > 0 && item.LastErr != nil {
			addFailure(&result, "query", item.Endpoint.Address, item.LastErr, item.Failures)
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(item.Endpoint.Name), model.RawValue(item.Endpoint.Address),
			model.RawValue(fmt.Sprintf("%d/%d", len(item.Values), attempts)),
			model.RawValue(formatMilliseconds(median)), model.RawValue(formatMilliseconds(p95)),
			model.RawValue(fmt.Sprintf("%.2f ms", jitter)), dnsStatusValue(status),
		})
		prefix := fmt.Sprintf("dns_resolver_%02d", itemIndex+1)
		successPercent := 0.0
		if attempts > 0 {
			successPercent = float64(len(item.Values)) / float64(attempts) * 100
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "_success_percent", Label: "probe.dns.metric.resolver",
			Value: successPercent, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.1f %%", successPercent)),
			Method: "udp-a-query-warm-v1", HigherIsBetter: model.BoolPtr(true),
		})
		if len(item.Values) > 0 {
			result.Measurements = append(result.Measurements,
				model.Measurement{
					Key: prefix + "_p50_ms", Label: "probe.dns.metric.resolver",
					Value: float64(median) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(median)),
					Method: "udp-a-query-warm-v1", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_p95_ms", Label: "probe.dns.metric.resolver",
					Value: float64(p95) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(p95)),
					Method: "udp-a-query-warm-v1", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_jitter_ms", Label: "probe.dns.metric.resolver",
					Value: jitter, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", jitter)),
					Method: "udp-a-query-warm-v1", HigherIsBetter: model.BoolPtr(false),
				},
			)
		}
	}
	result.Tables = []model.Table{table}
	result.Evidence = model.NewEvidence(validQueries, len(collected)*attempts, "query")
	if allFailed {
		result.Status = model.StatusWarning
	} else {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "best_dns_median_ms", Label: "probe.dns.metric.best_median",
			Value: float64(best) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(best)),
			Method: "udp-a-query-warm-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if result.Evidence.Valid == 0 && result.Evidence.Expected > 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.dns.summary.all_failed")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.dns.summary.values", dnsSummaryText(result))}
	}
	result.Finish(start)
	return result
}

func newDNSResult() model.Result {
	result := model.NewResult("dns", "module.dns.title")
	result.Description = "probe.dns.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "native DNS/UDP",
		Profile:         "probe.dns.profile",
		ComparisonScope: "probe.dns.comparison_scope",
	}
	return result
}

func dnsStatusValue(status string) model.Value {
	switch status {
	case dnsStatusOK:
		return model.KeyValue("probe.dns.status.ok")
	case dnsStatusFailed:
		return model.KeyValue("probe.dns.status.failed")
	case dnsStatusPartial:
		return model.KeyValue("probe.dns.status.partial")
	default:
		return model.RawValue(status)
	}
}

func dnsSummaryText(result model.Result) string {
	for _, measurement := range result.Measurements {
		if measurement.Key == "best_dns_median_ms" {
			return "best_p50=" + measurement.Display.Text()
		}
	}
	return ""
}

func dnsQueryForMode(ctx context.Context, address, name string, timeout time.Duration, mode string) (time.Duration, error) {
	return dnsQueryNetwork(ctx, address, name, timeout, udpNetworkForMode(mode))
}

func dnsQueryNetwork(ctx context.Context, address, name string, timeout time.Duration, network string) (time.Duration, error) {
	packet, id, err := buildDNSQuery(name)
	if err != nil {
		return 0, err
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, network, address)
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
	if err := validateDNSResponse(buffer[:n], id); err != nil {
		return elapsed, err
	}
	return elapsed, nil
}

func validateDNSResponse(packet []byte, id uint16) error {
	if len(packet) < 12 {
		return fmt.Errorf("DNS 响应过短")
	}
	if binary.BigEndian.Uint16(packet[:2]) != id {
		return fmt.Errorf("DNS 事务 ID 不匹配")
	}
	flags := binary.BigEndian.Uint16(packet[2:4])
	if flags&0x8000 == 0 {
		return fmt.Errorf("不是 DNS 响应")
	}
	if rcode := flags & 0x000f; rcode != 0 {
		return fmt.Errorf("DNS RCODE %d", rcode)
	}
	if binary.BigEndian.Uint16(packet[6:8]) == 0 {
		return fmt.Errorf("DNS 无应答记录")
	}
	return nil
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

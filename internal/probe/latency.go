package probe

import (
	"context"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type latencyProbe struct{}

func (latencyProbe) ID() string { return "latency" }

type latencyResult struct {
	Endpoint config.Endpoint
	Family   string
	// DialAddress 是解析后固定使用的 IP:port。
	DialAddress string
	// ResolveTime 是一次性 DNS 解析耗时，单独记录而不混进握手延迟。
	ResolveTime time.Duration
	ResolveErr  error
	Values      []time.Duration
	Failures    int
	LastErr     error
	// ICMP 是同一目标的 ICMP 往返统计，系统没有 ping 时为不可用。
	ICMP icmpStats
}

// resolveEndpoint 把 host:port 解析成可直接拨号的 ip:port。
//
// Go 的 net.Dialer 不缓存 DNS，对域名反复拨号会让每次采样都带上一次完整解析，
// 几十毫秒的解析耗时直接污染 P50/P95。这里只解析一次，之后固定对 IP 拨号，
// 让延迟真正只反映 TCP 握手。
func resolveEndpoint(ctx context.Context, address, family string) (string, time.Duration, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if family == config.IPVersion4 && ip.To4() == nil {
			return "", 0, fmt.Errorf("目标不是 IPv4")
		}
		if family == config.IPVersion6 && ip.To4() != nil {
			return "", 0, fmt.Errorf("目标不是 IPv6")
		}
		return address, 0, nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	network := "ip"
	if family == config.IPVersion4 || family == config.IPVersion6 {
		network += family
	}
	addresses, err := net.DefaultResolver.LookupNetIP(resolveCtx, network, host)
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, err
	}
	if len(addresses) == 0 {
		return "", elapsed, fmt.Errorf("解析 %s 未返回地址", host)
	}
	for _, address := range addresses {
		ip := address.Unmap()
		if family == config.IPVersion4 && !ip.Is4() {
			continue
		}
		if family == config.IPVersion6 && ip.Is4() {
			continue
		}
		return net.JoinHostPort(ip.String(), port), elapsed, nil
	}
	return "", elapsed, fmt.Errorf("解析 %s 没有 IPv%s 地址", host, family)
}

func latencyFamiliesWithCapability(address, mode string, hasIPv4, hasIPv6 bool) []string {
	var families []string
	for _, family := range config.IPVersions(mode) {
		if family == config.IPVersion4 && !hasIPv4 || family == config.IPVersion6 && !hasIPv6 {
			continue
		}
		if ip, _, err := net.SplitHostPort(address); err == nil {
			if parsed := net.ParseIP(ip); parsed != nil {
				if family == config.IPVersion4 && parsed.To4() != nil {
					families = append(families, family)
				}
				if family == config.IPVersion6 && parsed.To4() == nil {
					families = append(families, family)
				}
				continue
			}
		}
		families = append(families, family)
	}
	return families
}

// tcpInterceptRatio 是判定 TCP 建连被本地截获的倍数阈值。
//
// TCP 三次握手与 ICMP echo 都只花一个网络往返，对同一个 IP 应当在同一量级。
// 当 TCP 中位数比 ICMP 平均值小到数倍以上时，握手几乎不可能真的到达目标：
// 常见成因是本机或网关上的透明代理、TPROXY 重定向、加速器和企业中间盒——
// 它们代答 TCP 而不处理 ICMP。此时 TCP 数字反映的是到代理的距离。
const tcpInterceptRatio = 5

// tcpLikelyIntercepted 判断 TCP 建连延迟是否与同目标的 ICMP 往返严重背离。
//
// 只在 ICMP 确实拿到样本、且目标不在本地网络时判断；缺证据一律返回 false，
// 绝不把普通的网络波动说成代理截获。
func tcpLikelyIntercepted(tcpMedian time.Duration, icmp icmpStats) bool {
	if !icmp.Available || icmp.LossPercent >= 100 || icmp.AvgMS <= 1 {
		return false
	}
	tcpMS := float64(tcpMedian) / float64(time.Millisecond)
	if tcpMS <= 0 {
		return false
	}
	return icmp.AvgMS/tcpMS >= tcpInterceptRatio
}

func (latencyProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := newLatencyResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameter(result.Methodology.Parameters, "attempts", strconv.Itoa(env.Config.LatencyAttempts))
	addComparisonParameterHash(result.Methodology.Parameters, "targets_sha256", env.Config.LatencyTargets)

	attempts := env.Config.LatencyAttempts
	capacity := 0
	for _, endpoint := range env.Config.LatencyTargets {
		capacity += len(latencyFamiliesForEndpoint(endpoint, env.Config.IPVersion, env.Network.IPv4Usable, env.Network.IPv6Usable))
	}
	if capacity == 0 {
		result.Skip(model.NewMessage("probe.latency.summary.skipped"))
		result.Evidence = model.NewEvidence(0, 0, "sample")
		result.Notes = latencyNotes(result)
		result.Finish(start)
		return result
	}
	icmpEnabled := icmpAvailable()
	results := make(chan latencyResult, capacity)
	var wg sync.WaitGroup
	for _, endpoint := range env.Config.LatencyTargets {
		for _, family := range latencyFamiliesForEndpoint(endpoint, env.Config.IPVersion, env.Network.IPv4Usable, env.Network.IPv6Usable) {
			wg.Add(1)
			go func(endpoint config.Endpoint, family string) {
				defer wg.Done()
				item := latencyResult{Endpoint: endpoint, Family: family}
				dialAddress, resolveTime, err := resolveEndpoint(ctx, endpoint.Address, family)
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
					connection, dialErr := dialer.DialContext(ctx, "tcp"+family, dialAddress)
					elapsed := time.Since(begin)
					if dialErr != nil {
						item.Failures++
						item.LastErr = dialErr
						continue
					}
					_ = connection.Close()
					item.Values = append(item.Values, elapsed)
				}
				// ICMP 与 TCP 建连测的不是一回事：ICMP 反映纯网络往返，TCP 还包含
				// 目标服务的接受队列表现，两者并列才能看出是链路问题还是服务端问题。
				if icmpEnabled {
					host, _, splitErr := net.SplitHostPort(dialAddress)
					if splitErr == nil {
						item.ICMP = runICMPPingFamily(ctx, host, attempts, 2*time.Second, family)
					}
				}
				results <- item
			}(endpoint, family)
		}
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
		left, right := order[collected[i].Endpoint.Address], order[collected[j].Endpoint.Address]
		if left != right {
			return left < right
		}
		return collected[i].Family < collected[j].Family
	})

	table := model.Table{
		Key:   "network.latency.tcp_icmp",
		Title: "probe.latency.table.tcp_icmp",
		Columns: []model.TableColumn{
			{Key: "target", Label: "probe.latency.column.target"},
			{Key: "protocol", Label: "probe.latency.column.protocol"},
			{Key: "region", Label: "probe.latency.column.region"},
			{Key: "success", Label: "probe.latency.column.success"},
			{Key: "tcp_p50_ms", Label: "probe.latency.column.tcp_p50", Numeric: true},
			{Key: "tcp_p95_ms", Label: "probe.latency.column.tcp_p95", Numeric: true},
			{Key: "tcp_stddev_ms", Label: "probe.latency.column.tcp_stddev", Numeric: true},
			{Key: "icmp_min_ms", Label: "probe.latency.column.icmp_min", Numeric: true},
			{Key: "icmp_avg_ms", Label: "probe.latency.column.icmp_avg", Numeric: true},
			{Key: "icmp_max_ms", Label: "probe.latency.column.icmp_max", Numeric: true},
			{Key: "icmp_mdev_ms", Label: "probe.latency.column.icmp_mdev", Numeric: true},
			{Key: "icmp_loss_percent", Label: "probe.latency.column.icmp_loss", Numeric: true},
			{Key: "dns_resolution", Label: "probe.latency.column.dns", Numeric: true},
		},
	}
	var best time.Duration
	allFailed := true
	var intercepted []string
	validSamples := 0
	for itemIndex, item := range collected {
		median := medianDuration(item.Values)
		p95 := percentileDuration(item.Values, 0.95)
		floatValues := make([]float64, len(item.Values))
		for i, value := range item.Values {
			floatValues[i] = float64(value) / float64(time.Millisecond)
		}
		if len(item.Values) > 0 {
			validSamples += len(item.Values)
			allFailed = false
			if best == 0 || median < best {
				best = median
			}
		}
		icmpMin, icmpAvg, icmpMax, icmpMDev, icmpLoss := "n/a", "n/a", "n/a", "n/a", "n/a"
		if item.ICMP.RTTKnown {
			icmpMin = formatICMPMilliseconds(item.ICMP.MinMS)
			icmpAvg = formatICMPMilliseconds(item.ICMP.AvgMS)
			icmpMax = formatICMPMilliseconds(item.ICMP.MaxMS)
			if item.ICMP.StdDevKnown {
				icmpMDev = formatICMPMilliseconds(item.ICMP.StdDevMS)
			}
		}
		if item.ICMP.LossKnown {
			icmpLoss = fmt.Sprintf("%.0f %%", item.ICMP.LossPercent)
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(item.Endpoint.Name), model.RawValue("IPv" + item.Family), model.RawValue(item.Endpoint.Kind),
			model.RawValue(fmt.Sprintf("%d/%d", len(item.Values), attempts)), model.RawValue(formatMilliseconds(median)),
			model.RawValue(formatMilliseconds(p95)), model.RawValue(fmt.Sprintf("%.2f ms", stddevFloat(floatValues))),
			model.RawValue(icmpMin), model.RawValue(icmpAvg), model.RawValue(icmpMax), model.RawValue(icmpMDev),
			model.RawValue(icmpLoss), latencyResolutionValue(item),
		})
		prefix := fmt.Sprintf("tcp_target_%02d_ipv%s", itemIndex+1, item.Family)
		successPercent := 0.0
		if attempts > 0 {
			successPercent = float64(len(item.Values)) / float64(attempts) * 100
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "_success_percent", Label: "probe.latency.metric.tcp",
			Value: successPercent, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.1f %%", successPercent)),
			Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(true),
		})
		if len(item.Values) > 0 {
			jitter := stddevFloat(floatValues)
			result.Measurements = append(result.Measurements,
				model.Measurement{
					Key: prefix + "_p50_ms", Label: "probe.latency.metric.tcp",
					Value: float64(median) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(median)),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_p95_ms", Label: "probe.latency.metric.tcp",
					Value: float64(p95) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(p95)),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_jitter_ms", Label: "probe.latency.metric.tcp",
					Value: jitter, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", jitter)),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
			)
		}
		if item.ResolveErr != nil {
			addFailure(&result, "resolve", item.Endpoint.Address, item.ResolveErr, attempts)
		} else if item.Failures > 0 && item.LastErr != nil {
			addFailure(&result, "connect", item.DialAddress, item.LastErr, item.Failures)
		}
		appendICMPMeasurementsForFamily(&result, item.Endpoint.Name, item.Family, item.ICMP)
		if tcpLikelyIntercepted(median, item.ICMP) {
			intercepted = append(intercepted, item.Endpoint.Name)
		}
	}
	result.Tables = []model.Table{table}
	result.Evidence = model.NewEvidence(validSamples, len(collected)*attempts, "sample")
	if allFailed {
		result.Status = model.StatusWarning
	} else {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "best_tcp_median_ms", Label: "probe.latency.metric.best_median",
			Value: float64(best) / float64(time.Millisecond), Unit: "ms", Display: model.RawValue(formatMilliseconds(best)),
			Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if len(intercepted) > 0 {
		result.Status = model.StatusWarning
		// 目标名单单独成字段而不是嵌进说明句：嵌进去会让整句随目标变化，
		// 既无法翻译也无法在不同机器之间对照。
		result.Fields = append(result.Fields, model.Field{
			Key: "tcp_intercepted_targets", Label: "probe.latency.field.tcp_intercepted_targets",
			Value: model.RawValue(strings.Join(intercepted, "、")),
		})
	}
	result.Notes = latencyNotes(result)
	if result.Evidence.Valid == 0 && result.Evidence.Expected > 0 {
		result.Status = model.StatusWarning
		result.SummaryMessages = []model.Message{model.NewMessage("probe.latency.summary.all_failed")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.latency.summary.values", latencySummaryText(result))}
	}
	result.Finish(start)
	return result
}

func newLatencyResult() model.Result {
	result := model.NewResult("latency", "module.latency.title")
	result.Description = "probe.latency.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "native TCP connect",
		Profile:         "probe.latency.profile",
		ComparisonScope: "probe.latency.comparison_scope",
	}
	return result
}

func latencyResolutionValue(item latencyResult) model.Value {
	if item.ResolveErr != nil {
		return model.KeyValue("probe.latency.status.resolve_failed")
	}
	host := item.Endpoint.Address
	if parsedHost, _, err := net.SplitHostPort(item.Endpoint.Address); err == nil {
		host = parsedHost
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return model.KeyValue("probe.latency.status.no_resolution")
	}
	return model.RawValue(formatMilliseconds(item.ResolveTime))
}

func latencyNotes(result model.Result) []string {
	notes := []string{
		"probe.latency.note.resolution",
		"probe.latency.note.region",
	}
	hasICMP := false
	for _, measurement := range result.Measurements {
		if strings.HasPrefix(measurement.Key, "icmp_") {
			hasICMP = true
			break
		}
	}
	if hasICMP {
		notes = append(notes, "probe.latency.note.icmp")
	} else {
		notes = append(notes, "probe.latency.note.icmp_unavailable")
	}
	if _, ok := fieldByKey(result, "tcp_intercepted_targets"); ok {
		notes = append(notes, "probe.latency.note.intercepted")
	}
	return notes
}

func latencySummaryText(result model.Result) string {
	for _, measurement := range result.Measurements {
		if measurement.Key == "best_tcp_median_ms" {
			return "best_p50=" + measurement.Display.Text()
		}
	}
	return ""
}

func fieldByKey(result model.Result, key string) (model.Field, bool) {
	for _, field := range result.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return model.Field{}, false
}

func formatICMPMilliseconds(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f ms", value)
}

func appendICMPMeasurementsForFamily(result *model.Result, targetName, family string, stats icmpStats) {
	if result == nil || !stats.Available {
		return
	}
	slug := strings.ToLower(strings.ReplaceAll(targetName, " ", "_"))
	if family != "" {
		slug += "_ipv" + strings.ToLower(family)
	}
	appendMeasurement := func(suffix, unit, display string, value float64) {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key:            "icmp_" + suffix + "_" + slug,
			Label:          "probe.latency.metric.icmp",
			Value:          value,
			Unit:           unit,
			Display:        model.RawValue(display),
			Method:         "icmp-echo-v1",
			HigherIsBetter: model.BoolPtr(false),
		})
	}
	if stats.RTTKnown {
		appendMeasurement("min_ms", "ms", formatICMPMilliseconds(stats.MinMS), stats.MinMS)
		appendMeasurement("avg_ms", "ms", formatICMPMilliseconds(stats.AvgMS), stats.AvgMS)
		appendMeasurement("max_ms", "ms", formatICMPMilliseconds(stats.MaxMS), stats.MaxMS)
		if stats.StdDevKnown {
			appendMeasurement("mdev_ms", "ms", formatICMPMilliseconds(stats.StdDevMS), stats.StdDevMS)
		}
	}
	if stats.LossKnown {
		appendMeasurement("loss_percent", "%", fmt.Sprintf("%.2f %%", stats.LossPercent), stats.LossPercent)
	}
}

func latencyFamiliesForEndpoint(endpoint config.Endpoint, mode string, hasIPv4, hasIPv6 bool) []string {
	families := latencyFamiliesWithCapability(endpoint.Address, mode, hasIPv4, hasIPv6)
	if endpoint.Family != config.IPVersion4 && endpoint.Family != config.IPVersion6 {
		return families
	}
	filtered := families[:0]
	for _, family := range families {
		if family == endpoint.Family {
			filtered = append(filtered, family)
		}
	}
	return filtered
}

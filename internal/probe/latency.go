package probe

import (
	"context"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
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

// latencyFamilies keeps auto mode useful on hosts without IPv6 while still
// probing both families when the kernel has a real public IPv6 route.
func latencyFamilies(address, mode string) []string {
	hasIPv6 := false
	if mode != config.IPVersion4 {
		hasIPv6 = hostHasUsableIPv6()
	}
	return latencyFamiliesWithCapability(address, mode, hasIPv6)
}

func latencyFamiliesWithCapability(address, mode string, hasIPv6 bool) []string {
	var families []string
	for _, family := range config.IPVersions(mode) {
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
		if family == config.IPVersion6 && mode != config.IPVersion6 && !hasIPv6 {
			continue
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
	icmpEnabled := icmpAvailable()
	hasIPv6 := false
	if env.Config.IPVersion != config.IPVersion4 {
		hasIPv6 = hostHasUsableIPv6()
	}
	capacity := 0
	for _, endpoint := range env.Config.LatencyTargets {
		capacity += len(latencyFamiliesForEndpoint(endpoint, env.Config.IPVersion, hasIPv6))
	}
	results := make(chan latencyResult, capacity)
	var wg sync.WaitGroup
	for _, endpoint := range env.Config.LatencyTargets {
		for _, family := range latencyFamiliesForEndpoint(endpoint, env.Config.IPVersion, hasIPv6) {
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
		Title:                 "TCP 建连与 ICMP 往返",
		Columns:               []string{"目标", "协议", "区域", "成功", "TCP P50", "TCP P95", "标准差", "ICMP 最小", "ICMP 平均", "ICMP 最大", "ICMP mdev", "ICMP 丢包", "DNS 解析"},
		NumericColumns:        []int{4, 5, 6, 7, 8, 9, 10, 11, 12},
		NumericHigherIsBetter: []bool{false, false, false, false, false, false, false, false, false},
	}
	var best time.Duration
	var bestName string
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
				bestName = item.Endpoint.Name
			}
		}
		resolveText := formatMilliseconds(item.ResolveTime)
		if item.ResolveErr != nil {
			resolveText = "解析失败"
		} else if item.ResolveTime == 0 {
			resolveText = "无需解析"
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
		table.Rows = append(table.Rows, []string{
			item.Endpoint.Name,
			"IPv" + item.Family,
			item.Endpoint.Kind,
			fmt.Sprintf("%d/%d", len(item.Values), attempts),
			formatMilliseconds(median),
			formatMilliseconds(p95),
			fmt.Sprintf("%.2f ms", stddevFloat(floatValues)),
			icmpMin,
			icmpAvg,
			icmpMax,
			icmpMDev,
			icmpLoss,
			resolveText,
		})
		prefix := fmt.Sprintf("tcp_target_%02d_ipv%s", itemIndex+1, item.Family)
		successPercent := 0.0
		if attempts > 0 {
			successPercent = float64(len(item.Values)) / float64(attempts) * 100
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "_success_percent", Label: fmt.Sprintf("%s IPv%s TCP 成功率", item.Endpoint.Name, item.Family),
			Value: successPercent, Unit: "%", Display: fmt.Sprintf("%.1f %%", successPercent),
			Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(true),
		})
		if len(item.Values) > 0 {
			jitter := stddevFloat(floatValues)
			result.Measurements = append(result.Measurements,
				model.Measurement{
					Key: prefix + "_p50_ms", Label: fmt.Sprintf("%s IPv%s TCP P50", item.Endpoint.Name, item.Family),
					Value: float64(median) / float64(time.Millisecond), Unit: "ms", Display: formatMilliseconds(median),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_p95_ms", Label: fmt.Sprintf("%s IPv%s TCP P95", item.Endpoint.Name, item.Family),
					Value: float64(p95) / float64(time.Millisecond), Unit: "ms", Display: formatMilliseconds(p95),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_jitter_ms", Label: fmt.Sprintf("%s IPv%s TCP 标准差", item.Endpoint.Name, item.Family),
					Value: jitter, Unit: "ms", Display: fmt.Sprintf("%.2f ms", jitter),
					Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
				},
			)
		}
		if item.ResolveErr != nil {
			result.Notes = append(result.Notes, fmt.Sprintf("%s 解析失败：%s", item.Endpoint.Name, compactError(item.ResolveErr)))
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
		result.Summary = "全部 TCP 延迟目标不可达"
	} else {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "best_tcp_median_ms", Label: "最佳 TCP P50",
			Value: float64(best) / float64(time.Millisecond), Unit: "ms", Display: formatMilliseconds(best),
			Method: "tcp-connect-resolved-v2", HigherIsBetter: model.BoolPtr(false),
		})
		result.Summary = fmt.Sprintf("%s 最快 · P50 %s", bestName, formatMilliseconds(best))
	}
	result.Notes = append(result.Notes,
		"每个目标只解析一次 DNS，之后固定对该 IP 建连；表中的 TCP 延迟只包含三次握手，解析耗时单列。",
		"区域标签说明服务归属，不保证本次连接实际落在该地区。",
	)
	if icmpEnabled {
		result.Notes = append(result.Notes,
			"ICMP 列由系统 ping 提供，反映纯网络往返；与 TCP 列并列可区分链路问题与服务端排队。",
			"部分网络会限速或丢弃 ICMP，ICMP 丢包高但 TCP 正常时通常是策略限制而非线路故障。",
		)
	} else {
		result.Notes = append(result.Notes, "系统没有可用的 ping，本次只有 TCP 建连延迟；ecs 不会用 TCP 数字冒充 ICMP 结果。")
	}
	if len(intercepted) > 0 {
		result.Status = model.StatusWarning
		// 目标名单单独成字段而不是嵌进说明句：嵌进去会让整句随目标变化，
		// 既无法翻译也无法在不同机器之间对照。
		result.Fields = append(result.Fields, model.Field{
			Key: "tcp_intercepted_targets", Label: "疑似被代答的目标",
			Value: strings.Join(intercepted, "、"),
		})
		result.Notes = append(result.Notes, fmt.Sprintf(
			"%d 个目标的 TCP 建连延迟不到同目标 ICMP 往返的 1/%d：握手几乎不可能真的到达目标，"+
				"通常是本机或网关上的透明代理、TPROXY 重定向或加速器代答了 TCP。"+
				"此时 TCP 列反映的是到代理的距离，不能当作本机到目标的链路延迟；"+
				"ICMP 列不经代理，更接近真实往返。",
			len(intercepted), tcpInterceptRatio,
		))
	}
	result.Finish(start)
	return result
}

func formatICMPMilliseconds(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f ms", value)
}

func appendICMPMeasurements(result *model.Result, targetName string, stats icmpStats) {
	appendICMPMeasurementsForFamily(result, targetName, "", stats)
}

func appendICMPMeasurementsForFamily(result *model.Result, targetName, family string, stats icmpStats) {
	if result == nil || !stats.Available {
		return
	}
	slug := strings.ToLower(strings.ReplaceAll(targetName, " ", "_"))
	displayName := targetName
	if family != "" {
		slug += "_ipv" + strings.ToLower(family)
		displayName += " IPv" + family
	}
	appendMeasurement := func(suffix, label, unit, display string, value float64) {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return
		}
		result.Measurements = append(result.Measurements, model.Measurement{
			Key:            "icmp_" + suffix + "_" + slug,
			Label:          displayName + " ICMP " + label,
			Value:          value,
			Unit:           unit,
			Display:        display,
			Method:         "icmp-echo-v1",
			HigherIsBetter: model.BoolPtr(false),
		})
	}
	if stats.RTTKnown {
		appendMeasurement("min_ms", "最小", "ms", formatICMPMilliseconds(stats.MinMS), stats.MinMS)
		appendMeasurement("avg_ms", "平均", "ms", formatICMPMilliseconds(stats.AvgMS), stats.AvgMS)
		appendMeasurement("max_ms", "最大", "ms", formatICMPMilliseconds(stats.MaxMS), stats.MaxMS)
		if stats.StdDevKnown {
			appendMeasurement("mdev_ms", "mdev", "ms", formatICMPMilliseconds(stats.StdDevMS), stats.StdDevMS)
		}
	}
	if stats.LossKnown {
		appendMeasurement("loss_percent", "丢包", "%", fmt.Sprintf("%.2f %%", stats.LossPercent), stats.LossPercent)
	}
}

func latencyFamiliesForEndpoint(endpoint config.Endpoint, mode string, hasIPv6 bool) []string {
	families := latencyFamiliesWithCapability(endpoint.Address, mode, hasIPv6)
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

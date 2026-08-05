package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
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

	// IPv6 目标的地址来自运营商前缀；IPv6 的末端目标由地区节点域名
	// 提供，实际命中的骨干仍以路径中的跳为证据。前缀规则故意保持粗粒度，
	// 只做“看到了哪家骨干”的线索，不把它伪装成精确 ASN 归属证明。
	{Prefix: "240e:", ASN: 4134, Code: "CT-v6", Label: "电信 IPv6 骨干（240e）", Carrier: "电信", Quality: 10},
	{Prefix: "2408:8120:", ASN: 9929, Code: "CUII-v6", Label: "联通 CUII IPv6（2408:8120）", Carrier: "联通", Quality: 30},
	{Prefix: "2408:8000:", ASN: 4837, Code: "169-v6", Label: "联通 169 IPv6（2408:8000）", Carrier: "联通", Quality: 10},
	{Prefix: "2409:", ASN: 9808, Code: "CMNET-v6", Label: "移动 CMNET IPv6（2409）", Carrier: "移动", Quality: 10},
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

// backtraceHop is the structured, bounded view of one NextTrace hop used by
// the terminal report.  Unknown fields stay as an em dash; they are never
// inferred from a hostname or filled with fabricated geolocation.
type backtraceHop struct {
	Hop      int
	IP       string
	Latency  string
	ASN      string
	Network  string
	Location string
	Status   string
}

// backtraceRow 是一个参考目标的追踪结论。
type backtraceRow struct {
	Target  config.Endpoint
	Hits    []backtraceHit
	Hops    []string
	Details []backtraceHop
	Raw     string
	Err     error
}

// hopPrefixPattern 匹配历史文本解析器的跳号前缀。该解析器只保留给离线
// fixture/兼容性单测，运行时探针始终使用 NextTrace JSON。
var (
	hopLinePattern = regexp.MustCompile(`^\s*(\d{1,2})\s+(.*)$`)
	latencyPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*ms`)
)

func (backtraceProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("backtrace", "三网回程")
	result.Description = "向北京、上海、广州、成都的三网 IPv4/IPv6 参考目标追踪路径，识别中国骨干线路"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "启发式判断",
		Engine:          "NextTrace + 骨干网段特征表",
		Profile:         "china backbone signatures v2, max 20 hops, IPv4+IPv6 targets",
		ComparisonScope: "当次探测的路径特征；不是性能基准，也不等同于反向抓包",
	}

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Skip("未发现 NextTrace")
		result.Notes = append(result.Notes, "当前运行环境没有 NextTrace；run.sh 只会在 ECS_AUTO_DEPS 未关闭时从 NextTrace 官方 GitHub Release 临时准备已校验的 full 二进制，失败时跳过回程探测。")
		result.Finish(start)
		return result
	}

	targets := env.Config.BacktraceTargets
	if len(targets) == 0 {
		result.Skip("未配置三网回程参考目标")
		result.Finish(start)
		return result
	}
	targets = endpointsForIPVersion(targets, env.Config.IPVersion)
	if len(targets) == 0 {
		result.Skip("没有匹配当前协议族的三网回程目标")
		result.Notes = append(result.Notes, "当前协议族没有匹配的回程目标；IPv6 目标使用地区节点域名，需 DNS 与 IPv6 出口可用。")
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
			rows[index] = runBacktraceTarget(ctx, engine, target, endpointFamily(target, env.Config.IPVersion))
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
	detailTable := model.Table{
		Title:   "逐跳明细",
		Columns: []string{"参考目标", "运营商", "跳数", "延迟", "IP", "ASN", "网络/线路", "地理位置", "状态"},
		// IP 会暴露机房出口位置，默认按段遮盖；其余列来自路径工具或
		// 已知骨干特征，未知值统一使用占位符。
		SensitiveColumns: []int{4},
	}
	for _, row := range rows {
		if row.Err != nil || len(row.Details) == 0 {
			detailTable.Rows = append(detailTable.Rows, []string{
				row.Target.Name, row.Target.Kind, "—", "—", "—", "—", "—", "—", "追踪失败",
			})
			continue
		}
		for _, hop := range row.Details {
			detailTable.Rows = append(detailTable.Rows, []string{
				row.Target.Name,
				row.Target.Kind,
				strconv.Itoa(hop.Hop),
				fallbackBacktraceValue(hop.Latency),
				fallbackBacktraceValue(hop.IP),
				fallbackBacktraceValue(hop.ASN),
				fallbackBacktraceValue(hop.Network),
				fallbackBacktraceValue(hop.Location),
				fallbackBacktraceValue(hop.Status),
			})
		}
	}
	result.Tables = []model.Table{table, detailTable}
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
		"线路判定只依据路径上出现的骨干网段前缀，报告仅保留命中跳号、IP 等结构化信息供复核，不写入原始工具输出。",
		"CN2 的 GIA 与 GT 依据 59.43 段出现位置推断，带“推测”标注；精确区分需要更细的入口段表。",
		"IPv6 目标采用各省三网 TCP-Ping 节点域名，地址可能随 CDN 调度变化；报告仅保留目标名与结构化路径信息供复核，不写入原始路径。",
		"未命中任何已知特征时返回“未识别”，不会猜测线路类型。",
	)
	result.Finish(start)
	return result
}

// runBacktraceTarget 追踪单个参考目标并匹配线路特征。
func runBacktraceTarget(ctx context.Context, engine routeEngine, target config.Endpoint, family string) backtraceRow {
	row := backtraceRow{Target: target}
	traceCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	output, err := runRouteCommandForFamily(traceCtx, engine, target.Address, backtraceMaxHops, family)
	row.Raw = sanitizeCommandOutput(output)
	row.Details = extractTraceDetails(engine.Name, row.Raw)
	row.Hops = make([]string, len(row.Details))
	for index, detail := range row.Details {
		if detail.IP != "—" {
			row.Hops[index] = detail.IP
		}
	}
	// NextTrace 到国内 IP 时末段被丢弃是常态，只要拿到跳就继续分析；
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
	annotateBacktraceDetails(row.Details)
	return row
}

// extractTraceHops 按顺序取出每一跳的 IP，无响应的跳用空串占位。
func extractTraceHops(engineName, output string) []string {
	details := extractTraceDetails(engineName, output)
	hops := make([]string, len(details))
	for index, detail := range details {
		if detail.IP != "—" {
			hops[index] = detail.IP
		}
	}
	return hops
}

// extractTraceDetails parses NextTrace JSON. The historical classic-text
// parser remains available as an explicit parser-only helper, but is never a
// runtime fallback when NextTrace output is missing or malformed.
func extractTraceDetails(engineName, output string) []backtraceHop {
	if engineName != "nexttrace" {
		return nil
	}
	details, _ := extractNextTraceDetails(output)
	return details
}

func extractClassicTraceDetails(output string) []backtraceHop {
	details := []backtraceHop{}
	for _, line := range strings.Split(output, "\n") {
		match := hopLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		hopNumber, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		address := ""
		for _, token := range strings.Fields(match[2]) {
			token = strings.Trim(token, "[](),<>")
			if zone := strings.LastIndexByte(token, '%'); zone >= 0 {
				token = token[:zone]
			}
			if parsed := net.ParseIP(token); parsed != nil {
				address = parsed.String()
				break
			}
		}
		latency := "—"
		if matched := latencyPattern.FindStringSubmatch(match[2]); len(matched) > 1 {
			latency = matched[1] + " ms"
		}
		status := "已响应"
		if address == "" {
			status = "无响应"
			address = "—"
		}
		details = append(details, backtraceHop{
			Hop: hopNumber, IP: address, Latency: latency,
			ASN: "—", Network: "—", Location: "—", Status: status,
		})
	}
	return details
}

// extractNextTraceHops 解析 NextTrace 的 JSON 输出。
func extractNextTraceHops(output string) ([]string, bool) {
	details, ok := extractNextTraceDetails(output)
	if !ok {
		return nil, false
	}
	hops := make([]string, len(details))
	for index, detail := range details {
		if detail.IP != "—" {
			hops[index] = detail.IP
		}
	}
	return hops, true
}

func extractNextTraceDetails(output string) ([]backtraceHop, bool) {
	var payload struct {
		Hops [][]json.RawMessage `json:"Hops"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil || len(payload.Hops) == 0 {
		return nil, false
	}
	details := make([]backtraceHop, 0, len(payload.Hops))
	for index, probes := range payload.Hops {
		detail := backtraceHop{
			Hop: index + 1, IP: "—", Latency: "—", ASN: "—",
			Network: "—", Location: "—", Status: "无响应",
		}
		for _, rawProbe := range probes {
			probe := map[string]json.RawMessage{}
			_ = json.Unmarshal(rawProbe, &probe)
			address := normalizeTraceAddress(jsonMapString(probe, "Address", "IP", "Ip"))
			if address == "" {
				var scalar string
				if json.Unmarshal(rawProbe, &scalar) == nil {
					address = normalizeTraceAddress(scalar)
				}
			}
			if address == "" {
				continue
			}
			detail.IP = address
			detail.Latency = normalizeTraceLatency(jsonMapString(probe, "RTT", "Latency", "Delay", "Time", "AvgRTT"))
			detail.ASN = normalizeTraceASN(jsonMapString(probe, "ASN", "ASNumber", "AS", "ASNO"))
			detail.Network = firstNonEmptyTraceValue(jsonMapString(probe, "ASName", "Organization", "Org", "ISP", "Network", "Owner", "PTR", "Host", "Hostname"), "—")
			detail.Location = traceLocation(probe)
			detail.Status = "已响应"
			break
		}
		details = append(details, detail)
	}
	return details, true
}

func jsonMapString(values map[string]json.RawMessage, names ...string) string {
	raw, ok := jsonMapRaw(values, names...)
	if !ok {
		return ""
	}
	return jsonRawString(raw)
}

func jsonMapRaw(values map[string]json.RawMessage, names ...string) (json.RawMessage, bool) {
	for _, name := range names {
		for key, raw := range values {
			if !strings.EqualFold(key, name) {
				continue
			}
			return raw, true
		}
	}
	return nil, false
}

func jsonRawString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		for _, item := range values {
			if value := jsonRawString(item); value != "" {
				return value
			}
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for _, key := range []string{"Name", "name", "City", "city", "Location", "location", "Value", "value"} {
			if item, ok := object[key]; ok {
				if value := jsonRawString(item); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func normalizeTraceAddress(value string) string {
	value = strings.TrimSpace(value)
	if zone := strings.LastIndexByte(value, '%'); zone >= 0 {
		value = value[:zone]
	}
	if parsed := net.ParseIP(strings.Trim(value, "[](),<>")); parsed != nil {
		return parsed.String()
	}
	return ""
}

func normalizeTraceLatency(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	if match := latencyPattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + " ms"
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return strconv.FormatFloat(number, 'f', -1, 64) + " ms"
	}
	return value
}

func normalizeTraceASN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "—"
	}
	if strings.HasPrefix(strings.ToUpper(value), "AS") {
		return value
	}
	if number, err := strconv.Atoi(value); err == nil && number > 0 {
		return "AS" + strconv.Itoa(number)
	}
	return value
}

func firstNonEmptyTraceValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func traceLocation(values map[string]json.RawMessage) string {
	parts := traceLocationParts(values)
	if len(parts) > 0 {
		return strings.Join(parts, " / ")
	}
	for _, name := range []string{"Location", "Geo"} {
		raw, ok := jsonMapRaw(values, name)
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			if nestedParts := traceLocationParts(nested); len(nestedParts) > 0 {
				return strings.Join(nestedParts, " / ")
			}
		}
	}
	if value := strings.TrimSpace(jsonMapString(values, "Location", "Geo", "Region")); value != "" {
		return value
	}
	lat := strings.TrimSpace(jsonMapString(values, "lat", "latitude"))
	lng := strings.TrimSpace(jsonMapString(values, "lng", "lon", "longitude"))
	if lat != "" && lng != "" {
		return lat + ", " + lng
	}
	return "—"
}

func traceLocationParts(values map[string]json.RawMessage) []string {
	parts := make([]string, 0, 4)
	for _, name := range []string{"Country", "country", "Prov", "prov", "State", "state", "City", "city", "District", "district"} {
		if value := strings.TrimSpace(jsonMapString(values, name)); value != "" {
			duplicate := false
			for _, existing := range parts {
				if strings.EqualFold(existing, value) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				parts = append(parts, value)
			}
		}
	}
	return parts
}

func fallbackBacktraceValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func annotateBacktraceDetails(details []backtraceHop) {
	for index := range details {
		if details[index].IP == "" || details[index].IP == "—" {
			continue
		}
		for _, signature := range chinaRouteSignatures {
			if !strings.HasPrefix(strings.ToLower(details[index].IP), strings.ToLower(signature.Prefix)) {
				continue
			}
			// A prefix signature identifies the route label, not the exact
			// ASN of this hop.  Keep ASN unknown unless the probe supplied it;
			// otherwise the inferred signature would look like fabricated
			// metadata in the hop table.
			if details[index].Network == "" || details[index].Network == "—" {
				details[index].Network = signature.Label
			}
			break
		}
	}
}

// matchRouteSignatures 按跳序匹配所有已知骨干特征。
func matchRouteSignatures(hops []string) []backtraceHit {
	var hits []backtraceHit
	for index, address := range hops {
		if address == "" {
			continue
		}
		for _, signature := range chinaRouteSignatures {
			if strings.HasPrefix(strings.ToLower(address), strings.ToLower(signature.Prefix)) {
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

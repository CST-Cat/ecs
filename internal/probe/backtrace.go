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

func (backtraceProbe) ID() string { return "backtrace" }

const (
	backtraceStatusIdentified   = "probe.backtrace.status.identified"
	backtraceStatusUnidentified = "probe.backtrace.status.unidentified"
	backtraceStatusFailed       = "probe.backtrace.status.failed"

	backtraceReasonSignatureMatch    = "probe.backtrace.reason.signature_match"
	backtraceReasonForeignOnly       = "probe.backtrace.reason.foreign_carrier_only"
	backtraceReasonNoKnownSignature  = "probe.backtrace.reason.no_known_signature"
	backtraceReasonLimitedOrFiltered = "probe.backtrace.reason.limited_or_filtered"
	backtraceReasonTraceError        = "probe.backtrace.reason.trace_error"
	backtraceReasonParseFailed       = "probe.backtrace.reason.parse_failed"
	backtraceReasonNoResponsiveHops  = "probe.backtrace.reason.no_responsive_hops"

	backtraceLineTelecomCN2      = "probe.backtrace.line.telecom.cn2"
	backtraceLineTelecomCN2GIA   = "probe.backtrace.line.telecom.cn2.gia"
	backtraceLineTelecomCN2GT    = "probe.backtrace.line.telecom.cn2.gt"
	backtraceLineTelecom163      = "probe.backtrace.line.telecom.163"
	backtraceLineUnicomCUII      = "probe.backtrace.line.unicom.cuii"
	backtraceLineUnicom169       = "probe.backtrace.line.unicom.169"
	backtraceLineMobileCMI       = "probe.backtrace.line.mobile.cmi"
	backtraceLineMobileCMNET     = "probe.backtrace.line.mobile.cmnet"
	backtraceLineTelecomIPv6     = "probe.backtrace.line.telecom.ipv6"
	backtraceLineUnicomCUIIIPv6  = "probe.backtrace.line.unicom.cuii_ipv6"
	backtraceLineUnicom169IPv6   = "probe.backtrace.line.unicom.169_ipv6"
	backtraceLineMobileCMNETIPv6 = "probe.backtrace.line.mobile.cmnet_ipv6"
	backtraceMissingValue        = "probe.backtrace.value.missing"
)

// routeSignature 是一条中国骨干网线路的识别特征。
type routeSignature struct {
	// Prefix 是该线路骨干网段的粗粒度地址前缀，只用于线路分类线索。
	Prefix string
	// Code 是线路简称，用于表格。
	Code string
	// LineKey is the stable catalog key for the complete line identity.
	LineKey string
	// Carrier 是所属运营商的稳定 machine identity。
	Carrier string
	// Quality 越大代表线路越优质，用于在多个命中之间挑选结论。
	Quality int
}

// chinaRouteSignatures 覆盖三大运营商的主要骨干线路。
//
// 这里只做粗粒度前缀匹配，用于识别运营商与线路分类；逐跳 ASN 必须来自
// NextTrace 返回的 hop 元数据，不能由静态前缀签名推断。
var chinaRouteSignatures = []routeSignature{
	// 中国电信：CN2 优于 163。
	{Prefix: "59.43.", Code: "CN2", LineKey: backtraceLineTelecomCN2, Carrier: config.BacktraceCarrierTelecom, Quality: 30},
	{Prefix: "202.97.", Code: "163", LineKey: backtraceLineTelecom163, Carrier: config.BacktraceCarrierTelecom, Quality: 10},

	// 中国联通：CUII/A 网优于 169。
	{Prefix: "218.105.", Code: "CUII", LineKey: backtraceLineUnicomCUII, Carrier: config.BacktraceCarrierUnicom, Quality: 30},
	{Prefix: "210.51.", Code: "CUII", LineKey: backtraceLineUnicomCUII, Carrier: config.BacktraceCarrierUnicom, Quality: 30},
	{Prefix: "219.158.", Code: "169", LineKey: backtraceLineUnicom169, Carrier: config.BacktraceCarrierUnicom, Quality: 10},

	// 中国移动：CMI 国际优于普通 CMNET。
	{Prefix: "223.120.", Code: "CMI", LineKey: backtraceLineMobileCMI, Carrier: config.BacktraceCarrierMobile, Quality: 30},
	{Prefix: "221.183.", Code: "CMNET", LineKey: backtraceLineMobileCMNET, Carrier: config.BacktraceCarrierMobile, Quality: 10},

	// IPv6 目标的地址来自运营商前缀；IPv6 的末端目标由地区节点域名
	// 提供，实际命中的骨干仍以路径中的跳为证据。前缀规则故意保持粗粒度，
	// 只做“看到了哪家骨干/线路分类”的线索，不推断逐跳 ASN。
	{Prefix: "240e:", Code: "CT-v6", LineKey: backtraceLineTelecomIPv6, Carrier: config.BacktraceCarrierTelecom, Quality: 10},
	{Prefix: "2408:8120:", Code: "CUII-v6", LineKey: backtraceLineUnicomCUIIIPv6, Carrier: config.BacktraceCarrierUnicom, Quality: 30},
	{Prefix: "2408:8000:", Code: "169-v6", LineKey: backtraceLineUnicom169IPv6, Carrier: config.BacktraceCarrierUnicom, Quality: 10},
	{Prefix: "2409:", Code: "CMNET-v6", LineKey: backtraceLineMobileCMNETIPv6, Carrier: config.BacktraceCarrierMobile, Quality: 10},
}

// backtraceMaxHops 是回程识别的跳数上限。
//
// 从海外 VPS 到中国骨干通常在 10-15 跳之间进入 202.97 / 59.43 / 219.158 这类
// 特征段，之后往往被目标侧过滤成连续的 `*`。20 跳能覆盖特征段又不会把时间浪费
// 在必然无响应的尾部；12 跳的路径快照上限则会让特征来不及出现。
const backtraceMaxHops = 20

// BacktraceMaxHops 是 backtrace 模块实际使用的跳数上限，供比较签名引用。
const BacktraceMaxHops = backtraceMaxHops

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
// the terminal report. Unknown fields stay empty in canonical JSON; the
// renderer supplies the localized missing-value label at the display edge.
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
	Target           config.Endpoint
	Hits             []backtraceHit
	Hops             []string
	Details          []backtraceHop
	Raw              string
	Err              error
	ParseFailed      bool
	NoResponsiveHops bool
}

// latencyPattern extracts latency values from the structured NextTrace hop
// fields that are rendered in the current report.
var (
	latencyPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*ms`)
)

func (backtraceProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("backtrace", "module.backtrace.title")
	result.Description = "probe.backtrace.description"
	result.Methodology = model.Methodology{
		Kind:            "heuristic",
		Label:           "methodology.heuristic",
		Engine:          "probe.backtrace.methodology.engine",
		Profile:         "probe.backtrace.profile",
		ComparisonScope: "probe.backtrace.comparison_scope",
	}
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameterJSON(result.Methodology.Parameters, "targets", env.Config.BacktraceTargets)
	addComparisonParameter(result.Methodology.Parameters, "max_hops", strconv.Itoa(backtraceMaxHops))
	addComparisonParameter(result.Methodology.Parameters, "signature_set", "china-backbone-v2")

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.tool_missing")}
		result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: routeEngineTiny, Count: 1})
		result.Evidence = model.NewEvidence(0, len(env.Config.BacktraceTargets), "target")
		result.Notes = []string{"probe.backtrace.note.tool_missing"}
		result.Finish(start)
		return result
	}

	targets := env.Config.BacktraceTargets
	if len(targets) == 0 {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.no_targets")}
		result.Evidence = model.NewEvidence(0, 0, "target")
		result.Notes = []string{"probe.backtrace.note.no_targets"}
		result.Finish(start)
		return result
	}
	targets = endpointsForIPVersion(targets, env.Config.IPVersion)
	if len(targets) == 0 {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.no_family_targets")}
		result.Evidence = model.NewEvidence(0, 0, "target")
		result.Notes = []string{"probe.backtrace.note.no_family_targets"}
		result.Finish(start)
		return result
	}
	result.Fields = []model.Field{
		{Key: "nexttrace_binary", Label: "probe.backtrace.field.nexttrace_binary", Value: model.RawValue(engine.Name)},
		{Key: "nexttrace_version", Label: "probe.backtrace.field.nexttrace_version", Value: model.RawValue(fallback(engine.Version, "unknown"))},
		{Key: "arguments", Label: "probe.backtrace.field.arguments", Value: model.RawValue(strings.Join(routeCommandArgsForFamily(engine, "<target>", backtraceMaxHops, endpointFamily(targets[0], env.Config.IPVersion)), " "))},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", fallback(engine.Version, "unknown"))

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
		Key:   "network.backtrace.summary",
		Title: "probe.backtrace.table.summary",
		Columns: []model.TableColumn{
			{Key: "provider", Label: "probe.backtrace.column.provider"},
			{Key: "reference_target", Label: "probe.backtrace.column.target"},
			{Key: "line", Label: "probe.backtrace.column.line"},
			{Key: "hit_hop", Label: "probe.backtrace.column.hit_hop"},
			{Key: "hit_ip", Label: "probe.backtrace.column.hit_ip"},
			{Key: "status", Label: "probe.backtrace.column.status"},
			{Key: "reason", Label: "probe.backtrace.column.reason"},
		},
	}
	identified := 0
	validTraces := 0
	failedTargets := 0
	parseFailed := false
	for _, row := range rows {
		// 原始路径无论识别成功与否都要保留：未识别时它恰恰是判断"线路确实没走
		// 已知骨干"还是"探测被限速打断"的唯一依据。
		if row.Raw != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    "probe.backtrace.raw_output",
				Language: "text",
				Content:  row.Raw,
			})
		}
		if row.Err != nil {
			failedTargets++
			addFailure(&result, "trace", row.Target.Address, row.Err)
			if countValidTraceHops(row.Details) == 0 {
				table.Rows = append(table.Rows, []model.Value{
					backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
					model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonTraceError),
				})
				continue
			}
			validTraces++
		} else if row.ParseFailed {
			failedTargets++
			result.AddFailure(model.Failure{Category: model.FailureParse, Stage: "parse", Target: row.Target.Address, Count: 1})
			parseFailed = true
			table.Rows = append(table.Rows, []model.Value{
				backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
				model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonParseFailed),
			})
			continue
		} else if row.NoResponsiveHops {
			failedTargets++
			result.AddFailure(model.Failure{Category: model.FailureUnknown, Stage: "trace", Target: row.Target.Address, Count: 1})
			table.Rows = append(table.Rows, []model.Value{
				backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
				model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonNoResponsiveHops),
			})
			continue
		} else {
			validTraces++
		}
		best, ok := bestBacktraceHit(row.Hits, row.Target.Kind)
		status := backtraceStatusUnidentified
		reason := backtraceUnidentifiedReason(row)
		line, hitHop, hitIP := backtraceMissingValue, backtraceMissingValue, backtraceMissingValue
		if ok {
			identified++
			line = backtraceLineKey(best, row.Hits)
			hitHop = strconv.Itoa(best.Hop)
			hitIP = best.IP
			status = backtraceStatusIdentified
			reason = backtraceReasonSignatureMatch
		}
		table.Rows = append(table.Rows, []model.Value{
			backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(line), backtraceDataValue(hitHop), backtraceDataValue(hitIP), model.KeyValue(status), model.KeyValue(reason),
		})
	}
	detailTable := model.Table{
		Key:   "network.backtrace.hops",
		Title: "probe.backtrace.table.hops",
		Columns: []model.TableColumn{
			{Key: "reference_target", Label: "probe.backtrace.column.target"},
			{Key: "provider", Label: "probe.backtrace.column.provider"},
			{Key: "hop", Label: "probe.backtrace.column.hop"},
			{Key: "latency_ms", Label: "probe.backtrace.column.latency"},
			{Key: "ip", Label: "probe.backtrace.column.ip"},
			{Key: "asn", Label: "probe.backtrace.column.asn"},
			{Key: "network", Label: "probe.backtrace.column.network"},
			{Key: "location", Label: "probe.backtrace.column.location"},
			{Key: "status", Label: "probe.backtrace.column.status"},
		},
		// 回程跳点是远端路径信息；按要求只脱敏本机出口 IP。
	}
	for _, row := range rows {
		if len(row.Details) == 0 {
			detailTable.Rows = append(detailTable.Rows, []model.Value{
				backtraceTargetValue(row.Target.Name), backtraceCarrierValue(row.Target.Kind), model.KeyValue(backtraceMissingValue), backtraceDataValue(backtraceMissingValue), backtraceDataValue(backtraceMissingValue), backtraceDataValue(backtraceMissingValue), backtraceDataValue(backtraceMissingValue), backtraceDataValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed),
			})
			continue
		}
		for _, hop := range row.Details {
			detailTable.Rows = append(detailTable.Rows, []model.Value{
				backtraceTargetValue(row.Target.Name),
				backtraceCarrierValue(row.Target.Kind),
				model.RawValue(strconv.Itoa(hop.Hop)),
				backtraceDataValue(backtraceCellValue(hop.Latency)),
				backtraceDataValue(backtraceCellValue(hop.IP)),
				backtraceDataValue(backtraceCellValue(hop.ASN)),
				backtraceNetworkValue(backtraceCellValue(hop.Network)),
				backtraceDataValue(backtraceCellValue(hop.Location)),
				model.KeyValue(backtraceCellValue(hop.Status)),
			})
		}
	}
	result.Tables = []model.Table{table, detailTable}
	result.Measurements = []model.Measurement{
		{
			Key: "backtrace_identified", Label: "probe.backtrace.metric.identified",
			Value: float64(identified), Unit: "count",
			Display: model.RawValue(fmt.Sprintf("%d/%d", identified, len(targets))),
			Method:  "china-backbone-signature-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	result.Evidence = model.NewEvidence(validTraces, len(targets), "target")
	if validTraces == 0 {
		result.Status = model.StatusError
	} else if failedTargets > 0 {
		result.Status = model.StatusWarning
	}
	result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.values", identified, len(targets))}
	result.Sources = []model.Source{
		{Name: "probe.backtrace.source.method.name", URL: "https://github.com/zhanghanyun/backtrace", Purpose: "probe.backtrace.source.method"},
	}
	result.Notes = []string{"probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified"}
	if parseFailed {
		result.Notes = append(result.Notes, "probe.backtrace.note.parse_failed")
	}
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
	if cause := contextCauseError(traceCtx); cause != nil {
		row.Err = cause
		return row
	}
	var parsed bool
	row.Details, parsed = extractNextTraceDetails(row.Raw)
	row.Hops = make([]string, len(row.Details))
	for index, detail := range row.Details {
		if detail.IP != "" {
			row.Hops[index] = detail.IP
		}
	}
	// NextTrace 到国内 IP 时末段被丢弃是常态，只要拿到一个有效跳就继续分析；
	// malformed JSON 或空 Hops 才是解析失败；合法但全部无响应的路径保留为
	// 独立 machine fact，不能把 hop.no_response 误报成 parse_failed。
	if countValidTraceHops(row.Details) == 0 {
		if err != nil {
			row.Err = err
		} else if !parsed || len(row.Details) == 0 {
			row.ParseFailed = true
		} else {
			row.NoResponsiveHops = true
		}
		return row
	}
	row.Err = err
	row.Hits = matchRouteSignatures(row.Hops)
	annotateBacktraceDetails(row.Details)
	return row
}

// extractTraceHops 按顺序取出每一跳的 IP，无响应的跳用空串占位。
func extractTraceHops(engineName, output string) []string {
	details := extractTraceDetails(engineName, output)
	hops := make([]string, len(details))
	for index, detail := range details {
		if detail.IP != "" {
			hops[index] = detail.IP
		}
	}
	return hops
}

// extractTraceDetails parses the supported NextTrace Tiny JSON output.
func extractTraceDetails(engineName, output string) []backtraceHop {
	if !isNextTraceEngine(engineName) {
		return nil
	}
	details, _ := extractNextTraceDetails(output)
	return details
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
			Hop: index + 1, Status: "probe.backtrace.hop.no_response",
		}
		for _, rawProbe := range probes {
			probe := map[string]json.RawMessage{}
			_ = json.Unmarshal(rawProbe, &probe)
			address := normalizeTraceAddress(jsonRawAddress(jsonMapRawValue(probe, "Address", "IP", "Ip")))
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
			detail.ASN = normalizeTraceASN(traceProbeValue(probe, "ASN", "ASNumber", "AS", "ASNO", "Asnumber"))
			detail.Network = firstNonEmptyTraceValue(traceNetworkValue(probe), "")
			detail.Location = traceLocation(probe)
			detail.Status = "probe.backtrace.hop.responded"
			break
		}
		details = append(details, detail)
	}
	return details, true
}

func countValidTraceHops(details []backtraceHop) int {
	count := 0
	for _, detail := range details {
		if detail.IP != "" {
			count++
		}
	}
	return count
}

func jsonMapRawValue(values map[string]json.RawMessage, names ...string) json.RawMessage {
	raw, _ := jsonMapRaw(values, names...)
	return raw
}

func jsonRawAddress(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if value := jsonRawString(raw); value != "" {
		return value
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(raw, &nested) != nil {
		return ""
	}
	return jsonRawString(jsonMapRawValue(nested, "IP", "Ip", "Address", "Addr"))
}

func traceProbeValue(probe map[string]json.RawMessage, names ...string) string {
	if value := jsonMapString(probe, names...); value != "" {
		return value
	}
	geoRaw, ok := jsonMapRaw(probe, "Geo")
	if !ok {
		return ""
	}
	var geo map[string]json.RawMessage
	if json.Unmarshal(geoRaw, &geo) != nil {
		return ""
	}
	return jsonMapString(geo, names...)
}

// traceNetworkValue keeps network identity ahead of presentation-only host
// names. Official NextTrace JSON commonly puts the carrier in Geo.owner while
// also exposing a reverse-DNS Hostname at the probe level; checking every
// probe key in one flat list would let that Hostname mask the carrier.
func traceNetworkValue(probe map[string]json.RawMessage) string {
	if value := jsonMapString(probe, "ASName", "Organization", "Org", "ISP", "Isp", "Network"); value != "" {
		return value
	}
	if geoRaw, ok := jsonMapRaw(probe, "Geo"); ok {
		var geo map[string]json.RawMessage
		if json.Unmarshal(geoRaw, &geo) == nil {
			if value := jsonMapString(geo, "ASName", "Organization", "Org", "ISP", "Isp", "Network", "Owner"); value != "" {
				return value
			}
		}
	}
	return jsonMapString(probe, "Owner", "PTR", "Host", "Hostname")
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
		for _, key := range []string{"Name", "name", "City", "city", "Location", "location", "Value", "value", "IP", "Ip", "Address", "Addr"} {
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
		return ""
	}
	if match := latencyPattern.FindStringSubmatch(value); len(match) > 1 {
		return match[1] + " ms"
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		// NextTrace serializes net.Duration RTT values as nanoseconds.  Keep
		// Small numeric RTT values are emitted as milliseconds by some Tiny builds.
		if number >= 1000 {
			number /= float64(time.Millisecond)
		}
		return strconv.FormatFloat(number, 'f', -1, 64) + " ms"
	}
	return value
}

func normalizeTraceASN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
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
	return ""
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

func backtraceCellValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return backtraceMissingValue
	}
	return value
}

// backtraceTargetValue uses the explicit built-in target-key format. Custom
// target names remain literal even when they happen to resemble a catalog
// entry; no catalog lookup is used to infer the variant.
func backtraceTargetValue(name string) model.Value {
	const prefix = "probe.backtrace.target."
	suffix, ok := strings.CutPrefix(name, prefix)
	if ok {
		parts := strings.Split(suffix, ".")
		if len(parts) == 3 && isBacktraceCity(parts[0]) && isBacktraceCarrier(parts[1]) && isBacktraceFamily(parts[2]) {
			return model.KeyValue(name)
		}
	}
	return model.RawValue(name)
}

func isBacktraceCity(city string) bool {
	for _, known := range config.BacktraceCityOrder() {
		if city == known {
			return true
		}
	}
	return false
}

func isBacktraceCarrier(carrier string) bool {
	switch carrier {
	case config.BacktraceCarrierTelecom, config.BacktraceCarrierUnicom, config.BacktraceCarrierMobile:
		return true
	default:
		return false
	}
}

func isBacktraceFamily(family string) bool {
	return family == "ipv4" || family == "ipv6"
}

func backtraceCarrierValue(carrier string) model.Value {
	key := backtraceCarrierKey(carrier)
	if key == carrier {
		return model.RawValue(carrier)
	}
	return model.KeyValue(key)
}

func backtraceDataValue(value string) model.Value {
	if value == backtraceMissingValue {
		return model.KeyValue(value)
	}
	return model.RawValue(value)
}

func backtraceNetworkValue(value string) model.Value {
	if value == backtraceMissingValue || isBacktraceLineKey(value) {
		return model.KeyValue(value)
	}
	return model.RawValue(value)
}

func isBacktraceLineKey(value string) bool {
	switch value {
	case backtraceLineTelecomCN2, backtraceLineTelecomCN2GIA, backtraceLineTelecomCN2GT,
		backtraceLineTelecom163, backtraceLineUnicomCUII, backtraceLineUnicom169,
		backtraceLineMobileCMI, backtraceLineMobileCMNET, backtraceLineTelecomIPv6,
		backtraceLineUnicomCUIIIPv6, backtraceLineUnicom169IPv6, backtraceLineMobileCMNETIPv6:
		return true
	default:
		return false
	}
}

func backtraceCarrierKey(carrier string) string {
	switch carrier {
	case config.BacktraceCarrierTelecom:
		return "probe.backtrace.carrier.telecom"
	case config.BacktraceCarrierUnicom:
		return "probe.backtrace.carrier.unicom"
	case config.BacktraceCarrierMobile:
		return "probe.backtrace.carrier.mobile"
	default:
		return carrier
	}
}

func annotateBacktraceDetails(details []backtraceHop) {
	for index := range details {
		if details[index].IP == "" {
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
			if details[index].Network == "" {
				details[index].Network = signature.LineKey
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

// bestBacktraceHit 在目标运营商自己的命中里挑出代表本次线路的那一条。
//
// 同一路径上同时出现 163 与 CN2 是常见情况（先经骨干再进 CN2），此时应以更优质
// 的线路作为结论，质量相同则取更靠前的跳。异网骨干可以保留为证据，但不能
// 被当作这个参考目标的运营商结论。
func bestBacktraceHit(hits []backtraceHit, targetCarrier string) (backtraceHit, bool) {
	matching := make([]backtraceHit, 0, len(hits))
	for _, hit := range hits {
		if hit.Signature.Carrier == targetCarrier {
			matching = append(matching, hit)
		}
	}
	if len(matching) == 0 {
		return backtraceHit{}, false
	}
	sorted := append([]backtraceHit(nil), matching...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Signature.Quality != sorted[j].Signature.Quality {
			return sorted[i].Signature.Quality > sorted[j].Signature.Quality
		}
		return sorted[i].Hop < sorted[j].Hop
	})
	return sorted[0], true
}

func backtraceUnidentifiedReason(row backtraceRow) string {
	foreign := make(map[string]bool)
	for _, hit := range row.Hits {
		if hit.Signature.Carrier != "" && hit.Signature.Carrier != row.Target.Kind {
			foreign[hit.Signature.Carrier] = true
		}
	}
	if len(foreign) > 0 {
		return backtraceReasonForeignOnly
	}

	responded := 0
	for _, hop := range row.Hops {
		if hop != "" {
			responded++
		}
	}
	// 绝大多数跳都没响应时，更可能是探测被限速或过滤，而不是线路真的陌生。
	if len(row.Hops) > 0 && responded*2 < len(row.Hops) {
		return backtraceReasonLimitedOrFiltered
	}
	return backtraceReasonNoKnownSignature
}

// backtraceLineKey returns a finite line key. For CN2, the order of the 163
// and CN2 signatures distinguishes the two documented variants; the result
// remains a machine key and never embeds presentation prose.
func backtraceLineKey(best backtraceHit, hits []backtraceHit) string {
	lineKey := best.Signature.LineKey
	if best.Signature.Code != "CN2" {
		return lineKey
	}
	firstCN2, first163 := -1, -1
	for _, hit := range hits {
		if hit.Signature.Carrier != best.Signature.Carrier {
			continue
		}
		if hit.Signature.Code == "CN2" && (firstCN2 < 0 || hit.Hop < firstCN2) {
			firstCN2 = hit.Hop
		}
		if hit.Signature.Code == "163" && (first163 < 0 || hit.Hop < first163) {
			first163 = hit.Hop
		}
	}
	switch {
	case first163 < 0 || firstCN2 < first163:
		return backtraceLineTelecomCN2GIA
	default:
		return backtraceLineTelecomCN2GT
	}
}

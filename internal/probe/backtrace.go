package probe

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/failure"
	"ecs/internal/model"
)

// 三网回程线路识别。
//
// 原理与 backtrace 一致：从 VPS 向三大运营商的参考 IP 做路由追踪，观察路径上
// 出现的来源支持的中国运营商网络/线路身份。国内运营商的进出向路由通常走同一
// 条线路，因此这条路径上的可审计事实可用于描述"中国用户访问该 VPS 时数据回来
// 走哪条线"；不具备直接证据的身份保持未知。
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

	backtraceLineTelecomCN2VariantUnknown = "probe.backtrace.line.telecom.cn2.variant_unknown"
	backtraceLineTelecomChinanet          = "probe.backtrace.line.telecom.chinanet"
	backtraceLineUnicomNetworkUnknown     = "probe.backtrace.line.unicom.network.variant_unknown"
	backtraceLineUnicomBackbone           = "probe.backtrace.line.unicom.backbone"
	backtraceLineUnicomCUII               = "probe.backtrace.line.unicom.cuii"
	backtraceLineUnicom169                = "probe.backtrace.line.unicom.169"
	backtraceLineMobileCMI                = "probe.backtrace.line.mobile.cmi"
	backtraceLineMobileNetworkUnknown     = "probe.backtrace.line.mobile.network.variant_unknown"
	backtraceLineMobileCMNET              = "probe.backtrace.line.mobile.cmnet"
	backtraceLineTelecomIPv6              = "probe.backtrace.line.telecom.ipv6"
	backtraceLineUnicomIPv6               = "probe.backtrace.line.unicom.ipv6"
	backtraceLineMobileCMNETIPv6          = "probe.backtrace.line.mobile.cmnet_ipv6"
	backtraceMissingValue                 = "probe.backtrace.value.missing"
	// Keep the existing stdout text-block key stable for backtrace reports;
	// stderr and normalized facts are the minimal additional audit blocks.
	backtraceRawOutputTitle      = "probe.backtrace.raw_output"
	backtraceRawStderrTitle      = "probe.backtrace.raw_stderr"
	backtraceNormalizedJSONTitle = "probe.backtrace.normalized_trace_json"
)

type signatureMatchSource string

const (
	signatureMatchByASN       signatureMatchSource = "asn"
	signatureMatchByCIDR      signatureMatchSource = "cidr"
	signatureMatchByHeuristic signatureMatchSource = "heuristic"
	backtraceSignatureVersion                      = "v3"
)

// routeSignature 是一条可审计的线路分类规则。ASNs 和 CIDRs 是有来源的
// identity 事实；HeuristicPrefixes 只保留给明确标注为低置信证据的规则，不能
// 取代 ASN 或精确 CIDR，也不能反向填入 traceHop 的观测字段。
type routeSignature struct {
	// Code 是规则简称，用于表格；它不能超出 SourceURL 支持的身份事实。
	Code string
	// LineKey is the stable catalog key for the complete line identity.
	LineKey string
	// Carrier 是所属运营商的稳定 machine identity。
	Carrier string
	// Quality 越大代表规则选择优先级；它不表示网络性能或产品质量。
	Quality int
	// ASNs are authoritative registry identities for the rule's stated network
	// or line identity. Values use the canonical "AS####" spelling.
	ASNs []string
	// CIDRs are exact registry allocations or routed prefixes. They are used
	// only after no matching ASN is present on the canonical hop.
	CIDRs []netip.Prefix
	// HeuristicPrefixes are deliberately not populated by the production table
	// until a source proves their meaning. Tests exercise this final, explicit
	// low-confidence tier without making it an authoritative identity.
	HeuristicPrefixes []string
	// SourceURL points at the registry object that supports this rule.
	SourceURL string
}

// chinaRouteSignatures 覆盖三大运营商的来源支持网络/线路身份。
//
// The production set deliberately contains no broad string-prefix rules. Every
// CIDR below is an exact APNIC RDAP allocation, while ASN entries use APNIC
// aut-num identities. This prevents a registry object such as 202.97.96.0/19
// (China Telecom HK) or 2409::/40 (outside the China Mobile allocation) from
// being classified merely because it shares an old textual prefix.
var chinaRouteSignatures = []routeSignature{
	// ASN facts. The line names below are used only when the corresponding
	// APNIC aut-num object states that identity. AS9808 names China Mobile but
	// does not state CMNET, so it deliberately uses a neutral network key.
	// CN2's GIA/GT variant cannot be established from a hop ASN or path order,
	// so it is intentionally represented as "variant unknown".
	{Code: "CN2", LineKey: backtraceLineTelecomCN2VariantUnknown, Carrier: config.BacktraceCarrierTelecom, Quality: 30, ASNs: []string{"AS4809"}, SourceURL: "https://rdap.apnic.net/autnum/4809"},
	{Code: "CHINANET", LineKey: backtraceLineTelecomChinanet, Carrier: config.BacktraceCarrierTelecom, Quality: 10, ASNs: []string{"AS4134"}, SourceURL: "https://rdap.apnic.net/autnum/4134"},
	{Code: "CUII", LineKey: backtraceLineUnicomCUII, Carrier: config.BacktraceCarrierUnicom, Quality: 30, ASNs: []string{"AS9929"}, SourceURL: "https://rdap.apnic.net/autnum/9929"},
	{Code: "169", LineKey: backtraceLineUnicom169, Carrier: config.BacktraceCarrierUnicom, Quality: 10, ASNs: []string{"AS4837"}, SourceURL: "https://rdap.apnic.net/autnum/4837"},
	{Code: "CMI", LineKey: backtraceLineMobileCMI, Carrier: config.BacktraceCarrierMobile, Quality: 30, ASNs: []string{"AS58453"}, SourceURL: "https://rdap.apnic.net/autnum/58453"},
	{Code: "CHINAMOBILE", LineKey: backtraceLineMobileNetworkUnknown, Carrier: config.BacktraceCarrierMobile, Quality: 20, ASNs: []string{"AS9808"}, SourceURL: "https://rdap.apnic.net/autnum/9808"},
	{Code: "CMNET", LineKey: backtraceLineMobileCMNET, Carrier: config.BacktraceCarrierMobile, Quality: 10, ASNs: []string{"AS56048"}, SourceURL: "https://rdap.apnic.net/autnum/56048"},

	// Exact IPv4 allocations. 218.104/14 identifies a China Unicom network but
	// does not state backbone, CUII, or China169, so its fallback is neutral.
	// The 210.51 CNC-BJ-IDC allocation is deliberately absent: its registry
	// object does not establish a current operator or routing identity.
	{Code: "CN2", LineKey: backtraceLineTelecomCN2VariantUnknown, Carrier: config.BacktraceCarrierTelecom, Quality: 30, CIDRs: routePrefixes("59.43.0.0/16"), SourceURL: "https://rdap.apnic.net/ip/59.43.0.0/16"},
	{Code: "CHINANET", LineKey: backtraceLineTelecomChinanet, Carrier: config.BacktraceCarrierTelecom, Quality: 10, CIDRs: routePrefixes("202.97.0.0/19"), SourceURL: "https://rdap.apnic.net/ip/202.97.0.0/19"},
	{Code: "CHINANET", LineKey: backtraceLineTelecomChinanet, Carrier: config.BacktraceCarrierTelecom, Quality: 10, CIDRs: routePrefixes("202.97.32.0/19"), SourceURL: "https://rdap.apnic.net/ip/202.97.32.0/19"},
	{Code: "CHINANET", LineKey: backtraceLineTelecomChinanet, Carrier: config.BacktraceCarrierTelecom, Quality: 10, CIDRs: routePrefixes("202.97.64.0/20"), SourceURL: "https://rdap.apnic.net/ip/202.97.64.0/20"},
	{Code: "CHINANET", LineKey: backtraceLineTelecomChinanet, Carrier: config.BacktraceCarrierTelecom, Quality: 10, CIDRs: routePrefixes("202.97.80.0/20"), SourceURL: "https://rdap.apnic.net/ip/202.97.80.0/20"},
	{Code: "UNICOM", LineKey: backtraceLineUnicomNetworkUnknown, Carrier: config.BacktraceCarrierUnicom, Quality: 20, CIDRs: routePrefixes("218.104.0.0/14"), SourceURL: "https://rdap.apnic.net/ip/218.104.0.0/14"},
	{Code: "UNICOM-BACKBONE", LineKey: backtraceLineUnicomBackbone, Carrier: config.BacktraceCarrierUnicom, Quality: 20, CIDRs: routePrefixes("219.158.0.0/19"), SourceURL: "https://rdap.apnic.net/ip/219.158.0.0/19"},
	{Code: "CMI", LineKey: backtraceLineMobileCMI, Carrier: config.BacktraceCarrierMobile, Quality: 30, CIDRs: routePrefixes("223.120.0.0/17"), SourceURL: "https://rdap.apnic.net/ip/223.120.0.0/17"},
	{Code: "CMNET", LineKey: backtraceLineMobileCMNET, Carrier: config.BacktraceCarrierMobile, Quality: 10, CIDRs: routePrefixes("221.176.0.0/13"), SourceURL: "https://rdap.apnic.net/ip/221.176.0.0/13"},

	// Exact IPv6 allocations. The former 2408:8120: and 2409: textual rules
	// were removed: APNIC records one 2408:8000::/20 China Unicom object and
	// the relevant China Mobile allocation as 2409:8000::/20.
	{Code: "CT-v6", LineKey: backtraceLineTelecomIPv6, Carrier: config.BacktraceCarrierTelecom, Quality: 10, CIDRs: routePrefixes("240e::/18"), SourceURL: "https://rdap.apnic.net/ip/240e::/18"},
	{Code: "UNICOM-v6", LineKey: backtraceLineUnicomIPv6, Carrier: config.BacktraceCarrierUnicom, Quality: 20, CIDRs: routePrefixes("2408:8000::/20"), SourceURL: "https://rdap.apnic.net/ip/2408:8000::/20"},
	{Code: "CMNET-v6", LineKey: backtraceLineMobileCMNETIPv6, Carrier: config.BacktraceCarrierMobile, Quality: 10, CIDRs: routePrefixes("2409:8000::/20"), SourceURL: "https://rdap.apnic.net/ip/2409:8000::/20"},
}

func routePrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(fmt.Sprintf("invalid route signature CIDR %q: %v", value, err))
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
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
	Source    signatureMatchSource
	ASN       string
	CIDR      string
}

// backtraceHop is a bounded presentation view generated from one canonical
// trace hop. It contains no inferred route classification; unknown facts stay
// empty until the renderer supplies the localized missing-value label.
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
	Trace            traceResult
	Hits             []backtraceHit
	Hops             []string
	Details          []backtraceHop
	RawStdout        string
	RawStderr        string
	NormalizedJSON   string
	Err              error
	ParseErr         error
	ParseFailed      bool
	NoResponsiveHops bool
}

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
	addComparisonParameter(result.Methodology.Parameters, "signature_set", "china-backbone-"+backtraceSignatureVersion)

	backend := detectTraceBackend(ctx)
	backendAvailable := traceBackendAvailableForFamily(backend, config.IPVersion4) || traceBackendAvailableForFamily(backend, config.IPVersion6)
	if !backendAvailable {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.tool_missing")}
		result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: backend.Name, Count: 1})
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
	backendForTarget := false
	for _, target := range targets {
		if traceBackendAvailableForFamily(backend, endpointFamily(target, env.Config.IPVersion)) {
			backendForTarget = true
			break
		}
	}
	if !backendForTarget {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.backtrace.summary.tool_missing")}
		result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: backend.Name, Count: 1})
		result.Evidence = model.NewEvidence(0, len(targets), "target")
		result.Notes = []string{"probe.backtrace.note.tool_missing"}
		result.Finish(start)
		return result
	}
	commandArguments := traceArgumentsForTargetsWithMaxHops(backend, targets, env.Config.IPVersion, backtraceMaxHops)
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.backtrace.field.engine", Value: model.RawValue(backend.Name)},
		{Key: "version", Label: "probe.backtrace.field.version", Value: model.RawValue(fallback(backend.Version, "unknown"))},
		{Key: "adapter", Label: "probe.backtrace.field.adapter", Value: model.RawValue(backend.Adapter)},
		{Key: "arguments", Label: "probe.backtrace.field.arguments", Value: model.RawValue(commandArguments)},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", fallback(backend.Version, "unknown"))
	addComparisonParameter(result.Methodology.Parameters, "adapter", backend.Adapter)
	addComparisonParameter(result.Methodology.Parameters, "arguments", commandArguments)

	rows := make([]backtraceRow, len(targets))
	semaphore := make(chan struct{}, backtraceConcurrency)
	var wg sync.WaitGroup
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target config.Endpoint) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			rows[index] = runBacktraceTarget(ctx, backend, target, endpointFamily(target, env.Config.IPVersion))
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
			{Key: "evidence", Label: "probe.backtrace.column.evidence"},
		},
	}
	identified := 0
	validTraces := 0
	failedTargets := 0
	parseFailed := false
	for _, row := range rows {
		// 原始路径无论识别成功与否都要保留：未识别时它恰恰是判断"线路确实没走
		// 已知网络/线路"还是"探测被限速打断"的唯一依据。
		if row.RawStdout != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    backtraceRawOutputTitle,
				Language: traceRawLanguage(backend),
				Content:  row.RawStdout,
			})
		}
		if row.RawStderr != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    backtraceRawStderrTitle,
				Language: "text",
				Content:  row.RawStderr,
			})
		}
		if row.NormalizedJSON != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    backtraceNormalizedJSONTitle,
				Language: "json",
				Content:  row.NormalizedJSON,
			})
		}
		if row.Err != nil {
			failedTargets++
			entry := failure.FromError("trace", row.Target.Address, row.Err)
			if row.ParseErr != nil {
				if entry.Category == model.FailureParse {
					entry.Category = model.FailureUnknown
				}
				entry.Message = fmt.Sprintf("%s; parser diagnostic: %s", row.Err, row.ParseErr)
			}
			result.AddFailure(entry)
			if countResponsiveTraceHops(row.Trace) == 0 {
				table.Rows = append(table.Rows, []model.Value{
					backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
					model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonTraceError), model.KeyValue(backtraceMissingValue),
				})
				continue
			}
			validTraces++
		} else if row.ParseFailed || row.ParseErr != nil {
			failedTargets++
			message := "trace output could not be parsed"
			if row.ParseErr != nil {
				message = row.ParseErr.Error()
			}
			result.AddFailure(model.Failure{Category: model.FailureParse, Stage: "parse", Target: row.Target.Address, Message: message, Count: 1})
			parseFailed = true
			table.Rows = append(table.Rows, []model.Value{
				backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
				model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonParseFailed), model.KeyValue(backtraceMissingValue),
			})
			continue
		} else if row.NoResponsiveHops {
			failedTargets++
			result.AddFailure(model.Failure{Category: model.FailureUnknown, Stage: "trace", Target: row.Target.Address, Count: 1})
			table.Rows = append(table.Rows, []model.Value{
				backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(backtraceMissingValue),
				model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceMissingValue), model.KeyValue(backtraceStatusFailed), model.KeyValue(backtraceReasonNoResponsiveHops), model.KeyValue(backtraceMissingValue),
			})
			continue
		} else {
			validTraces++
		}
		best, ok := bestBacktraceHit(row.Hits, row.Target.Kind)
		status := backtraceStatusUnidentified
		reason := backtraceUnidentifiedReason(row)
		line, hitHop, hitIP := backtraceMissingValue, backtraceMissingValue, backtraceMissingValue
		evidence := backtraceMissingValue
		if ok {
			identified++
			line = backtraceLineKey(best)
			hitHop = strconv.Itoa(best.Hop)
			hitIP = best.IP
			status = backtraceStatusIdentified
			reason = backtraceReasonSignatureMatch
			evidence = backtraceEvidenceKey(best.Source)
		}
		table.Rows = append(table.Rows, []model.Value{
			backtraceCarrierValue(row.Target.Kind), backtraceTargetValue(row.Target.Name), model.KeyValue(line), backtraceDataValue(hitHop), backtraceDataValue(hitIP), model.KeyValue(status), model.KeyValue(reason), model.KeyValue(evidence),
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
			Method:  "china-backbone-signature-" + backtraceSignatureVersion, HigherIsBetter: model.BoolPtr(true),
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
		traceBackendSource(backend),
		{Name: "probe.backtrace.source.method.name", URL: "https://github.com/zhanghanyun/backtrace", Purpose: "probe.backtrace.source.method"},
	}
	result.Notes = []string{"probe.backtrace.note.active_path", "probe.backtrace.note.signature_scope", "probe.backtrace.note.cn2_variant_inference", "probe.backtrace.note.ipv6_targets", "probe.backtrace.note.unidentified"}
	if parseFailed {
		result.Notes = append(result.Notes, "probe.backtrace.note.parse_failed")
	}
	result.Finish(start)
	return result
}

// runBacktraceTarget executes the selected trace backend once, then derives
// both the hop view and the independent classification evidence from the
// resulting canonical trace facts. It never parses a backend-native format.
func runBacktraceTarget(ctx context.Context, backend traceBackend, target config.Endpoint, family string) backtraceRow {
	row := backtraceRow{Target: target}
	traceCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	traceRun := runTraceCommandForFamily(traceCtx, backend, target.Address, backtraceMaxHops, family)
	row.RawStdout = sanitizeCommandOutput(traceRun.Stdout)
	row.RawStderr = sanitizeCommandOutput(traceRun.Stderr)
	row.Trace = traceRun.Trace
	row.Err = traceRun.Err
	row.ParseErr = traceRun.ParseErr
	if cause := contextCauseError(traceCtx); cause != nil {
		row.Err = cause
		return row
	}
	if !traceRun.Parsed {
		row.ParseFailed = row.Err == nil && row.ParseErr != nil
		return row
	}
	normalized, err := row.Trace.canonicalJSON()
	if err != nil {
		row.ParseErr = fmt.Errorf("canonical trace JSON: %w", err)
		if row.Err == nil {
			row.ParseFailed = true
		}
		return row
	}
	row.NormalizedJSON = string(normalized)
	row.Details = backtraceDetailsFromTrace(row.Trace)
	row.Hops = traceAddresses(row.Trace)
	// Classification is a separate consumer of canonical facts. It returns
	// evidence only; it cannot mutate row.Trace or the presentation details.
	row.Hits = matchTraceSignatures(row.Trace)
	if countResponsiveTraceHops(row.Trace) == 0 {
		if row.Err == nil {
			row.NoResponsiveHops = true
		}
	}
	return row
}

func backtraceDetailsFromTrace(trace traceResult) []backtraceHop {
	details := make([]backtraceHop, 0, len(trace.Hops))
	for _, hop := range trace.Hops {
		detail := backtraceHop{
			Hop:      hop.Hop,
			IP:       hop.IP,
			ASN:      hop.ASN,
			Network:  hop.Network,
			Location: hop.Location,
			Status:   "probe.backtrace.hop.no_response",
		}
		if hop.RTTMS != nil {
			detail.Latency = strconv.FormatFloat(*hop.RTTMS, 'f', -1, 64) + " ms"
		}
		if hop.Responded {
			detail.Status = "probe.backtrace.hop.responded"
		}
		details = append(details, detail)
	}
	return details
}

func traceAddresses(trace traceResult) []string {
	hops := make([]string, len(trace.Hops))
	for index, hop := range trace.Hops {
		if hop.Responded {
			hops[index] = hop.IP
		}
	}
	return hops
}

func countResponsiveTraceHops(trace traceResult) int {
	count := 0
	for _, hop := range trace.Hops {
		if hop.Responded {
			count++
		}
	}
	return count
}

func matchTraceSignatures(trace traceResult) []backtraceHit {
	return matchTraceSignaturesWith(trace, chinaRouteSignatures)
}

// matchTraceSignaturesWith encodes the evidence hierarchy explicitly:
// canonical hop ASN, then exact CIDR, then an explicitly low-confidence
// heuristic indicator. Once a higher tier matches a hop, lower tiers are not
// allowed to override it. The returned hits are evidence only; this function
// never changes the canonical trace or its presentation view.
func matchTraceSignaturesWith(trace traceResult, signatures []routeSignature) []backtraceHit {
	var hits []backtraceHit
	for _, hop := range trace.Hops {
		if !hop.Responded || strings.TrimSpace(hop.IP) == "" {
			continue
		}
		if strings.TrimSpace(hop.ASN) != "" {
			// An observed but unrecognized ASN is still stronger evidence than an
			// address allocation. Do not let a stale CIDR rule contradict it.
			hits = append(hits, matchTraceSignaturesAtTier(hop, signatures, signatureMatchByASN)...)
			continue
		}
		if matched := matchTraceSignaturesAtTier(hop, signatures, signatureMatchByCIDR); len(matched) > 0 {
			hits = append(hits, matched...)
			continue
		}
		hits = append(hits, matchTraceSignaturesAtTier(hop, signatures, signatureMatchByHeuristic)...)
	}
	return hits
}

func matchTraceSignaturesAtTier(hop traceHop, signatures []routeSignature, source signatureMatchSource) []backtraceHit {
	var address netip.Addr
	if source != signatureMatchByASN {
		parsed, err := netip.ParseAddr(strings.TrimSpace(hop.IP))
		if err != nil {
			return nil
		}
		address = parsed
	}
	normalizedASN := normalizeRouteASN(hop.ASN)
	var hits []backtraceHit
	for _, signature := range signatures {
		matched := false
		matchedCIDR := ""
		switch source {
		case signatureMatchByASN:
			matched = normalizedASN != "" && routeSignatureHasASN(signature, normalizedASN)
		case signatureMatchByCIDR:
			for _, prefix := range signature.CIDRs {
				if prefix.Contains(address) {
					matched = true
					matchedCIDR = prefix.String()
					break
				}
			}
		case signatureMatchByHeuristic:
			for _, prefix := range signature.HeuristicPrefixes {
				if strings.HasPrefix(strings.ToLower(hop.IP), strings.ToLower(prefix)) {
					matched = true
					break
				}
			}
		}
		if matched {
			hits = append(hits, backtraceHit{Signature: signature, Hop: hop.Hop, IP: hop.IP, Source: source, ASN: normalizedASN, CIDR: matchedCIDR})
		}
	}
	return hits
}

func routeSignatureHasASN(signature routeSignature, asn string) bool {
	for _, candidate := range signature.ASNs {
		if normalizeRouteASN(candidate) == asn {
			return true
		}
	}
	return false
}

func normalizeRouteASN(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToUpper(value), "AS") {
		return "AS" + strings.TrimSpace(value[2:])
	}
	if number, err := strconv.ParseUint(value, 10, 32); err == nil && number > 0 {
		return "AS" + strconv.FormatUint(number, 10)
	}
	return strings.ToUpper(value)
}

func backtraceEvidenceKey(source signatureMatchSource) string {
	switch source {
	case signatureMatchByASN:
		return "probe.backtrace.evidence.asn"
	case signatureMatchByCIDR:
		return "probe.backtrace.evidence.cidr"
	case signatureMatchByHeuristic:
		return "probe.backtrace.evidence.heuristic"
	default:
		return backtraceMissingValue
	}
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
	case backtraceLineTelecomCN2VariantUnknown, backtraceLineTelecomChinanet,
		backtraceLineUnicomNetworkUnknown, backtraceLineUnicomBackbone,
		backtraceLineUnicomCUII, backtraceLineUnicom169,
		backtraceLineMobileCMI, backtraceLineMobileNetworkUnknown, backtraceLineMobileCMNET,
		backtraceLineTelecomIPv6, backtraceLineUnicomIPv6, backtraceLineMobileCMNETIPv6:
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

// bestBacktraceHit 在目标运营商自己的命中里挑出代表本次线路的那一条。
//
// 同一路径上同时出现 CHINANET 与 CN2 是常见情况（先经骨干再进 CN2），此时应以
// 更高规则优先级的线路作为结论，优先级相同则取更靠前的跳；证据层级则始终先比较 ASN、
// 再比较精确 CIDR、最后才比较明确标注的低置信启发式指标。异网骨干可以保留为
// 证据，但不能被当作这个参考目标的运营商结论。
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
		if signatureMatchPriority(sorted[i].Source) != signatureMatchPriority(sorted[j].Source) {
			return signatureMatchPriority(sorted[i].Source) > signatureMatchPriority(sorted[j].Source)
		}
		if sorted[i].Signature.Quality != sorted[j].Signature.Quality {
			return sorted[i].Signature.Quality > sorted[j].Signature.Quality
		}
		return sorted[i].Hop < sorted[j].Hop
	})
	return sorted[0], true
}

func signatureMatchPriority(source signatureMatchSource) int {
	switch source {
	case signatureMatchByASN:
		return 3
	case signatureMatchByCIDR:
		return 2
	case signatureMatchByHeuristic:
		return 1
	default:
		return 0
	}
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

// backtraceLineKey returns the line key carried by the selected, source-backed
// classification evidence. CN2 deliberately carries the variant-unknown key;
// no path-order heuristic may turn it into GIA or GT.
func backtraceLineKey(best backtraceHit) string {
	return best.Signature.LineKey
}

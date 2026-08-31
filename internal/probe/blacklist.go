package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

// DNS 黑名单（DNSBL / RBL）查询。
//
// 这是判断一台机器能否用来发信、以及它的 IP 是否有历史劣迹的直接证据：
// 被主流黑名单收录的 IP 发出的邮件大概率进垃圾箱甚至被直接拒收。
//
// 实现是标准的 DNSBL 协议：把 IPv4 四段反转后拼上黑名单域名查 A 记录，
// 有记录即为收录，返回的 127.0.0.x 子码表示收录原因。零第三方依赖。
//
// 两个只有实测才会发现的坑，直接决定判定正确性：
//
//  1. **拒绝码不是命中**。Spamhaus 禁止经公共解析器查询，此时返回 127.255.255.254
//     而不是无记录。实测经 1.1.1.1 查 zen.spamhaus.org 必得该码——若把任何 127.x
//     都算作"已收录"，用 Cloudflare DNS 的机器会永远被误报为进了 Spamhaus。
//     因此 127.255.255.0/24 一律判为"查询被拒绝"，结论存疑而非命中。
//  2. **有些区域对干净地址也返回记录**。实测 hostkarma.junkemailfilter.com 对
//     127.0.0.1 同样有应答——它是 karma/白名单系统，语义与黑名单相反，
//     收进来必然误报，因此不列入清单。

type blacklistProbe struct{}

func (blacklistProbe) ID() string { return "blacklist" }

func newBlacklistResult() model.Result {
	result := model.NewResult("blacklist", "module.blacklist.title")
	result.Description = "probe.blacklist.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "DNSBL over DNS A lookup",
		Profile:         "probe.blacklist.profile",
		ComparisonScope: "probe.blacklist.comparison_scope",
	}
	return result
}

// dnsblZone 是一个黑名单区域。
type dnsblZone struct {
	Zone string
	Name string
	// Purpose 说明这个名单收录什么，用于解释命中意味着什么。
	Purpose string
}

// dnsblZones 是实测可用的黑名单清单。
//
// 2026-08-01 逐个用 DNSBL 规范约定的测试地址验证：127.0.0.2 必须被判为收录、
// 127.0.0.1 必须无记录。未通过的一律不收录，清单不凭记忆填写：
//
//	dnsbl.sorbs.net / spam.dnsbl.sorbs.net  DNS 已无记录（SORBS 已停止服务）
//	db.wpbl.info / ubl.unsubscore.com       解析失败
//	hostkarma.junkemailfilter.com           对干净地址也返回记录，语义不符
func dnsblZones() []dnsblZone {
	return []dnsblZone{
		{Zone: "zen.spamhaus.org", Name: "Spamhaus ZEN", Purpose: "综合名单，邮件服务商采用最广"},
		{Zone: "bl.spamcop.net", Name: "SpamCop", Purpose: "基于用户举报的垃圾邮件源"},
		{Zone: "b.barracudacentral.org", Name: "Barracuda", Purpose: "梭子鱼信誉库"},
		{Zone: "cbl.abuseat.org", Name: "CBL/abuseat", Purpose: "被感染主机与僵尸网络出口"},
		{Zone: "psbl.surriel.com", Name: "PSBL", Purpose: "被动式垃圾邮件蜜罐"},
		{Zone: "bl.blocklist.de", Name: "blocklist.de", Purpose: "被举报的攻击源（SSH/邮件爆破等）"},
		{Zone: "dnsbl-1.uceprotect.net", Name: "UCEPROTECT L1", Purpose: "单个 IP 的发信滥用"},
		{Zone: "dnsbl-2.uceprotect.net", Name: "UCEPROTECT L2", Purpose: "整段/ASN 连带收录，常误伤邻居"},
		{Zone: "dnsbl.dronebl.org", Name: "DroneBL", Purpose: "开放代理与被控主机"},
		{Zone: "all.s5h.net", Name: "s5h", Purpose: "综合滥用名单"},
		{Zone: "spam.spamrats.com", Name: "SpamRats SPAM", Purpose: "确认的垃圾邮件行为"},
		{Zone: "noptr.spamrats.com", Name: "SpamRats NoPtr", Purpose: "缺少反向解析的发信主机"},
		{Zone: "dyna.spamrats.com", Name: "SpamRats Dyna", Purpose: "疑似动态住宅地址段"},
		{Zone: "truncate.gbudb.net", Name: "GBUdb Truncate", Purpose: "只发垃圾邮件的来源"},
		{Zone: "z.mailspike.net", Name: "Mailspike Z", Purpose: "垃圾邮件发送信誉"},
		{Zone: "bl.mailspike.net", Name: "Mailspike BL", Purpose: "综合黑名单"},
		{Zone: "ips.backscatterer.org", Name: "Backscatterer", Purpose: "回退散射（伪造退信）来源"},
	}
}

// dnsblOutcome 是一次黑名单查询的结论。
type dnsblOutcome string

const (
	dnsblListed  dnsblOutcome = "已收录"
	dnsblClean   dnsblOutcome = "未收录"
	dnsblRefused dnsblOutcome = "查询被拒"
	dnsblFailed  dnsblOutcome = "查询失败"
)

// dnsblFinding 是单个黑名单的查询结果。
type dnsblFinding struct {
	Zone     dnsblZone
	Outcome  dnsblOutcome
	Codes    []string
	Detail   string
	Duration time.Duration
}

// reverseIPv4 把 IPv4 反转成 DNSBL 查询用的前缀。
func reverseIPv4(ip net.IP) (string, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return "", false
	}
	return fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0]), true
}

// classifyDNSBLCodes 判断一组返回地址代表什么。
//
// 127.255.255.0/24 是各家用来表达"查询本身被拒绝"的保留段（Spamhaus 的
// 127.255.255.252/254/255 分别对应格式错误、来自公共解析器、超出查询配额）。
// 这类应答绝不能算作命中，否则会把解析器配置问题误报成 IP 有劣迹。
func classifyDNSBLCodes(addresses []string) (dnsblOutcome, string) {
	if len(addresses) == 0 {
		return dnsblClean, ""
	}
	refused := 0
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		if v4[0] == 127 && v4[1] == 255 && v4[2] == 255 {
			refused++
		}
	}
	if refused == len(addresses) {
		return dnsblRefused, "返回 127.255.255.x：查询被拒绝（多为使用了公共解析器或超出配额），不代表该 IP 被收录"
	}
	return dnsblListed, ""
}

// queryDNSBL 查询单个黑名单。
//
// 刻意使用系统默认解析器：Spamhaus 等名单会拒绝来自公共解析器的查询，
// 而 VPS 自带的解析器通常是被允许的。
func queryDNSBL(ctx context.Context, resolver *net.Resolver, prefix string, zone dnsblZone) dnsblFinding {
	finding := dnsblFinding{Zone: zone}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	start := time.Now()
	addresses, err := resolver.LookupHost(queryCtx, prefix+"."+zone.Zone)
	finding.Duration = time.Since(start)
	if err != nil {
		var dnsErr *net.DNSError
		if ok := asDNSError(err, &dnsErr); ok && dnsErr.IsNotFound {
			// 无记录正是"未被收录"的标准表达。
			finding.Outcome = dnsblClean
			return finding
		}
		finding.Outcome = dnsblFailed
		finding.Detail = compactError(err)
		return finding
	}
	finding.Codes = addresses
	finding.Outcome, finding.Detail = classifyDNSBLCodes(addresses)
	return finding
}

var (
	dnsblQueryForProbe       = queryDNSBL
	appendReverseDNSForProbe = appendReverseDNS
)

// asDNSError 在不引入额外依赖的前提下做一次类型断言。
func asDNSError(err error, target **net.DNSError) bool {
	if dnsErr, ok := err.(*net.DNSError); ok {
		*target = dnsErr
		return true
	}
	return false
}

func (blacklistProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := newBlacklistResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameter(result.Methodology.Parameters, "zone_set", "dnsbl-zones-v1")
	zones := dnsblZones()
	if env.Config.IPVersion == config.IPVersion6 {
		result.Skip(model.NewMessage("probe.blacklist.summary.skipped"))
		result.Evidence = model.NewEvidence(0, len(zones), "query")
		result.Notes = blacklistNotes()
		result.Finish(start)
		return result
	}

	egressIP, err := env.Egress.IPFor(config.IPVersion4)
	if err != nil {
		result.Status = model.StatusError
		addFailure(&result, "egress", "IPv4", err)
		result.Notes = blacklistNotes()
		result.Evidence = model.NewEvidence(0, len(zones), "query")
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.unavailable")}
		result.Finish(start)
		return result
	}
	ip := net.ParseIP(egressIP)
	prefix, ok := reverseIPv4(ip)
	if !ok {
		result.Status = model.StatusError
		result.AddFailure(model.Failure{
			Category: model.FailureParse, Stage: "validate", Target: "IPv4", Count: 1,
			Message: "egress address is not a valid IPv4 address",
		})
		result.Notes = blacklistNotes()
		result.Evidence = model.NewEvidence(0, len(zones), "query")
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.unavailable")}
		result.Finish(start)
		return result
	}

	findings := make([]dnsblFinding, len(zones))
	// 限制并发：DNSBL 对突发查询敏感，过快会被判为滥用而拒绝服务。
	semaphore := make(chan struct{}, 6)
	resolver := &net.Resolver{}
	var wg sync.WaitGroup
	for index, zone := range zones {
		wg.Add(1)
		go func(index int, zone dnsblZone) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			findings[index] = dnsblQueryForProbe(ctx, resolver, prefix, zone)
		}(index, zone)
	}
	wg.Wait()

	table := model.Table{
		Key:   "network.dnsbl.results",
		Title: "probe.blacklist.table.results",
		Columns: []model.TableColumn{
			{Key: "list", Label: "probe.blacklist.column.list"},
			{Key: "outcome", Label: "probe.blacklist.column.outcome"},
			{Key: "response_code", Label: "probe.blacklist.column.code"},
			{Key: "scope", Label: "probe.blacklist.column.scope"},
			{Key: "latency_ms", Label: "probe.blacklist.column.latency", Numeric: true, HigherIsBetter: false},
		},
		RowIdentity: "list",
	}
	listed, refused, failed := 0, 0, 0
	for _, finding := range findings {
		switch finding.Outcome {
		case dnsblListed:
			listed++
		case dnsblRefused:
			refused++
			result.AddFailure(model.Failure{
				Category: model.FailureDNS, Stage: "dnsbl_query", Target: finding.Zone.Zone,
				Retryable: false, Count: 1, Message: finding.Detail,
			})
		case dnsblFailed:
			failed++
			addFailureMessage(&result, "dnsbl_query", finding.Zone.Zone, finding.Detail)
		}
	}
	orderedFindings := append([]dnsblFinding(nil), findings...)
	sort.SliceStable(orderedFindings, func(i, j int) bool {
		// Sort from the structured DNSBL outcome, before it is converted to a
		// localized table cell.
		return dnsblOutcomeRank(orderedFindings[i].Outcome) < dnsblOutcomeRank(orderedFindings[j].Outcome)
	})
	for _, finding := range orderedFindings {
		codes := strings.Join(finding.Codes, ", ")
		if codes == "" {
			codes = "—"
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue(finding.Zone.Name),
			dnsblOutcomeValue(finding.Outcome),
			model.RawValue(codes),
			model.RawValue(finding.Zone.Purpose),
			model.RawValue(formatMilliseconds(finding.Duration)),
		})
	}
	result.Tables = []model.Table{table}

	result.Fields = []model.Field{
		{Key: "queried_ip", Label: "probe.blacklist.field.queried_ip", Value: model.RawValue(egressIP), Sensitive: true},
		{Key: "zones_total", Label: "probe.blacklist.field.zones_total", Value: model.RawValue(fmt.Sprintf("%d", len(zones)))},
		{Key: "resolver", Label: "probe.blacklist.field.resolver", Value: model.RawValue("系统默认（部分名单拒绝公共解析器查询）")},
	}
	clean := len(zones) - listed - refused - failed
	result.Measurements = dnsblCountMeasurements(listed, clean, refused, failed, len(zones))
	result.Evidence = model.NewEvidence(listed+clean, len(zones), "query")
	result.Sources = []model.Source{
		{Name: "Spamhaus", URL: "https://www.spamhaus.org/", Purpose: "probe.blacklist.source.list"},
		{Name: "SpamCop", URL: "https://www.spamcop.net/", Purpose: "probe.blacklist.source.list"},
		{Name: "Barracuda", URL: "https://www.barracudacentral.org/", Purpose: "probe.blacklist.source.list"},
	}
	result.Notes = blacklistNotes()
	appendReverseDNSForProbe(ctx, &result, egressIP)

	validOutcomes := listed + clean
	queryFailures := refused + failed
	switch {
	case len(zones) > 0 && validOutcomes == 0:
		result.Status = model.StatusError
	case listed > 0 || queryFailures > 0:
		result.Status = model.StatusWarning
	}
	switch {
	case listed > 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.listed", listed, clean+listed+refused+failed)}
	case refused > 0 && clean == 0 && failed == 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.refused")}
	default:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.blacklist.summary.clean", clean)}
	}
	result.Finish(start)
	return result
}

func blacklistNotes() []string {
	return []string{
		"probe.blacklist.note.query_scope",
		"probe.blacklist.note.list_semantics",
		"probe.blacklist.note.refused_code",
	}
}

func dnsblOutcomeValue(outcome dnsblOutcome) model.Value {
	switch outcome {
	case dnsblListed:
		return model.KeyValue("probe.blacklist.outcome.listed")
	case dnsblClean:
		return model.KeyValue("probe.blacklist.outcome.clean")
	case dnsblRefused:
		return model.KeyValue("probe.blacklist.outcome.refused")
	case dnsblFailed:
		return model.KeyValue("probe.blacklist.outcome.failed")
	default:
		return model.RawValue(string(outcome))
	}
}

func dnsblCountMeasurements(listed, clean, refused, failed, total int) []model.Measurement {
	return []model.Measurement{
		{
			Key: "dnsbl_listed_count", Label: "probe.blacklist.metric.dnsbl_listed_count",
			Value: float64(listed), Unit: "项", Display: model.RawValue(fmt.Sprintf("%d/%d", listed, total)),
			Method: "dnsbl-a-lookup-v1", HigherIsBetter: model.BoolPtr(false),
		},
		{
			Key: "dnsbl_clean_count", Label: "probe.blacklist.metric.dnsbl_clean_count",
			Value: float64(clean), Unit: "项", Display: model.RawValue(fmt.Sprintf("%d/%d", clean, total)),
			Method: "dnsbl-a-lookup-v1", HigherIsBetter: model.BoolPtr(true),
		},
		{
			Key: "dnsbl_refused_count", Label: "probe.blacklist.metric.dnsbl_refused_count",
			Value: float64(refused), Unit: "项", Display: model.RawValue(fmt.Sprintf("%d/%d", refused, total)),
			Method: "dnsbl-a-lookup-v1", HigherIsBetter: model.BoolPtr(false),
		},
		{
			Key: "dnsbl_failed_count", Label: "probe.blacklist.metric.dnsbl_failed_count",
			Value: float64(failed), Unit: "项", Display: model.RawValue(fmt.Sprintf("%d/%d", failed, total)),
			Method: "dnsbl-a-lookup-v1", HigherIsBetter: model.BoolPtr(false),
		},
	}
}

func dnsblOutcomeRank(outcome dnsblOutcome) int {
	switch outcome {
	case dnsblListed:
		return 0
	case dnsblRefused:
		return 1
	case dnsblFailed:
		return 2
	default:
		return 3
	}
}

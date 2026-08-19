package probe

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"ecs/internal/model"
)

// 反向解析与 FCrDNS 校验。
//
// 单纯"反查出 PTR 就说明是家宽"这类模式匹配价值有限——实测中 DigitalOcean 的
// 出口和中国电信家宽出口都根本没有 PTR 记录。真正有判定力的是 FCrDNS
// （Forward-Confirmed reverse DNS）：PTR 存在，且把 PTR 指向的域名再正向解析
// 回来能得到原 IP。
//
// 这是主流邮件服务商的硬性要求：没有 PTR，或正反解对不上，发出的邮件大概率被
// 直接拒收或判为垃圾邮件。它和 DNSBL 是同一个问题的两面，因此放在同一个模块。

// rdnsResult 是一次反解校验的结果。
type rdnsResult struct {
	IP        string
	Names     []string
	Forward   []string
	Confirmed bool
	Err       error
	Hints     []string
}

// residentialPTRHints 是家宽/动态地址在 PTR 里的常见特征词。
//
// 命中只能作为线索：机房也可能用这些词命名，家宽也可能没有 PTR。
// 因此结论只写"疑似"，绝不据此断言线路类型。
var residentialPTRHints = []string{
	"dsl", "adsl", "pppoe", "ppp", "dynamic", "dyn", "dial",
	"broadband", "cable", "pool", "dhcp", "client", "customer",
	"res", "home", "user",
}

// checkReverseDNS 反查 IP 并做正向确认。
func checkReverseDNS(ctx context.Context, resolver *net.Resolver, ip string) rdnsResult {
	result := rdnsResult{IP: ip}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	names, err := resolver.LookupAddr(lookupCtx, ip)
	if err != nil || len(names) == 0 {
		result.Err = err
		return result
	}
	result.Names = names

	// 正向确认：PTR 指向的域名必须能解析回同一个 IP，否则任何人都能把 PTR
	// 指向别人的域名来伪装身份。
	for _, name := range names {
		host := strings.TrimSuffix(name, ".")
		addresses, err := resolver.LookupHost(lookupCtx, host)
		if err != nil {
			continue
		}
		result.Forward = append(result.Forward, addresses...)
		for _, address := range addresses {
			if address == ip {
				result.Confirmed = true
			}
		}
	}

	result.Hints = matchResidentialHints(strings.Join(names, " "))
	return result
}

// matchResidentialHints 在主机名里找家宽/动态地址的命名特征。
//
// 必须按分隔符切成 token 后精确匹配，不能用子串包含：实测 "server1.resources
// .example.com" 会被 ".res" 命中而误判成家宽。宁可漏判也不能误判——
// 这个结论会影响用户对线路类型的判断。
func matchResidentialHints(names string) []string {
	tokens := strings.FieldsFunc(strings.ToLower(names), func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == ' ' || r == ','
	})
	present := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		present[token] = true
	}
	var hits []string
	for _, hint := range residentialPTRHints {
		if present[hint] {
			hits = append(hits, hint)
		}
	}
	return hits
}

// appendReverseDNS 在黑名单结果上追加反解与 FCrDNS 判定。
func appendReverseDNS(ctx context.Context, result *model.Result, ip string) {
	resolver := &net.Resolver{}
	check := checkReverseDNS(ctx, resolver, ip)

	table := model.Table{
		Key:         "network.reverse_dns.checks",
		Title:       "反向解析与 FCrDNS",
		Columns:     []string{"项目", "结果", "说明"},
		ColumnKeys:  []string{"item", "result", "description"},
		RowIdentity: "item",
	}
	ptrValue := "无 PTR 记录"
	if len(check.Names) > 0 {
		ptrValue = strings.Join(check.Names, ", ")
	}
	table.Rows = append(table.Rows, []string{
		"PTR 记录", ptrValue, "邮件服务商普遍要求发信 IP 具备 PTR",
	})

	fcrdns := "未通过"
	fcrdnsWhy := "PTR 缺失或正向解析回不到本 IP"
	switch {
	case check.Confirmed:
		fcrdns = "通过"
		fcrdnsWhy = "PTR 指向的域名能正向解析回本 IP"
	case len(check.Names) > 0:
		fcrdnsWhy = "PTR 存在但正向解析对不上——任何人都能把 PTR 指向别人的域名，因此必须正向确认"
	}
	table.Rows = append(table.Rows, []string{"FCrDNS 正反一致", fcrdns, fcrdnsWhy})

	if len(check.Hints) > 0 {
		table.Rows = append(table.Rows, []string{
			"命名特征", strings.Join(check.Hints, ", "),
			"疑似家宽/动态地址的命名模式；仅为线索，机房也可能这样命名",
		})
	}
	result.Tables = append(result.Tables, table)

	result.Fields = append(result.Fields,
		model.Field{Key: "ptr_record", Label: "PTR 记录", Value: ptrValue},
		model.Field{Key: "fcrdns", Label: "FCrDNS", Value: fcrdns},
	)
	result.Measurements = append(result.Measurements, model.Measurement{
		Key: "fcrdns_passed", Label: "FCrDNS 通过",
		Value: boolValue(check.Confirmed), Unit: "项", Display: fcrdns,
		Method: "reverse-dns-forward-confirm-v1", HigherIsBetter: model.BoolPtr(true),
	})

	switch {
	case len(check.Names) == 0:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes,
			"该 IP 没有 PTR 记录。主流邮件服务商会因此直接拒收或判为垃圾邮件；"+
				"若要用这台机器发信，需要让服务商为该 IP 配置反向解析。"+
				"（注意：很多云厂商的默认出口都没有 PTR，这与 IP 是否干净无关。）")
	case !check.Confirmed:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes,
			"PTR 存在但正向解析回不到本 IP（FCrDNS 未通过）。PTR 由 IP 持有者单方面设置，"+
				"不做正向确认就可以指向任意域名，因此邮件服务商普遍要求正反一致。")
	default:
		result.Notes = append(result.Notes,
			"FCrDNS 正反一致，满足主流邮件服务商对发信 IP 的基本要求；"+
				"能否送达还取决于黑名单收录情况、SPF/DKIM/DMARC 与发信内容。")
	}
	if len(check.Hints) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"PTR 里出现 %s 等命名，是家宽或动态地址的常见特征；许多邮件服务商对这类地址段"+
				"有额外限制。这只是线索——机房也可能这样命名，不能据此断定线路类型。",
			strings.Join(check.Hints, "、")))
	}
}

// boolValue 把布尔转成指标数值。
func boolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

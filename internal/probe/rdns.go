package probe

import (
	"context"
	"errors"
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

type rdnsResult struct {
	IP         string
	Names      []string
	Forward    []string
	Confirmed  bool
	ReverseErr error
	ForwardErr error
	Hints      []string
}

type rdnsResolver interface {
	LookupAddr(context.Context, string) ([]string, error)
	LookupHost(context.Context, string) ([]string, error)
}

var residentialPTRHints = []string{
	"dsl", "adsl", "pppoe", "ppp", "dynamic", "dyn", "dial",
	"broadband", "cable", "pool", "dhcp", "client", "customer",
	"res", "home", "user",
}

func checkReverseDNS(ctx context.Context, resolver rdnsResolver, ip string) rdnsResult {
	result := rdnsResult{IP: ip}
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	names, err := resolver.LookupAddr(lookupCtx, ip)
	if err != nil {
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
			result.ReverseErr = err
		}
		return result
	}
	if len(names) == 0 {
		return result
	}
	result.Names = names

	var forwardErr error
	forwardSucceeded := false
	for _, name := range names {
		host := strings.TrimSuffix(name, ".")
		addresses, err := resolver.LookupHost(lookupCtx, host)
		if err != nil {
			if forwardErr == nil {
				forwardErr = err
			}
			continue
		}
		forwardSucceeded = true
		result.Forward = append(result.Forward, addresses...)
		for _, address := range addresses {
			if address == ip {
				result.Confirmed = true
			}
		}
	}
	if !forwardSucceeded {
		result.ForwardErr = forwardErr
	}

	result.Hints = matchResidentialHints(strings.Join(names, " "))
	return result
}

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

func appendReverseDNS(ctx context.Context, result *model.Result, ip string) {
	resolver := &net.Resolver{}
	check := checkReverseDNS(ctx, resolver, ip)
	appendReverseDNSResult(result, check)
}

// appendReverseDNSResult records presentation identities as stable keys. Raw
// resolver diagnostics remain in Failure.Message and are never translated.
func appendReverseDNSResult(result *model.Result, check rdnsResult) {
	table := model.Table{
		Key:   "network.reverse_dns.checks",
		Title: "probe.rdns.table.title",
		Columns: []string{
			"probe.rdns.column.item",
			"probe.rdns.column.result",
			"probe.rdns.column.description",
		},
		ColumnKeys:  []string{"item", "result", "description"},
		RowIdentity: "item",
	}

	ptrValue := "probe.rdns.ptr.none"
	if check.ReverseErr != nil {
		ptrValue = "probe.rdns.ptr.query_failed"
	} else if len(check.Names) > 0 {
		ptrValue = strings.Join(check.Names, ", ")
	}
	table.Rows = append(table.Rows, []string{
		"probe.rdns.item.ptr", ptrValue, "probe.rdns.description.ptr",
	})

	fcrdns := "probe.rdns.status.failed"
	fcrdnsWhy := "probe.rdns.description.fcrdns.missing_or_mismatch"
	switch {
	case check.Confirmed:
		fcrdns = "probe.rdns.status.passed"
		fcrdnsWhy = "probe.rdns.description.fcrdns.confirmed"
	case check.ReverseErr != nil:
		fcrdnsWhy = "probe.rdns.description.fcrdns.reverse_failed"
	case check.ForwardErr != nil:
		fcrdnsWhy = "probe.rdns.description.fcrdns.forward_failed"
	case len(check.Names) > 0:
		fcrdnsWhy = "probe.rdns.description.fcrdns.mismatch"
	}
	table.Rows = append(table.Rows, []string{"probe.rdns.item.fcrdns", fcrdns, fcrdnsWhy})

	if len(check.Hints) > 0 {
		table.Rows = append(table.Rows, []string{
			"probe.rdns.item.naming_hints", strings.Join(check.Hints, ", "), "probe.rdns.description.naming_hints",
		})
	}
	result.Tables = append(result.Tables, table)

	result.Fields = append(result.Fields,
		model.Field{Key: "ptr_record", Label: "probe.rdns.field.ptr", Value: ptrValue},
		model.Field{Key: "fcrdns", Label: "probe.rdns.field.fcrdns", Value: fcrdns},
	)
	if len(check.Hints) > 0 {
		result.Fields = append(result.Fields, model.Field{Key: "ptr_naming_hints", Label: "probe.rdns.field.naming_hints", Value: strings.Join(check.Hints, ",")})
	}
	result.Measurements = append(result.Measurements, model.Measurement{
		Key: "fcrdns_passed", Label: "probe.rdns.metric.fcrdns_passed",
		Value: boolValue(check.Confirmed), Unit: "boolean", Display: fcrdns,
		Method: "reverse-dns-forward-confirm-v1", HigherIsBetter: model.BoolPtr(true),
	})
	if check.ReverseErr != nil {
		result.AddFailure(model.Failure{Category: model.FailureDNS, Stage: "reverse_lookup", Target: check.IP, Count: 1, Message: compactError(check.ReverseErr)})
	}
	if check.ForwardErr != nil {
		result.AddFailure(model.Failure{Category: model.FailureDNS, Stage: "forward_confirmation", Target: check.IP, Count: 1, Message: compactError(check.ForwardErr)})
	}

	switch {
	case check.ReverseErr != nil:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.rdns.note.reverse_failed")
	case len(check.Names) == 0:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.rdns.note.no_ptr")
	case check.ForwardErr != nil:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.rdns.note.forward_failed")
	case !check.Confirmed:
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "probe.rdns.note.mismatch")
	default:
		result.Notes = append(result.Notes, "probe.rdns.note.confirmed")
	}
	if len(check.Hints) > 0 {
		result.Notes = append(result.Notes, "probe.rdns.note.hints")
	}
}

func boolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

package probe

import (
	"context"
	"net"
	"strings"

	"ecs/internal/config"
	"ecs/internal/model"
)

type dnsSemanticProbe struct{}

func (dnsSemanticProbe) ID() string         { return "dns" }
func (dnsSemanticProbe) Title() string      { return "module.dns.title" }
func (dnsSemanticProbe) NeedsNetwork() bool { return true }

func (dnsSemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (dnsProbe{}).Run(ctx, env)
	stabilizeDNSResult(&result)
	return result
}

func stabilizeDNSResult(result *model.Result) {
	if result == nil {
		return
	}
	result.Title = "module.dns.title"
	result.Description = "probe.dns.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.dns.profile"
	result.Methodology.ComparisonScope = "probe.dns.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.dns.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		if measurement.Key == "best_dns_median_ms" {
			measurement.Label = "probe.dns.metric.best_median"
		} else {
			measurement.Label = "probe.dns.metric.resolver"
		}
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.dns.resolvers" {
			continue
		}
		table.Title = "probe.dns.table.resolvers"
		table.Columns = []string{
			"probe.dns.column.resolver", "probe.dns.column.address", "probe.dns.column.success",
			"probe.dns.column.p50", "probe.dns.column.p95", "probe.dns.column.jitter", "probe.dns.column.status",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			switch row[len(row)-1] {
			case dnsStatusOK:
				row[len(row)-1] = "probe.dns.status.ok"
			case dnsStatusFailed:
				row[len(row)-1] = "probe.dns.status.failed"
			case dnsStatusPartial:
				row[len(row)-1] = "probe.dns.status.partial"
			}
		}
	}
	result.Notes = []string{"probe.dns.note.warmup", "probe.dns.note.udp_scope"}
	result.Summary = ""
	switch {
	case result.Status == model.StatusSkipped:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.dns.summary.skipped")}
	case result.Evidence != nil && result.Evidence.Valid == 0 && result.Evidence.Expected > 0:
		result.Status = model.StatusWarning
		result.SummaryMessages = []model.Message{model.NewMessage("probe.dns.summary.all_failed")}
	default:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.dns.summary.values", dnsMachineSummary(*result))}
	}
}

func dnsMachineSummary(result model.Result) string {
	for _, measurement := range result.Measurements {
		if measurement.Key == "best_dns_median_ms" {
			return "best_p50=" + measurement.Display
		}
	}
	return ""
}

type latencySemanticProbe struct{}

func (latencySemanticProbe) ID() string         { return "latency" }
func (latencySemanticProbe) Title() string      { return "module.latency.title" }
func (latencySemanticProbe) NeedsNetwork() bool { return true }

func (latencySemanticProbe) Run(ctx context.Context, env Environment) model.Result {
	result := (latencyProbe{}).Run(ctx, env)
	stabilizeLatencyResult(&result, env.Config.LatencyTargets)
	return result
}

func stabilizeLatencyResult(result *model.Result, targets []config.Endpoint) {
	if result == nil {
		return
	}
	result.Title = "module.latency.title"
	result.Description = "probe.latency.description"
	result.Methodology.Label = "methodology.protocol-measurement"
	result.Methodology.Profile = "probe.latency.profile"
	result.Methodology.ComparisonScope = "probe.latency.comparison_scope"
	for index := range result.Fields {
		result.Fields[index].Label = "probe.latency.field." + result.Fields[index].Key
	}
	for index := range result.Measurements {
		measurement := &result.Measurements[index]
		switch {
		case measurement.Key == "best_tcp_median_ms":
			measurement.Label = "probe.latency.metric.best_median"
		case strings.HasPrefix(measurement.Key, "icmp_"):
			measurement.Label = "probe.latency.metric.icmp"
		default:
			measurement.Label = "probe.latency.metric.tcp"
		}
	}
	for index := range result.Tables {
		table := &result.Tables[index]
		if table.Key != "network.latency.tcp_icmp" {
			continue
		}
		table.Title = "probe.latency.table.tcp_icmp"
		table.Columns = []string{
			"probe.latency.column.target", "probe.latency.column.protocol", "probe.latency.column.region",
			"probe.latency.column.success", "probe.latency.column.tcp_p50", "probe.latency.column.tcp_p95",
			"probe.latency.column.tcp_stddev", "probe.latency.column.icmp_min", "probe.latency.column.icmp_avg",
			"probe.latency.column.icmp_max", "probe.latency.column.icmp_mdev", "probe.latency.column.icmp_loss",
			"probe.latency.column.dns",
		}
		for rowIndex := range table.Rows {
			row := table.Rows[rowIndex]
			if len(row) == 0 {
				continue
			}
			if key := latencyResolutionKey(row, targets, result.Failures); key != "" {
				row[len(row)-1] = key
			}
		}
	}
	result.Notes = []string{
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
		result.Notes = append(result.Notes, "probe.latency.note.icmp")
	} else {
		result.Notes = append(result.Notes, "probe.latency.note.icmp_unavailable")
	}
	if _, ok := fieldByKey(*result, "tcp_intercepted_targets"); ok {
		result.Notes = append(result.Notes, "probe.latency.note.intercepted")
	}
	result.Summary = ""
	if result.Status == model.StatusSkipped {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.latency.summary.skipped")}
	} else if result.Evidence != nil && result.Evidence.Valid == 0 && result.Evidence.Expected > 0 {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.latency.summary.all_failed")}
	} else {
		result.SummaryMessages = []model.Message{model.NewMessage("probe.latency.summary.values", latencyMachineSummary(*result))}
	}
}

func latencyResolutionKey(row []string, targets []config.Endpoint, failures []model.Failure) string {
	if len(row) < 2 {
		return ""
	}
	name := row[0]
	for _, target := range targets {
		if target.Name != name {
			continue
		}
		for _, failure := range failures {
			if failure.Stage == "resolve" && failure.Target == target.Address {
				return "probe.latency.status.resolve_failed"
			}
		}
		host := target.Address
		if parsedHost, _, err := net.SplitHostPort(target.Address); err == nil {
			host = parsedHost
		}
		if net.ParseIP(strings.Trim(host, "[]")) != nil {
			return "probe.latency.status.no_resolution"
		}
		return ""
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

func latencyMachineSummary(result model.Result) string {
	for _, measurement := range result.Measurements {
		if measurement.Key == "best_tcp_median_ms" {
			return "best_p50=" + measurement.Display
		}
	}
	return ""
}

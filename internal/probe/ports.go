package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type portsProbe struct{}

func (portsProbe) ID() string         { return "ports" }
func (portsProbe) Title() string      { return "module.ports.title" }
func (portsProbe) NeedsNetwork() bool { return true }

type portTarget struct {
	Service string
	Address string
	TypeKey string
}

type portResult struct {
	Target  portTarget
	Open    bool
	Latency time.Duration
	Error   string
}

func (portsProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("ports", "module.ports.title")
	result.Description = "probe.ports.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "native TCP connect",
		Profile:         "probe.ports.profile",
		ComparisonScope: "probe.ports.comparison_scope",
	}

	dnsTarget := "1.1.1.1:53"
	if env.Config.IPVersion == config.IPVersion6 {
		dnsTarget = "[2606:4700:4700::1111]:53"
	}
	targets := []portTarget{
		{Service: "HTTP", Address: "example.com:80", TypeKey: "probe.ports.target_type.web"},
		{Service: "HTTPS", Address: "example.com:443", TypeKey: "probe.ports.target_type.web"},
		{Service: "SSH", Address: "github.com:22", TypeKey: "probe.ports.target_type.git"},
		{Service: "DNS TCP", Address: dnsTarget, TypeKey: "probe.ports.target_type.dns"},
		{Service: "SMTP", Address: "smtp.gmail.com:25", TypeKey: "probe.ports.target_type.mail"},
		{Service: "SMTPS", Address: "smtp.gmail.com:465", TypeKey: "probe.ports.target_type.mail"},
		{Service: "Submission", Address: "smtp.gmail.com:587", TypeKey: "probe.ports.target_type.mail"},
		{Service: "IMAPS", Address: "imap.gmail.com:993", TypeKey: "probe.ports.target_type.mail"},
	}
	results := make(chan portResult, len(targets))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(target portTarget) {
			defer wg.Done()
			dialer := net.Dialer{Timeout: 3 * time.Second}
			begin := time.Now()
			connection, err := dialer.DialContext(ctx, tcpNetworkForMode(env.Config.IPVersion), target.Address)
			elapsed := time.Since(begin)
			item := portResult{Target: target, Latency: elapsed}
			if err == nil {
				item.Open = true
				_ = connection.Close()
			} else {
				item.Error = compactError(err)
			}
			results <- item
		}(target)
	}
	wg.Wait()
	close(results)
	collected := make([]portResult, 0, len(targets))
	for item := range results {
		collected = append(collected, item)
	}
	order := make(map[string]int, len(targets))
	for index, target := range targets {
		order[target.Address] = index
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return order[collected[i].Target.Address] < order[collected[j].Target.Address]
	})

	table := model.Table{
		Key:   "network.ports.tcp",
		Title: "probe.ports.table.title",
		Columns: []string{
			"probe.ports.column.service",
			"probe.ports.column.target",
			"probe.ports.column.type",
			"probe.ports.column.status",
			"probe.ports.column.detail",
		},
		ColumnKeys:  []string{"service", "target", "target_type", "status", "detail"},
		RowIdentity: "target",
	}
	openCount := 0
	emailOpen := 0
	validAttempts := 0
	for _, item := range collected {
		status := "probe.ports.status.unreachable"
		detail := item.Error
		if item.Open {
			status = "probe.ports.status.reachable"
			detail = formatMilliseconds(item.Latency)
			openCount++
			if item.Target.TypeKey == "probe.ports.target_type.mail" {
				emailOpen++
			}
		}
		if item.Open || item.Error != "" {
			validAttempts++
		}
		if !item.Open && item.Error != "" {
			addFailureMessage(&result, "connect", item.Target.Address, item.Error)
		}
		table.Rows = append(table.Rows, []string{item.Target.Service, item.Target.Address, item.Target.TypeKey, status, detail})
	}
	result.Tables = []model.Table{table}
	result.Measurements = []model.Measurement{
		{Key: "reachable_ports", Label: "probe.ports.metric.reachable", Value: float64(openCount), Unit: "count", Display: fmt.Sprintf("%d/%d", openCount, len(targets)), Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true)},
		{Key: "reachable_mail_ports", Label: "probe.ports.metric.reachable_mail", Value: float64(emailOpen), Unit: "count", Display: fmt.Sprintf("%d/4", emailOpen), Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true)},
	}
	result.Evidence = model.NewEvidence(validAttempts, len(targets), "target")
	result.Notes = append(result.Notes,
		"probe.ports.note.handshake_only",
		"probe.ports.note.failure_scope",
	)
	result.Summary = ""
	result.SummaryMessages = []model.Message{model.NewMessage("probe.ports.summary", openCount, len(targets), emailOpen)}
	result.Finish(start)
	return result
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	text := err.Error()
	if len(text) > 100 {
		return text[:100] + "…"
	}
	return text
}

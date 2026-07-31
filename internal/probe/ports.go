package probe

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"ecs/internal/model"
)

type portsProbe struct{}

func (portsProbe) ID() string         { return "ports" }
func (portsProbe) Title() string      { return "出站端口" }
func (portsProbe) NeedsNetwork() bool { return true }

type portTarget struct {
	Service string
	Address string
	Note    string
}

type portResult struct {
	Target  portTarget
	Open    bool
	Latency time.Duration
	Error   string
}

func (portsProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("ports", "出站端口")
	result.Description = "常用 Web、SSH、DNS 与邮件端口的 TCP 出站建连"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "native TCP connect",
		Profile:         "fixed public endpoints",
		ComparisonScope: "可达性诊断；单目标失败不能等同于端口被封",
	}

	targets := []portTarget{
		{Service: "HTTP", Address: "example.com:80", Note: "Web"},
		{Service: "HTTPS", Address: "example.com:443", Note: "Web"},
		{Service: "SSH", Address: "github.com:22", Note: "Git"},
		{Service: "DNS TCP", Address: "1.1.1.1:53", Note: "DNS"},
		{Service: "SMTP", Address: "smtp.gmail.com:25", Note: "邮件"},
		{Service: "SMTPS", Address: "smtp.gmail.com:465", Note: "邮件"},
		{Service: "Submission", Address: "smtp.gmail.com:587", Note: "邮件"},
		{Service: "IMAPS", Address: "imap.gmail.com:993", Note: "邮件"},
	}
	results := make(chan portResult, len(targets))
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(target portTarget) {
			defer wg.Done()
			dialer := net.Dialer{Timeout: 3 * time.Second}
			begin := time.Now()
			connection, err := dialer.DialContext(ctx, "tcp", target.Address)
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
		Title:   "TCP 出站能力",
		Columns: []string{"服务", "目标", "类型", "结果", "延迟/原因"},
	}
	openCount := 0
	emailOpen := 0
	for _, item := range collected {
		status := "阻断/不可达"
		detail := item.Error
		if item.Open {
			status = "可达"
			detail = formatMilliseconds(item.Latency)
			openCount++
			if item.Target.Note == "邮件" {
				emailOpen++
			}
		}
		table.Rows = append(table.Rows, []string{item.Target.Service, item.Target.Address, item.Target.Note, status, detail})
	}
	result.Tables = []model.Table{table}
	result.Measurements = []model.Measurement{
		{Key: "reachable_ports", Label: "可达端口", Value: float64(openCount), Unit: "项", Display: fmt.Sprintf("%d/%d", openCount, len(targets)), Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true)},
		{Key: "reachable_mail_ports", Label: "可达邮件端口", Value: float64(emailOpen), Unit: "项", Display: fmt.Sprintf("%d/4", emailOpen), Method: "tcp-connect-v1", HigherIsBetter: model.BoolPtr(true)},
	}
	result.Notes = append(result.Notes,
		"只完成 TCP 握手，不发送邮件、认证信息或应用层命令。",
		"失败可能来自本机防火墙、上游封锁、DNS、目标服务或地区限制；单一目标失败不能证明端口被运营商封锁。",
	)
	result.Summary = fmt.Sprintf("%d/%d 可达 · 邮件 %d/4", openCount, len(targets), emailOpen)
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

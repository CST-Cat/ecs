package probe

import (
	"context"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func TestParseOoklaJSONExtractsSafeMeasurementFields(t *testing.T) {
	output := `noise before {"ping":{"jitter":1.25,"latency":8.5},"download":{"bandwidth":125000000},"upload":{"bandwidth":25000000},"packetLoss":0.5,"isp":"Example ISP","interface":{"externalIp":"203.0.113.10","macAddr":"should-not-be-copied"},"server":{"id":42,"name":"Example","location":"Test City","country":"CN"},"result":{"url":"https://example.invalid/result","persisted":true}} trailing`
	parsed, err := parseOoklaJSON([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	if got := ooklaBandwidthMbps(parsed.Download.Bandwidth); got != 1000 {
		t.Fatalf("download Mbps = %v", got)
	}
	if parsed.Interface.ExternalIP != "203.0.113.10" || parsed.Server.ID != 42 {
		t.Fatalf("parsed = %+v", parsed)
	}
	var result strings.Builder
	appendOoklaMeasurementsToString(&result, parsed)
	if strings.Contains(result.String(), "macAddr") || strings.Contains(result.String(), "example.invalid") {
		t.Fatalf("unsafe raw fields leaked: %q", result.String())
	}
}

// appendOoklaMeasurementsToString mirrors the report's selected fields; it
// keeps this parser test independent from report rendering details.
func appendOoklaMeasurementsToString(builder *strings.Builder, parsed ooklaResult) {
	builder.WriteString(parsed.ISP)
	builder.WriteString(" ")
	builder.WriteString(parsed.Interface.ExternalIP)
	builder.WriteString(" ")
	builder.WriteString(parsed.Server.Name)
}

func TestOoklaBandwidthRejectsNonPositiveValues(t *testing.T) {
	if got := ooklaBandwidthMbps(0); got != 0 {
		t.Fatalf("zero bandwidth = %v", got)
	}
	if got := ooklaBandwidthMbps(-1); got != 0 {
		t.Fatalf("negative bandwidth = %v", got)
	}
}

func TestOoklaSkipIncludesReasonAndNextStepInText(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Exposure = config.ExposureThirdParty
	cfg.Accepted = nil
	result := (ooklaProbe{}).Run(context.Background(), Environment{Config: cfg})
	if result.Status != model.StatusSkipped {
		t.Fatalf("status = %s, want skipped", result.Status)
	}
	if len(result.Fields) < 2 {
		t.Fatalf("skip details missing: %+v", result.Fields)
	}
	out := report.Text(model.Report{
		Tool:    model.ToolInfo{Name: "ecs", Version: "test"},
		Summary: model.Summary{Headline: "1 项跳过"},
		Results: []model.Result{result},
	}, report.TextOptions{Color: termcolor.LevelNone})
	for _, want := range []string{"跳过原因", "未确认 Ookla 许可与隐私条款", "下一步", "显式接受 Ookla 许可与隐私条款后重跑模块"} {
		if !strings.Contains(out, want) {
			t.Fatalf("skip text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "命令参数") || strings.Contains(out, "原始输出") {
		t.Fatalf("skip text should not expose command/raw output:\n%s", out)
	}
}

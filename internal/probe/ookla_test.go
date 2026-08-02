package probe

import (
	"strings"
	"testing"
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

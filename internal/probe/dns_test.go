package probe

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestDNSQueryResponseAndFormattingContracts(t *testing.T) {
	packet, id, err := buildDNSQuery("www.example.com")
	if err != nil || len(packet) < 20 || binary.BigEndian.Uint16(packet[:2]) != id || binary.BigEndian.Uint16(packet[4:6]) != 1 || packet[12] != 3 || string(packet[13:16]) != "www" || !strings.Contains(string(packet[12:]), "example") {
		t.Fatalf("DNS query packet = %d/%d/%v", len(packet), id, err)
	}
	if _, _, err := buildDNSQuery("www..example"); err == nil || !strings.Contains(err.Error(), "无效 DNS 名称") {
		t.Fatalf("invalid DNS query = %v", err)
	}

	valid := make([]byte, 12)
	binary.BigEndian.PutUint16(valid[:2], id)
	binary.BigEndian.PutUint16(valid[2:4], 0x8000)
	binary.BigEndian.PutUint16(valid[6:8], 1)
	responses := []struct {
		name, marker string
		packet       []byte
	}{
		{name: "short", packet: valid[:4], marker: "响应过短"},
		{name: "transaction ID", packet: func() []byte { p := append([]byte(nil), valid...); binary.BigEndian.PutUint16(p[:2], id+1); return p }(), marker: "事务 ID"},
		{name: "not response", packet: func() []byte { p := append([]byte(nil), valid...); binary.BigEndian.PutUint16(p[2:4], 0); return p }(), marker: "不是 DNS 响应"},
		{name: "rcode", packet: func() []byte {
			p := append([]byte(nil), valid...)
			binary.BigEndian.PutUint16(p[2:4], 0x8002)
			return p
		}(), marker: "RCODE"},
		{name: "no answer", packet: func() []byte { p := append([]byte(nil), valid...); binary.BigEndian.PutUint16(p[6:8], 0); return p }(), marker: "无应答记录"},
	}
	for _, test := range responses {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDNSResponse(test.packet, id); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("DNS response error = %v, want %q", err, test.marker)
			}
		})
	}
	if err := validateDNSResponse(valid, id); err != nil {
		t.Fatalf("valid DNS response rejected: %v", err)
	}
	for _, test := range []struct {
		mode, network string
	}{{config.IPVersion4, "udp4"}, {config.IPVersion6, "udp6"}, {config.IPVersionAuto, "udp"}} {
		if got := udpNetworkForMode(test.mode); got != test.network {
			t.Fatalf("UDP network for %q = %q, want %q", test.mode, got, test.network)
		}
	}
	if formatMilliseconds(1500*time.Microsecond) != "1.50 ms" || formatMilliseconds(0) != "n/a" {
		t.Fatal("DNS millisecond formatting contract failed")
	}
}

func TestDNSProducerDirectResult(t *testing.T) {
	t.Run("skip without resolver", func(t *testing.T) {
		result := (dnsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		if result.Status != model.StatusSkipped || result.Title != "module.dns.title" || result.Description != "probe.dns.description" {
			t.Fatalf("DNS skip result = %+v", result)
		}
		if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 0 || result.Evidence.Unit != "query" {
			t.Fatalf("DNS skip evidence = %+v", result.Evidence)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.skipped" {
			t.Fatalf("DNS skip summary = %+v", result.SummaryMessages)
		}
		if got := strings.Join(result.Notes, ","); got != "probe.dns.note.warmup,probe.dns.note.udp_scope" {
			t.Fatalf("DNS skip notes = %q", got)
		}
	})

	t.Run("success", func(t *testing.T) {
		address := startDNSFixture(t, func(int) bool { return true })
		result := (dnsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion:    config.IPVersion4,
			DNSAttempts:  2,
			DNSResolvers: []config.Endpoint{{Name: "fixture", Address: address}},
		}})
		if result.Status != model.StatusOK || result.Title != "module.dns.title" || result.Description != "probe.dns.description" {
			t.Fatalf("DNS success status/metadata = %s/%+v", result.Status, result)
		}
		if result.Methodology.Label != "methodology.protocol-measurement" || result.Methodology.Profile != "probe.dns.profile" || result.Methodology.ComparisonScope != "probe.dns.comparison_scope" {
			t.Fatalf("DNS success methodology = %+v", result.Methodology)
		}
		if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 2 || result.Evidence.Unit != "query" {
			t.Fatalf("DNS success evidence = %+v", result.Evidence)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.values" {
			t.Fatalf("DNS success summary = %+v", result.SummaryMessages)
		}
		if len(result.Notes) != 2 || result.Notes[0] != "probe.dns.note.warmup" || result.Notes[1] != "probe.dns.note.udp_scope" {
			t.Fatalf("DNS success notes = %v", result.Notes)
		}
		if result.StartedAt.IsZero() {
			t.Fatal("DNS success result was not finished")
		}
		if len(result.Tables) != 1 || result.Tables[0].Title != "probe.dns.table.resolvers" || len(result.Tables[0].Columns) != 7 || len(result.Tables[0].Rows) != 1 {
			t.Fatalf("DNS success table = %+v", result.Tables)
		}
		for index, column := range result.Tables[0].Columns {
			if column.Label == "" || column.Key == "" {
				t.Fatalf("DNS success column %d = %+v", index, column)
			}
		}
		row := result.Tables[0].Rows[0]
		if len(row) != len(result.Tables[0].Columns) {
			t.Fatalf("DNS success row width = %d/%d", len(row), len(result.Tables[0].Columns))
		}
		if row[0].Text() != "fixture" || row[1].Text() != address || row[2].Text() != "2/2" {
			t.Fatalf("DNS success raw cells = %+v", row[:3])
		}
		if key, ok := row[6].Key(); !ok || key != "probe.dns.status.ok" {
			t.Fatalf("DNS success status cell = %+v", row[6])
		}
		if _, ok := row[6].Raw(); ok {
			t.Fatal("DNS success status cell unexpectedly has raw variant")
		}
		if len(result.Measurements) != 5 || result.Measurements[0].Label != "probe.dns.metric.resolver" || result.Measurements[4].Label != "probe.dns.metric.best_median" {
			t.Fatalf("DNS success measurements = %+v", result.Measurements)
		}
		for _, measurement := range result.Measurements {
			if _, ok := measurement.Display.Raw(); !ok {
				t.Fatalf("DNS measurement display is not raw: %+v", measurement)
			}
		}
	})

	t.Run("partial", func(t *testing.T) {
		address := startDNSFixture(t, func(request int) bool { return request != 3 })
		result := (dnsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion:    config.IPVersion4,
			DNSAttempts:  2,
			DNSResolvers: []config.Endpoint{{Name: "fixture", Address: address}},
		}})
		if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 2 {
			t.Fatalf("DNS partial status/evidence = %s/%+v", result.Status, result.Evidence)
		}
		if len(result.Failures) != 1 || result.Failures[0].Stage != "query" || result.Failures[0].Count != 1 {
			t.Fatalf("DNS partial failures = %+v", result.Failures)
		}
		if key, ok := result.Tables[0].Rows[0][6].Key(); !ok || key != "probe.dns.status.partial" {
			t.Fatalf("DNS partial status cell = %+v", result.Tables[0].Rows[0][6])
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.values" {
			t.Fatalf("DNS partial summary = %+v", result.SummaryMessages)
		}
	})

	t.Run("all failed", func(t *testing.T) {
		address := startDNSFixture(t, func(int) bool { return false })
		result := (dnsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion:    config.IPVersion4,
			DNSAttempts:  2,
			DNSResolvers: []config.Endpoint{{Name: "fixture", Address: address}},
		}})
		if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 2 {
			t.Fatalf("DNS all-failed status/evidence = %s/%+v", result.Status, result.Evidence)
		}
		if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.dns.summary.all_failed" {
			t.Fatalf("DNS all-failed summary = %+v", result.SummaryMessages)
		}
		if key, ok := result.Tables[0].Rows[0][6].Key(); !ok || key != "probe.dns.status.failed" {
			t.Fatalf("DNS all-failed status cell = %+v", result.Tables[0].Rows[0][6])
		}
	})
}

func startDNSFixture(t *testing.T, valid func(request int) bool) string {
	t.Helper()
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 1500)
		request := 0
		for {
			n, address, err := connection.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			request++
			response := make([]byte, 12)
			if n >= 2 {
				copy(response[:2], buffer[:2])
			}
			binary.BigEndian.PutUint16(response[2:4], 0x8000)
			if valid(request) {
				binary.BigEndian.PutUint16(response[6:8], 1)
			}
			_, _ = connection.WriteToUDP(response, address)
		}
	}()
	t.Cleanup(func() {
		_ = connection.Close()
		<-done
	})
	return connection.LocalAddr().String()
}

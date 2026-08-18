package probe

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"ecs/internal/config"
)

func TestDNSProbeEmitsPerResolverStatisticsAndEvidence(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	const attempts = 3
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 1500)
		for index := 0; index < attempts+1; index++ { // one warm-up + measured queries
			_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
			count, address, readErr := server.ReadFromUDP(buffer)
			if readErr != nil || count < 2 {
				return
			}
			response := make([]byte, 12)
			copy(response[:2], buffer[:2])
			binary.BigEndian.PutUint16(response[2:4], 0x8180)
			binary.BigEndian.PutUint16(response[6:8], 1)
			_, _ = server.WriteToUDP(response, address)
		}
	}()

	cfg := config.Runtime{
		IPVersion:   config.IPVersion4,
		DNSAttempts: attempts,
		DNSResolvers: []config.Endpoint{{
			Name: "fixture", Address: server.LocalAddr().String(),
		}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := (dnsProbe{}).Run(ctx, Environment{Config: cfg})
	<-done

	wantKeys := map[string]bool{
		"dns_resolver_01_success_percent": false,
		"dns_resolver_01_p50_ms":          false,
		"dns_resolver_01_p95_ms":          false,
		"dns_resolver_01_jitter_ms":       false,
		"best_dns_median_ms":              false,
	}
	var bestDNSMethod string
	for _, measurement := range result.Measurements {
		if _, ok := wantKeys[measurement.Key]; ok {
			wantKeys[measurement.Key] = true
		}
		if measurement.Key == "best_dns_median_ms" {
			bestDNSMethod = measurement.Method
		}
	}
	for key, found := range wantKeys {
		if !found {
			t.Errorf("DNS result missing %q: %+v", key, result.Measurements)
		}
	}
	if bestDNSMethod != "udp-a-query-warm-v1" {
		t.Errorf("best_dns_median_ms.Method = %q, want %q", bestDNSMethod, "udp-a-query-warm-v1")
	}
	if result.Evidence == nil || result.Evidence.Valid != attempts || result.Evidence.Expected != attempts || result.Evidence.Unit != "query" {
		t.Fatalf("DNS evidence = %+v", result.Evidence)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].NumericColumns) != 3 {
		t.Fatalf("DNS numeric table metadata = %+v", result.Tables)
	}
}

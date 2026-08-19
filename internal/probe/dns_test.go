package probe

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
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

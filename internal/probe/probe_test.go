package probe

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestBuildDNSQuery(t *testing.T) {
	packet, id, err := buildDNSQuery("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) < 20 || binary.BigEndian.Uint16(packet[:2]) != id || binary.BigEndian.Uint16(packet[4:6]) != 1 {
		t.Fatalf("DNS query header is invalid: id=%d packet=%d", id, len(packet))
	}
	if !strings.Contains(string(packet), "example") {
		t.Fatal("encoded query name is missing")
	}
}

func TestBuiltinsHaveUniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, item := range Builtins() {
		if item == nil || item.ID() == "" || seen[item.ID()] {
			t.Fatalf("invalid or duplicate builtin probe: %#v", item)
		}
		seen[item.ID()] = true
	}
	if len(seen) == 0 {
		t.Fatal("builtin probe list is empty")
	}
}

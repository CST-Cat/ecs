package probe

import (
	"net"
	"testing"
)

func TestDNSBLReverseAndClassifyBasicOutcomes(t *testing.T) {
	prefix, ok := reverseIPv4(net.ParseIP("203.0.113.45"))
	if !ok || prefix != "45.113.0.203" {
		t.Fatalf("reverseIPv4 = %q, %v", prefix, ok)
	}

	if got, _ := classifyDNSBLCodes(nil); got != dnsblClean {
		t.Fatalf("empty DNSBL response = %q, want %q", got, dnsblClean)
	}
	if got, _ := classifyDNSBLCodes([]string{"127.0.0.2"}); got != dnsblListed {
		t.Fatalf("listed DNSBL response = %q, want %q", got, dnsblListed)
	}
}

package probe

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestDNSBLClassificationReverseAndPresentation(t *testing.T) {
	if prefix, ok := reverseIPv4(net.ParseIP("203.0.113.45")); !ok || prefix != "45.113.0.203" {
		t.Fatalf("IPv4 reverse = %q/%v", prefix, ok)
	}
	if prefix, ok := reverseIPv4(net.ParseIP("2001:db8::1")); ok || prefix != "" {
		t.Fatalf("IPv6 reverse = %q/%v", prefix, ok)
	}
	for _, test := range []struct {
		name, want, detail string
		addresses          []string
	}{
		{name: "clean", want: string(dnsblClean)},
		{name: "listed", addresses: []string{"127.0.0.2"}, want: string(dnsblListed)},
		{name: "all refused", addresses: []string{"127.255.255.254", "127.255.255.255"}, want: string(dnsblRefused), detail: "查询被拒绝"},
		{name: "mixed refused and listed", addresses: []string{"127.255.255.254", "127.0.0.2"}, want: string(dnsblListed)},
	} {
		outcome, detail := classifyDNSBLCodes(test.addresses)
		if string(outcome) != test.want || test.detail != "" && !strings.Contains(detail, test.detail) {
			t.Fatalf("DNSBL %s = %q/%q", test.name, outcome, detail)
		}
	}
	var dnsErr *net.DNSError
	if !asDNSError(&net.DNSError{IsNotFound: true}, &dnsErr) || asDNSError(errors.New("not DNS"), &dnsErr) {
		t.Fatal("DNS error type classification failed")
	}

	measurements := dnsblCountMeasurements(1, 2, 3, 4, 10)
	if len(measurements) != 4 || measurements[0].Key != "dnsbl_listed_count" || measurements[1].Key != "dnsbl_clean_count" || measurements[2].Key != "dnsbl_refused_count" || measurements[3].Key != "dnsbl_failed_count" {
		t.Fatalf("DNSBL measurements = %+v", measurements)
	}
	if measurements[0].HigherIsBetter == nil || *measurements[0].HigherIsBetter || measurements[1].HigherIsBetter == nil || !*measurements[1].HigherIsBetter {
		t.Fatal("DNSBL measurement directions are incorrect")
	}
	for _, test := range []struct {
		outcome string
		want    int
	}{{string(dnsblListed), 0}, {string(dnsblRefused), 1}, {string(dnsblFailed), 2}, {string(dnsblClean), 3}} {
		if got := dnsblRowRank(test.outcome); got != test.want {
			t.Errorf("DNSBL row rank %q = %d, want %d", test.outcome, got, test.want)
		}
	}
}

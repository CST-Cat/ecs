package probe

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"ecs/internal/model"
)

type fixtureRDNSResolver struct {
	addresses []string
	addrErr   error
	hosts     map[string][]string
	hostErr   map[string]error
}

func (fixture fixtureRDNSResolver) LookupAddr(context.Context, string) ([]string, error) {
	return fixture.addresses, fixture.addrErr
}

func (fixture fixtureRDNSResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if err := fixture.hostErr[host]; err != nil {
		return nil, err
	}
	return fixture.hosts[host], nil
}

func TestReverseDNSFixturesPreserveLookupDiagnoses(t *testing.T) {
	if hits := matchResidentialHints("pppoe-dynamic.example.net"); len(hits) == 0 {
		t.Fatal("residential PTR hint was not recognized")
	}
	if hits := matchResidentialHints("server1.resources.example.net"); len(hits) != 0 {
		t.Fatalf("substring falsely matched residential hint: %v", hits)
	}
	const ip = "203.0.113.1"
	cases := []struct {
		name, ptr, fcrdns, note string
		resolver                fixtureRDNSResolver
		status                  model.Status
		failures                int
		stage, detail           string
	}{
		{name: "PTR not found", resolver: fixtureRDNSResolver{addrErr: &net.DNSError{IsNotFound: true, Err: "NXDOMAIN"}}, status: model.StatusWarning, ptr: "probe.rdns.ptr.none", fcrdns: "probe.rdns.status.failed", note: "probe.rdns.note.no_ptr"},
		{name: "PTR query failed", resolver: fixtureRDNSResolver{addrErr: errors.New("resolver unavailable")}, status: model.StatusWarning, ptr: "probe.rdns.ptr.query_failed", fcrdns: "probe.rdns.status.failed", note: "probe.rdns.note.reverse_failed", failures: 1, stage: "reverse_lookup", detail: "resolver unavailable"},
		{name: "FCrDNS confirmed", resolver: fixtureRDNSResolver{addresses: []string{"mail.example.net."}, hosts: map[string][]string{"mail.example.net": {ip}}}, status: model.StatusOK, ptr: "mail.example.net.", fcrdns: "probe.rdns.status.passed", note: "probe.rdns.note.confirmed"},
		{name: "forward mismatch", resolver: fixtureRDNSResolver{addresses: []string{"other.example.net."}, hosts: map[string][]string{"other.example.net": {"198.51.100.2"}}}, status: model.StatusWarning, ptr: "other.example.net.", fcrdns: "probe.rdns.status.failed", note: "probe.rdns.note.mismatch"},
		{name: "forward query failed", resolver: fixtureRDNSResolver{addresses: []string{"mail.example.net."}, hostErr: map[string]error{"mail.example.net": errors.New("forward unavailable")}}, status: model.StatusWarning, ptr: "mail.example.net.", fcrdns: "probe.rdns.status.failed", note: "probe.rdns.note.forward_failed", failures: 1, stage: "forward_confirmation", detail: "forward unavailable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			check := checkReverseDNS(context.Background(), test.resolver, ip)
			result := model.NewResult("rdns", "module.blacklist.title")
			appendReverseDNSResult(&result, check)
			if result.Status != test.status || result.Fields[0].Value.Text() != test.ptr || result.Fields[1].Value.Text() != test.fcrdns || len(result.Failures) != test.failures || !containsString(result.Notes, test.note) || test.detail != "" && (len(result.Failures) == 0 || result.Failures[0].Stage != test.stage || !strings.Contains(result.Failures[0].Message, test.detail)) {
				t.Fatalf("rDNS result = status:%s fields:%v failures:%v notes:%v", result.Status, result.Fields, result.Failures, result.Notes)
			}
			if len(result.Tables) != 1 || result.Tables[0].RowIdentity != "item" || result.Tables[0].Title != "probe.rdns.table.title" || len(result.Measurements) != 1 || result.Measurements[0].Value != boolValue(check.Confirmed) || result.Measurements[0].Label != "probe.rdns.metric.fcrdns_passed" {
				t.Fatalf("rDNS schema = table:%+v measurements:%+v", result.Tables[0], result.Measurements)
			}
		})
	}
}

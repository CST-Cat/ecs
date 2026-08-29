package probe

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
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
		outcome dnsblOutcome
		want    int
	}{{dnsblListed, 0}, {dnsblRefused, 1}, {dnsblFailed, 2}, {dnsblClean, 3}} {
		if got := dnsblOutcomeRank(test.outcome); got != test.want {
			t.Errorf("DNSBL row rank %q = %d, want %d", test.outcome, got, test.want)
		}
	}
}

func TestBlacklistProducerSkipBuildsStableResult(t *testing.T) {
	result := (blacklistProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion6}})
	if result.Title != "module.blacklist.title" || result.Description != "probe.blacklist.description" || result.Status != model.StatusSkipped {
		t.Fatalf("blacklist direct metadata/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.protocol-measurement" || result.Methodology.Profile != "probe.blacklist.profile" || result.Methodology.ComparisonScope != "probe.blacklist.comparison_scope" {
		t.Fatalf("blacklist methodology = %+v", result.Methodology)
	}
	if result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != len(dnsblZones()) || len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.blacklist.summary.skipped" {
		t.Fatalf("blacklist skip evidence/summary = %+v/%+v", result.Evidence, result.SummaryMessages)
	}
	if len(result.Notes) != 3 {
		t.Fatalf("blacklist skip notes = %v", result.Notes)
	}
}

func TestBlacklistProducerReportsEgressDegradation(t *testing.T) {
	tests := []struct {
		name       string
		egress     EgressAddress
		category   model.FailureCategory
		stage      string
		wantDetail string
	}{
		{
			name:     "typed egress discovery failure",
			egress:   EgressAddress{Err: context.DeadlineExceeded},
			category: model.FailureTimeout,
			stage:    "egress",
		},
		{
			name:       "invalid egress address",
			egress:     EgressAddress{IP: "2001:db8::1"},
			category:   model.FailureParse,
			stage:      "validate",
			wantDetail: "valid IPv4",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := (blacklistProbe{}).Run(context.Background(), Environment{
				Config: config.Runtime{IPVersion: config.IPVersion4},
				Egress: Egress{ByVersion: map[string]EgressAddress{config.IPVersion4: test.egress}},
			})
			if result.Status != model.StatusWarning || len(result.Failures) != 1 {
				t.Fatalf("blacklist egress degradation = status:%s failures:%+v", result.Status, result.Failures)
			}
			if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.blacklist.summary.unavailable" {
				t.Fatalf("blacklist unavailable summary = %+v", result.SummaryMessages)
			}
			failure := result.Failures[0]
			if failure.Category != test.category || failure.Stage != test.stage || failure.Target != "IPv4" || failure.Message == "" {
				t.Fatalf("blacklist egress failure = %+v", failure)
			}
			if test.wantDetail != "" && !strings.Contains(failure.Message, test.wantDetail) {
				t.Fatalf("blacklist validation detail = %q, want %q", failure.Message, test.wantDetail)
			}
		})
	}
}

func TestBlacklistOutcomeValuesAreExplicitKeys(t *testing.T) {
	for _, test := range []struct {
		outcome dnsblOutcome
		key     string
	}{{dnsblListed, "probe.blacklist.outcome.listed"}, {dnsblClean, "probe.blacklist.outcome.clean"}, {dnsblRefused, "probe.blacklist.outcome.refused"}, {dnsblFailed, "probe.blacklist.outcome.failed"}} {
		value := dnsblOutcomeValue(test.outcome)
		if key, ok := value.Key(); !ok || key != test.key {
			t.Errorf("DNSBL outcome %q = %#v, want key %q", test.outcome, value, test.key)
		}
	}
}
func TestBlacklistProducerBuildsStableResultFromFindings(t *testing.T) {
	originalQuery := dnsblQueryForProbe
	originalReverse := appendReverseDNSForProbe
	t.Cleanup(func() {
		dnsblQueryForProbe = originalQuery
		appendReverseDNSForProbe = originalReverse
	})
	dnsblQueryForProbe = func(_ context.Context, _ *net.Resolver, _ string, zone dnsblZone) dnsblFinding {
		finding := dnsblFinding{Zone: zone, Duration: 2 * time.Millisecond}
		switch zone.Name {
		case "Spamhaus ZEN":
			finding.Outcome = dnsblListed
			finding.Codes = []string{"127.0.0.2"}
		case "SpamCop":
			finding.Outcome = dnsblRefused
			finding.Detail = "fixture query refused"
		case "Barracuda":
			finding.Outcome = dnsblFailed
			finding.Detail = "fixture query failed"
		default:
			finding.Outcome = dnsblClean
		}
		return finding
	}
	appendReverseDNSForProbe = func(context.Context, *model.Result, string) {}

	result := (blacklistProbe{}).Run(context.Background(), Environment{
		Config: config.Runtime{IPVersion: config.IPVersion4},
		Egress: Egress{ByVersion: map[string]EgressAddress{
			config.IPVersion4: {Version: config.IPVersion4, IP: "203.0.113.9"},
		}},
	})
	if result.Title != "module.blacklist.title" || result.Description != "probe.blacklist.description" || result.Status != model.StatusWarning {
		t.Fatalf("blacklist direct metadata/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.protocol-measurement" || result.Methodology.Profile != "probe.blacklist.profile" || result.Methodology.ComparisonScope != "probe.blacklist.comparison_scope" {
		t.Fatalf("blacklist methodology = %+v", result.Methodology)
	}
	if result.Evidence == nil || result.Evidence.Valid != len(dnsblZones())-2 || result.Evidence.Expected != len(dnsblZones()) || len(result.Failures) != 2 {
		t.Fatalf("blacklist evidence/failures = %+v/%+v", result.Evidence, result.Failures)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.blacklist.summary.listed" || len(result.SummaryMessages[0].Args) != 2 || result.SummaryMessages[0].Args[0] != "1" {
		t.Fatalf("blacklist summary = %+v", result.SummaryMessages)
	}
	if len(result.Fields) != 3 || result.Fields[0].Label != "probe.blacklist.field.queried_ip" || result.Fields[0].Value.Text() != "203.0.113.9" {
		t.Fatalf("blacklist fields = %+v", result.Fields)
	}
	if len(result.Measurements) != 4 || result.Measurements[0].Label != "probe.blacklist.metric.dnsbl_listed_count" {
		t.Fatalf("blacklist measurements = %+v", result.Measurements)
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.blacklist.table.results" || result.Tables[0].Columns[1].Label != "probe.blacklist.column.outcome" {
		t.Fatalf("blacklist table schema = %+v", result.Tables)
	}
	if len(result.Tables[0].Rows) != len(dnsblZones()) || result.Tables[0].Rows[0][0].Text() != "Spamhaus ZEN" || result.Tables[0].Rows[0][1].Text() != "probe.blacklist.outcome.listed" {
		t.Fatalf("blacklist table ordering = %+v", result.Tables[0].Rows)
	}
	if _, ok := result.Tables[0].Rows[0][1].Key(); !ok {
		t.Fatalf("blacklist outcome is not a key value: %+v", result.Tables[0].Rows[0][1])
	}
	if _, ok := result.Tables[0].Rows[0][0].Raw(); !ok {
		t.Fatalf("blacklist list name should be raw: %+v", result.Tables[0].Rows[0][0])
	}
	if len(result.Sources) != 3 || result.Sources[0].Purpose != "probe.blacklist.source.list" || len(result.Notes) != 3 {
		t.Fatalf("blacklist sources/notes = %+v/%v", result.Sources, result.Notes)
	}
}

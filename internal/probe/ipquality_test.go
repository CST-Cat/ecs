package probe

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestIPQualityJSONParsersAndProviderFailures(t *testing.T) {
	cases := []struct {
		name, body, country, usage                 string
		parse                                      func([]byte) qualityFinding
		wantScore                                  *float64
		wantProxy, wantServer, wantVPN, wantAbuser bool
	}{
		{name: "ipregistry", parse: parseIPregistryJSON, body: `{"location":{"country":{"code":"us"}},"connection":{"type":"hosting"},"company":{"type":"business"},"security":{"is_proxy":true,"is_cloud_provider":true}}`, country: "US", usage: "probe.network.network_type.datacenter", wantProxy: true, wantServer: true},
		{name: "ip2location", parse: parseIP2LocationJSON, body: `{"country_code":"us","usage_type":"DCH","fraud_score":42,"is_proxy":true}`, country: "US", usage: "probe.network.network_type.datacenter", wantScore: floatPtr(42), wantProxy: true},
		{name: "ipqs", parse: parseIPQSJSON, body: `{"success":true,"fraud_score":88,"country_code":"us","proxy":true,"connection_type":"hosting"}`, country: "US", usage: "probe.network.network_type.datacenter", wantScore: floatPtr(88), wantProxy: true},
		{name: "dbip extended", parse: parseDBIPExtendedJSON, body: `{"countryCode":"us","usageType":"hosting","isProxy":true,"proxyType":"vpn","threatLevel":"medium","threatDetails":["attack-source"]}`, country: "US", usage: "probe.network.network_type.datacenter", wantScore: floatPtr(50), wantProxy: true, wantServer: true, wantVPN: true, wantAbuser: true},
		{name: "dbip free", parse: parseDBIPFreeJSON, body: `{"countryCode":"us"}`, country: "US"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			finding := test.parse([]byte(test.body))
			if finding.Err != nil || finding.Country != test.country || finding.Usage != test.usage {
				t.Fatalf("provider finding = %+v", finding)
			}
			if test.wantScore == nil != (finding.Score == nil) || test.wantScore != nil && *finding.Score != *test.wantScore {
				t.Fatalf("provider score = %v, want %v", finding.Score, test.wantScore)
			}
			if test.wantProxy && (!finding.Proxy.Known || !finding.Proxy.Value) || test.wantServer && (!finding.Server.Known || !finding.Server.Value) || test.wantVPN && (!finding.VPN.Known || !finding.VPN.Value) || test.wantAbuser && (!finding.Abuser.Known || !finding.Abuser.Value) {
				t.Fatalf("provider signals = %+v", finding)
			}
		})
	}
	partial := parseIP2LocationJSON([]byte(`{"country_code":"US","usage_type":"ISP"}`))
	if partial.Err != nil || partial.Partial == "" {
		t.Fatalf("IP2Location partial finding = %+v", partial)
	}
	partial = parseIPQSJSON([]byte(`{"success":true,"country_code":"US","proxy":false}`))
	if partial.Err != nil || partial.Partial != networkPartialScore || !partial.Proxy.Known || partial.Proxy.Value {
		t.Fatalf("IPQS partial finding = %+v", partial)
	}

	if finding := parseIPregistryJSON([]byte(`{"location":`)); finding.Err == nil || !strings.Contains(finding.Err.Error(), "响应不是有效 JSON") {
		t.Fatal("malformed provider JSON did not retain diagnostic")
	}
	if finding := parseIPregistryJSON([]byte(`{}`)); finding.Err == nil || !strings.Contains(finding.Err.Error(), "缺少所需字段") {
		t.Fatal("missing provider evidence did not retain diagnostic")
	}
	if finding := parseIPQSJSON([]byte(`{"success":false,"country_code":"US"}`)); finding.Err == nil || !strings.Contains(finding.Err.Error(), "上游未返回风险数据") {
		t.Fatalf("IPQS upstream rejection = %+v", finding)
	}
	if finding := parseDBIPExtendedJSON([]byte(`{"errorCode":"forbidden"}`)); finding.Err == nil || !strings.Contains(finding.Err.Error(), "DB-IP API 拒绝") {
		t.Fatalf("DB-IP rejection = %+v", finding)
	}
	if finding := parseDBIPFreeJSON([]byte(`{"errorCode":"limited"}`)); finding.Err == nil || !strings.Contains(finding.Err.Error(), "DB-IP free API 拒绝") {
		t.Fatalf("DB-IP free rejection = %+v", finding)
	}
	if finding := parseIP2LocationJSON([]byte(`{"country_code":"US","is_proxy":true,"fraud_score":101}`)); finding.Err != nil || finding.Score != nil {
		t.Fatalf("invalid score should be partial, got %+v", finding)
	}
}

func floatPtr(value float64) *float64 { return &value }

func TestIPQualityPublicPagesScoresAndSignals(t *testing.T) {
	const ip = "203.0.113.9"
	page := `<html><body>` + ip + ` located in the US that is assigned to a provider. This IP address (` + ip + `) is a proxy connection. identified ` + ip + ` as a VPN connection. scored 42 out of 100. No recent abuse detected from this connection.</body></html>`
	finding := parseIPQSPublicPage([]byte(page), ip)
	if finding.Err != nil || finding.Country != "US" || finding.Score == nil || *finding.Score != 42 || !finding.Proxy.Known || !finding.Proxy.Value || !finding.VPN.Known || !finding.VPN.Value || finding.Abuser.Value {
		t.Fatalf("IPQS public page = %+v", finding)
	}
	dbip := parseDBIPPublicPage([]byte(`{"countryCode":"US"} Estimated threat level for this IP address is <span>high</span>`))
	if dbip.Err != nil || dbip.Country != "US" || dbip.Score == nil || *dbip.Score != 100 || dbip.Risk != "probe.network.risk.high" {
		t.Fatalf("DB-IP public page = %+v", dbip)
	}
	for _, test := range []struct {
		name, body, marker string
	}{
		{name: "missing target", body: "scored 42 out of 100", marker: "未返回目标 IP"},
		{name: "lookup limit", body: ip + " Daily max lookups reached", marker: "查询上限"},
		{name: "missing score", body: ip + " located in the US", marker: "未返回欺诈分"},
		{name: "score range", body: ip + " scored 101 out of 100", marker: "超出范围"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := parseIPQSPublicPage([]byte(test.body), ip)
			if got.Err == nil || !strings.Contains(got.Err.Error(), test.marker) {
				t.Fatalf("public page error = %v, want %q", got.Err, test.marker)
			}
		})
	}
	if scoreBar(42) == "────────────" || formatScore(42.5) != "42.50" || durationText(0) != networkMissingValue || durationText(time.Second) != "1000 ms" {
		t.Fatal("IP quality display helpers failed")
	}
}

func TestIPQualitySignalsAndScoreBuckets(t *testing.T) {
	trueValue, falseValue := true, false
	if !pointerSignal(&trueValue).Known || !pointerSignal(&trueValue).Value || !pointerSignal(&falseValue).Known || pointerSignal(&falseValue).Value || pointerSignal(nil).Known {
		t.Fatal("pointer signal states failed")
	}
	if got := anyPointerSignal(nil, &falseValue); !got.Known || got.Value || !anyKnownSignal(knownSignal(false)) || anyKnownSignal(qualitySignal{}) {
		t.Fatal("combined signal states failed")
	}
	if got := firstKnownSignal(qualitySignal{}, knownSignal(true)); !got.Known || !got.Value || !knownWhenNonEmpty(false, "source").Known || knownWhenNonEmpty(false, "").Known {
		t.Fatal("known signal fallback failed")
	}
	if got := threatDetailSignal([]string{"informational"}); !got.Known || got.Value || !threatDetailSignal([]string{"attack-source"}).Value || yesNoSignal("yes").Value != true || yesNoSignal("not detected").Value || yesNoSignal("unknown").Known {
		t.Fatal("risk signal parsing failed")
	}
	if got := signalFromPageText("proxy connection", "proxy connection", "not proxy"); !got.Known || !got.Value || signalFromPageText("not proxy", "proxy", "not proxy").Value {
		t.Fatal("page signal parsing failed")
	}
	if !findingHasEvidence(qualityFinding{Country: "US"}) || findingHasEvidence(qualityFinding{}) {
		t.Fatal("finding evidence classification failed")
	}

	for _, test := range []struct {
		name, want string
		fn         func(*float64) string
		values     []float64
	}{
		{name: "IP2Location", fn: riskIP2Location, values: []float64{20, 50, 80}, want: "probe.network.risk.low,probe.network.risk.medium,probe.network.risk.high"},
		{name: "Scamalytics", fn: riskScamalytics, values: []float64{10, 30, 70, 95}, want: "probe.network.risk.low,probe.network.risk.medium,probe.network.risk.high,probe.network.risk.very_high"},
		{name: "AbuseIPDB", fn: riskAbuseIPDB, values: []float64{10, 50, 80}, want: "probe.network.risk.low,probe.network.risk.suspicious,probe.network.risk.high"},
		{name: "IPQS", fn: riskIPQS, values: []float64{70, 80, 88, 95}, want: "probe.network.risk.low,probe.network.risk.suspicious,probe.network.risk.high,probe.network.risk.very_high"},
	} {
		labels := make([]string, 0, len(test.values))
		for _, value := range test.values {
			labels = append(labels, test.fn(floatPtr(value)))
		}
		if got := strings.Join(labels, ","); got != test.want {
			t.Errorf("%s risk buckets = %q, want %q", test.name, got, test.want)
		}
	}
	for _, test := range []struct {
		level, want string
	}{
		{"low", "probe.network.risk.low"}, {"medium", "probe.network.risk.medium"}, {"high", "probe.network.risk.high"}, {"unknown", ""},
	} {
		var finding qualityFinding
		setDBIPRisk(&finding, test.level)
		if test.level == "unknown" {
			if finding.Score != nil || finding.Risk != "" {
				t.Errorf("unknown DB-IP risk = %+v", finding)
			}
		} else if finding.Score == nil || finding.Risk != test.want {
			t.Errorf("DB-IP %s risk = %+v", test.level, finding)
		}
	}
	if parseProbabilityScore("42%") == nil || *parseProbabilityScore("42%") != 42 || parseProbabilityScore("not-a-score") != nil || validScore(floatPtr(101)) != nil {
		t.Fatal("score parsing/range states failed")
	}
	if scoreLabel("0.42 (Low)") != "Low" || translateRiskLabel("Very High") != "probe.network.risk.very_high" || translateRiskLabel("custom") != networkRiskUnknown {
		t.Fatal("risk label translation failed")
	}
}

func TestIPQualityBundleTablesAndMeasurements(t *testing.T) {
	bundle := ipQualityBundle{Version: "4", Origin: originAssessment{Enabled: true, Label: "probe.network.ip_type.native", UsageCountry: "US", RegisteredCountry: "US"}, Findings: map[string]qualityFinding{}}
	for _, id := range qualitySourceOrder {
		if id != "maxmind" {
			bundle.Findings[id] = qualityFinding{ID: id}
		}
	}
	bundle.Findings["ipapi"] = qualityFinding{ID: "ipapi", Enabled: true, Access: "fixture", Country: "US", Usage: "probe.network.network_type.datacenter", Score: floatPtr(42), ScoreKind: networkScoreKindIPFraud, Risk: "probe.network.risk.low", Proxy: knownSignal(false)}
	bundle.Findings["ip2location"] = qualityFinding{ID: "ip2location", Enabled: true, Access: "fixture", Country: "US", Score: floatPtr(20), ScoreKind: networkScoreKindIP2Proxy, Risk: "probe.network.risk.low", Partial: networkPartialMultiple}
	bundle.Findings["dbip"] = qualityFinding{ID: "dbip", Enabled: true, Access: "fixture", Country: "US", Score: floatPtr(50), ScoreKind: networkScoreKindThreat, Risk: "probe.network.risk.medium"}
	bundle.Findings["ipinfo"] = qualityFinding{ID: "ipinfo", Enabled: true, Access: "fixture", Country: "US", Usage: "probe.network.network_type.residential"}
	bundle.Findings["ipqs"] = qualityFinding{ID: "ipqs", Enabled: true, Access: "fixture", Err: errors.New("fixture provider failure")}
	tables := []model.Table{bundle.typeTable(), bundle.scoreTable(), bundle.factorTable(), bundle.statusTable()}
	for _, table := range tables {
		if table.Key == "" || len(table.Columns) != len(table.ColumnKeys) || table.RowIdentity == "" || len(table.Rows) == 0 {
			t.Fatalf("invalid IP quality table = %+v", table)
		}
	}
	measurements := bundle.measurements()
	if len(measurements) != 3 || !strings.Contains(measurements[2].Display, "*") {
		t.Fatalf("IP quality measurements = %+v", measurements)
	}
	for _, measurement := range measurements {
		if measurement.HigherIsBetter == nil || *measurement.HigherIsBetter {
			t.Fatal("risk score must be lower-is-better")
		}
	}
	successful, enabled := bundle.successfulSources()
	if successful != 5 || enabled != 6 {
		t.Fatalf("source counts = %d/%d", successful, enabled)
	}
	failed := bundle.failedSourceIDs()
	partial := bundle.partialSourceIDs()
	if len(failed) != 1 || failed[0] != "ipqs" || len(partial) != 1 || partial[0] != "ip2location" {
		t.Fatalf("source summaries = failed:%v partial:%v", failed, partial)
	}
	if findingValue(bundle.Findings["ipqs"], "x") != "probe.network.status.failed" || findingValue(bundle.Findings["ipsb"], "") != "probe.network.status.disabled" || findingStatus(bundle.Findings["ip2location"]) != "probe.network.status.partial" {
		t.Fatal("finding status/value states failed")
	}
}

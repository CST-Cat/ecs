package probe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
)

func TestNetworkBundleWritesStableSourceAndScoreSemantics(t *testing.T) {
	bundle := ipQualityBundle{Version: "4", Findings: make(map[string]qualityFinding, len(qualitySourceOrder))}
	for _, id := range qualitySourceOrder {
		bundle.Findings[id] = qualityFinding{ID: id, Enabled: true, Access: networkChannelDirect}
	}
	bundle.Origin = originAssessment{Enabled: true, Label: "probe.network.ip_type.native", Access: networkChannelCommunity}
	score := 42.0
	ip2location := bundle.Findings["ip2location"]
	ip2location.Score = &score
	ip2location.ScoreKind = networkScoreKindIP2Proxy
	ip2location.Risk = "probe.network.risk.medium"
	bundle.Findings["ip2location"] = ip2location
	dbip := bundle.Findings["dbip"]
	dbip.Score = &score
	dbip.ScoreKind = networkScoreKindThreat
	dbip.Risk = "probe.network.risk.medium"
	bundle.Findings["dbip"] = dbip
	ipapi := bundle.Findings["ipapi"]
	ipapi.Usage = "probe.network.network_type.residential"
	ipapi.Company = "probe.network.network_type.business"
	ipapi.Country = "US"
	ipapi.Proxy = knownSignal(false)
	bundle.Findings["ipapi"] = ipapi

	tables := []model.Table{bundle.typeTable(), bundle.scoreTable(), bundle.factorTable(), bundle.statusTable()}
	for _, table := range tables {
		if !i18n.Has(i18n.LangZH, table.Title) || !i18n.Has(i18n.LangEN, table.Title) {
			t.Fatalf("table title is not bilingual stable key: %+v", table)
		}
		for _, column := range table.Columns {
			if !i18n.Has(i18n.LangZH, column) || !i18n.Has(i18n.LangEN, column) {
				t.Fatalf("table column is not bilingual stable key: %q", column)
			}
		}
	}

	sourceKeys := make(map[string]bool, len(qualitySourceOrder))
	for _, id := range qualitySourceOrder {
		sourceKeys[networkSourceNameKey(id)] = true
	}
	seenSourceKeys := make(map[string]bool, len(qualitySourceOrder))
	for _, table := range tables {
		if strings.HasSuffix(table.Key, ".factors") {
			continue
		}
		for _, row := range table.Rows {
			if len(row) == 0 || !sourceKeys[row[0]] {
				t.Fatalf("source row is not keyed by producer ID: table=%s row=%v", table.Key, row)
			}
			seenSourceKeys[row[0]] = true
		}
	}
	for _, id := range qualitySourceOrder {
		if !seenSourceKeys[networkSourceNameKey(id)] {
			t.Fatalf("source row for %s is missing", id)
		}
	}

	scores := bundle.scoreTable()
	foundDBIP := false
	for _, row := range scores.Rows {
		if row[0] == networkSourceNameKey("dbip") {
			foundDBIP = true
			if row[5] != "probe.network.score_band.dbip" || strings.Contains(row[5], "db-ip") {
				t.Fatalf("DB-IP score bucket = %q", row[5])
			}
		}
	}
	if !foundDBIP {
		t.Fatal("DB-IP score row is missing")
	}
	statusTable := bundle.statusTable()
	for _, id := range []string{"dbip", "ipapicom", "ipsb"} {
		found := false
		for _, row := range statusTable.Rows {
			if len(row) > 0 && row[0] == networkSourceNameKey(id) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("status row for %s is missing", id)
		}
		key := networkSourceNameKey(id)
		if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
			t.Fatalf("source-name key for %s is not bilingual: %q", id, key)
		}
	}
	for _, measurement := range bundle.measurements() {
		if !i18n.Has(i18n.LangZH, measurement.Label) || !i18n.Has(i18n.LangEN, measurement.Label) || !i18n.Has(i18n.LangZH, measurement.Rating) || !i18n.Has(i18n.LangEN, measurement.Rating) || !i18n.Has(i18n.LangZH, measurement.Method) || !i18n.Has(i18n.LangEN, measurement.Method) {
			t.Fatalf("measurement semantics are not bilingual stable keys: %+v", measurement)
		}
	}
	sources := networkFixtureResult(t).Sources
	sourceByKey := make(map[string]model.Source, len(sources))
	for _, source := range sources {
		sourceByKey[source.Name] = source
		for _, key := range []string{source.Name, source.Purpose} {
			if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
				t.Fatalf("source semantic is not bilingual stable key: %q", key)
			}
		}
	}
	for _, id := range qualitySourceOrder {
		source, ok := sourceByKey[networkSourceNameKey(id)]
		if !ok || source.URL == "" {
			t.Fatalf("provider source disclosure is missing for %s: %+v", id, source)
		}
	}
	for _, id := range []string{"ipapicom", "ipsb"} {
		if source := sourceByKey[networkSourceNameKey(id)]; source.Purpose == "" {
			t.Fatalf("provider source purpose is missing for %s", id)
		}
	}
	if got := appendPartial(networkPartialScore, networkPartialFallback); got != networkPartialMultiple || strings.ContainsAny(got, "；；") {
		t.Fatalf("partial state is not a stable finite key: %q", got)
	}
	for _, value := range []string{
		bundle.Origin.Label,
		findingValue(bundle.Findings["ipapi"], bundle.Findings["ipapi"].Usage),
		factorSignal(bundle.Findings["ipapi"], bundle.Findings["ipapi"].Proxy),
		findingStatus(bundle.Findings["ipapi"]),
		findingAccess(bundle.Findings["ipapi"]),
	} {
		for _, r := range value {
			if unicode.Is(unicode.Han, r) {
				t.Fatalf("canonical network semantic contains Han text: %q", value)
			}
		}
	}
}

func TestNetworkFiniteSemanticKeysHaveChineseAndEnglishCatalogEntries(t *testing.T) {
	keys := []string{
		networkTitleKey, networkDescriptionKey, networkMethodologyLabel, networkMethodologyProfile, networkComparisonScope,
		networkMissingValue, networkRiskUnknown, networkChannelDirect, networkChannelAPIKey, networkChannelPublicDemo,
		networkChannelTryout, networkChannelCommunity, networkChannelOfficialFree, networkChannelPublicPage,
		networkChannelJina, networkChannelJinaProxy, networkChannelMixedFallback, networkChannelFreeFallback,
		networkChannelExtendedAPIKey, networkScoreKindCompanyAbuse, networkScoreKindASNAbuse, networkScoreKindIP2Proxy,
		networkScoreKindAbuse, networkScoreKindWebFraud, networkScoreKindIPFraud, networkScoreKindThreat,
		networkPartialPrivacy, networkPartialScore, networkPartialThreat, networkPartialSecurity, networkPartialPublicFields,
		networkPartialCachedFields, networkPartialFallback, networkPartialMultiple,
		"probe.network.summary.version", "probe.network.summary.version.additional",
		"probe.network.ip_type.native", "probe.network.ip_type.broadcast", "probe.network.ip_type.unknown",
		"probe.network.boolean.yes", "probe.network.boolean.no", "probe.network.boolean.unknown",
		"probe.network.status.ok", "probe.network.status.failed", "probe.network.status.partial", "probe.network.status.disabled",
		"probe.network.risk.very_low", "probe.network.risk.low", "probe.network.risk.medium", "probe.network.risk.suspicious",
		"probe.network.risk.high", "probe.network.risk.very_high", "probe.network.table.overview", "probe.network.table.ipquality.types",
		"probe.network.table.ipquality.scores", "probe.network.table.ipquality.factors", "probe.network.table.ipquality.sources",
		"probe.network.metric.risk_score", "probe.network.note.third_party", "probe.network.note.no_upload",
		"probe.network.note.source_semantics", "probe.network.note.origin_scope", "probe.network.note.partial_sources",
		"probe.network.note.egress_lookup_failed", "probe.network.note.egress_unavailable", "probe.network.note.no_ipapi_intel",
		"probe.network.note.failed_sources", "probe.network.note.dbip_mapping", "probe.network.note.proxy_fallback",
	}
	for _, key := range keys {
		if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
			t.Fatalf("network key lacks bilingual catalog entry: %q", key)
		}
	}
	for _, id := range qualitySourceOrder {
		key := networkSourceNameKey(id)
		if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
			t.Fatalf("source name key lacks bilingual catalog entry: %q", key)
		}
	}
	for _, id := range scoreSourceOrder {
		for _, key := range []string{networkScoreBandKey(id), networkScoreMethodKey(id)} {
			if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
				t.Fatalf("score key lacks bilingual catalog entry: %q", key)
			}
		}
	}
	for _, key := range []string{
		"probe.network.field.ip_version_mode", "probe.network.field.egress", "probe.network.field.asn", "probe.network.field.route",
		"probe.network.field.location", "probe.network.field.owner", "probe.network.field.ip_type", "probe.network.field.lookup_error",
		"probe.network.field.usage_country", "probe.network.field.registered_country", "probe.network.ip_family.ipv4", "probe.network.ip_family.ipv6",
		"probe.network.methodology.engine",
	} {
		if !i18n.Has(i18n.LangZH, key) || !i18n.Has(i18n.LangEN, key) {
			t.Fatalf("field key lacks bilingual catalog entry: %q", key)
		}
	}
}

func TestNetworkRenderersLocalizeOneCanonicalFixtureWithoutMutation(t *testing.T) {
	result := networkFixtureResult(t)
	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture", Commit: "test"},
		Run:           model.RunInfo{ID: "network-fixture", Profile: "full", Exposure: "local", Offline: true, Requested: []string{"network"}},
		Results:       []model.Result{result},
		Summary:       model.Summary{Status: result.Status, OK: 1},
	}
	before, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range string(before) {
		if unicode.Is(unicode.Han, r) {
			t.Fatalf("canonical network fixture contains presentation Han text: %q", before)
		}
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 120})
		markdownOutput := report.Markdown(data, nil)
		htmlOutput, err := report.HTML(data, nil)
		if err != nil {
			t.Fatalf("HTML %s: %v", language, err)
		}
		outputs := []string{textOutput, markdownOutput, string(htmlOutput)}
		for _, output := range outputs {
			for _, prefix := range []string{"probe.network.", "module.network.title", "methodology.provider-assessment"} {
				if strings.Contains(output, prefix) {
					t.Fatalf("%s output leaked stable key %q: %s", language, prefix, output)
				}
			}
		}
		if language == i18n.LangEN {
			for _, output := range outputs {
				for _, r := range output {
					if unicode.Is(unicode.Han, r) {
						t.Fatalf("English network output contains Han text: %q", output)
					}
				}
			}
			if !strings.Contains(textOutput, "MaxMind") || !strings.Contains(markdownOutput, "DB-IP") || !strings.Contains(string(htmlOutput), "IPQuality") {
				t.Logf("text has MaxMind=%v, markdown has DB-IP=%v, html has IPQuality=%v", strings.Contains(textOutput, "MaxMind"), strings.Contains(markdownOutput, "DB-IP"), strings.Contains(string(htmlOutput), "IPQuality"))
				t.Fatalf("English source names were not localized across renderers")
			}
		}
		after, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatalf("%s rendering mutated canonical report", language)
		}
	}
}

func TestNetworkFailureKeepsRawDiagnosticWithStableStatus(t *testing.T) {
	client := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fixture provider transport")
	})}
	result := networkProbe{}.Run(context.Background(), networkFixtureEnvironment([]string{"ipinfo"}, client))
	if len(result.Failures) == 0 || !strings.Contains(result.Failures[0].Message, "fixture provider transport") {
		t.Fatalf("raw provider failure was not retained: %+v", result.Failures)
	}
	if len(result.Tables) < 4 {
		t.Fatalf("network tables = %+v", result.Tables)
	}
	status := result.Tables[len(result.Tables)-1]
	if status.Key != "network.ipquality.ipv4.sources" {
		t.Fatalf("status table = %+v", status)
	}
	for _, row := range status.Rows {
		if len(row) > 1 && row[0] == networkSourceNameKey("ipinfo") && row[1] != "probe.network.status.failed" {
			t.Fatalf("failed source status = %v", row)
		}
	}
}

func TestNetworkResultStatusTracksProviderMachineState(t *testing.T) {
	failedClient := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("fixture provider transport")
	})}
	failed := networkProbe{}.Run(context.Background(), networkFixtureEnvironment([]string{"ipinfo"}, failedClient))
	if failed.Status != model.StatusWarning {
		t.Fatalf("failed provider did not warn: status=%v failures=%+v", failed.Status, failed.Failures)
	}

	partialBody := `{"status":"success","countryCode":"US","isp":"Fixture ISP","org":"Fixture Org","proxy":false,"hosting":true,"mobile":false}`
	partialClient := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(partialBody))), nil
	})}
	partial := networkProbe{}.Run(context.Background(), networkFixtureEnvironment([]string{"ipapicom"}, partialClient))
	if partial.Status != model.StatusWarning {
		t.Fatalf("partial provider did not warn: status=%v failures=%+v", partial.Status, partial.Failures)
	}

	successBody := `{"country_code":"US","asn":{"type":"hosting"},"company":{"type":"business"},"privacy":{"proxy":false,"tor":false,"vpn":false,"hosting":true}}`
	successClient := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
		return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(successBody))), nil
	})}
	success := networkProbe{}.Run(context.Background(), networkFixtureEnvironment([]string{"ipinfo"}, successClient))
	if success.Status != model.StatusOK || len(success.Failures) != 0 {
		t.Fatalf("successful provider was marked warning: status=%v failures=%+v", success.Status, success.Failures)
	}
}

func TestNetworkStatusRowsUseFiniteMachineStates(t *testing.T) {
	originCases := []struct {
		name   string
		origin originAssessment
		want   string
	}{
		{name: "disabled takes precedence", origin: originAssessment{Err: errors.New("ignored")}, want: "probe.network.status.disabled"},
		{name: "failed", origin: originAssessment{Enabled: true, Err: errors.New("fixture")}, want: "probe.network.status.failed"},
		{name: "ok", origin: originAssessment{Enabled: true}, want: "probe.network.status.ok"},
	}
	for _, test := range originCases {
		t.Run("origin/"+test.name, func(t *testing.T) {
			if got := originStatus(test.origin); got != test.want {
				t.Fatalf("origin status = %q, want %q", got, test.want)
			}
			bundle := ipQualityBundle{Version: "4", Origin: test.origin, Findings: map[string]qualityFinding{}}
			table := bundle.statusTable()
			for _, row := range table.Rows {
				if len(row) > 1 && row[0] == networkSourceNameKey("maxmind") {
					if row[1] != test.want {
						t.Fatalf("origin row = %v, want status %q", row, test.want)
					}
					return
				}
			}
			t.Fatalf("origin row %q is missing", networkSourceNameKey("maxmind"))
		})
	}

	findingCases := []struct {
		id      string
		finding qualityFinding
		want    string
	}{
		{id: "ipinfo", finding: qualityFinding{ID: "ipinfo"}, want: "probe.network.status.disabled"},
		{id: "ipregistry", finding: qualityFinding{ID: "ipregistry", Enabled: true, Err: errors.New("fixture")}, want: "probe.network.status.failed"},
		{id: "ipapi", finding: qualityFinding{ID: "ipapi", Enabled: true, Partial: networkPartialScore}, want: "probe.network.status.partial"},
		{id: "ip2location", finding: qualityFinding{ID: "ip2location", Enabled: true}, want: "probe.network.status.ok"},
	}
	findings := make(map[string]qualityFinding, len(findingCases))
	for _, test := range findingCases {
		findings[test.id] = test.finding
	}
	bundle := ipQualityBundle{Version: "4", Findings: findings}
	table := bundle.statusTable()
	for _, test := range findingCases {
		found := false
		for _, row := range table.Rows {
			if len(row) > 1 && row[0] == networkSourceNameKey(test.id) {
				found = true
				if row[1] != test.want {
					t.Fatalf("%s row = %v, want status %q", test.id, row, test.want)
				}
				break
			}
		}
		if !found {
			t.Fatalf("finding row %q is missing", networkSourceNameKey(test.id))
		}
	}
}

func TestNetworkDualStackSummaryAndNotesAreDelimitedAndAggregated(t *testing.T) {
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "ip-api.com" {
			body := `{"status":"success","countryCode":"US","isp":"Fixture ISP","org":"Fixture Org","proxy":false,"hosting":true,"mobile":false}`
			return fixtureResponse(http.StatusOK, io.NopCloser(strings.NewReader(body))), nil
		}
		return nil, errors.New("fixture provider transport")
	})}
	result := networkProbe{}.Run(context.Background(), networkDualStackFixtureEnvironment([]string{"ipinfo", "ipapicom"}, client))
	if len(result.SummaryMessages) != 2 || result.SummaryMessages[0].Key != "probe.network.summary.version" || result.SummaryMessages[1].Key != "probe.network.summary.version.additional" {
		t.Fatalf("dual-stack summary messages = %+v", result.SummaryMessages)
	}
	if countKey(result.Notes, "probe.network.note.failed_sources") != 1 || countKey(result.Notes, "probe.network.note.partial_sources") != 1 {
		t.Fatalf("dual-stack provider notes were not aggregated: %v", result.Notes)
	}

	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "network-dual-stack", Profile: "full", Exposure: "local", Offline: true, Requested: []string{"network"}},
		Results:       []model.Result{result},
		Summary:       model.Summary{Status: result.Status},
	}
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 120})
		markdownOutput := report.Markdown(data, nil)
		htmlOutput, err := report.HTML(data, nil)
		if err != nil {
			t.Fatalf("HTML %s: %v", language, err)
		}
		separator := "；IPv6"
		if language == i18n.LangEN {
			separator = "; IPv6"
		}
		for _, output := range []string{textOutput, markdownOutput, string(htmlOutput)} {
			if !strings.Contains(output, "IPv4") || !strings.Contains(output, separator) || strings.Contains(output, "0/0IPv6") {
				t.Fatalf("%s dual-stack summary is not delimited: %q", language, output)
			}
			if strings.Contains(output, "probe.network.summary.version") || strings.Contains(output, "probe.network.summary.version.additional") {
				t.Fatalf("%s summary leaked stable key: %q", language, output)
			}
		}
	}

	single := networkFixtureResult(t)
	singleData := data
	singleData.Results = []model.Result{single}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		textOutput := report.Text(singleData, report.TextOptions{Color: termcolor.LevelNone, Width: 120})
		markdownOutput := report.Markdown(singleData, nil)
		htmlOutput, err := report.HTML(singleData, nil)
		if err != nil {
			t.Fatalf("single-stack HTML %s: %v", language, err)
		}
		for _, output := range []string{textOutput, markdownOutput, string(htmlOutput)} {
			if strings.Contains(output, "；IPv6") || strings.Contains(output, "; IPv6") {
				t.Fatalf("%s single-stack summary contains an extra family separator: %q", language, output)
			}
		}
	}
}

func countKey(values []string, key string) int {
	count := 0
	for _, value := range values {
		if value == key {
			count++
		}
	}
	return count
}

func networkFixtureResult(t *testing.T) model.Result {
	t.Helper()
	return networkProbe{}.Run(context.Background(), networkFixtureEnvironment([]string{"none"}, nil))
}

func networkDualStackFixtureEnvironment(sources []string, client *http.Client) Environment {
	env := networkFixtureEnvironment(sources, client)
	env.Config.IPVersion = config.IPVersionAuto
	ipv6 := env.Egress.ByVersion[config.IPVersion4]
	ipv6.Version = config.IPVersion6
	ipv6.IP = "2001:db8::9"
	ipv6.Intel.IP = ipv6.IP
	env.Egress.ByVersion[config.IPVersion6] = ipv6
	return env
}

func networkFixtureEnvironment(sources []string, client *http.Client) Environment {
	intel := ipAPIResponse{
		IP:           "203.0.113.9",
		CountryCode:  "US",
		ASNNum:       64500,
		ASNOrg:       "FIXTURE-AS",
		CompanyName:  "Fixture Hosting",
		IsDatacenter: true,
		IsProxy:      false,
		BooleanPresence: ipAPIBooleanPresence{
			IsDatacenter: true,
			IsProxy:      true,
			IsVPN:        true,
			IsTor:        true,
			IsAbuser:     true,
			IsCrawler:    true,
		},
	}
	intel.ASN.ASN = 64500
	intel.ASN.Organization = "FIXTURE-AS"
	intel.ASN.Type = "hosting"
	intel.Company.Name = "Fixture Hosting"
	intel.Company.Type = "hosting"
	intel.Location.Country = "United States"
	intel.Location.CountryCode = "US"
	env := Environment{
		Config: config.Runtime{
			Profile:          config.ProfileFull,
			Modules:          []string{"network"},
			Exposure:         config.ExposureLocal,
			IPVersion:        config.IPVersion4,
			IPQualitySources: sources,
			HTTPTimeout:      time.Second,
		},
		UserAgent:  "ecs-network-fixture",
		HTTPClient: client,
		Egress: Egress{ByVersion: map[string]EgressAddress{
			"4": {
				Version:        "4",
				IP:             "203.0.113.9",
				HasIntel:       true,
				IntelAttempted: true,
				Intel:          intel,
			},
		}},
	}
	return env
}

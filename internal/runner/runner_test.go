package runner

import (
	"context"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
)

type canonicalTitleProbe struct{}

func (canonicalTitleProbe) ID() string         { return "system" }
func (canonicalTitleProbe) Title() string      { return "探针自带标题" }
func (canonicalTitleProbe) NeedsNetwork() bool { return false }
func (canonicalTitleProbe) Run(context.Context, probe.Environment) model.Result {
	return model.Result{ID: "system", Title: "探针自带标题", Status: model.StatusOK}
}

func TestRunStoresCanonicalModuleTitleAcrossUILanguages(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangEN)

	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	result := runOne(context.Background(), canonicalTitleProbe{}, cfg, probe.Environment{}, false)
	want := i18n.TL(i18n.LangZH, "module.system.title")
	if result.Title != want {
		t.Fatalf("stored title = %q, want canonical %q", result.Title, want)
	}
	if localized := localizedTitle("system", result.Title); localized != i18n.T("module.system.title") {
		t.Fatalf("progress/display title = %q, want localized %q", localized, i18n.T("module.system.title"))
	}
}

func TestLocalExposureSkipsNetworkProbe(t *testing.T) {
	setNetworkCapabilityDetector(t, probe.NetworkCapabilities{IPv4Usable: true})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network"}
	cfg.Exposure = config.ExposureLocal
	report := Run(context.Background(), cfg, nil)
	if len(report.Results) != 1 || report.Results[0].Status != model.StatusSkipped {
		t.Fatalf("results = %+v", report.Results)
	}
	if report.Summary.Skipped != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if report.Results[0].Methodology.Label != "第三方评估" {
		t.Fatalf("methodology = %+v", report.Results[0].Methodology)
	}
	if report.Results[0].Methodology.Parameters["scope_revision"] == "" || report.Results[0].Methodology.Parameters["ip_version"] != cfg.IPVersion {
		t.Fatalf("machine comparison parameters = %+v", report.Results[0].Methodology.Parameters)
	}
	if evidence := report.Results[0].Evidence; evidence == nil || evidence.Valid != 0 || evidence.Expected != 1 || evidence.Unit != "module" {
		t.Fatalf("offline evidence = %+v", evidence)
	}
	if report.Run.Exposure != config.ExposureNameLocal || !report.Run.Offline {
		t.Fatalf("run info = %+v", report.Run)
	}
}

func setNetworkCapabilityDetector(t *testing.T, capabilities probe.NetworkCapabilities) {
	t.Helper()
	previous := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		return capabilities
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previous
	})
}

func setEgressDiscoverer(t *testing.T, discover func(context.Context, probe.Environment) probe.Egress) {
	t.Helper()
	previous := discoverEgress
	discoverEgress = discover
	t.Cleanup(func() {
		discoverEgress = previous
	})
}

func TestRunDetectsNetworkCapabilitiesOnceAndKeepsLocalModulesRunning(t *testing.T) {
	calls := 0
	previous := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		calls++
		return probe.NetworkCapabilities{}
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previous
	})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network", "system"}
	cfg.Exposure = config.ExposurePublic
	report := Run(context.Background(), cfg, nil)
	if calls != 1 {
		t.Fatalf("network capability detector calls = %d, want one", calls)
	}
	if len(report.Results) != 2 {
		t.Fatalf("results = %d, want network and system", len(report.Results))
	}
	var networkSkipped, systemRan bool
	for _, result := range report.Results {
		switch result.ID {
		case "network":
			networkSkipped = result.Status == model.StatusSkipped && result.Summary == "未检测到用户请求的可用 IPv4/IPv6 出站能力"
		case "system":
			systemRan = result.Status != model.StatusSkipped
		}
	}
	if !networkSkipped || !systemRan {
		t.Fatalf("network/system gating results = %+v", report.Results)
	}
}

func TestRunDoesNotDetectCapabilitiesForLocalOnlyRound(t *testing.T) {
	calls := 0
	previous := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		calls++
		return probe.NetworkCapabilities{}
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previous
	})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"system"}
	cfg.Exposure = config.ExposureLocal
	_ = Run(context.Background(), cfg, nil)
	if calls != 0 {
		t.Fatalf("local-only detector calls = %d, want zero", calls)
	}
}

func TestRunDoesNotClaimRedactionBeforeSchemaRedaction(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = nil
	cfg.Exposure = config.ExposureLocal
	cfg.Reveal = false

	raw := Run(context.Background(), cfg, nil)
	if raw.Run.Redacted {
		t.Fatal("runner returned raw diagnostics while claiming run.redacted=true")
	}
	redacted := model.RedactedCopy(raw, false)
	if !redacted.Run.Redacted {
		t.Fatal("RedactedCopy must be the boundary that sets run.redacted=true")
	}
}

func TestRunDoesNotInitializeNetworkForPublicLocalOnlyRound(t *testing.T) {
	detectorCalls := 0
	previousDetector := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		detectorCalls++
		return probe.NetworkCapabilities{}
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previousDetector
	})
	setEgressDiscoverer(t, func(context.Context, probe.Environment) probe.Egress {
		t.Fatal("public local-only round entered egress discovery")
		return probe.Egress{}
	})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"system"}
	cfg.Exposure = config.ExposurePublic
	report := Run(context.Background(), cfg, nil)
	if detectorCalls != 0 {
		t.Fatalf("public local-only detector calls = %d, want zero", detectorCalls)
	}
	if len(report.Results) != 1 || report.Results[0].ID != "system" {
		t.Fatalf("public local-only results = %+v", report.Results)
	}
}

func TestRunDoesNotDetectCapabilitiesForOfflineNetworkRound(t *testing.T) {
	calls := 0
	previous := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		calls++
		return probe.NetworkCapabilities{IPv4Usable: true, IPv6Usable: true}
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previous
	})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network", "system"}
	cfg.Exposure = config.ExposureLocal
	report := Run(context.Background(), cfg, nil)
	if calls != 0 {
		t.Fatalf("offline detector calls = %d, want zero", calls)
	}
	if report.Run.IPVersion != cfg.IPVersion {
		t.Fatalf("offline raw run IP version = %q, want %q", report.Run.IPVersion, cfg.IPVersion)
	}
	for _, result := range report.Results {
		switch result.ID {
		case "network":
			if result.Status != model.StatusSkipped || result.Summary != "离线模式" {
				t.Fatalf("offline network result = %+v", result)
			}
			if got := result.Methodology.Parameters["ip_version"]; got != cfg.IPVersion {
				t.Fatalf("offline comparison ip version = %q, want %q", got, cfg.IPVersion)
			}
		case "system":
			if result.Status == model.StatusSkipped {
				t.Fatalf("offline local result unexpectedly skipped: %+v", result)
			}
		}
	}
}

func TestRunRecordsEffectiveIPVersionButPreservesRawRequest(t *testing.T) {
	setEgressDiscoverer(t, func(context.Context, probe.Environment) probe.Egress {
		return probe.Egress{}
	})
	for _, testCase := range []struct {
		name         string
		capabilities probe.NetworkCapabilities
		effective    string
	}{
		{name: "auto to IPv4", capabilities: probe.NetworkCapabilities{IPv4Usable: true}, effective: config.IPVersion4},
		{name: "auto to IPv6", capabilities: probe.NetworkCapabilities{IPv6Usable: true}, effective: config.IPVersion6},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			setNetworkCapabilityDetector(t, testCase.capabilities)
			cfg, err := config.Defaults(config.ProfileStandard)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Modules = []string{"network"}
			cfg.Exposure = config.ExposurePublic
			cfg.IPVersion = config.IPVersionAuto
			report := Run(context.Background(), cfg, nil)
			if report.Run.IPVersion != config.IPVersionAuto {
				t.Fatalf("raw run IP version = %q, want auto", report.Run.IPVersion)
			}
			if got := report.Results[0].Methodology.Parameters["ip_version"]; got != testCase.effective {
				t.Fatalf("effective comparison ip version = %q, want %q", got, testCase.effective)
			}
		})
	}
}

func TestRunDetectsBeforeDiscoverEgressWithEffectiveEnvironment(t *testing.T) {
	events := make([]string, 0, 2)
	previousDetector := detectNetworkCapabilities
	detectNetworkCapabilities = func() probe.NetworkCapabilities {
		events = append(events, "detect")
		return probe.NetworkCapabilities{IPv4Usable: true}
	}
	t.Cleanup(func() {
		detectNetworkCapabilities = previousDetector
	})
	setEgressDiscoverer(t, func(_ context.Context, env probe.Environment) probe.Egress {
		events = append(events, "egress:"+env.Config.IPVersion)
		if !env.Network.IPv4Usable || env.Network.IPv6Usable {
			t.Fatalf("egress saw wrong capability snapshot: %+v", env.Network)
		}
		return probe.Egress{}
	})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network"}
	cfg.Exposure = config.ExposurePublic
	cfg.IPVersion = config.IPVersionAuto
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = Run(ctx, cfg, nil)
	if got := strings.Join(events, ","); got != "detect,egress:4" {
		t.Fatalf("runner stage order/effective family = %q, want detect,egress:4", got)
	}
}

func TestRunDoesNotFallbackExplicitUnavailableFamily(t *testing.T) {
	setNetworkCapabilityDetector(t, probe.NetworkCapabilities{IPv4Usable: true})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network", "system"}
	cfg.Exposure = config.ExposurePublic
	cfg.IPVersion = config.IPVersion6
	report := Run(context.Background(), cfg, nil)
	if report.Run.IPVersion != config.IPVersion6 {
		t.Fatalf("raw run IP version = %q, want 6", report.Run.IPVersion)
	}
	for _, result := range report.Results {
		if result.ID == "network" {
			if result.Status != model.StatusSkipped || result.Summary != "未检测到用户请求的可用 IPv4/IPv6 出站能力" {
				t.Fatalf("explicit unavailable network result = %+v", result)
			}
			if result.Methodology.Parameters["ip_version"] != config.IPVersion6 {
				t.Fatalf("explicit unavailable comparison ip version = %q", result.Methodology.Parameters["ip_version"])
			}
			return
		}
	}
	t.Fatal("network result not found")
}

func TestRunDoesNotFallbackExplicitUnavailableIPv4(t *testing.T) {
	setNetworkCapabilityDetector(t, probe.NetworkCapabilities{IPv6Usable: true})
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network", "system"}
	cfg.Exposure = config.ExposurePublic
	cfg.IPVersion = config.IPVersion4
	report := Run(context.Background(), cfg, nil)
	if report.Run.IPVersion != config.IPVersion4 {
		t.Fatalf("raw run IP version = %q, want 4", report.Run.IPVersion)
	}
	for _, result := range report.Results {
		if result.ID != "network" {
			continue
		}
		if result.Status != model.StatusSkipped || result.Summary != "未检测到用户请求的可用 IPv4/IPv6 出站能力" {
			t.Fatalf("explicit unavailable IPv4 result = %+v", result)
		}
		if result.Methodology.Parameters["ip_version"] != config.IPVersion4 {
			t.Fatalf("explicit unavailable IPv4 comparison ip version = %q", result.Methodology.Parameters["ip_version"])
		}
		return
	}
	t.Fatal("network result not found")
}

func TestComparisonParametersCaptureDynamicWorkloadInputs(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	result := model.Result{Fields: []model.Field{
		{Key: "version", Value: "sysbench 1.0.20"},
		{Key: "binary_sha256", Value: "abc"},
		{Key: "threads", Value: "1 / 4"},
		{Key: "duration", Value: "15s"},
		{Key: "prime", Value: "20000"},
	}}
	parameters := comparisonParameters("cpu", cfg, result)
	for key, want := range map[string]string{
		"scope_revision": "1", "configured_duration": cfg.CPUTime.String(),
		"tool_version": "sysbench 1.0.20", "tool_sha256": "abc", "threads": "1 / 4", "duration": "15s", "prime": "20000",
	} {
		if parameters[key] != want {
			t.Errorf("parameter %s = %q, want %q", key, parameters[key], want)
		}
	}

	first := comparisonParameters("dns", cfg, model.Result{})
	cfg.DNSResolvers[0].Address = "9.9.9.9:53"
	second := comparisonParameters("dns", cfg, model.Result{})
	if first["resolvers_sha256"] == second["resolvers_sha256"] {
		t.Fatal("different resolver sets produced the same comparison scope")
	}
}

func TestZstdComparisonParametersIgnoreVerifiedCorpusTemporaryPath(t *testing.T) {
	fields := func(path string) []model.Field {
		return []model.Field{
			{Key: "version", Value: "*** Zstandard CLI v1.5.7 ***"},
			{Key: "binary_sha256", Value: "binary-sha"},
			{Key: "method_version", Value: "zstd-silesia-l3-v1"},
			{Key: "compression_level", Value: "3"},
			{Key: "threads", Value: "1 / 8"},
			{Key: "duration", Value: "5s"},
			{Key: "corpus_bytes", Value: "211938580 bytes"},
			{Key: "corpus_sha256", Value: "corpus-sha"},
			{Key: "corpus_source_sha256", Value: "source-sha"},
			{Key: "arguments_1t", Value: "-q -b3 -i5 -T1 " + path},
			{Key: "arguments_nt", Value: "-q -b3 -i5 -T8 " + path},
		}
	}
	first := comparisonParameters("zstd", config.Runtime{}, model.Result{Fields: fields("/tmp/ecs-run.one/corpus")})
	second := comparisonParameters("zstd", config.Runtime{}, model.Result{Fields: fields("/tmp/ecs-run.two/corpus")})
	for _, key := range []string{"arguments_1t_sha256", "arguments_nt_sha256"} {
		if first[key] == "" || first[key] != second[key] {
			t.Fatalf("zstd comparison parameter %s changed with only the temporary corpus path: %q != %q", key, first[key], second[key])
		}
	}
	changed := fields("/tmp/ecs-run.two/corpus")
	for index := range changed {
		if changed[index].Key == "arguments_nt" {
			changed[index].Value = "-q -b3 -i5 -T7 /tmp/ecs-run.two/corpus"
		}
	}
	third := comparisonParameters("zstd", config.Runtime{}, model.Result{Fields: changed})
	if first["arguments_nt_sha256"] == third["arguments_nt_sha256"] {
		t.Fatal("zstd comparison scope ignored a changed worker argument")
	}
}

type conditionalRetryProbe struct {
	id     string
	runs   int
	result func(int) model.Result
}

func (p *conditionalRetryProbe) ID() string         { return p.id }
func (p *conditionalRetryProbe) Title() string      { return p.id }
func (p *conditionalRetryProbe) NeedsNetwork() bool { return false }
func (p *conditionalRetryProbe) Run(context.Context, probe.Environment) model.Result {
	p.runs++
	return p.result(p.runs)
}

func TestConditionalRetryRunsExactlyOnceOnlyAfterDetectedInterference(t *testing.T) {
	item := &conditionalRetryProbe{id: "cpu", result: func(run int) model.Result {
		return model.Result{
			ID: "cpu", Status: model.StatusOK, Evidence: model.NewEvidence(1, 1, "run"),
			Measurements: []model.Measurement{{Key: "rate", Value: float64(run), Method: "test-v1", HigherIsBetter: model.BoolPtr(true)}},
		}
	}}
	captures, assessments := 0, 0
	result := runWithConditionalRetryHooks(context.Background(), item, probe.Environment{},
		func() probe.EnvironmentSnapshot {
			captures++
			return probe.EnvironmentSnapshot{}
		},
		func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
			assessments++
			if assessments == 1 {
				return model.Interference{Detected: true, Score: 3, Reasons: []string{"load"}}
			}
			return model.Interference{}
		},
	)
	if item.runs != 2 || captures != 4 || assessments != 2 {
		t.Fatalf("conditional retry counts: runs=%d captures=%d assessments=%d", item.runs, captures, assessments)
	}
	if result.Retry == nil || !result.Retry.Triggered || len(result.Retry.Attempts) != 2 || result.Retry.SelectedAttempt != 2 {
		t.Fatalf("conditional retry audit = %+v", result.Retry)
	}
}

func TestConditionalRetryDoesNotExtendCleanOrInvalidRun(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		status   model.Status
		evidence *model.Evidence
		detected bool
	}{
		{name: "clean", status: model.StatusOK, evidence: model.NewEvidence(1, 1, "run")},
		{name: "no evidence", status: model.StatusWarning, evidence: model.NewEvidence(0, 1, "run"), detected: true},
		{name: "failed", status: model.StatusError, evidence: model.NewEvidence(0, 1, "run"), detected: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			item := &conditionalRetryProbe{id: "memory", result: func(int) model.Result {
				return model.Result{ID: "memory", Status: testCase.status, Evidence: testCase.evidence}
			}}
			assessments := 0
			result := runWithConditionalRetryHooks(context.Background(), item, probe.Environment{},
				func() probe.EnvironmentSnapshot { return probe.EnvironmentSnapshot{} },
				func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
					assessments++
					return model.Interference{Detected: testCase.detected, Score: 2}
				},
			)
			if item.runs != 1 || assessments != 1 || result.Retry != nil {
				t.Fatalf("run was extended: runs=%d assessments=%d retry=%+v", item.runs, assessments, result.Retry)
			}
		})
	}
}

func TestConditionalRetryNeverAppliesToOtherModules(t *testing.T) {
	item := &conditionalRetryProbe{id: "speed", result: func(int) model.Result {
		return model.Result{ID: "speed", Status: model.StatusOK, Evidence: model.NewEvidence(1, 1, "run")}
	}}
	captures := 0
	result := runWithConditionalRetryHooks(context.Background(), item, probe.Environment{},
		func() probe.EnvironmentSnapshot {
			captures++
			return probe.EnvironmentSnapshot{}
		},
		func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
			t.Fatal("non-benchmark module was assessed for retry")
			return model.Interference{}
		},
	)
	if item.runs != 1 || captures != 0 || result.Retry != nil {
		t.Fatalf("non-benchmark retry state: runs=%d captures=%d retry=%+v", item.runs, captures, result.Retry)
	}
}

package runner

import (
	"context"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/probe"
)

func TestLocalExposureSkipsNetworkProbe(t *testing.T) {
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

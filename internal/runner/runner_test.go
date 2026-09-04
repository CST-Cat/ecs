package runner

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/module"
	"ecs/internal/probe"
)

type runIDErrorReader struct {
	err error
}

func (reader runIDErrorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type runnerTestProbe struct {
	id         string
	title      string
	runs       *int
	result     model.Result
	panicValue any
}

func runnerCatalog() module.Catalog {
	catalog, err := probe.CatalogFromDefinitions(probe.BuiltinDefinitions())
	if err != nil {
		panic(err)
	}
	return catalog
}

func TestNewRunIDHas32LowercaseHexCharactersAndDoesNotRepeat(t *testing.T) {
	const count = 4096
	seen := make(map[string]struct{}, count)
	for index := 0; index < count; index++ {
		id, err := newRunID()
		if err != nil {
			t.Fatalf("newRunID() error = %v", err)
		}
		if len(id) != 32 || id != strings.ToLower(id) {
			t.Fatalf("newRunID() = %q, want 32 lowercase hex characters", id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("newRunID() = %q is not hex: %v", id, err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("newRunID() repeated identity %q at iteration %d", id, index)
		}
		seen[id] = struct{}{}
	}
}

func TestRunReturnsErrorWithoutReportWhenRandomReadFails(t *testing.T) {
	originalReader := runIDRandomReader
	randomError := errors.New("fixture random source failure")
	runIDRandomReader = runIDErrorReader{err: randomError}
	t.Cleanup(func() {
		runIDRandomReader = originalReader
	})

	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), probe.BuiltinDefinitions(), runnerCatalog(), cfg, nil)
	if !errors.Is(err, randomError) {
		t.Fatalf("Run() error = %v, want wrapped random source failure", err)
	}
	if report.Run.ID != "" || len(report.Results) != 0 {
		t.Fatalf("Run() returned a report after identity failure: %+v", report)
	}
}

func (p *runnerTestProbe) ID() string {
	if p.id != "" {
		return p.id
	}
	return p.result.ID
}

func (p *runnerTestProbe) Run(context.Context, probe.Environment) model.Result {
	if p.runs != nil {
		(*p.runs)++
	}
	if p.panicValue != nil {
		panic(p.panicValue)
	}
	result := p.result
	if result.ID == "" {
		result.ID = p.ID()
	}
	if result.Title == "" {
		result.Title = p.title
		if result.Title == "" {
			result.Title = p.ID()
		}
	}
	return result
}

func TestRunDefinitionKeepsCanonicalMachineMetadata(t *testing.T) {
	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := runnerCatalog().Lookup("system")
	if !ok {
		t.Fatal("system descriptor missing")
	}
	runs := 0
	item := &runnerTestProbe{
		id: "system", title: "probe title", runs: &runs,
		result: model.Result{
			ID: "system", Status: model.StatusOK,
			Methodology: model.Methodology{
				Kind:       "fixture",
				Label:      "probe methodology",
				Parameters: map[string]string{"scope_revision": "producer", "owned": "producer"},
			},
			Evidence: model.NewEvidence(1, 2, "sample"),
		},
	}
	first := runDefinition(context.Background(), probe.Definition{Descriptor: descriptor, Probe: item}, cfg, probe.Environment{}, true)
	second := runDefinition(context.Background(), probe.Definition{Descriptor: descriptor, Probe: item}, cfg, probe.Environment{}, true)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("runner result changed between identical machine runs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Title != descriptor.TitleKey || first.Methodology.Label != "probe methodology" || first.Evidence == nil || first.Evidence.Valid != 1 || first.Evidence.Expected != 2 {
		t.Fatalf("canonical result metadata = %+v", first)
	}
	wantParameters := map[string]string{"scope_revision": "producer", "owned": "producer"}
	if !reflect.DeepEqual(first.Methodology.Parameters, wantParameters) || runs != 2 {
		t.Fatalf("runner parameters/runs = %v/%d, want producer-owned parameters", first.Methodology.Parameters, runs)
	}
}

func TestRunDefinitionNormalizesMalformedEvidence(t *testing.T) {
	descriptor, ok := runnerCatalog().Lookup("system")
	if !ok {
		t.Fatal("system descriptor missing")
	}
	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	got := runDefinition(context.Background(), probe.Definition{
		Descriptor: descriptor,
		Probe: &runnerTestProbe{result: model.Result{
			Status:   model.StatusOK,
			Evidence: &model.Evidence{Valid: 4, Expected: 2, Unit: "sample"},
		}},
	}, cfg, probe.Environment{}, true)
	if got.Evidence == nil || got.Evidence.Valid != 2 || got.Evidence.Expected != 2 || got.Evidence.Unit != "sample" {
		t.Fatalf("runner did not normalize malformed evidence: %+v", got.Evidence)
	}
}

func TestRunDefinitionSkipAndEvidenceFallback(t *testing.T) {
	descriptor, _ := runnerCatalog().Lookup("network")
	localDescriptor, _ := runnerCatalog().Lookup("system")
	defaultConfig, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name            string
		descriptor      module.Descriptor
		configExposure  module.Exposure
		networkRunnable bool
		status          model.Status
		wantStatus      model.Status
		wantSummaryKey  string
		wantValid       int
		wantRuns        int
		wantMethodology bool
		wantFailure     bool
	}{
		{name: "offline network", descriptor: descriptor, configExposure: module.ExposureLocal, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusSkipped, wantSummaryKey: "message.runner.skip.offline", wantValid: 0},
		{name: "unavailable family", descriptor: descriptor, configExposure: module.ExposureThirdParty, networkRunnable: false, status: model.StatusOK, wantStatus: model.StatusSkipped, wantSummaryKey: "message.runner.skip.noRequestedIP", wantValid: 0},
		{name: "network executes", descriptor: descriptor, configExposure: module.ExposureThirdParty, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusOK, wantValid: 1, wantRuns: 1},
		{name: "ok fallback", descriptor: localDescriptor, configExposure: module.ExposureLocal, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusOK, wantValid: 1, wantRuns: 1, wantMethodology: true},
		{name: "warning fallback", descriptor: localDescriptor, configExposure: module.ExposureLocal, networkRunnable: true, status: model.StatusWarning, wantStatus: model.StatusWarning, wantValid: 1, wantRuns: 1},
		{name: "error fallback", descriptor: localDescriptor, configExposure: module.ExposureLocal, networkRunnable: true, status: model.StatusError, wantStatus: model.StatusError, wantValid: 0, wantRuns: 1, wantFailure: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultConfig
			cfg.Exposure = test.configExposure
			runs := 0
			item := &runnerTestProbe{id: test.descriptor.ID, runs: &runs, result: model.Result{Status: test.status}}
			if test.status == model.StatusError {
				item.result.AddFailure(model.Failure{Category: model.FailureUnknown, Stage: "fixture", Target: test.descriptor.ID, Message: "fixture failure"})
			}
			got := runDefinition(context.Background(), probe.Definition{Descriptor: test.descriptor, Probe: item}, cfg, probe.Environment{}, test.networkRunnable)
			if got.Status != test.wantStatus || got.Evidence == nil || got.Evidence.Valid != test.wantValid || got.Evidence.Expected != 1 || runs != test.wantRuns {
				t.Fatalf("result = %+v, want status=%s evidence=%d/1", got, test.wantStatus, test.wantValid)
			}
			if test.wantSummaryKey != "" {
				if len(got.SummaryMessages) != 1 || got.SummaryMessages[0].Key != test.wantSummaryKey {
					t.Fatalf("structured skip summary = messages %+v, want %q", got.SummaryMessages, test.wantSummaryKey)
				}
			}
			if test.wantMethodology && got.Methodology.Label != test.descriptor.Methodology.Label {
				t.Fatalf("methodology = %+v, want descriptor metadata", got.Methodology)
			}
			if test.wantFailure && (len(got.Failures) == 0 || got.Failures[0].Stage != "fixture" || got.Failures[0].Target != test.descriptor.ID || got.Failures[0].Category == "" || !strings.Contains(got.Failures[0].Message, "fixture failure")) {
				t.Fatalf("error diagnostics = %+v", got.Failures)
			}
		})
	}
}

func TestRunDefinitionPreservesWarningFailureOwnership(t *testing.T) {
	descriptor, ok := runnerCatalog().Lookup("system")
	if !ok {
		t.Fatal("system descriptor missing")
	}
	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name   string
		result model.Result
	}{
		{name: "error without structured failure", result: model.Result{ID: "system", Status: model.StatusError}},
		{name: "warning natural language note", result: model.Result{ID: "dns", Status: model.StatusWarning, Notes: []string{"invalid JSON response"}}},
		{name: "warning stable key note", result: model.Result{ID: "route", Status: model.StatusWarning, Notes: []string{"probe.route.note.parse_failed"}}},
		{name: "warning without error or note", result: model.Result{ID: "latency", Status: model.StatusWarning}},
		{name: "warning finding", result: model.Result{ID: "nat", Status: model.StatusWarning}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := runDefinition(context.Background(), probe.Definition{
				Descriptor: descriptor,
				Probe:      &runnerTestProbe{result: test.result},
			}, cfg, probe.Environment{}, true)
			if len(got.Failures) != 0 {
				t.Fatalf("semantic status gained inferred failure: %+v", got.Failures)
			}
		})
	}

	wantFailure := model.Failure{
		Category: model.FailureDNS, Stage: "query", Target: "fixture", Retryable: true, Count: 1, Message: "fixture DNS failure",
	}
	owned := runDefinition(context.Background(), probe.Definition{
		Descriptor: descriptor,
		Probe: &runnerTestProbe{result: model.Result{
			Status:   model.StatusWarning,
			Notes:    []string{"probe.foo.timeout"},
			Failures: []model.Failure{wantFailure},
		}},
	}, cfg, probe.Environment{}, true)
	if !reflect.DeepEqual(owned.Failures, []model.Failure{wantFailure}) {
		t.Fatalf("runner changed producer-owned warning failure: got %+v want %+v", owned.Failures, []model.Failure{wantFailure})
	}

	wantErrorFailure := model.Failure{Category: model.FailurePermissionDenied, Count: 2}
	ownedError := runDefinition(context.Background(), probe.Definition{
		Descriptor: descriptor,
		Probe: &runnerTestProbe{result: model.Result{
			Status:   model.StatusError,
			Failures: []model.Failure{wantErrorFailure},
		}},
	}, cfg, probe.Environment{}, true)
	if !reflect.DeepEqual(ownedError.Failures, []model.Failure{wantErrorFailure}) {
		t.Fatalf("runner changed producer-owned error failure: got %+v want %+v", ownedError.Failures, []model.Failure{wantErrorFailure})
	}
}

func TestSafeRunIsolatePanic(t *testing.T) {
	panicItem := &runnerTestProbe{id: "panic-probe", title: "panic probe", panicValue: "fixture panic"}
	panicResult := safeRun(context.Background(), panicItem, probe.Environment{})
	if panicResult.Status != model.StatusError || !reflect.DeepEqual(panicResult.SummaryMessages, []model.Message{model.NewMessage("message.runner.panic")}) {
		t.Fatalf("panic result = %+v", panicResult)
	}
	if len(panicResult.Failures) != 1 || panicResult.Failures[0].Stage != "panic" || panicResult.Failures[0].Target != "panic-probe" || panicResult.Failures[0].Category != model.FailureUnknown || panicResult.Failures[0].Message != "fixture panic" {
		t.Fatalf("panic failure = %+v", panicResult.Failures)
	}
}

func TestRunDefinitionRunsOrdinaryModuleOnceWithoutSnapshots(t *testing.T) {
	descriptor, ok := runnerCatalog().Lookup("system")
	if !ok {
		t.Fatal("system descriptor missing")
	}
	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}

	originalCapture := captureEnvironmentSnapshot
	originalBuilder := buildBenchmarkWindowMeasurements
	t.Cleanup(func() {
		captureEnvironmentSnapshot = originalCapture
		buildBenchmarkWindowMeasurements = originalBuilder
	})
	snapshots, builders := 0, 0
	captureEnvironmentSnapshot = func() probe.EnvironmentSnapshot {
		snapshots++
		return probe.EnvironmentSnapshot{}
	}
	buildBenchmarkWindowMeasurements = func(probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) []model.Measurement {
		builders++
		return []model.Measurement{{Key: "unexpected-window"}}
	}

	runs := 0
	got := runDefinition(context.Background(), probe.Definition{
		Descriptor: descriptor,
		Probe: &runnerTestProbe{id: "system", runs: &runs, result: model.Result{
			Status:       model.StatusOK,
			Measurements: []model.Measurement{{Key: "ordinary-measurement"}},
			Evidence:     model.NewEvidence(1, 1, "sample"),
		}},
	}, cfg, probe.Environment{}, true)
	if runs != 1 || snapshots != 0 || builders != 0 {
		t.Fatalf("ordinary module execution = runs %d/snapshots %d/builders %d, want 1/0/0", runs, snapshots, builders)
	}
	if len(got.Measurements) != 1 || got.Measurements[0].Key != "ordinary-measurement" {
		t.Fatalf("ordinary module gained window measurements: %+v", got.Measurements)
	}
}

func TestRunDefinitionAppendsOneBenchmarkWindowWithoutChangingResult(t *testing.T) {
	descriptor, ok := runnerCatalog().Lookup("cpu")
	if !ok {
		t.Fatal("cpu descriptor missing")
	}
	cfg, err := config.Defaults(runnerCatalog(), config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}

	originalCapture := captureEnvironmentSnapshot
	originalBuilder := buildBenchmarkWindowMeasurements
	t.Cleanup(func() {
		captureEnvironmentSnapshot = originalCapture
		buildBenchmarkWindowMeasurements = originalBuilder
	})

	for _, test := range []struct {
		name       string
		beforeLoad float64
		afterLoad  float64
		display    string
	}{
		{name: "low", beforeLoad: 0.1, afterLoad: 0.2, display: "0.10"},
		{name: "zero", beforeLoad: 0, afterLoad: 0, display: "0.00"},
		{name: "high", beforeLoad: 100, afterLoad: 100.1, display: "100.00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := probe.EnvironmentSnapshot{CapturedAt: time.Unix(100, 0), Load1: test.beforeLoad, LoadKnown: true}
			after := probe.EnvironmentSnapshot{CapturedAt: time.Unix(101, 0), Load1: test.afterLoad, LoadKnown: true}
			runs, snapshots, assemblies := 0, 0, 0
			captureEnvironmentSnapshot = func() probe.EnvironmentSnapshot {
				snapshots++
				switch snapshots {
				case 1:
					return before
				case 2:
					return after
				default:
					t.Fatalf("benchmark captured snapshot %d, want exactly two", snapshots)
					return probe.EnvironmentSnapshot{}
				}
			}
			buildBenchmarkWindowMeasurements = func(gotBefore, gotAfter probe.EnvironmentSnapshot) []model.Measurement {
				assemblies++
				if !reflect.DeepEqual(gotBefore, before) || !reflect.DeepEqual(gotAfter, after) {
					t.Fatalf("window assembly snapshots = before %+v/after %+v, want injected before %+v/after %+v", gotBefore, gotAfter, before, after)
				}
				return benchmarkWindowMeasurements(gotBefore, gotAfter)
			}

			wantSummary := []model.Message{model.NewMessage("fixture.benchmark.summary")}
			wantFields := []model.Field{{Key: "benchmark-field", Label: "Benchmark field", Value: model.RawValue("kept")}}
			wantTables := []model.Table{{Key: "benchmark-table", Rows: [][]model.Value{{model.RawValue("kept")}}}}
			wantNotes := []string{"fixture.benchmark.note"}
			wantEvidence := model.NewEvidence(2, 2, "run")
			runsResult := model.Result{
				Status:          model.StatusOK,
				SummaryMessages: wantSummary,
				Fields:          wantFields,
				Measurements:    []model.Measurement{{Key: "benchmark-measurement"}},
				Tables:          wantTables,
				Notes:           wantNotes,
				Evidence:        wantEvidence,
			}
			got := runDefinition(context.Background(), probe.Definition{
				Descriptor: descriptor,
				Probe:      &runnerTestProbe{id: "cpu", runs: &runs, result: runsResult},
			}, cfg, probe.Environment{}, true)
			if runs != 1 {
				t.Fatalf("%s window probe runs = %d, want exactly one run", test.name, runs)
			}
			if snapshots != 2 {
				t.Fatalf("%s window snapshot captures = %d, want exactly two", test.name, snapshots)
			}
			if assemblies != 1 {
				t.Fatalf("%s window measurement assemblies = %d, want exactly one", test.name, assemblies)
			}
			if len(got.Measurements) != 2 || got.Measurements[0].Key != "benchmark-measurement" || got.Measurements[1].Key != "pretest_load_1m" || got.Measurements[1].Value != test.beforeLoad || got.Measurements[1].Display.Text() != test.display {
				t.Fatalf("%s benchmark/window measurements = %+v, want original plus one %s snapshot-derived window value", test.name, got.Measurements, test.name)
			}
			if got.Status != model.StatusOK {
				t.Fatalf("%s window value changed status: got %s, want %s", test.name, got.Status, model.StatusOK)
			}
			if !reflect.DeepEqual(got.SummaryMessages, wantSummary) || !reflect.DeepEqual(got.Fields, wantFields) || !reflect.DeepEqual(got.Tables, wantTables) || !reflect.DeepEqual(got.Notes, wantNotes) || !reflect.DeepEqual(got.Evidence, wantEvidence) {
				t.Fatalf("%s benchmark result metadata changed: %+v", test.name, got)
			}
		})
	}
}

func TestBenchmarkWindowMeasurementsPreserveRawPretestLoad(t *testing.T) {
	before := probe.EnvironmentSnapshot{
		CapturedAt: time.Unix(100, 0),
		Load1:      123,
		LoadKnown:  true,
	}
	after := probe.EnvironmentSnapshot{CapturedAt: time.Unix(101, 0)}
	got := benchmarkWindowMeasurements(before, after)
	if len(got) != 1 || got[0].Key != "pretest_load_1m" || got[0].Value != 123 || got[0].Label != "probe.pressure.metric.pretest_load_1m" {
		t.Fatalf("pretest load measurement = %+v, want one raw measurement", got)
	}
}

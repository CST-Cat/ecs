package runner

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/probe"
)

type runnerTestProbe struct {
	id         string
	title      string
	network    bool
	runs       *int
	result     model.Result
	panicValue any
}

func (p *runnerTestProbe) ID() string {
	if p.id != "" {
		return p.id
	}
	return p.result.ID
}

func (p *runnerTestProbe) Title() string {
	if p.title != "" {
		return p.title
	}
	return p.ID()
}

func (p *runnerTestProbe) NeedsNetwork() bool { return p.network }

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
		result.Title = p.Title()
	}
	return result
}

func TestRunBindingKeepsCanonicalMachineMetadata(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := config.ModuleDescriptorFor("system")
	if !ok {
		t.Fatal("system descriptor missing")
	}
	runs := 0
	item := &runnerTestProbe{
		id: "system", title: "probe title", runs: &runs,
		result: model.Result{
			ID: "system", Status: model.StatusOK,
			Methodology: model.Methodology{Kind: "fixture", Label: "probe methodology"},
			Evidence:    model.NewEvidence(1, 2, "sample"),
		},
	}
	first := runBinding(context.Background(), moduleBinding{Descriptor: descriptor, Probe: item}, cfg, probe.Environment{}, true)
	second := runBinding(context.Background(), moduleBinding{Descriptor: descriptor, Probe: item}, cfg, probe.Environment{}, true)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("runner result changed between identical machine runs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.Title != "probe title" || first.Methodology.Label != "probe methodology" || first.Evidence == nil || first.Evidence.Valid != 1 || first.Evidence.Expected != 2 {
		t.Fatalf("canonical result metadata = %+v", first)
	}
	if first.Methodology.Parameters["scope_revision"] == "" || runs != 2 {
		t.Fatalf("runner parameters/runs = %v/%d", first.Methodology.Parameters, runs)
	}
}

func TestRunBindingSkipAndEvidenceFallback(t *testing.T) {
	descriptor, _ := config.ModuleDescriptorFor("network")
	localDescriptor, _ := config.ModuleDescriptorFor("system")
	defaultConfig, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name            string
		descriptor      config.ModuleDescriptor
		configExposure  config.Exposure
		networkRunnable bool
		status          model.Status
		wantStatus      model.Status
		wantSummaryKey  string
		wantValid       int
		wantRuns        int
		wantMethodology bool
		wantFailure     bool
	}{
		{name: "offline network", descriptor: descriptor, configExposure: config.ExposureLocal, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusSkipped, wantSummaryKey: "message.runner.skip.offline", wantValid: 0},
		{name: "unavailable family", descriptor: descriptor, configExposure: config.ExposureThirdParty, networkRunnable: false, status: model.StatusOK, wantStatus: model.StatusSkipped, wantSummaryKey: "message.runner.skip.noRequestedIP", wantValid: 0},
		{name: "network executes", descriptor: descriptor, configExposure: config.ExposureThirdParty, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusOK, wantValid: 1, wantRuns: 1},
		{name: "ok fallback", descriptor: localDescriptor, configExposure: config.ExposureLocal, networkRunnable: true, status: model.StatusOK, wantStatus: model.StatusOK, wantValid: 1, wantRuns: 1, wantMethodology: true},
		{name: "warning fallback", descriptor: localDescriptor, configExposure: config.ExposureLocal, networkRunnable: true, status: model.StatusWarning, wantStatus: model.StatusWarning, wantValid: 1, wantRuns: 1},
		{name: "error fallback", descriptor: localDescriptor, configExposure: config.ExposureLocal, networkRunnable: true, status: model.StatusError, wantStatus: model.StatusError, wantValid: 0, wantRuns: 1, wantFailure: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := defaultConfig
			cfg.Exposure = test.configExposure
			runs := 0
			item := &runnerTestProbe{id: test.descriptor.ID, runs: &runs, result: model.Result{Status: test.status}}
			if test.status == model.StatusError {
				item.result.Error = "fixture failure"
			}
			got := runBinding(context.Background(), moduleBinding{Descriptor: test.descriptor, Probe: item}, cfg, probe.Environment{}, test.networkRunnable)
			if got.Status != test.wantStatus || got.Evidence == nil || got.Evidence.Valid != test.wantValid || got.Evidence.Expected != 1 || runs != test.wantRuns {
				t.Fatalf("result = %+v, want status=%s evidence=%d/1", got, test.wantStatus, test.wantValid)
			}
			if test.wantSummaryKey != "" {
				if got.Summary != "" || len(got.SummaryMessages) != 1 || got.SummaryMessages[0].Key != test.wantSummaryKey {
					t.Fatalf("structured skip summary = summary %q messages %+v, want %q", got.Summary, got.SummaryMessages, test.wantSummaryKey)
				}
			}
			if test.wantMethodology && got.Methodology.Label != test.descriptor.Methodology.Label {
				t.Fatalf("methodology = %+v, want descriptor metadata", got.Methodology)
			}
			if test.wantFailure && (len(got.Failures) == 0 || got.Failures[0].Stage != "module" || got.Failures[0].Category == "" || !strings.Contains(got.Failures[0].Message, "fixture failure")) {
				t.Fatalf("error diagnostics = %+v", got.Failures)
			}
		})
	}
}

func TestRunOneAndSafeRunIsolatePanic(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	item := &runnerTestProbe{id: "custom", title: "custom probe", runs: &runs, result: model.Result{Status: model.StatusOK, Summary: "ok"}}
	result := runOne(context.Background(), item, cfg, probe.Environment{}, true)
	if runs != 1 || result.ID != "custom" || result.Status != model.StatusOK || result.Summary != "ok" || result.Evidence == nil || result.Evidence.Valid != 1 {
		t.Fatalf("runOne result = %+v, runs=%d", result, runs)
	}

	panicItem := &runnerTestProbe{id: "panic-probe", title: "panic probe", panicValue: "fixture panic"}
	panicResult := safeRun(context.Background(), panicItem, probe.Environment{})
	if panicResult.Status != model.StatusError || panicResult.Error != "fixture panic" || panicResult.Summary != "" || !reflect.DeepEqual(panicResult.SummaryMessages, []model.Message{model.NewMessage("message.runner.panic")}) {
		t.Fatalf("panic result = %+v", panicResult)
	}
	if len(panicResult.Failures) != 1 || panicResult.Failures[0].Stage != "panic" || panicResult.Failures[0].Target != "panic-probe" || panicResult.Failures[0].Category != model.FailureUnknown || panicResult.Failures[0].Message != "fixture panic" {
		t.Fatalf("panic failure = %+v", panicResult.Failures)
	}
}

func TestConditionalRetryOrchestration(t *testing.T) {
	baseResult := model.Result{Status: model.StatusOK, Evidence: model.NewEvidence(1, 1, "sample")}
	nonBenchmarkRuns := 0
	nonBenchmarkCaptures := 0
	nonBenchmarkAssesses := 0
	nonBenchmark := &runnerTestProbe{id: "network", runs: &nonBenchmarkRuns, result: baseResult}
	_ = runWithConditionalRetryHooks(context.Background(), nonBenchmark, false, probe.Environment{}, func() probe.EnvironmentSnapshot {
		nonBenchmarkCaptures++
		return probe.EnvironmentSnapshot{}
	}, func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
		nonBenchmarkAssesses++
		return model.Interference{Detected: true}
	})
	if nonBenchmarkRuns != 1 || nonBenchmarkCaptures != 0 || nonBenchmarkAssesses != 0 {
		t.Fatalf("non-benchmark retry hooks = runs %d/captures %d/assesses %d", nonBenchmarkRuns, nonBenchmarkCaptures, nonBenchmarkAssesses)
	}

	noInterferenceRuns, captures, assesses := 0, 0, 0
	noInterference := &runnerTestProbe{id: "fixture", runs: &noInterferenceRuns, result: baseResult}
	noInterferenceResult := runWithConditionalRetryHooks(context.Background(), noInterference, true, probe.Environment{}, func() probe.EnvironmentSnapshot {
		captures++
		return probe.EnvironmentSnapshot{}
	}, func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
		assesses++
		return model.Interference{}
	})
	if noInterferenceRuns != 1 || captures != 2 || assesses != 1 || noInterferenceResult.Retry != nil {
		t.Fatalf("no-interference retry = result %+v/runs %d/captures %d/assesses %d", noInterferenceResult, noInterferenceRuns, captures, assesses)
	}

	rejected := []struct {
		name     string
		status   model.Status
		evidence *model.Evidence
		canceled bool
	}{
		{name: "canceled context", status: model.StatusOK, evidence: model.NewEvidence(1, 1, "sample"), canceled: true},
		{name: "error result", status: model.StatusError, evidence: model.NewEvidence(1, 1, "sample")},
		{name: "skipped result", status: model.StatusSkipped, evidence: model.NewEvidence(1, 1, "sample")},
		{name: "no valid evidence", status: model.StatusOK, evidence: model.NewEvidence(0, 1, "sample")},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			runs := 0
			item := &runnerTestProbe{id: "cpu", runs: &runs, result: model.Result{Status: test.status, Evidence: test.evidence}}
			captures, assesses := 0, 0
			got := runWithConditionalRetryHooks(ctx, item, true, probe.Environment{}, func() probe.EnvironmentSnapshot {
				captures++
				return probe.EnvironmentSnapshot{}
			}, func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
				assesses++
				return model.Interference{Detected: true, Score: 4, Reasons: []model.Message{model.NewMessage("fixture.interference")}}
			})
			if runs != 1 || captures != 2 || assesses != 1 || got.Retry != nil {
				t.Fatalf("rejected retry = %+v/runs %d/captures %d/assesses %d", got, runs, captures, assesses)
			}
		})
	}

	for _, test := range []struct {
		firstScore, secondScore, selected int
		wantReasonKey                     string
	}{{5, 1, 2, "fixture.second"}, {1, 5, 1, "fixture.first"}} {
		runs, captures, assesses := 0, 0, 0
		item := &runnerTestProbe{id: "cpu", runs: &runs, result: baseResult}
		got := runWithConditionalRetryHooks(context.Background(), item, true, probe.Environment{}, func() probe.EnvironmentSnapshot {
			captures++
			return probe.EnvironmentSnapshot{}
		}, func(string, probe.EnvironmentSnapshot, probe.EnvironmentSnapshot) model.Interference {
			assesses++
			if assesses == 1 {
				return model.Interference{Detected: true, Score: test.firstScore, Reasons: []model.Message{model.NewMessage("fixture.first")}}
			}
			return model.Interference{Detected: true, Score: test.secondScore, Reasons: []model.Message{model.NewMessage("fixture.second")}}
		})
		if runs != 2 || captures != 4 || assesses != 2 || got.Retry == nil || got.Retry.SelectedAttempt != test.selected || got.Retry.TriggerReasons[0].Key != "fixture.first" || got.Interference == nil || got.Interference.Score != map[int]int{1: test.firstScore, 2: test.secondScore}[test.selected] || len(got.Notes) != 0 || len(got.Fields) != 0 || len(got.Tables) != 0 || got.Interference.Reasons[0].Key != test.wantReasonKey {
			t.Fatalf("retry result = %+v/runs %d/captures %d/assesses %d", got, runs, captures, assesses)
		}
	}
}

package score

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func copySubmission(value Submission) Submission {
	metrics := value.Metrics
	value.Metrics = make(map[string]float64, len(value.Metrics))
	for key, metric := range metrics {
		value.Metrics[key] = metric
	}
	return value
}

func TestSubmissionBuildWhitelistFingerprintAndRoundTrip(t *testing.T) {
	report := scoreReportFixture()
	system := findResult(&report, "system")
	system.Fields = append(system.Fields,
		model.Field{Key: "public_ip", Value: model.RawValue("203.0.113.10")},
	)
	provider, region := ExtractSubmissionMetadata(report)
	if provider != "fixture-cloud" || region != "fixture-region" {
		t.Fatalf("safe report metadata = %q/%q", provider, region)
	}
	submission, err := BuildSubmission(report, SubmissionOptions{
		Region:   "us\x00-west",
		Provider: "Fixture Cloud",
		Note:     "diagnostic\x00 fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if submission.Schema != SubmissionSchema || len(submission.ID) != 32 || submission.ID != strings.ToLower(submission.ID) || submission.SampleID != "389beaef109ed18ac4db40b5cb4cf81fe140f625251cc054a9c5f9c527c49ad5" || submission.Host.VCPU != 4 || submission.Host.MemoryGiB != 8 {
		t.Fatalf("submission metadata = %+v", submission)
	}
	if _, err := hex.DecodeString(submission.ID); err != nil {
		t.Fatalf("submission ID %q is not lowercase hex: %v", submission.ID, err)
	}
	if submission.Host.Region != "us-west" || submission.Host.Provider != "Fixture Cloud" || submission.Note != "diagnostic fixture" {
		t.Fatalf("sanitized metadata = %+v", submission)
	}
	if submission.MemoryBackend != memoryBackendStream || submission.Tool.ECS != "ecs-test" || submission.Tool.Sysbench != "sysbench 1.0.20" || submission.Tool.Fio != "fio-3.35" || submission.Tool.IPerf3 != "iperf 3.12" {
		t.Fatalf("submission whitelist metadata = %+v", submission)
	}
	if len(submission.Metrics) < len(Dimensions()) || len(submission.SampleID) != 64 || strings.Trim(submission.SampleID, "0123456789abcdef") != "" || !strings.Contains(submission.FileName(), "fixture-cloud-us-west.json") {
		t.Fatalf("submission metrics/filename = %d/%q", len(submission.Metrics), submission.FileName())
	}
	encoded, err := submission.Encode()
	if err != nil || len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		t.Fatalf("submission encode = %v", err)
	}
	encodedText := string(encoded)
	if !strings.Contains(encodedText, `"sample_id":`) || strings.Contains(encodedText, `"fingerprint_version":`) || strings.Contains(encodedText, report.Run.ID) {
		t.Fatalf("submission identity encoding = %s", encodedText)
	}
	if strings.Contains(encodedText, "public_ip") {
		t.Fatal("submission encoded non-whitelisted report fields")
	}

	directory := t.TempDir()
	path := filepath.Join(directory, submission.FileName())
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSubmission(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != submission.ID || loaded.Host != submission.Host || loaded.Note != submission.Note {
		t.Fatalf("loaded submission = %+v", loaded)
	}
	if loaded.SampleID != submission.SampleID {
		t.Fatalf("loaded sample ID = %q, want %q", loaded.SampleID, submission.SampleID)
	}
	baseline, err := BuildBaseline([]model.Report{loaded.AsReport()}, "submission fixture")
	if err != nil || baseline.Metrics["cpu_single"] != 150 {
		t.Fatalf("submission baseline = %v/%v", baseline.Metrics, err)
	}
	projected := loaded.AsReport()
	if len(projected.Results) == 0 || len(projected.Results[0].Fields) != 2 ||
		projected.Results[0].Fields[0].Label != "cloud_provider" || projected.Results[0].Fields[1].Label != "cloud_region" {
		t.Fatalf("submission report metadata labels = %+v", projected.Results)
	}

	unchanged := copySubmission(submission)
	unchanged.RanAt = unchanged.RanAt.Add(24 * time.Hour)
	if unchanged.fingerprint() != submission.ID {
		t.Fatal("fingerprint changed with RanAt")
	}
	for _, options := range []SubmissionOptions{
		{Region: "different-region"},
		{Provider: "different-provider"},
		{Note: "different note"},
	} {
		changedMetadata, err := BuildSubmission(report, options)
		if err != nil {
			t.Fatal(err)
		}
		if changedMetadata.SampleID != submission.SampleID || changedMetadata.ID == submission.ID {
			t.Fatalf("metadata identity = sample %q/%q, artifact %q/%q", changedMetadata.SampleID, submission.SampleID, changedMetadata.ID, submission.ID)
		}
	}
	metadataReport := scoreReportFixture()
	findResult(&metadataReport, "system").Fields[2].Value = model.RawValue("changed-virtualization")
	changedMetadata, err := BuildSubmission(metadataReport, SubmissionOptions{
		Region: "us-west", Provider: "Fixture Cloud", Note: "diagnostic fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changedMetadata.SampleID != submission.SampleID || changedMetadata.ID == submission.ID {
		t.Fatalf("report metadata identity = sample %q/%q, artifact %q/%q", changedMetadata.SampleID, submission.SampleID, changedMetadata.ID, submission.ID)
	}
	metricReport := scoreReportFixture()
	if !setReportMeasurement(&metricReport, "sysbench_cpu_single_events_s", 151) {
		t.Fatal("score fixture CPU measurement missing")
	}
	changedMetric, err := BuildSubmission(metricReport, SubmissionOptions{
		Region: "us-west", Provider: "Fixture Cloud", Note: "diagnostic fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changedMetric.SampleID != submission.SampleID || changedMetric.ID == submission.ID {
		t.Fatalf("report metric identity = sample %q/%q, artifact %q/%q", changedMetric.SampleID, submission.SampleID, changedMetric.ID, submission.ID)
	}
	changed := copySubmission(submission)
	changed.Metrics["cpu_single"]++
	if changed.fingerprint() == submission.ID {
		t.Fatal("fingerprint ignored a public metric change")
	}
	if err := changed.Validate(); err == nil {
		t.Fatal("changed metric passed validation")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Submission)
	}{
		{name: "host vcpu", mutate: func(value *Submission) { value.Host.VCPU++ }},
		{name: "host memory", mutate: func(value *Submission) { value.Host.MemoryGiB++ }},
		{name: "host cpu model", mutate: func(value *Submission) { value.Host.CPUModel += " changed" }},
		{name: "host virtualization", mutate: func(value *Submission) { value.Host.Virtualization += " changed" }},
		{name: "host architecture", mutate: func(value *Submission) { value.Host.Arch += "-changed" }},
		{name: "host region", mutate: func(value *Submission) { value.Host.Region += "-changed" }},
		{name: "host provider", mutate: func(value *Submission) { value.Host.Provider += "-changed" }},
		{name: "tool ecs", mutate: func(value *Submission) { value.Tool.ECS += "-changed" }},
		{name: "tool sysbench", mutate: func(value *Submission) { value.Tool.Sysbench += "-changed" }},
		{name: "tool fio", mutate: func(value *Submission) { value.Tool.Fio += "-changed" }},
		{name: "tool iperf3", mutate: func(value *Submission) { value.Tool.IPerf3 += "-changed" }},
		{name: "profile", mutate: func(value *Submission) { value.Profile += "-changed" }},
		{name: "memory backend", mutate: func(value *Submission) { value.MemoryBackend = "changed" }},
		{name: "note", mutate: func(value *Submission) { value.Note += " changed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := copySubmission(submission)
			test.mutate(&changed)
			if changed.fingerprint() == submission.ID {
				t.Fatal("fingerprint ignored an artifact content change")
			}
			if err := changed.Validate(); err == nil {
				t.Fatal("changed artifact content passed validation")
			}
		})
	}

	unsafe := scoreReportFixture()
	unsafeSystem := findResult(&unsafe, "system")
	unsafeSystem.Fields[0].Value = model.RawValue("https://provider.example")
	unsafeSystem.Fields[1].Value = model.RawValue("203.0.113.10")
	provider, region = ExtractSubmissionMetadata(unsafe)
	if provider != "" || region != "" {
		t.Fatalf("unsafe report metadata was retained: %q/%q", provider, region)
	}
	longNote, err := BuildSubmission(report, SubmissionOptions{Note: strings.Repeat("n", maxNoteLength+10)})
	if err != nil || len([]rune(longNote.Note)) != maxNoteLength {
		t.Fatalf("long note was not bounded: len=%d err=%v", len([]rune(longNote.Note)), err)
	}
}

func TestSubmissionLoadAndValidationDiagnostics(t *testing.T) {
	submission, err := BuildSubmission(scoreReportFixture(), SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := submission.Encode()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	write := func(name string, content []byte) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := LoadSubmission(filepath.Join(directory, "missing.json")); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatalf("missing submission error = %v", err)
	}
	if _, err := LoadSubmission(write("syntax.json", []byte(`{"schema":`))); err == nil || !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("syntax submission error = %v", err)
	}
	if _, err := LoadSubmission(write("trailing.json", append(append([]byte(nil), encoded...), encoded...))); err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("trailing submission error = %v", err)
	}
	unknown := map[string]any{}
	if err := json.Unmarshal(encoded, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["future_field"] = true
	unknownJSON, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSubmission(write("unknown.json", unknownJSON)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	delete(unknown, "future_field")
	unknown["fingerprint_version"] = "v2"
	legacyJSON, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSubmission(write("fingerprint-version.json", legacyJSON)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("legacy fingerprint version was accepted: %v", err)
	}
	missingSampleID := map[string]any{}
	if err := json.Unmarshal(encoded, &missingSampleID); err != nil {
		t.Fatal(err)
	}
	delete(missingSampleID, "sample_id")
	missingSampleIDJSON, err := json.Marshal(missingSampleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSubmission(write("missing-sample-id.json", missingSampleIDJSON)); err == nil || !strings.Contains(err.Error(), "sample_id") {
		t.Fatalf("missing sample ID was accepted: %v", err)
	}
	largePath := filepath.Join(directory, "large.json")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(256*1024 + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := LoadSubmission(largePath); err == nil || !strings.Contains(err.Error(), "256 KiB") {
		t.Fatalf("oversize submission error = %v", err)
	}

	badCases := []struct {
		name   string
		mutate func(*Submission)
		marker string
	}{
		{name: "schema", mutate: func(value *Submission) { value.Schema = "other/v1" }, marker: "unsupported submission schema"},
		{name: "sample ID", mutate: func(value *Submission) { value.SampleID = "tampered" }, marker: "sample_id"},
		{name: "no metrics", mutate: func(value *Submission) { value.Metrics = nil }, marker: "contains no metrics"},
		{name: "memory marker", mutate: func(value *Submission) { value.MemoryBackend = "" }, marker: "STREAM metrics require"},
		{name: "unknown metric", mutate: func(value *Submission) { value.Metrics["not_a_metric"] = 1 }, marker: "unknown metric"},
		{name: "nonpositive metric", mutate: func(value *Submission) { value.Metrics["cpu_single"] = 0 }, marker: "must be positive"},
		{name: "host vcpu", mutate: func(value *Submission) { value.Host.VCPU = 0 }, marker: "host vcpu must be positive"},
		{name: "host memory", mutate: func(value *Submission) { value.Host.MemoryGiB = math.NaN() }, marker: "host memory must be positive"},
		{name: "tool", mutate: func(value *Submission) { value.Tool.ECS = "" }, marker: "tool version is required"},
		{name: "note", mutate: func(value *Submission) { value.Note = strings.Repeat("x", maxNoteLength+1) }, marker: "note exceeds"},
		{name: "empty id", mutate: func(value *Submission) { value.ID = "" }, marker: "submission id is required"},
		{name: "id", mutate: func(value *Submission) { value.ID = "tampered" }, marker: "does not match"},
	}
	for _, test := range badCases {
		t.Run(test.name, func(t *testing.T) {
			bad := copySubmission(submission)
			test.mutate(&bad)
			if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("validation error = %v, want %q", err, test.marker)
			}
		})
	}
	noScore := model.Report{Run: model.RunInfo{ID: "no-score"}, Results: []model.Result{{ID: "system", Status: model.StatusOK}}}
	if _, err := BuildSubmission(noScore, SubmissionOptions{}); err == nil || !strings.Contains(err.Error(), "no scoreable measurements") {
		t.Fatalf("no-score report error = %v", err)
	}
	emptyRunID := scoreReportFixture()
	emptyRunID.Run.ID = ""
	if _, err := BuildSubmission(emptyRunID, SubmissionOptions{}); err == nil || !strings.Contains(err.Error(), "Run.ID") {
		t.Fatalf("empty Run.ID report error = %v", err)
	}
	noHost := scoreReportFixture()
	noHost.Results = noHost.Results[1:]
	if _, err := BuildSubmission(noHost, SubmissionOptions{}); err == nil || !strings.Contains(err.Error(), "host vcpu must be positive") {
		t.Fatalf("missing-host report error = %v", err)
	}
}

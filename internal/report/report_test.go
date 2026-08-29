package report

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
	"ecs/internal/model"
)

func sampleReport() model.Report {
	start := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	higher := true
	lower := false
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test", Commit: "abc"},
		Run: model.RunInfo{
			ID: "run-1", Profile: "standard", StartedAt: start, CompletedAt: start.Add(time.Second),
			DurationMS: 1000, Exposure: "local", IPVersion: "6", Redacted: true,
			Requested: []string{"system", "cpu"}, OutputFormats: []string{"json", "md"},
		},
		Summary:      model.Summary{Status: model.StatusOK, OK: 1, Messages: []model.Message{model.NewMessage("message.summary.allOK", 1)}},
		Notices:      []model.Message{model.NewMessage("probe.memory.stream_missing"), model.NewMessage("module.system.title")},
		SensitiveIPs: []string{"192.0.2.10"},
		Results: []model.Result{{
			ID: "system", Title: "module.system.title", Description: "probe.system.description", Status: model.StatusOK,
			StartedAt: start, DurationMS: 20, SummaryMessages: []model.Message{model.NewMessage("probe.network.summary.values", "sample=ok")},
			Methodology: model.Methodology{
				Kind: "inventory", Label: "methodology.inventory", Engine: "system-inventory", Profile: "probe.system.profile",
				ComparisonScope: "probe.system.comparison_scope",
				Parameters:      map[string]string{"scope_revision": "1", "workload": "standard"},
			},
			Fields: []model.Field{
				{Key: "state", Label: "probe.system.field.hostname", Value: model.KeyValue("probe.network.status.ok")},
				{Key: "secret", Label: "probe.system.field.os", Value: model.RawValue("token"), Sensitive: true},
			},
			Measurements: []model.Measurement{{
				Key: "events", Label: "probe.system.metric.logical_cpus", Value: 780, Unit: "events/s", Display: model.RawValue("780"),
				Rating: "probe.network.status.ok", Method: "sysbench-v1", HigherIsBetter: &higher,
			}},
			Tables: []model.Table{{
				Key: "system.state", Title: "probe.system.pressure.table.title", Columns: []model.TableColumn{
					{Key: "state", Label: "probe.system.pressure.column.resource", Sensitive: true},
					{Key: "value", Label: "probe.system.pressure.column.cumulative_events", Numeric: true, HigherIsBetter: true},
				},
				RowIdentity: "state", Rows: [][]model.Value{{model.KeyValue("probe.network.status.ok"), model.RawValue("780")}},
			}},
			TextBlocks: []model.TextBlock{{Title: "说明", Language: "en", Content: "raw output 192.0.2.10", Sensitive: true}},
			Notes:      []string{"probe.system.note.partial_inventory"},
			Sources:    []model.Source{{Name: "kernel", URL: "https://example.test/source", Purpose: "probe.system.note.hardware_privacy"}},
			Evidence:   &model.Evidence{Valid: 1, Expected: 2, Unit: "sample"},
			Failures: []model.Failure{{
				Category: model.FailureTimeout, Stage: "fetch", Target: "api.example", Retryable: true,
				Count: 1, Message: "raw timeout",
			}},
			Retry: &model.RetryInfo{
				Triggered: true, SelectedAttempt: 1, SelectionRule: model.NewMessage("probe.retry.selection_rule.interference_score"), TriggerReasons: []model.Message{model.NewMessage("probe.system.note.partial_inventory"), model.NewMessage("probe.network.status.ok")},
				Attempts: []model.RetryAttempt{{
					Number: 1, Status: model.StatusWarning, DurationMS: 5,
					Evidence: &model.Evidence{Valid: 1, Expected: 1, Unit: "attempt"},
					Interference: model.Interference{
						Detected: true, Score: 1, Reasons: []model.Message{model.NewMessage("probe.system.note.partial_inventory")},
						Measurements: []model.Measurement{{Key: "load", Label: "probe.system.metric.logical_cpus", Value: 1, Unit: "load", Display: model.RawValue("1"), HigherIsBetter: &lower}},
					},
					Measurements: []model.Measurement{{Key: "events", Label: "probe.system.metric.logical_cpus", Value: 700, Unit: "events/s", Display: model.RawValue("700"), HigherIsBetter: &higher}},
				}},
			},
		}},
	}
}

func writeReportFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFilesCanonicalAndLanguageSpecificOutputs(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := sampleReport()
	canonical, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	written, err := WriteFilesWithOptions(data, directory, " report/unsafe ", []string{"json", "md", "html"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 3 {
		t.Fatalf("written files = %v", written)
	}
	for _, format := range []string{"json", "md", "html"} {
		path := written[format]
		if filepath.Base(path) != "report-unsafe."+format {
			t.Fatalf("sanitized %s path = %q", format, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s output: %v", format, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v", format, info.Mode().Perm())
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if format == "json" {
			if !bytes.Equal(content, canonical) {
				t.Fatal("JSON output was localized or otherwise changed")
			}
			continue
		}
		output := string(content)
		if !strings.Contains(output, "Hostname") || !strings.Contains(output, "raw output 192.0.2.10") || !strings.Contains(output, "raw timeout") {
			t.Fatalf("localized %s output lost translated display or raw evidence:\n%s", format, output)
		}
	}
	if data.Results[0].Fields[0].Value.Text() != "probe.network.status.ok" || data.Results[0].Tables[0].Rows[0][0].Text() != "probe.network.status.ok" {
		t.Fatal("WriteFiles mutated the canonical report")
	}
	fallbackWritten, err := WriteFilesWithOptions(data, t.TempDir(), "...", []string{"json"}, Options{})
	if err != nil || filepath.Base(fallbackWritten["json"]) != "ecs-report.json" {
		t.Fatalf("sanitize fallback = %v, %v", fallbackWritten, err)
	}

	workingDirectory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	defaultWritten, err := WriteFiles(data, "", "", []string{"json"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(defaultWritten["json"]) != "ecs-report-20260731-010203.json" {
		t.Fatalf("default timestamp filename = %q", defaultWritten["json"])
	}
}

func TestJSONCanonicalAndInvalidNumber(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	data := sampleReport()
	var canonical []byte
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		content, err := JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if len(content) == 0 || content[len(content)-1] != '\n' {
			t.Fatalf("JSON %s output has no trailing newline", language)
		}
		if bytes.Contains(content, []byte(`"offline"`)) {
			t.Fatalf("JSON %s output persisted derived offline field: %s", language, content)
		}
		if bytes.Contains(content, []byte(`"grade"`)) {
			t.Fatalf("JSON %s output persisted derived grade: %s", language, content)
		}
		if canonical == nil {
			canonical = content
		} else if !bytes.Equal(content, canonical) {
			t.Fatalf("JSON changed with language %s", language)
		}
	}
	data.Results[0].Measurements[0].Value = math.NaN()
	if _, err := JSON(data); err == nil || !strings.Contains(err.Error(), "unsupported value") {
		t.Fatalf("JSON NaN error = %v", err)
	}
}

func TestNetworkModeDerivesFromExposureAndRejectsLegacyOfflineInput(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	for _, test := range []struct {
		name, exposure, want, forbid string
	}{
		{name: "local", exposure: "local", want: "Offline", forbid: "Online"},
		{name: "public", exposure: "public", want: "Online", forbid: "Offline"},
		{name: "thirdparty", exposure: "thirdparty", want: "Online", forbid: "Offline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := model.Report{
				SchemaVersion: buildinfo.SchemaVersion,
				Run:           model.RunInfo{Exposure: test.exposure},
			}
			content, err := JSON(data)
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}
			if bytes.Contains(content, []byte(`"offline"`)) {
				t.Fatalf("JSON persisted derived offline field: %s", content)
			}
			output := Markdown(data, nil)
			if !strings.Contains(output, test.want) || strings.Contains(output, test.forbid) {
				t.Fatalf("Markdown exposure=%q output=%q, want %q without %q", test.exposure, output, test.want, test.forbid)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "legacy-offline.json")
	writeReportFile(t, path, []byte(`{"schema_version":"ecs.report/v1","run":{"exposure":"public","offline":true}}`))
	if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), `unknown field "offline"`) {
		t.Fatalf("legacy offline input error = %v", err)
	}
}

func TestLoadJSONValidationAndComparison(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	directory := t.TempDir()
	valid, err := JSON(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(directory, "valid.json")
	writeReportFile(t, validPath, valid)
	loaded, err := LoadJSON(validPath)
	if err != nil || loaded.SchemaVersion != buildinfo.SchemaVersion || loaded.Results[0].Evidence == nil || loaded.Results[0].Evidence.DerivedGrade() != model.EvidencePartial {
		t.Fatalf("valid LoadJSON = schema=%q evidence=%+v err=%v", loaded.SchemaVersion, loaded.Results[0].Evidence, err)
	}

	unknownPath := filepath.Join(directory, "unknown.json")
	writeReportFile(t, unknownPath, []byte(`{"schema_version":"ecs.report/v1","unknown_field":true}`))
	if _, err := LoadJSON(unknownPath); err == nil || !strings.Contains(err.Error(), `unknown field "unknown_field"`) {
		t.Fatalf("top-level unknown field error = %v", err)
	}
	nestedTypoPath := filepath.Join(directory, "nested-typo.json")
	writeReportFile(t, nestedTypoPath, []byte(`{"schema_version":"ecs.report/v1","run":{"duration_mss":1234}}`))
	if _, err := LoadJSON(nestedTypoPath); err == nil || !strings.Contains(err.Error(), `unknown field "duration_mss"`) {
		t.Fatalf("nested unknown field error = %v", err)
	}
	if _, err := LoadJSON(filepath.Join(directory, "missing.json")); err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatalf("open failure = %v", err)
	}

	schemaMismatch := bytes.Replace(valid, []byte("ecs.report/v1"), []byte("ecs.report/v2"), 1)
	secondObject := append(append([]byte(nil), valid...), valid...)
	trailing := append(append([]byte(nil), valid...), []byte(" trailing")...)
	cases := []struct {
		name    string
		content []byte
		marker  string
	}{
		{name: "syntax", content: []byte(`{"schema_version":`), marker: "unexpected EOF"},
		{name: "second object", content: secondObject, marker: "exactly one JSON object"},
		{name: "trailing", content: trailing, marker: "invalid trailing content"},
		{name: "missing schema", content: []byte(`{"tool":{}}`), marker: "missing schema_version"},
		{name: "schema mismatch", content: schemaMismatch, marker: "unsupported schema_version"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(directory, test.name+".json")
			writeReportFile(t, path, test.content)
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("LoadJSON error = %v, want %q", err, test.marker)
			}
		})
	}
	largePath := filepath.Join(directory, "large.json")
	large, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(32*1024*1024 + 1); err != nil {
		_ = large.Close()
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadJSON(largePath); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("oversize LoadJSON error = %v", err)
	}

	for _, schema := range []string{buildinfo.SchemaVersion, "ecs.report/v9", "other/v1"} {
		content := bytes.Replace(valid, []byte("ecs.report/v1"), []byte(schema), 1)
		path := filepath.Join(directory, "comparison-"+strings.ReplaceAll(schema, "/", "-")+".json")
		writeReportFile(t, path, content)
		got, err := LoadJSON(path)
		if schema == buildinfo.SchemaVersion {
			if err != nil || got.SchemaVersion != schema {
				t.Fatalf("current schema %q = %q, %v", schema, got.SchemaVersion, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
			t.Fatalf("historical schema %q error = %v", schema, err)
		}
	}
	malformedTablePath := filepath.Join(directory, "malformed-current-table.json")
	writeReportFile(t, malformedTablePath, []byte(`{"schema_version":"ecs.report/v1","results":[{"tables":[{"key":"network.state","columns":[{"key":"id","label":"ID"}],"rows":[[]]}]}]}`))
	if _, err := LoadJSON(malformedTablePath); err == nil || !strings.Contains(err.Error(), "table row 0") {
		t.Fatalf("malformed current table error = %v", err)
	}
	legacyTablePath := filepath.Join(directory, "legacy-table.json")
	writeReportFile(t, legacyTablePath, []byte(`{"schema_version":"ecs.report/v1","results":[{"tables":[{"columns":[{"key":"id","label":"ID"}],"rows":[["legacy"]]}]}]}`))
	if _, err := LoadJSON(legacyTablePath); err == nil || !strings.Contains(err.Error(), "tagged object") {
		t.Fatalf("legacy table error = %v", err)
	}
	loaderBoundaryCases := []struct {
		name    string
		content []byte
		marker  string
	}{
		{
			name:    "unknown typed table field",
			content: []byte(`{"schema_version":"ecs.report/v1","results":[{"tables":[{"columns":[{"key":"id","label":"ID","legacy":true}],"rows":[[{"raw":"one"}]]}]}]}`),
			marker:  "unknown field",
		},
		{
			name:    "unknown value tag",
			content: []byte(`{"schema_version":"ecs.report/v1","results":[{"fields":[{"key":"state","label":"State","value":{"legacy":"ok"}}]}]}`),
			marker:  "unknown tag",
		},
	}
	for _, test := range loaderBoundaryCases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(directory, test.name+".json")
			writeReportFile(t, path, test.content)
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("LoadJSON error = %v, want %q", err, test.marker)
			}
		})
	}
}

func TestWriteFilesErrorsAndAtomicity(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := sampleReport()
	root := t.TempDir()
	parentFile := filepath.Join(root, "not-a-directory")
	writeReportFile(t, parentFile, []byte("file"))
	if _, err := WriteFiles(data, filepath.Join(parentFile, "child"), "report", []string{"json"}); err == nil || !strings.Contains(err.Error(), "create output directory") {
		t.Fatalf("directory creation error = %v", err)
	}

	partialDirectory := t.TempDir()
	written, err := WriteFiles(data, partialDirectory, "report", []string{"json", "bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown report format") || len(written) != 0 {
		t.Fatalf("partial unknown-format result = %v, %v", written, err)
	}
	if _, statErr := os.Stat(filepath.Join(partialDirectory, "report.json")); !os.IsNotExist(statErr) {
		t.Fatalf("renderer failure left a partial JSON file: %v", statErr)
	}

	invalid := sampleReport()
	invalid.Results[0].Measurements[0].Value = math.Inf(1)
	written, err = WriteFiles(invalid, t.TempDir(), "report", []string{"json"})
	if err == nil || !strings.Contains(err.Error(), "generate json report") || !strings.Contains(err.Error(), "unsupported value") || len(written) != 0 {
		t.Fatalf("JSON generation failure = %v, %v", written, err)
	}

	atomicDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(atomicDirectory, "report.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	written, err = WriteFiles(data, atomicDirectory, "report", []string{"json"})
	if err == nil || !strings.Contains(err.Error(), "write json report") || len(written) != 0 {
		t.Fatalf("atomic write failure = %v, %v", written, err)
	}
}

package report

import (
	"bytes"
	"encoding/json"
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
	defaultWritten, err := WriteFilesWithOptions(data, "", "", []string{"json"}, Options{})
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

func TestJSONSerializesWindowMeasurementWithoutRemovedStructures(t *testing.T) {
	data := sampleReport()
	data.SchemaVersion = "ecs.report/v1"
	data.Results[0].Measurements = append(data.Results[0].Measurements, model.Measurement{
		Key:            "cpu_steal_percent_window",
		Label:          "probe.pressure.metric.cpu_steal_percent_window",
		Value:          6.25,
		Unit:           "%",
		Display:        model.RawValue("6.25 %"),
		Method:         "proc-stat-steal-window-v1",
		HigherIsBetter: model.BoolPtr(false),
	})

	content, err := JSON(data)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode JSON object: %v", err)
	}
	var schemaVersion string
	if err := json.Unmarshal(document["schema_version"], &schemaVersion); err != nil {
		t.Fatalf("decode schema_version: %v", err)
	}
	if schemaVersion != "ecs.report/v1" {
		t.Fatalf("schema_version = %q, want ecs.report/v1", schemaVersion)
	}

	var results []map[string]json.RawMessage
	if err := json.Unmarshal(document["results"], &results); err != nil {
		t.Fatalf("decode results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results count = %d, want 1", len(results))
	}
	result := results[0]
	for _, field := range []string{"retry", "interference", "selected_attempt", "selection_rule", "trigger_reasons"} {
		if _, present := result[field]; present {
			t.Fatalf("removed result field %q was serialized", field)
		}
	}

	var measurements []struct {
		Key   string  `json:"key"`
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(result["measurements"], &measurements); err != nil {
		t.Fatalf("decode measurements: %v", err)
	}
	var foundWindowMeasurement bool
	for _, measurement := range measurements {
		if measurement.Key == "cpu_steal_percent_window" {
			if measurement.Value != 6.25 {
				t.Fatalf("window measurement value = %v, want 6.25", measurement.Value)
			}
			foundWindowMeasurement = true
		}
	}
	if !foundWindowMeasurement {
		t.Fatal("JSON omitted cpu_steal_percent_window measurement")
	}

	var textBlocks []map[string]json.RawMessage
	if err := json.Unmarshal(result["text_blocks"], &textBlocks); err != nil {
		t.Fatalf("decode text_blocks: %v", err)
	}
	for index, block := range textBlocks {
		if _, present := block["attempt"]; present {
			t.Fatalf("removed text_blocks[%d].attempt was serialized", index)
		}
	}

	path := filepath.Join(t.TempDir(), "window.json")
	writeReportFile(t, path, content)
	loaded, err := LoadJSON(path)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if loaded.SchemaVersion != "ecs.report/v1" {
		t.Fatalf("loaded schema_version = %q, want ecs.report/v1", loaded.SchemaVersion)
	}
	var loadedWindowMeasurement bool
	for _, measurement := range loaded.Results[0].Measurements {
		if measurement.Key == "cpu_steal_percent_window" {
			if measurement.Value != 6.25 {
				t.Fatalf("loaded window measurement value = %v, want 6.25", measurement.Value)
			}
			loadedWindowMeasurement = true
		}
	}
	if !loadedWindowMeasurement {
		t.Fatal("LoadJSON omitted cpu_steal_percent_window measurement")
	}
}

// 身份契约的 owner 是读入边界 LoadJSON，不是序列化。JSON 只负责把已经通过
// owner 的报告写出去，因此这里从磁盘读回来验证拒绝行为。
func TestLoadJSONRejectsInvalidReportIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*model.Report)
		wantErr string
	}{
		{
			name:    "empty result ID",
			mutate:  func(data *model.Report) { data.Results[0].ID = "" },
			wantErr: "empty result ID",
		},
		{
			name: "duplicate measurement key",
			mutate: func(data *model.Report) {
				data.Results[0].Measurements = append(data.Results[0].Measurements, model.Measurement{Key: "events"})
			},
			wantErr: `duplicate measurement key "events"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := sampleReport()
			test.mutate(&data)
			content, err := JSON(data)
			if err != nil {
				t.Fatalf("JSON: %v", err)
			}
			path := filepath.Join(t.TempDir(), "report.json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("LoadJSON error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadJSONRejectsRemovedResultFields(t *testing.T) {
	cases := []struct {
		name    string
		content string
		marker  string
	}{
		{
			name:    "result retry",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","retry":{}}]}`,
			marker:  `unknown field "retry"`,
		},
		{
			name:    "result interference",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","interference":{}}]}`,
			marker:  `unknown field "interference"`,
		},
		{
			name:    "text block attempt",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","text_blocks":[{"content":"raw","attempt":1}]}]}`,
			marker:  `unknown field "attempt"`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			writeReportFile(t, path, []byte(test.content))
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("LoadJSON error = %v, want %q", err, test.marker)
			}
		})
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

func TestLoadJSONRejectsUnsupportedSchemaEnums(t *testing.T) {
	cases := []struct {
		name    string
		content string
		marker  string
	}{
		{
			name:    "summary status",
			content: `{"schema_version":"ecs.report/v1","summary":{"status":"bogus"}}`,
			marker:  `unsupported summary.status "bogus"`,
		},
		{
			name:    "result status",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","status":"bogus"}]}`,
			marker:  `unsupported results[0].status "bogus"`,
		},
		{
			name:    "run exposure",
			content: `{"schema_version":"ecs.report/v1","run":{"exposure":"bogus"}}`,
			marker:  `unsupported run.exposure "bogus"`,
		},
		{
			name:    "methodology kind",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","methodology":{"kind":"bogus"}}]}`,
			marker:  `unsupported results[0].methodology.kind "bogus"`,
		},
		{
			name:    "evidence unit",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","evidence":{"valid":1,"expected":1,"unit":"bogus"}}]}`,
			marker:  `unsupported results[0].evidence.unit "bogus"`,
		},
		{
			name:    "failure category",
			content: `{"schema_version":"ecs.report/v1","results":[{"id":"module","failures":[{"category":"bogus"}]}]}`,
			marker:  `unsupported results[0].failures[0].category "bogus"`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			writeReportFile(t, path, []byte(test.content))
			if _, err := LoadJSON(path); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("LoadJSON error = %v, want %q", err, test.marker)
			}
		})
	}
}

func TestLoadJSONMeasurementIdentity(t *testing.T) {
	load := func(t *testing.T, data model.Report) error {
		t.Helper()
		content, err := JSON(data)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "report.json")
		writeReportFile(t, path, content)
		_, err = LoadJSON(path)
		return err
	}
	loadRaw := func(t *testing.T, data model.Report) error {
		t.Helper()
		content, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "report.json")
		writeReportFile(t, path, content)
		_, err = LoadJSON(path)
		return err
	}

	t.Run("same key across result owners is allowed", func(t *testing.T) {
		data := sampleReport()
		data.Results = append(data.Results, model.Result{
			ID: "cpu", Status: model.StatusOK,
			Measurements: []model.Measurement{{Key: "events", Value: 999}},
		})
		if err := load(t, data); err != nil {
			t.Fatalf("cross-module duplicate key was rejected: %v", err)
		}
	})

	t.Run("duplicate measurement in one result is rejected", func(t *testing.T) {
		data := sampleReport()
		data.Results[0].Measurements = append(data.Results[0].Measurements, model.Measurement{Key: "events", Value: 999})
		if err := loadRaw(t, data); err == nil || !strings.Contains(err.Error(), `duplicate measurement key "events"`) {
			t.Fatalf("duplicate measurement error = %v", err)
		}
	})

	t.Run("duplicate result ID is rejected", func(t *testing.T) {
		data := sampleReport()
		data.Results = append(data.Results, model.Result{ID: "system", Status: model.StatusSkipped})
		if err := loadRaw(t, data); err == nil || !strings.Contains(err.Error(), `duplicate result ID "system"`) {
			t.Fatalf("duplicate result error = %v", err)
		}
	})

	t.Run("empty result ID is rejected", func(t *testing.T) {
		data := sampleReport()
		data.Results[0].ID = ""
		if err := loadRaw(t, data); err == nil || !strings.Contains(err.Error(), "empty result ID") {
			t.Fatalf("empty result ID error = %v", err)
		}
	})

	t.Run("empty measurement key is rejected", func(t *testing.T) {
		data := sampleReport()
		data.Results[0].Measurements[0].Key = ""
		if err := loadRaw(t, data); err == nil || !strings.Contains(err.Error(), "empty measurement key") {
			t.Fatalf("empty measurement key error = %v", err)
		}
	})
}

func TestWriteFilesErrorsAndAtomicity(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)
	data := sampleReport()
	root := t.TempDir()
	parentFile := filepath.Join(root, "not-a-directory")
	writeReportFile(t, parentFile, []byte("file"))
	if _, err := WriteFilesWithOptions(data, filepath.Join(parentFile, "child"), "report", []string{"json"}, Options{}); err == nil || !strings.Contains(err.Error(), "create output directory") {
		t.Fatalf("directory creation error = %v", err)
	}

	partialDirectory := t.TempDir()
	written, err := WriteFilesWithOptions(data, partialDirectory, "report", []string{"json", "bogus"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "unknown report format") || len(written) != 0 {
		t.Fatalf("partial unknown-format result = %v, %v", written, err)
	}
	if _, statErr := os.Stat(filepath.Join(partialDirectory, "report.json")); !os.IsNotExist(statErr) {
		t.Fatalf("renderer failure left a partial JSON file: %v", statErr)
	}

	freshDirectory := filepath.Join(root, "renderer-failure-directory")
	written, err = WriteFilesWithOptions(data, freshDirectory, "report", []string{"json", "bogus"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "unknown report format") || len(written) != 0 {
		t.Fatalf("fresh-directory renderer failure = %v, %v", written, err)
	}
	if _, statErr := os.Stat(freshDirectory); !os.IsNotExist(statErr) {
		t.Fatalf("renderer failure created output directory: %v", statErr)
	}

	invalid := sampleReport()
	invalid.Results[0].Measurements[0].Value = math.Inf(1)
	written, err = WriteFilesWithOptions(invalid, t.TempDir(), "report", []string{"json"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "generate json report") || !strings.Contains(err.Error(), "unsupported value") || len(written) != 0 {
		t.Fatalf("JSON generation failure = %v, %v", written, err)
	}

	atomicDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(atomicDirectory, "report.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	written, err = WriteFilesWithOptions(data, atomicDirectory, "report", []string{"json"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "write json report") || len(written) != 0 {
		t.Fatalf("atomic write failure = %v, %v", written, err)
	}
}

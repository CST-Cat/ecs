package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

// 提交库按月份分子目录存放，只扫一层会什么都找不到——CI 上就是这么红的。
func TestExpandReportPathsRecursesIntoSubdirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "2026-08")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "top.json"),
		filepath.Join(nested, "a.json"),
		filepath.Join(nested, "b.json"),
		filepath.Join(root, "README.md"),
	} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 隐藏目录不该被收进来。
	hidden := filepath.Join(root, ".git")
	_ = os.MkdirAll(hidden, 0o755)
	_ = os.WriteFile(filepath.Join(hidden, "c.json"), []byte("{}"), 0o600)

	got := expandReportPaths([]string{root})
	if len(got) != 3 {
		t.Fatalf("应收集到 3 个 json（含子目录、不含隐藏目录与非 json），得到 %d 个：%v", len(got), got)
	}
	for _, path := range got {
		if filepath.Ext(path) != ".json" {
			t.Errorf("收集到非 json 文件：%s", path)
		}
		if filepath.Base(filepath.Dir(path)) == ".git" {
			t.Errorf("收集到隐藏目录里的文件：%s", path)
		}
	}
}

func submitTestReport() model.Report {
	report := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC()},
		Summary:       model.Summary{Status: model.StatusOK, OK: 2, Headline: "完成"},
		Results: []model.Result{{
			ID: "cpu", Status: model.StatusOK,
			Measurements: []model.Measurement{
				{Key: "sysbench_cpu_single_events_s", Value: 900},
				{Key: "sysbench_cpu_multi_events_s", Value: 3400},
			},
		}},
	}
	report.Results = append([]model.Result{{
		ID: "system", Status: model.StatusOK,
		Measurements: []model.Measurement{
			{Key: "logical_cpus", Value: 4},
			{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
		},
	}}, report.Results...)
	return report
}

func writeBaselineReport(t *testing.T, name string, report model.Report) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBaselineCommandWritesReadableResult(t *testing.T) {
	input := writeBaselineReport(t, "report.json", submitTestReport())
	output := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	if status := baselineCommand([]string{"--output", output, input}, &stdout, &stderr); status != 0 {
		t.Fatalf("baseline command failed: status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	baseline, err := score.LoadBaseline(output)
	if err != nil {
		t.Fatalf("written baseline is not readable: %v", err)
	}
	if baseline.Schema != score.BaselineSchema || baseline.SampleCount != 1 || len(baseline.Metrics) == 0 {
		t.Fatalf("unexpected baseline result: %+v", baseline)
	}
}

func TestBaselineStrictRejectsBadInputWithoutWriting(t *testing.T) {
	valid := writeBaselineReport(t, "valid.json", submitTestReport())
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	status := baselineCommand([]string{
		"--strict", "--output", target, valid, bad,
	}, &stdout, &stderr)
	if status == 0 {
		t.Fatalf("strict baseline accepted bad JSON: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("strict baseline wrote output after bad input: err=%v", err)
	}
}

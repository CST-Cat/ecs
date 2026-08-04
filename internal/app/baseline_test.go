package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
	reporter "ecs/internal/report"
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

func TestExpandReportPathsAcceptsExplicitFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := expandReportPaths([]string{path}); len(got) != 1 || got[0] != path {
		t.Fatalf("显式文件应原样收集：%v", got)
	}
	if got := expandReportPaths([]string{filepath.Join(root, "missing.json")}); len(got) != 0 {
		t.Fatalf("不存在的路径应被忽略：%v", got)
	}
}

func submitTestReport(validHost bool) model.Report {
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
	if validHost {
		report.Results = append([]model.Result{{
			ID: "system", Status: model.StatusOK,
			Measurements: []model.Measurement{
				{Key: "logical_cpus", Value: 4},
				{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
			},
		}}, report.Results...)
	}
	return report
}

func writeSubmitTestReport(t *testing.T, report model.Report) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubmitCommandRejectsInvalidSubmissionBeforeWrite(t *testing.T) {
	input := writeSubmitTestReport(t, submitTestReport(false))
	target := filepath.Join(t.TempDir(), "submission.json")
	var stdout, stderr bytes.Buffer
	if status := submitCommand([]string{"--input", input, "--output", target}, &stdout, &stderr); status == 0 {
		t.Fatalf("submitCommand accepted a report without host dimensions: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("invalid submission created output %s (err=%v)", target, err)
	}
}

func TestSubmissionOutputPathSafety(t *testing.T) {
	defaultTarget, err := resolveSubmissionTarget("", "submission.json", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(defaultTarget) != filepath.Clean(os.TempDir()) {
		t.Fatalf("default target escaped temp directory: %s", defaultTarget)
	}

	directory := t.TempDir()
	if err := preflightSubmissionOutput(directory); err != nil {
		t.Fatalf("existing output directory should be accepted: %v", err)
	}
	resolved, err := resolveSubmissionTarget(directory, "submission.json", true)
	if err != nil || resolved != filepath.Join(directory, "submission.json") {
		t.Fatalf("directory target = %q, err=%v", resolved, err)
	}

	existing := filepath.Join(directory, "existing.json")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preflightSubmissionOutput(existing); err == nil {
		t.Fatal("existing output file should be rejected")
	}
	if err := writeSubmissionExclusive(existing, []byte("overwrite")); err == nil {
		t.Fatal("exclusive writer overwrote an existing file")
	}
	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "keep" {
		t.Fatalf("existing output changed: %q, err=%v", content, err)
	}

	symlink := filepath.Join(directory, "submission-link.json")
	if err := os.Symlink(existing, symlink); err != nil {
		t.Fatal(err)
	}
	if err := preflightSubmissionOutput(symlink); err == nil {
		t.Fatal("symlink output should be rejected")
	}

	for _, unsafe := range []string{"", "bad\nname", "bad\tname", "bad\x1bname"} {
		if err := preflightSubmissionOutput(unsafe); err == nil {
			t.Fatalf("unsafe output path %q was accepted", unsafe)
		}
	}
	missingParent := filepath.Join(directory, "missing", "submission.json")
	if err := preflightSubmissionOutput(missingParent); err == nil {
		t.Fatal("missing output parent should be rejected")
	}
}

func TestWriteSubmissionExclusiveIsAtomicAndPrivate(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "submission.json")
	if err := writeSubmissionExclusive(target, []byte("payload\n")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "payload\n" {
		t.Fatalf("written content = %q, err=%v", content, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("submission mode = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ecs-submit-") {
			t.Fatalf("temporary submission link was left behind: %s", entry.Name())
		}
	}
}

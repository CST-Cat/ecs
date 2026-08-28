package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/score"
)

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

func TestBaselineStrictRejectsInvalidInputWithoutWriting(t *testing.T) {
	valid := writeBaselineReport(t, "valid.json", submitTestReport())
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, test := range []struct {
		name  string
		input []string
	}{
		{name: "bad JSON", input: []string{valid, bad}},
		{name: "missing path", input: []string{missing}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "baseline.json")
			args := append([]string{"baseline", "--lang", "en", "--strict", "--output", output}, test.input...)
			status, stdout, stderr := invokeAppMain(args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "strict mode rejected") {
				t.Fatalf("strict input status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("strict input wrote output: %v", err)
			}
		})
	}
}

func TestBaselineInputStates(t *testing.T) {
	root := t.TempDir()
	valid := writeBaselineReport(t, "valid.json", submitTestReport())
	bad := filepath.Join(root, "bad.json")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	noMetrics := writeSubmitFixtureReport(t, filepath.Join(root, "no-metrics.json"), modelReportWithoutScoreableMeasurements())
	firstSubmission := writeSubmissionFixture(t, filepath.Join(root, "first-submission.json"))
	secondSubmission := writeSubmissionFixture(t, filepath.Join(root, "second-submission.json"))
	cases := []struct {
		name       string
		args       func(string) []string
		wantStatus int
		markers    []string
		checkOut   bool
	}{
		{
			name:       "no input",
			args:       func(output string) []string { return []string{"--lang", "en", "--output", output} },
			wantStatus: 1,
			markers:    []string{"at least one report file"},
		},
		{
			name: "nonstrict skips bad report",
			args: func(output string) []string {
				return []string{"--lang", "en", "--output", output, valid, bad}
			},
			markers:  []string{"Skipped"},
			checkOut: true,
		},
		{
			name: "nonstrict skips duplicate submission",
			args: func(output string) []string {
				return []string{"--lang", "en", "--output", output, firstSubmission, secondSubmission}
			},
			markers:  []string{"Skipped"},
			checkOut: true,
		},
		{
			name:       "no usable report",
			args:       func(output string) []string { return []string{"--lang", "en", "--output", output, noMetrics} },
			wantStatus: 1,
			markers:    []string{"Skipped", "no scoreable measurements", "no usable report files"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "baseline.json")
			status, stdout, stderr := invokeAppMain(append([]string{"baseline"}, test.args(output)...)...)
			if status != test.wantStatus {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			for _, marker := range test.markers {
				if !strings.Contains(stdout+stderr, marker) {
					t.Fatalf("missing marker %q in stdout=%q stderr=%q", marker, stdout, stderr)
				}
			}
			if test.checkOut {
				if _, err := score.LoadBaseline(output); err != nil {
					t.Fatalf("nonstrict input did not produce baseline: %v", err)
				}
			}
		})
	}
}

func TestBaselineReportsOutputWriteFailure(t *testing.T) {
	input := writeBaselineReport(t, "report.json", submitTestReport())
	outputDirectory := t.TempDir()
	status, stdout, stderr := invokeAppMain("baseline", "--lang", "en", "--output", outputDirectory, input)
	if status != 1 || stdout != "" || !strings.Contains(stderr, "is a directory") {
		t.Fatalf("baseline output failure status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(outputDirectory); err != nil {
		t.Fatalf("output failure changed the destination: %v", err)
	}
}

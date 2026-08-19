package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/score"
)

func modelReportWithoutScoreableMeasurements() model.Report {
	return model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "empty", StartedAt: time.Unix(0, 0).UTC()},
		Results: []model.Result{{
			ID: "system", Status: model.StatusOK,
		}},
	}
}

func writeSubmitFixtureReport(t *testing.T, path string, report model.Report) string {
	t.Helper()
	content, err := reporter.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSubmitCommandWritesLoadableSubmission(t *testing.T) {
	input := writeBaselineReport(t, "report.json", submitTestReport())
	for _, test := range []struct {
		name          string
		path          func(*testing.T) string
		defaultOutput bool
	}{
		{
			name: "explicit file",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "submission.json") },
		},
		{
			name: "explicit directory",
			path: func(t *testing.T) string {
				return t.TempDir()
			},
		},
		{
			name: "default temporary directory",
			path: func(t *testing.T) string {
				directory := t.TempDir()
				t.Setenv("TMPDIR", directory)
				return ""
			},
			defaultOutput: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := test.path(t)
			args := []string{"submit", "--lang", "en", "--input", input, "--region", "us", "--provider", "fixture", "--note", "diagnostic fixture"}
			if output != "" {
				args = append(args, "--output", output)
			}
			status, stdout, stderr := invokeAppMain(args...)
			if status != 0 || stderr != "" || !strings.Contains(stdout, "Submission written to") {
				t.Fatalf("submit status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if test.defaultOutput {
				submission, err := score.BuildSubmission(submitTestReport(), score.SubmissionOptions{Region: "us", Provider: "fixture", Note: "diagnostic fixture"})
				if err != nil {
					t.Fatal(err)
				}
				output = filepath.Join(os.TempDir(), submission.FileName())
			} else if info, err := os.Stat(output); err == nil && info.IsDir() {
				entries, err := os.ReadDir(output)
				if err != nil || len(entries) != 1 {
					t.Fatalf("directory output entries=%v err=%v", entries, err)
				}
				output = filepath.Join(output, entries[0].Name())
			}
			submission, err := score.LoadSubmission(output)
			if err != nil {
				t.Fatalf("written submission is not loadable: %v", err)
			}
			if submission.ID == "" || submission.Host.VCPU != 4 || submission.Host.Region != "us" ||
				submission.Host.Provider != "fixture" || submission.Note != "diagnostic fixture" {
				t.Fatalf("unexpected submission: %+v", submission)
			}
		})
	}
}

func TestSubmitCommandReportsDistinctFailures(t *testing.T) {
	root := t.TempDir()
	valid := writeBaselineReport(t, "report.json", submitTestReport())
	badJSON := filepath.Join(root, "bad.json")
	if err := os.WriteFile(badJSON, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	noMetrics := writeSubmitFixtureReport(t, filepath.Join(root, "no-metrics.json"), modelReportWithoutScoreableMeasurements())
	occupied := filepath.Join(root, "occupied.json")
	if err := os.WriteFile(occupied, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(root, "parent-file")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingParent := filepath.Join(root, "missing-parent", "submission.json")
	targetSymlink := filepath.Join(root, "target-link")
	if err := os.Symlink(filepath.Join(root, "target.json"), targetSymlink); err != nil {
		t.Fatal(err)
	}
	realParent := filepath.Join(root, "real-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	parentSymlink := filepath.Join(root, "parent-link")
	if err := os.Symlink(realParent, parentSymlink); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		args   []string
		marker string
	}{
		{
			name:   "flag parse",
			args:   []string{"submit", "--lang", "en", "--unknown"},
			marker: "flag provided but not defined",
		},
		{
			name:   "missing input",
			args:   []string{"submit", "--lang", "en"},
			marker: "--input is required",
		},
		{
			name:   "load JSON",
			args:   []string{"submit", "--lang", "en", "--input", badJSON},
			marker: "error:",
		},
		{
			name:   "build submission",
			args:   []string{"submit", "--lang", "en", "--input", noMetrics},
			marker: "no scoreable measurements",
		},
		{
			name:   "existing output",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", occupied},
			marker: "submission output already exists",
		},
		{
			name:   "output parent",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", filepath.Join(parentFile, "submission.json")},
			marker: "not a directory",
		},
		{
			name:   "missing output parent",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", missingParent},
			marker: "does not exist",
		},
		{
			name:   "target symlink",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", targetSymlink},
			marker: "submission output must not be a symlink",
		},
		{
			name:   "parent symlink",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", filepath.Join(parentSymlink, "submission.json")},
			marker: "submission output parent must not be a symlink",
		},
		{
			name:   "empty explicit output",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", ""},
			marker: "submission output path must not be empty",
		},
		{
			name:   "control character output",
			args:   []string{"submit", "--lang", "en", "--input", valid, "--output", filepath.Join(root, "bad\npath")},
			marker: "control character",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestSubmitCommandHelp(t *testing.T) {
	status, stdout, stderr := invokeAppMain("submit", "--lang", "en", "--help")
	if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs submit") {
		t.Fatalf("submit help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

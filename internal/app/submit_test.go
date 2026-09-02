package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/model"
	"ecs/internal/probe"
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
			args := []string{"--lang", "en", "submit", "--input", input, "--region", "us", "--provider", "fixture", "--note", "diagnostic fixture"}
			if output != "" {
				args = append(args, "--output", output)
			}
			status, stdout, stderr := invokeAppMain(args...)
			if status != 0 || stderr != "" || !strings.Contains(stdout, "Submission written to") {
				t.Fatalf("submit status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if test.defaultOutput {
				submission, err := score.BuildSubmission(probe.BuiltinCatalog(), submitTestReport(), score.SubmissionOptions{Region: "us", Provider: "fixture", Note: "diagnostic fixture"})
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
			args:   []string{"--lang", "en", "submit", "--unknown"},
			marker: "flag provided but not defined",
		},
		{
			name:   "missing input",
			args:   []string{"--lang", "en", "submit"},
			marker: "--input is required",
		},
		{
			name:   "extra argument",
			args:   []string{"--lang", "en", "submit", "unexpected"},
			marker: "unexpected arguments",
		},
		{
			name:   "load JSON",
			args:   []string{"--lang", "en", "submit", "--input", badJSON},
			marker: "error:",
		},
		{
			name:   "build submission",
			args:   []string{"--lang", "en", "submit", "--input", noMetrics},
			marker: "no scoreable measurements",
		},
		{
			name:   "existing output",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", occupied},
			marker: "submission output already exists",
		},
		{
			name:   "output parent",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", filepath.Join(parentFile, "submission.json")},
			marker: "not a directory",
		},
		{
			name:   "missing output parent",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", missingParent},
			marker: "does not exist",
		},
		{
			name:   "target symlink",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", targetSymlink},
			marker: "submission output must not be a symlink",
		},
		{
			name:   "parent symlink",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", filepath.Join(parentSymlink, "submission.json")},
			marker: "submission output parent must not be a symlink",
		},
		{
			name:   "empty explicit output",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", ""},
			marker: "submission output path must not be empty",
		},
		{
			name:   "control character output",
			args:   []string{"--lang", "en", "submit", "--input", valid, "--output", filepath.Join(root, "bad\npath")},
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

func TestWriteSubmissionExclusivePreservesSafetyAndPermissions(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "submission.json")
	first := []byte("first submission")
	if err := writeSubmissionExclusive(target, first); err != nil {
		t.Fatalf("write initial submission: %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("submission permissions = %o, want 600", info.Mode().Perm())
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(first) {
		t.Fatalf("initial submission content = %q, want %q", content, first)
	}
	if err := writeSubmissionExclusive(target, []byte("replacement")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing target error = %v", err)
	}
	content, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(first) {
		t.Fatalf("existing target was overwritten: %q", content)
	}

	realTarget := filepath.Join(root, "real.json")
	if err := os.WriteFile(realTarget, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkTarget := filepath.Join(root, "target-link")
	if err := os.Symlink(realTarget, symlinkTarget); err != nil {
		t.Fatal(err)
	}
	if err := writeSubmissionExclusive(symlinkTarget, []byte("replacement")); err == nil {
		t.Fatalf("existing symlink error = %v", err)
	}
	content, err = os.ReadFile(realTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "preserve" {
		t.Fatalf("symlink target was overwritten: %q", content)
	}

	directoryTarget := filepath.Join(root, "output-directory")
	if err := os.Mkdir(directoryTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSubmissionExclusive(directoryTarget, []byte("not a directory")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("directory target error = %v", err)
	}

	if err := writeSubmissionExclusive(filepath.Join(root, "missing", "submission.json"), []byte("missing parent")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing parent error = %v", err)
	}

	realParent := filepath.Join(root, "real-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(root, "parent-link")
	if err := os.Symlink(realParent, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := writeSubmissionExclusive(filepath.Join(parentLink, "submission.json"), []byte("symlink parent")); err == nil || !strings.Contains(err.Error(), "parent must not be a symlink") {
		t.Fatalf("symlink parent error = %v", err)
	}
}

func TestWriteSubmissionExclusiveRejectsTargetRace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "submission.json")
	const writers = 8
	contents := make([][]byte, writers)
	results := make([]error, writers)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(writers)
	for index := 0; index < writers; index++ {
		contents[index] = []byte{byte('a' + index)}
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results[index] = writeSubmissionExclusive(target, contents[index])
		}(index)
	}
	close(start)
	waitGroup.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("exclusive write successes = %d, want 1 (results=%v)", successes, results)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, candidate := range contents {
		if string(content) == string(candidate) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("race winner content = %q, not written by a worker", content)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "submission.json" {
		t.Fatalf("temporary submission files remain: %v", entries)
	}
}

func TestSubmitCommandHelp(t *testing.T) {
	status, stdout, stderr := invokeAppMain("--lang", "en", "submit", "--help")
	if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs submit") {
		t.Fatalf("submit help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

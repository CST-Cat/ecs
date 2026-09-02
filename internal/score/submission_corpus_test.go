package score

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const submissionCorpusEnvironment = "ECS_SUBMISSION_DIR"

// TestSubmissionCorpus is the repository and CI entry point for validating
// every leaderboard submission. Keep this as an ordinary, exactly named test:
// the workflows select it explicitly so an empty corpus is still a real pass.
func TestSubmissionCorpus(t *testing.T) {
	repositoryRoot, err := findSubmissionRepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	directory := os.Getenv(submissionCorpusEnvironment)
	if directory == "" {
		directory = "submissions"
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(repositoryRoot, directory)
	}
	if err := validateSubmissionCorpus(directory); err != nil {
		t.Fatal(err)
	}
}

func findSubmissionRepositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate submission repository root: %w", err)
	}
	for {
		info, statErr := os.Stat(filepath.Join(directory, "go.mod"))
		if statErr == nil && !info.IsDir() {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("locate submission repository root from %s: go.mod not found", filepath.ToSlash(directory))
		}
		directory = parent
	}
}

func validateSubmissionCorpus(directory string) error {
	directory = filepath.Clean(directory)
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("%s: inspect submission corpus: %w", filepath.ToSlash(directory), err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: submission corpus is not a directory", filepath.ToSlash(directory))
	}

	var candidates []string
	var issues []string
	addIssue := func(path, message string) {
		issues = append(issues, fmt.Sprintf("%s: %s", filepath.ToSlash(path), message))
	}
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			addIssue(path, fmt.Sprintf("inspect corpus entry: %v", walkErr))
			return nil
		}
		if path == directory {
			return nil
		}

		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil {
			addIssue(path, fmt.Sprintf("resolve corpus path: %v", relErr))
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) == 1 {
			switch entry.Name() {
			case "README.md", "README_EN.md", "baseline.json":
				if entry.Type().IsRegular() {
					return nil
				}
				addIssue(path, "allowed root entry must be a regular file")
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") {
					addIssue(path, "submission directory must not be hidden")
				}
				return nil
			}
			addIssue(path, "unexpected file at corpus root")
			return nil
		}

		if len(parts) != 2 {
			addIssue(path, "submission entries must be exactly one directory below the corpus root")
			return nil
		}
		if entry.IsDir() {
			addIssue(path, "nested submission directories are not allowed")
			return nil
		}
		if !entry.Type().IsRegular() {
			addIssue(path, "submission must be a regular JSON file")
			return nil
		}
		if filepath.Ext(entry.Name()) != ".json" {
			addIssue(path, "submission directory may contain only .json files")
			return nil
		}
		if strings.HasPrefix(parts[0], ".") {
			return nil
		}
		candidates = append(candidates, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s: walk submission corpus: %w", filepath.ToSlash(directory), err)
	}

	sort.Strings(candidates)
	seen := make(map[string]string, len(candidates))
	for _, path := range candidates {
		submission, loadErr := LoadSubmission(path)
		if loadErr != nil {
			addIssue(path, fmt.Sprintf("invalid submission: %v", loadErr))
			continue
		}
		if actual, expected := filepath.Base(path), submission.FileName(); actual != expected {
			addIssue(path, fmt.Sprintf("filename %q does not match submission filename %q", actual, expected))
		}
		if previous, exists := seen[submission.ID]; exists {
			addIssue(path, fmt.Sprintf("duplicate submission id %q (already used by %s)", submission.ID, filepath.ToSlash(previous)))
			continue
		}
		seen[submission.ID] = path
	}
	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return fmt.Errorf("submission corpus validation failed:\n%s", strings.Join(issues, "\n"))
}

func TestValidateSubmissionCorpus(t *testing.T) {
	newSubmission := func(t *testing.T, options SubmissionOptions) Submission {
		t.Helper()
		submission, err := BuildSubmission(scoreTestCatalog(), scoreReportFixture(), options)
		if err != nil {
			t.Fatal(err)
		}
		return submission
	}
	writeSubmission := func(t *testing.T, directory, group string, submission Submission, name string) string {
		t.Helper()
		groupDirectory := filepath.Join(directory, group)
		if err := os.MkdirAll(groupDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		content, err := submission.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if name == "" {
			name = submission.FileName()
		}
		path := filepath.Join(groupDirectory, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("empty corpus", func(t *testing.T) {
		if err := validateSubmissionCorpus(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("valid file and generated baseline", func(t *testing.T) {
		directory := t.TempDir()
		writeSubmission(t, directory, "2026-08", newSubmission(t, SubmissionOptions{}), "")
		for name, content := range map[string]string{
			"README.md":     "corpus documentation\n",
			"README_EN.md":  "corpus documentation\n",
			"baseline.json": `{"schema":"ecs.baseline/v1"}`,
		} {
			if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := validateSubmissionCorpus(directory); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("bad layout is sorted", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "root.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(directory, "2026-08", "deeper"), 0o755); err != nil {
			t.Fatal(err)
		}
		deepPath := filepath.Join(directory, "2026-08", "deeper", "record.json")
		if err := os.WriteFile(deepPath, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "2026-08", "note.txt"), []byte("no"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(directory, ".hidden"), 0o755); err != nil {
			t.Fatal(err)
		}

		err := validateSubmissionCorpus(directory)
		if err == nil {
			t.Fatal("bad corpus layout passed validation")
		}
		message := err.Error()
		markers := []string{".hidden", "deeper/record.json", "note.txt", "root.json"}
		last := -1
		for _, marker := range markers {
			index := strings.Index(message, marker)
			if index < 0 {
				t.Fatalf("layout error %q does not contain path marker %q", message, marker)
			}
			if index <= last {
				t.Fatalf("layout errors are not sorted: %q", message)
			}
			last = index
		}
	})

	t.Run("bad filename", func(t *testing.T) {
		directory := t.TempDir()
		path := writeSubmission(t, directory, "2026-08", newSubmission(t, SubmissionOptions{}), "wrong.json")
		err := validateSubmissionCorpus(directory)
		if err == nil || !strings.Contains(err.Error(), filepath.ToSlash(path)) || !strings.Contains(err.Error(), "does not match submission filename") {
			t.Fatalf("bad filename error = %v", err)
		}
	})

	t.Run("invalid submission", func(t *testing.T) {
		directory := t.TempDir()
		submission := newSubmission(t, SubmissionOptions{})
		submission.ID = "tampered"
		path := writeSubmission(t, directory, "2026-08", submission, submission.FileName())
		err := validateSubmissionCorpus(directory)
		if err == nil || !strings.Contains(err.Error(), filepath.ToSlash(path)) || !strings.Contains(err.Error(), "does not match its contents") {
			t.Fatalf("invalid submission error = %v", err)
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		directory := t.TempDir()
		submission := newSubmission(t, SubmissionOptions{})
		first := writeSubmission(t, directory, "2026-08", submission, "")
		second := writeSubmission(t, directory, "2026-09", submission, "")
		err := validateSubmissionCorpus(directory)
		if err == nil || !strings.Contains(err.Error(), filepath.ToSlash(first)) || !strings.Contains(err.Error(), filepath.ToSlash(second)) || !strings.Contains(err.Error(), "duplicate submission id") {
			t.Fatalf("duplicate submission error = %v", err)
		}
	})
}

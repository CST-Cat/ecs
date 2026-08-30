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

	got := expandReportPathsDetailed([]string{root}).paths
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

func TestExpandReportPathsDeduplicatesCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	reports := filepath.Join(root, "reports")
	if err := os.MkdirAll(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(reports, "a.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	uncleanPath := reports + string(os.PathSeparator) + "." + string(os.PathSeparator) + "a.json"
	got := expandReportPathsDetailed([]string{path, uncleanPath, reports}).paths
	if len(got) != 1 || got[0] != path {
		t.Fatalf("canonical duplicate paths = %v, want [%s]", got, path)
	}
}

func TestLeaderboardHandlesNestedTraversalErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		strict bool
	}{
		{name: "default continues", strict: false},
		{name: "strict rejects", strict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			readable := writeLeaderboardReport(t, filepath.Join(root, "readable.json"), "traversal-readable", 100)
			blocked := filepath.Join(root, "blocked")
			if err := os.Mkdir(blocked, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(blocked, "unreadable.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(blocked, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

			expanded := expandReportPathsDetailed([]string{root})
			if len(expanded.issues) == 0 {
				t.Skip("filesystem did not enforce the unreadable directory permission")
			}

			output := filepath.Join(root, "baseline.json")
			args := []string{"--lang", "en", "leaderboard", "--output", output}
			if test.strict {
				args = append(args, "--strict")
			}
			args = append(args, root)
			status, stdout, stderr := invokeAppMain(args...)
			if test.strict {
				if status != 1 || stdout != "" || !strings.Contains(strings.ToLower(stderr), "traversal error") {
					t.Fatalf("strict traversal status=%d stdout=%q stderr=%q", status, stdout, stderr)
				}
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatalf("strict traversal wrote output: %v", err)
				}
				return
			}
			if status != 0 || !strings.Contains(stdout, "written") ||
				!strings.Contains(stderr, "Skipped") || !strings.Contains(strings.ToLower(stderr), "traversal error") {
				t.Fatalf("default traversal status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			baseline, err := score.LoadBaseline(output)
			if err != nil {
				t.Fatalf("default traversal output is not loadable: %v", err)
			}
			if baseline.SampleCount != 1 || baseline.Metrics["cpu_single"] != 100 {
				t.Fatalf("default traversal baseline = %+v, readable input=%s", baseline, readable)
			}
		})
	}
}

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(runScriptPath(t))
}

func readRepositoryFile(t *testing.T, relativePath string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(contents)
}

func TestReleaseGeneratedDirectoriesAreIgnored(t *testing.T) {
	ignore := readRepositoryFile(t, ".gitignore")
	for _, directory := range []string{
		"/.ci-tools-stage/",
		"/artifact/",
		"/tools-artifacts/",
		"/tools-stage/",
	} {
		if !strings.Contains(ignore, directory+"\n") {
			t.Errorf(".gitignore does not exclude release staging directory %q", directory)
		}
	}
}

func TestReleaseWorkflowRejectsDirtyOrMismatchedGoBinaries(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/release.yml")
	releaseJobAt := strings.Index(workflow, "\n  release:\n")
	if releaseJobAt < 0 {
		t.Fatal("release workflow has no release job")
	}
	releaseJob := workflow[releaseJobAt:]

	for _, required := range []string{
		"go-version: 1.26.6",
		"Verify clean release source",
		"git status --porcelain=v1 --untracked-files=all",
		"Verify release VCS metadata and known vulnerabilities",
		"expected_revision=$(git rev-parse HEAD)",
		"expected_go=go1.26.6",
		"metadata=$(go version -m \"$binary\")",
		"vcs.revision='\"$expected_revision\"",
		"vcs.modified=false",
		"golang.org/x/vuln/cmd/govulncheck@v1.6.0",
		"govulncheck_bin\" -mode binary \"$binary\"",
		"test \"${#archives[@]}\" -eq 7",
	} {
		if !strings.Contains(releaseJob, required) {
			t.Errorf("release job is missing policy guard %q", required)
		}
	}
}

func TestCIEnforcesStaticAndVulnerabilityAnalysis(t *testing.T) {
	workflow := readRepositoryFile(t, ".github/workflows/ci.yml")
	for _, required := range []string{
		"honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...",
		"golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CI is missing analysis command %q", required)
		}
	}
}

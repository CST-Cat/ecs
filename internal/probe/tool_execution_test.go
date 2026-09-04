package probe

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeToolFixture(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLookupToolUsesFrozenStagingDirectory(t *testing.T) {
	hostDirectory := t.TempDir()
	stagedDirectory := t.TempDir()
	writeToolFixture(t, hostDirectory, "fixture-tool")
	want := writeToolFixture(t, stagedDirectory, "fixture-tool")
	t.Setenv("PATH", hostDirectory)
	t.Setenv(ToolBinEnv, stagedDirectory)

	got, err := LookupTool("fixture-tool")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("LookupTool path = %q, want staged path %q", got, want)
	}
}

func TestLookupToolDoesNotUseHostPath(t *testing.T) {
	hostDirectory := t.TempDir()
	writeToolFixture(t, hostDirectory, "fixture-tool")
	t.Setenv("PATH", hostDirectory)
	t.Setenv(ToolBinEnv, "")

	_, err := LookupTool("fixture-tool")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("LookupTool error = %v, want exec.ErrNotFound", err)
	}
}

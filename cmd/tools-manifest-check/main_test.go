package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exampleManifest(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tools", "manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeManifest(t *testing.T, directory, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := run(args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestRunValidatesMultipleManifests(t *testing.T) {
	directory := t.TempDir()
	data := exampleManifest(t)
	first := writeManifest(t, directory, "first.json", data)
	second := writeManifest(t, directory, "second.json", data)

	status, stdout, stderr := invoke(t, first, second)
	if status != 0 || stderr != "" {
		t.Fatalf("run status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if strings.Count(stdout, ": valid (amd64)\n") != 2 {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestRunReportsMissingAndInvalidManifests(t *testing.T) {
	directory := t.TempDir()
	valid := writeManifest(t, directory, "valid.json", exampleManifest(t))
	missing := filepath.Join(directory, "missing.json")

	status, stdout, stderr := invoke(t, missing, valid)
	if status != 1 || !strings.Contains(stderr, missing+": invalid manifest:") {
		t.Fatalf("missing status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if !strings.Contains(stdout, valid+": valid (amd64)") {
		t.Fatalf("valid manifest was not processed after missing file: %q", stdout)
	}

	invalidData := bytes.Replace(exampleManifest(t), []byte(`"architecture": "amd64"`), []byte(`"architecture": "windows-amd64"`), 1)
	invalid := writeManifest(t, directory, "invalid.json", invalidData)
	status, stdout, stderr = invoke(t, invalid)
	if status != 1 || stdout != "" || !strings.Contains(stderr, invalid+": invalid manifest:") {
		t.Fatalf("invalid status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestRunReportsUsageWithoutManifests(t *testing.T) {
	t.Run("no manifests", func(t *testing.T) {
		status, stdout, stderr := invoke(t)
		if status != 2 || stdout != "" || !strings.Contains(stderr, "usage:") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
	t.Run("unknown flag", func(t *testing.T) {
		status, stdout, stderr := invoke(t, "--unknown")
		if status != 2 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
}

func TestRunChecksEachExpectation(t *testing.T) {
	directory := t.TempDir()
	manifest := writeManifest(t, directory, "manifest.json", exampleManifest(t))
	cases := []struct {
		name     string
		matching string
		mismatch string
		want     string
	}{
		{name: "architecture", matching: "--architecture=amd64", mismatch: "--architecture=arm64", want: "architecture \"amd64\" does not match expected \"arm64\""},
		{name: "toolchain", matching: "--toolchain-mode=native", mismatch: "--toolchain-mode=cross", want: "build.toolchain_mode \"native\" does not match expected \"cross\""},
		{name: "smoke runner", matching: "--smoke-runner=direct", mismatch: "--smoke-runner=container", want: "build.smoke_runner \"direct\" does not match expected \"container\""},
		{name: "NPB smoke class", matching: "--npb-smoke-class=A", mismatch: "--npb-smoke-class=B", want: "tool \"npb-ep\" ci_smoke_class \"A\" does not match expected \"B\""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invoke(t, test.matching, manifest)
			if status != 0 || stderr != "" || !strings.Contains(stdout, manifest+": valid (amd64)") {
				t.Fatalf("matching status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}

			status, stdout, stderr = invoke(t, test.mismatch, manifest)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

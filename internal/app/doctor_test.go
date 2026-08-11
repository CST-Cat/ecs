package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorToolsUseCurrentDependencies(t *testing.T) {
	tools := doctorTools()
	seen := make(map[string]doctorTool, len(tools))
	for _, tool := range tools {
		seen[tool.name] = tool
	}
	for _, removed := range []string{"mbw", "ioping"} {
		if _, ok := seen[removed]; ok {
			t.Fatalf("doctor still checks removed tool %q", removed)
		}
	}
	stream, ok := seen["stream"]
	if !ok {
		t.Fatal("doctor is missing the official stream entry")
	}
	if len(stream.args) != 0 || stream.check == nil {
		t.Fatalf("stream doctor entry = %+v; it must use a non-executing check", stream)
	}
	for _, name := range []string{"zstd", "npb-ep", "npb-ft", "openssl"} {
		tool, ok := seen[name]
		if !ok || !tool.required || tool.check == nil || len(tool.args) != 0 {
			t.Fatalf("doctor fixed benchmark entry %q = %+v", name, tool)
		}
	}
	speedtest, ok := seen["speedtest"]
	if !ok || speedtest.required {
		t.Fatalf("speedtest doctor entry = %+v; it should remain optional", speedtest)
	}
}

func TestDoctorRequiresTinyNextTrace(t *testing.T) {
	directory := t.TempDir()
	tiny := filepath.Join(directory, "nexttrace-tiny")
	full := filepath.Join(directory, "nexttrace")
	for _, path := range []string{tiny, full} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	path, err := lookupNextTrace()
	if err != nil || path != tiny {
		t.Fatalf("tiny lookup = %q, %v; want %q", path, err, tiny)
	}
	if err := os.Remove(tiny); err != nil {
		t.Fatal(err)
	}
	path, err = lookupNextTrace()
	if err == nil || path != "" {
		t.Fatalf("full binary must not satisfy Tiny lookup: %q, %v", path, err)
	}
}

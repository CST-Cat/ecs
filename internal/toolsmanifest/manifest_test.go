package toolsmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func exampleBytes(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "tools", "manifest.example.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func exampleObject(t *testing.T) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(exampleBytes(t), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func objectBytes(t *testing.T, object map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExampleManifestParsesAndValidates(t *testing.T) {
	manifest, err := Parse(exampleBytes(t))
	if err != nil {
		t.Fatalf("example manifest rejected: %v", err)
	}
	if manifest.Architecture != "amd64" {
		t.Fatalf("example architecture = %q, want amd64", manifest.Architecture)
	}
	if len(manifest.Tools) != len(ToolNames) {
		t.Fatalf("example tool count = %d, want %d", len(manifest.Tools), len(ToolNames))
	}
	if len(manifest.SupportedArchitectures) != len(Architectures) {
		t.Fatalf("example supported architecture count = %d, want %d", len(manifest.SupportedArchitectures), len(Architectures))
	}
}

func TestExampleRecordsRequiredFeatureFlags(t *testing.T) {
	manifest, err := Parse(exampleBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]Tool, len(manifest.Tools))
	for _, tool := range manifest.Tools {
		byName[tool.Name] = tool
	}
	assertContains := func(name string, values []string, wanted ...string) {
		t.Helper()
		for _, feature := range wanted {
			found := false
			for _, value := range values {
				if value == feature {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s does not record feature %q: %#v", name, feature, values)
			}
		}
	}
	assertContains("sysbench", byName["sysbench"].DisabledFeatures, "database-drivers")
	assertContains("fio", byName["fio"].EnabledFeatures, "io_uring", "libaio", "psync")
	assertContains("fio", byName["fio"].DisabledFeatures, "ceph", "rbd", "rados", "gluster", "rdma")
	assertContains("iperf3", byName["iperf3"].DisabledFeatures, "sctp")
	if byName["nexttrace-tiny"].Fallback == "" || byName["ping"].Fallback == "" {
		t.Fatal("Tiny and ping fallback metadata must be present")
	}
	if len(byName["stream"].Parameters) == 0 {
		t.Fatal("STREAM parameters must be present")
	}
	if got, ok := byName["stream"].Parameters["array_size"].(float64); !ok || got != 10000000 {
		t.Fatalf("STREAM array_size = %#v, want 10000000", byName["stream"].Parameters["array_size"])
	}
	if got, ok := byName["stream"].Parameters["ntimes"].(float64); !ok || got != 10 {
		t.Fatalf("STREAM ntimes = %#v, want 10", byName["stream"].Parameters["ntimes"])
	}
	for _, flag := range []string{"-DSTREAM_ARRAY_SIZE=10000000", "-DNTIMES=10"} {
		if !contains(byName["stream"].BuildFlags, flag) {
			t.Fatalf("STREAM build_flags missing %q: %v", flag, byName["stream"].BuildFlags)
		}
	}
}

func TestParseRejectsMissingRequiredField(t *testing.T) {
	object := exampleObject(t)
	tools := object["tools"].([]any)
	delete(tools[0].(map[string]any), "build_flags")
	if _, err := Parse(objectBytes(t, object)); err == nil {
		t.Fatal("manifest with missing build_flags unexpectedly passed")
	}
}

func TestParseRejectsMissingParameters(t *testing.T) {
	object := exampleObject(t)
	tools := object["tools"].([]any)
	delete(tools[0].(map[string]any), "parameters")
	if _, err := Parse(objectBytes(t, object)); err == nil {
		t.Fatal("manifest with missing parameters unexpectedly passed")
	}
}

func TestParseRejectsBadHash(t *testing.T) {
	object := exampleObject(t)
	tools := object["tools"].([]any)
	tools[0].(map[string]any)["sha256"] = "not-a-sha256"
	if _, err := Parse(objectBytes(t, object)); err == nil {
		t.Fatal("manifest with bad sha256 unexpectedly passed")
	}
}

func TestParseRejectsBadArchitecture(t *testing.T) {
	object := exampleObject(t)
	object["architecture"] = "windows-amd64"
	if _, err := Parse(objectBytes(t, object)); err == nil {
		t.Fatal("manifest with bad architecture unexpectedly passed")
	}
}

func TestParseRejectsBadToolSetAndOokla(t *testing.T) {
	object := exampleObject(t)
	tools := object["tools"].([]any)
	tools[0].(map[string]any)["name"] = "ookla"
	if _, err := Parse(objectBytes(t, object)); err == nil {
		t.Fatal("manifest containing Ookla unexpectedly passed")
	}
}

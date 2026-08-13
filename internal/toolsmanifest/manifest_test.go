package toolsmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func schemaObject(t *testing.T) map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "tools", "ecs-tools-manifest.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return object
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
	if manifest.Build.ToolchainMode != "native" || manifest.Build.SmokeRunner != "direct" {
		t.Fatalf("example build metadata = %#v, want native/direct", manifest.Build)
	}
	if manifest.Build.Validation.Scope != "functional" || manifest.Build.Validation.PerformanceValid {
		t.Fatalf("example validation metadata = %#v, want functional/non-performance", manifest.Build.Validation)
	}
}

func TestJSONSchemaMirrorsRequiredBuildAndToolSetContract(t *testing.T) {
	schema := schemaObject(t)
	required := schema["required"].([]any)
	for _, field := range []string{"schema_version", "architecture", "build", "tools"} {
		found := false
		for _, value := range required {
			found = found || value == field
		}
		if !found {
			t.Errorf("JSON Schema required list omits %q", field)
		}
	}
	properties := schema["properties"].(map[string]any)
	tools := properties["tools"].(map[string]any)
	if tools["minItems"] != float64(len(ToolNames)) || tools["maxItems"] != float64(len(ToolNames)) {
		t.Fatalf("JSON Schema tool cardinality = %#v/%#v, want %d", tools["minItems"], tools["maxItems"], len(ToolNames))
	}
	definitions := schema["$defs"].(map[string]any)
	tool := definitions["tool"].(map[string]any)
	if tool["additionalProperties"] != false {
		t.Fatal("JSON Schema tool objects must reject unknown fields like the Go parser")
	}
	toolProperties := tool["properties"].(map[string]any)
	names := toolProperties["name"].(map[string]any)["enum"].([]any)
	if len(names) != len(ToolNames) {
		t.Fatalf("JSON Schema tool name count = %d, want %d", len(names), len(ToolNames))
	}
	for _, name := range ToolNames {
		found := false
		for _, value := range names {
			found = found || value == name
		}
		if !found {
			t.Errorf("JSON Schema tool enum omits %q", name)
		}
	}
	for _, definition := range []string{"build", "validation"} {
		if definitions[definition].(map[string]any)["additionalProperties"] != false {
			t.Errorf("JSON Schema %s object must reject unknown fields", definition)
		}
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
	for _, flag := range []string{"--with-system-luajit", "--with-system-ck"} {
		if !contains(byName["sysbench"].BuildFlags, flag) {
			t.Fatalf("sysbench build_flags missing %q: %v", flag, byName["sysbench"].BuildFlags)
		}
	}
	assertContains("zstd", byName["zstd"].EnabledFeatures, "benchmark", "multithread", "compression", "decompression")
	assertContains("zstd", byName["zstd"].DisabledFeatures, "zlib", "lzma", "lz4", "legacy-formats", "dictionary-builder", "trace")
	if got := byName["zstd"].Parameters["corpus_sha256"]; got != "8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0" {
		t.Fatalf("zstd corpus_sha256 = %#v", got)
	}
	if got, ok := byName["zstd"].Parameters["corpus_bytes"].(float64); !ok || got != 211938580 {
		t.Fatalf("zstd corpus_bytes = %#v, want 211938580", byName["zstd"].Parameters["corpus_bytes"])
	}
	if got := byName["zstd"].Parameters["corpus_path"]; got != "runtime/ecs-silesia-v1.corpus" {
		t.Fatalf("zstd corpus_path = %#v, want runtime/ecs-silesia-v1.corpus", got)
	}
	if got := byName["zstd"].Parameters["corpus_source"]; got != "https://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip" {
		t.Fatalf("zstd corpus_source = %#v, want fixed Silesia ZIP URL", got)
	}
	for _, name := range []string{"npb-ep", "npb-ft"} {
		assertContains(name, byName[name].EnabledFeatures, "NPB3.4-OMP", "Class A", "OpenMP")
		if got := byName[name].Parameters["source_sha256"]; got != "1ae219398e02a0a79ad51b7460fcffbf7b5df83a69d5d3d3a9dc2d8acf523549" {
			t.Fatalf("%s source_sha256 = %#v", name, got)
		}
		if got := byName[name].Parameters["compiler_flags"]; got != "-O3 -fopenmp -static" {
			t.Fatalf("%s compiler_flags = %#v", name, got)
		}
		if got := byName[name].Parameters["ci_smoke_class"]; got != "A" {
			t.Fatalf("%s ci_smoke_class = %#v", name, got)
		}
	}
	assertContains("openssl", byName["openssl"].EnabledFeatures, "speed", "EVP", "AES-256-GCM", "ChaCha20-Poly1305", "SHA-256", "multi-process")
	assertContains("openssl", byName["openssl"].BuildFlags, "no-ssl", "no-sock", "no-dso", "no-engine", "no-ec", "no-dh", "no-dsa", "no-legacy", "no-tests", "no-docs")
	if got := byName["openssl"].Parameters["source_commit"]; got != "8cf17aaeb4599f8af87fefd810b5b5fee90fe69e" {
		t.Fatalf("OpenSSL source_commit = %#v", got)
	}
	if got, ok := byName["openssl"].Parameters["block_bytes"].(float64); !ok || got != 16384 {
		t.Fatalf("OpenSSL block_bytes = %#v", byName["openssl"].Parameters["block_bytes"])
	}
	if got := byName["openssl"].Parameters["build_target"]; got != "apps/openssl" {
		t.Fatalf("OpenSSL build_target = %#v", got)
	}
	if got := byName["openssl"].Parameters["generated_target"]; got != "build_generated" {
		t.Fatalf("OpenSSL generated_target = %#v", got)
	}
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

func TestParseRequiresCompleteBuildMetadata(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(map[string]any)
	}{
		{
			name:  "build",
			field: `"build"`,
			mutate: func(object map[string]any) {
				delete(object, "build")
			},
		},
		{
			name:  "toolchain mode",
			field: `"toolchain_mode"`,
			mutate: func(object map[string]any) {
				delete(object["build"].(map[string]any), "toolchain_mode")
			},
		},
		{
			name:  "build triplet",
			field: `"build_triplet"`,
			mutate: func(object map[string]any) {
				delete(object["build"].(map[string]any), "build_triplet")
			},
		},
		{
			name:  "target triplet",
			field: `"target_triplet"`,
			mutate: func(object map[string]any) {
				delete(object["build"].(map[string]any), "target_triplet")
			},
		},
		{
			name:  "smoke runner",
			field: `"smoke_runner"`,
			mutate: func(object map[string]any) {
				delete(object["build"].(map[string]any), "smoke_runner")
			},
		},
		{
			name:  "validation",
			field: `"validation"`,
			mutate: func(object map[string]any) {
				delete(object["build"].(map[string]any), "validation")
			},
		},
		{
			name:  "validation scope",
			field: `"scope"`,
			mutate: func(object map[string]any) {
				build := object["build"].(map[string]any)
				delete(build["validation"].(map[string]any), "scope")
			},
		},
		{
			name:  "performance validity",
			field: `"performance_valid"`,
			mutate: func(object map[string]any) {
				build := object["build"].(map[string]any)
				delete(build["validation"].(map[string]any), "performance_valid")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := exampleObject(t)
			test.mutate(object)
			_, err := Parse(objectBytes(t, object))
			if err == nil {
				t.Fatalf("manifest missing %s unexpectedly passed", test.field)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error %q does not identify missing field %s", err, test.field)
			}
		})
	}
}

func TestParseAcceptsCrossBuildMetadata(t *testing.T) {
	object := exampleObject(t)
	build := object["build"].(map[string]any)
	build["toolchain_mode"] = "cross"
	build["build_triplet"] = "aarch64-linux-gnu"
	build["target_triplet"] = "arm-linux-gnueabihf"
	build["smoke_runner"] = "qemu-arm-static"

	manifest, err := Parse(objectBytes(t, object))
	if err != nil {
		t.Fatalf("cross-build manifest rejected: %v", err)
	}
	if manifest.Build.ToolchainMode != "cross" || manifest.Build.SmokeRunner != "qemu-arm-static" {
		t.Fatalf("cross-build metadata = %#v", manifest.Build)
	}
}

func TestParseRejectsInvalidBuildMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "invalid toolchain mode",
			mutate: func(build map[string]any) {
				build["toolchain_mode"] = "emulated"
			},
		},
		{
			name: "empty build triplet",
			mutate: func(build map[string]any) {
				build["build_triplet"] = ""
			},
		},
		{
			name: "empty target triplet",
			mutate: func(build map[string]any) {
				build["target_triplet"] = ""
			},
		},
		{
			name: "empty smoke runner",
			mutate: func(build map[string]any) {
				build["smoke_runner"] = ""
			},
		},
		{
			name: "performance scope",
			mutate: func(build map[string]any) {
				build["validation"].(map[string]any)["scope"] = "performance"
			},
		},
		{
			name: "performance claimed valid",
			mutate: func(build map[string]any) {
				build["validation"].(map[string]any)["performance_valid"] = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := exampleObject(t)
			test.mutate(object["build"].(map[string]any))
			if _, err := Parse(objectBytes(t, object)); err == nil {
				t.Fatal("manifest with invalid build metadata unexpectedly passed")
			}
		})
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

func TestParseRejectsUnknownFieldsAtEveryStrictSchemaLevel(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"manifest", func(object map[string]any) { object["unexpected"] = true }},
		{"build", func(object map[string]any) { object["build"].(map[string]any)["unexpected"] = true }},
		{"validation", func(object map[string]any) {
			object["build"].(map[string]any)["validation"].(map[string]any)["unexpected"] = true
		}},
		{"tool", func(object map[string]any) { object["tools"].([]any)[0].(map[string]any)["unexpected"] = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := exampleObject(t)
			test.mutate(object)
			if _, err := Parse(objectBytes(t, object)); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("unknown %s field was not rejected clearly: %v", test.name, err)
			}
		})
	}
}

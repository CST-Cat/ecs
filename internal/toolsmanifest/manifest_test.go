package toolsmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exampleManifestBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tools", "manifest.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func exampleManifestObject(t *testing.T) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(exampleManifestBytes(t), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func manifestBytes(t *testing.T, object map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func firstManifestTool(t *testing.T, object map[string]any) map[string]any {
	t.Helper()
	tools, ok := object["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("example manifest has no tools")
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatal("example manifest tool is not an object")
	}
	return tool
}

func TestExampleManifestParsesAndValidates(t *testing.T) {
	manifest, err := Parse(exampleManifestBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(manifest); err != nil {
		t.Fatalf("parsed manifest failed validation: %v", err)
	}
	if manifest.SchemaVersion != SchemaVersion || manifest.Architecture == "" || len(manifest.Tools) != len(ToolNames) {
		t.Fatalf("manifest = schema %q, architecture %q, tools %d", manifest.SchemaVersion, manifest.Architecture, len(manifest.Tools))
	}
}

func TestManifestParsingAndValidationDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  func(t *testing.T) []byte
		marker string
	}{
		{name: "malformed", input: func(*testing.T) []byte { return []byte("{") }, marker: "parse manifest"},
		{name: "nonobject", input: func(*testing.T) []byte { return []byte("null") }, marker: "must be a JSON object"},
		{name: "trailing", input: func(t *testing.T) []byte { return append(exampleManifestBytes(t), []byte("\n{}")...) }, marker: "more than one JSON value"},
	} {
		if _, err := Parse(test.input(t)); err == nil || !strings.Contains(err.Error(), test.marker) {
			t.Errorf("%s error = %v, want %q", test.name, err, test.marker)
		}
	}

	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
		marker string
	}{
		{name: "unknown top field", mutate: func(object map[string]any) { object["extra"] = true }, marker: `unknown field "extra"`},
		{name: "missing top field", mutate: func(object map[string]any) { delete(object, "schema_version") }, marker: `missing required field "schema_version"`},
		{name: "schema version", mutate: func(object map[string]any) { object["schema_version"] = "ecs-tools.manifest/v0" }, marker: "schema_version must be"},
		{name: "unsupported architecture", mutate: func(object map[string]any) { object["architecture"] = "windows-amd64" }, marker: "unsupported architecture"},
		{name: "build object", mutate: func(object map[string]any) { object["build"] = "bad" }, marker: "build must be a JSON object"},
		{name: "unknown build field", mutate: func(object map[string]any) { object["build"].(map[string]any)["extra"] = true }, marker: `build: unknown field "extra"`},
		{name: "missing build field", mutate: func(object map[string]any) { delete(object["build"].(map[string]any), "smoke_runner") }, marker: `build: missing required field "smoke_runner"`},
		{name: "validation object", mutate: func(object map[string]any) { object["build"].(map[string]any)["validation"] = "bad" }, marker: "build.validation must be a JSON object"},
		{name: "unknown validation field", mutate: func(object map[string]any) {
			object["build"].(map[string]any)["validation"].(map[string]any)["extra"] = true
		}, marker: `build.validation: unknown field "extra"`},
		{name: "missing validation field", mutate: func(object map[string]any) {
			delete(object["build"].(map[string]any)["validation"].(map[string]any), "scope")
		}, marker: `build.validation: missing required field "scope"`},
		{name: "invalid toolchain", mutate: func(object map[string]any) { object["build"].(map[string]any)["toolchain_mode"] = "other" }, marker: "build.toolchain_mode"},
		{name: "invalid scope", mutate: func(object map[string]any) {
			object["build"].(map[string]any)["validation"].(map[string]any)["scope"] = "benchmark"
		}, marker: "build.validation.scope"},
		{name: "performance validation", mutate: func(object map[string]any) {
			object["build"].(map[string]any)["validation"].(map[string]any)["performance_valid"] = true
		}, marker: "performance_valid"},
		{name: "nonempty build field", mutate: func(object map[string]any) { object["build"].(map[string]any)["build_triplet"] = "" }, marker: "build.build_triplet"},
		{name: "supported architecture set", mutate: func(object map[string]any) {
			values := object["supported_architectures"].([]any)
			values[1] = values[0]
		}, marker: "supported architecture"},
		{name: "supported architecture type", mutate: func(object map[string]any) { object["supported_architectures"] = "amd64" }, marker: "supported_architectures"},
		{name: "tool count", mutate: func(object map[string]any) { object["tools"] = []any{} }, marker: "tools must contain exactly"},
		{name: "tool unknown field", mutate: func(object map[string]any) { firstManifestTool(t, object)["extra"] = true }, marker: `tool 0: unknown field "extra"`},
		{name: "tool duplicate", mutate: func(object map[string]any) {
			tools := object["tools"].([]any)
			tools[1].(map[string]any)["name"] = tools[0].(map[string]any)["name"]
		}, marker: "tools contains duplicate"},
		{name: "tool name", mutate: func(object map[string]any) { firstManifestTool(t, object)["name"] = "unknown" }, marker: "unsupported name"},
		{name: "tool nonempty scalar", mutate: func(object map[string]any) { firstManifestTool(t, object)["upstream"] = "" }, marker: "upstream must be a non-empty string"},
		{name: "required array", mutate: func(object map[string]any) { delete(firstManifestTool(t, object), "build_flags") }, marker: `missing required field "build_flags"`},
		{name: "empty array element", mutate: func(object map[string]any) { firstManifestTool(t, object)["build_flags"] = []any{""} }, marker: "build_flags[0]"},
		{name: "tool architecture", mutate: func(object map[string]any) { firstManifestTool(t, object)["architecture"] = "arm64" }, marker: "does not match manifest architecture"},
		{name: "sha", mutate: func(object map[string]any) { firstManifestTool(t, object)["sha256"] = "bad" }, marker: "sha256"},
		{name: "parameters object", mutate: func(object map[string]any) { firstManifestTool(t, object)["parameters"] = "bad" }, marker: "parameters"},
		{name: "fallback", mutate: func(object map[string]any) { firstManifestTool(t, object)["fallback"] = "" }, marker: "fallback must be a non-empty string"},
	} {
		object := exampleManifestObject(t)
		test.mutate(object)
		_, err := Parse(manifestBytes(t, object))
		if err == nil || !strings.Contains(err.Error(), test.marker) {
			t.Errorf("%s error = %v, want %q", test.name, err, test.marker)
		}
	}
}

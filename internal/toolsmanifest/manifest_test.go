package toolsmanifest

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestParseRejectsUnsupportedArchitecture(t *testing.T) {
	data := bytes.Replace(exampleManifestBytes(t), []byte(`"architecture": "amd64"`), []byte(`"architecture": "windows-amd64"`), 1)
	if bytes.Equal(data, exampleManifestBytes(t)) {
		t.Fatal("test fixture architecture was not replaced")
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("unsupported architecture unexpectedly passed validation")
	}
}

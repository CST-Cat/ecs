package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamBinaryAndEnvironmentBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream")
	if err := os.WriteFile(path, []byte(strings.Join(streamOfficialMarkers, "\n")), 0o700); err != nil {
		t.Fatal(err)
	}
	if !IsOfficialStreamBinary(path) || IsOfficialStreamBinary(filepath.Join(filepath.Dir(path), "missing")) {
		t.Fatal("STREAM binary identification boundary failed")
	}
	env := streamEnvironment(4)
	seen := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && (key == "OMP_NUM_THREADS" || key == "OMP_DYNAMIC" || key == "LC_ALL" || key == "LANG") {
			if _, exists := seen[key]; exists {
				t.Fatalf("duplicate STREAM environment key %q", key)
			}
			seen[key] = value
		}
	}
	if seen["OMP_NUM_THREADS"] != "4" || seen["OMP_DYNAMIC"] != "FALSE" || seen["LC_ALL"] != "C" || seen["LANG"] != "C" {
		t.Fatalf("STREAM environment = %v", seen)
	}
}

func TestStreamBinaryDetectorHandlesLockedWindowsPESize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.exe")
	if err := os.WriteFile(path, []byte(strings.Join(streamOfficialMarkers, "\x00")), 0o700); err != nil {
		t.Fatal(err)
	}
	const largeCandidateSize = 64 << 20
	if err := os.Truncate(path, largeCandidateSize+1); err != nil {
		t.Fatal(err)
	}
	if !IsOfficialStreamBinary(path) {
		t.Fatalf("large official STREAM marker candidate was rejected; size=%d", largeCandidateSize+1)
	}
}

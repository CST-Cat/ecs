package app

import (
	"context"
	"strings"
	"testing"
)

func TestListMachineEmitsDescriptorManifest(t *testing.T) {
	var out, errOut strings.Builder
	if status := Main(context.Background(), []string{"list", "--machine"}, &out, &errOut); status != 0 {
		t.Fatalf("list --machine returned %d: stderr=%s", status, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 || lines[0] != "ecs-module-manifest\t1" {
		t.Fatalf("unexpected manifest header/output: %q", out.String())
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		fields := strings.Split(line, "\t")
		if fields[0] == "ecs-module-manifest" {
			continue
		}
		if len(fields) < 3 || (fields[0] != "profile" && fields[0] != "module") || fields[1] == "" {
			t.Fatalf("invalid manifest record %q", line)
		}
		seen[fields[0]] = true
	}
	if !seen["profile"] || !seen["module"] {
		t.Fatalf("manifest did not contain both record types: %q", out.String())
	}
}

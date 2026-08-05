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
	if len(lines) < 3 || lines[0] != "ecs-module-manifest\t1" {
		t.Fatalf("unexpected manifest header/output: %q", out.String())
	}
	if !strings.Contains(out.String(), "profile\tstandard\t") || !strings.Contains(out.String(), "profile\tfull\t") {
		t.Fatalf("manifest omitted profile plans: %q", out.String())
	}
	if !strings.Contains(out.String(), "module\troute\tpublic\tnexttrace") {
		t.Fatalf("manifest omitted route tool metadata: %q", out.String())
	}
}

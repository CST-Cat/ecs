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
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || fields[0] != "profile" {
			continue
		}
		modules := "," + fields[2] + ","
		switch fields[1] {
		case "standard":
			if strings.Contains(modules, ",ookla,") {
				t.Fatal("standard manifest must omit Ookla")
			}
		case "full":
			if !strings.Contains(modules, ",ookla,") {
				t.Fatal("full manifest must include Ookla")
			}
		}
	}
	if !strings.Contains(out.String(), "module\troute\tpublic\tnexttrace-tiny") {
		t.Fatalf("manifest omitted route tool metadata: %q", out.String())
	}
}

package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStandardOnlyCanSelectModulesOutsidePreset(t *testing.T) {
	planPath := t.TempDir() + "/plan"
	t.Setenv("ECS_PLAN_FILE", planPath)
	args := []string{"run", "--profile", "standard", "--only", "network,disk", "--yes"}
	var stdout, stderr strings.Builder
	if status := Main(context.Background(), args, &stdout, &stderr); status != 0 {
		t.Fatalf("standard --only returned %d: stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "standard\nnetwork,disk\n" {
		t.Fatalf("one-shot plan = %q, want standard/network,disk", string(data))
	}
}

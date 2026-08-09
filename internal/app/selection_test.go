package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStandardOnlyCanSelectModulesOutsidePreset(t *testing.T) {
	cases := []struct {
		name string
		only string
		want string
	}{
		{name: "network and disk", only: "network,disk", want: "network,disk"},
		{name: "ookla", only: "ookla", want: "ookla"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			planPath := t.TempDir() + "/plan"
			t.Setenv("ECS_PLAN_FILE", planPath)
			args := []string{"run", "--profile", "standard", "--only", testCase.only, "--yes"}
			var stdout, stderr strings.Builder
			if status := Main(context.Background(), args, &stdout, &stderr); status != 0 {
				t.Fatalf("standard --only %s returned %d: stdout=%s stderr=%s", testCase.only, status, stdout.String(), stderr.String())
			}
			data, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 2 || lines[0] != "standard" || lines[1] != testCase.want {
				t.Fatalf("one-shot plan = %q, want standard/%s", string(data), testCase.want)
			}
		})
	}
}

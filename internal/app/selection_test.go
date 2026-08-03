package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestQuickOnlyCanSelectModulesOutsidePreset(t *testing.T) {
	cases := []struct {
		name   string
		only   string
		accept string
		want   string
	}{
		{name: "cnspeed and disk", only: "cnspeed,disk", want: "disk,cnspeed"},
		{name: "ookla with consent", only: "ookla", accept: "ookla", want: "ookla"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			planPath := t.TempDir() + "/plan"
			t.Setenv("ECS_PLAN_FILE", planPath)
			args := []string{"run", "--profile", "quick", "--only", testCase.only, "--yes"}
			if testCase.accept != "" {
				args = append(args, "--accept", testCase.accept)
			}
			var stdout, stderr strings.Builder
			if status := Main(context.Background(), args, &stdout, &stderr); status != 0 {
				t.Fatalf("quick --only %s returned %d: stdout=%s stderr=%s", testCase.only, status, stdout.String(), stderr.String())
			}
			data, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) != 2 || lines[0] != "quick" || lines[1] != testCase.want {
				t.Fatalf("one-shot plan = %q, want quick/%s", string(data), testCase.want)
			}
		})
	}
}

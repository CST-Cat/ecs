package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPlanUsesConfigAndCLISelection(t *testing.T) {
	cases := []struct {
		name     string
		config   string
		args     func(string) []string
		wantPlan string
	}{
		{
			name: "profile and only flags",
			args: func(string) []string {
				return []string{"run", "--profile", "standard", "--only", "network,disk", "--yes"}
			},
			wantPlan: "standard\nnetwork,disk\n",
		},
		{
			name:   "config profile overridden by CLI only",
			config: `{"profile":"full","only":["system"]}`,
			args: func(configPath string) []string {
				return []string{"run", "--config", configPath, "--only", "network", "--yes"}
			},
			wantPlan: "full\nnetwork\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			planPath := filepath.Join(directory, "plan")
			t.Setenv("ECS_PLAN_FILE", planPath)
			configPath := ""
			if test.config != "" {
				configPath = filepath.Join(directory, "config.json")
				if err := os.WriteFile(configPath, []byte(test.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr strings.Builder
			if status := Main(context.Background(), test.args(configPath), &stdout, &stderr); status != 0 {
				t.Fatalf("run returned %d: stdout=%s stderr=%s", status, stdout.String(), stderr.String())
			}
			data, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != test.wantPlan {
				t.Fatalf("one-shot plan = %q, want %q", string(data), test.wantPlan)
			}
		})
	}
}

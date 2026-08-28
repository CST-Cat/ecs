package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/score"
)

func TestLeaderboardCommandsWriteReadableResults(t *testing.T) {
	root := t.TempDir()
	input := writeBaselineReport(t, "report.json", submitTestReport())
	previousPath := filepath.Join(root, "previous-baseline.json")
	previous, err := (score.Baseline{
		Schema: score.BaselineSchema, Source: "previous", SampleCount: 1,
		Metrics: map[string]float64{"sysbench_cpu_single_events_s": 1},
	}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"baseline", "leaderboard"} {
		t.Run(command, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), command+".json")
			status, stdout, stderr := invokeAppMain(command, "--lang", "en", "--source", "fixture", "--output", output, previousPath, input)
			if status != 0 || stderr != "" || !strings.Contains(stdout, "written") {
				t.Fatalf("%s status=%d stdout=%q stderr=%q", command, status, stdout, stderr)
			}
			if command == "baseline" {
				baseline, err := score.LoadBaseline(output)
				if err != nil {
					t.Fatalf("written baseline is not readable: %v", err)
				}
				if baseline.Schema != score.BaselineSchema || baseline.SampleCount != 1 || len(baseline.Metrics) == 0 || baseline.Source != "fixture" {
					t.Fatalf("unexpected baseline result: %+v", baseline)
				}
			} else if _, err := os.Stat(output); err != nil {
				t.Fatalf("leaderboard did not write output: %v", err)
			}
		})
	}
	t.Run("leaderboard annotations", func(t *testing.T) {
		root := t.TempDir()
		values := []float64{98, 99, 100, 101, 102, 103, 104, 1000}
		inputs := make([]string, 0, len(values))
		for index, value := range values {
			var multi *float64
			if index == 0 {
				onlyMulti := 3400.0
				multi = &onlyMulti
			}
			inputs = append(inputs, writeOutlierSubmissionFixture(t, filepath.Join(root, fmt.Sprintf("submission-%d.json", index)), value, multi))
		}
		output := filepath.Join(root, "annotated.json")
		args := []string{"leaderboard", "--lang", "en", "--source", "fixture", "--annotate", "--verbose", "--output", output}
		args = append(args, inputs...)
		status, stdout, stderr := invokeAppMain(args...)
		if status != 0 || stderr != "" || !strings.Contains(stdout, "Outlier notices:") ||
			!strings.Contains(stdout, "::warning::") ||
			!strings.Contains(stdout, "Combinations left unjudged for lack of samples:") ||
			!strings.Contains(stdout, "active") {
			t.Fatalf("leaderboard annotations status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
		if _, err := score.LoadBaseline(output); err != nil {
			t.Fatalf("annotated leaderboard output is not loadable: %v", err)
		}
	})
}

func TestBaselineAndLeaderboardHelp(t *testing.T) {
	for _, test := range []struct {
		command string
		marker  string
	}{
		{command: "baseline", marker: "Usage: ecs baseline"},
		{command: "leaderboard", marker: "Usage: ecs leaderboard"},
	} {
		t.Run(test.command, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.command, "--lang", "en", "--help")
			if status != 0 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("%s help status=%d stdout=%q stderr=%q", test.command, status, stdout, stderr)
			}
		})
	}
}

func TestBaselineAndLeaderboardRejectUnknownFlag(t *testing.T) {
	status, stdout, stderr := invokeAppMain("baseline", "--lang", "en", "--unknown")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("baseline unknown flag status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

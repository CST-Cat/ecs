package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/termcolor"
)

func TestRunRejectsInvalidColorAndExposureAtCLIEntry(t *testing.T) {
	for _, raw := range []string{"", "terminal-magic"} {
		t.Run("color-"+raw, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(
				"run", "--lang", "en", "--only", "system", "--yes", "--color="+raw,
			)
			if status == 0 || stdout != "" || !strings.Contains(stderr, "invalid terminal color mode") {
				t.Fatalf("invalid color status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}

	status, stdout, stderr := invokeAppMain(
		"run", "--lang", "en", "--only", "system", "--yes", "--no-color", "--color=terminal-magic",
	)
	if status == 0 || stdout != "" || !strings.Contains(stderr, "invalid terminal color mode") {
		t.Fatalf("invalid color with --no-color status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = invokeAppMain("run", "--lang", "en", "--version", "--color=terminal-magic")
	if status == 0 || stdout != "" || !strings.Contains(stderr, "invalid terminal color mode") {
		t.Fatalf("invalid color on --version status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	for _, raw := range []string{"auto", "none", "basic", "256", "truecolor", "always"} {
		t.Run("version-"+raw, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain("run", "--lang", "en", "--version", "--color="+raw)
			if status != 0 || stdout == "" || stderr != "" {
				t.Fatalf("valid color %q status=%d stdout=%q stderr=%q", raw, status, stdout, stderr)
			}
		})
	}

	for _, raw := range []string{"invalid", "4"} {
		t.Run("exposure-"+raw, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(
				"run", "--lang", "en", "--only", "system", "--yes", "--exposure", raw,
			)
			if status == 0 || stdout != "" || !strings.Contains(stderr, "unknown exposure level") {
				t.Fatalf("invalid exposure %q status=%d stdout=%q stderr=%q", raw, status, stdout, stderr)
			}
		})
	}
}

func TestRunHelpUsesFlagSetParseResultBeforeColorValidation(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--lang", "en", "--help", "--color=terminal-magic"},
		{"run", "--lang", "en", "--help", "--no-color", "--color=terminal-magic"},
	} {
		status, stdout, stderr := invokeAppMain(args...)
		if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs [run]") || strings.Contains(stderr, "invalid terminal color mode") {
			t.Fatalf("run help parse result args=%v status=%d stdout=%q stderr=%q", args, status, stdout, stderr)
		}
	}
}

func TestRunReportsFlagParseErrorWithoutSecondaryColorValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		marker string
	}{
		{
			name:   "unknown flag after invalid color",
			args:   []string{"run", "--lang", "en", "--color=terminal-magic", "--unknown"},
			marker: "flag provided but not defined: -unknown",
		},
		{
			name:   "missing color value",
			args:   []string{"run", "--lang", "en", "--color"},
			marker: "flag needs an argument: -color",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) || strings.Contains(stderr, "invalid terminal color mode") {
				t.Fatalf("run parse error args=%v status=%d stdout=%q stderr=%q", test.args, status, stdout, stderr)
			}
		})
	}
}

func TestCompareRejectsInvalidColorBeforeWriting(t *testing.T) {
	root := t.TempDir()
	first := writeLocalizedObservationInput(t, root, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, root, "second", "完成", "完成")
	for _, test := range []struct {
		name    string
		color   string
		noColor bool
	}{
		{name: "empty", color: ""},
		{name: "unknown", color: "terminal-magic"},
		{name: "unknown-with-no-color", color: "terminal-magic", noColor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(root, test.name)
			args := []string{
				"--lang", "en", "compare", first, second, "--format", "json",
				"--output", output, "--color=" + test.color,
			}
			if test.noColor {
				args = append(args, "--no-color")
			}
			status, stdout, stderr := invokeAppMain(args...)
			if status == 0 || stdout != "" || !strings.Contains(stderr, "invalid terminal color mode") {
				t.Fatalf("invalid comparison color status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("invalid comparison color created output: %v", err)
			}
		})
	}
}

func TestCompareHelpUsesFlagSetParseResultBeforeColorValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--lang", "en", "compare", "--help", "--color=terminal-magic"},
		{"--lang", "en", "compare", "--help", "--no-color", "--color=terminal-magic"},
	} {
		status, stdout, stderr := invokeAppMain(args...)
		if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs compare") || strings.Contains(stderr, "invalid terminal color mode") {
			t.Fatalf("compare help parse result args=%v status=%d stdout=%q stderr=%q", args, status, stdout, stderr)
		}
	}
}

func TestCompareReportsFlagParseErrorWithoutSecondaryColorValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		marker string
	}{
		{
			name:   "unknown flag after invalid color",
			args:   []string{"--lang", "en", "compare", "--color=terminal-magic", "--unknown"},
			marker: "flag provided but not defined: -unknown",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) || strings.Contains(stderr, "invalid terminal color mode") {
				t.Fatalf("compare parse error args=%v status=%d stdout=%q stderr=%q", test.args, status, stdout, stderr)
			}
		})
	}
}

func TestCompareAcceptsValidColorsThroughCLI(t *testing.T) {
	root := t.TempDir()
	first := writeLocalizedObservationInput(t, root, "first", "系统", "系统")
	second := writeLocalizedObservationInput(t, root, "second", "完成", "完成")
	for _, raw := range []string{"auto", "none", "basic", "256", "truecolor", "always"} {
		t.Run(raw, func(t *testing.T) {
			output := filepath.Join(root, raw)
			status, stdout, stderr := invokeAppMain(
				"--lang", "en", "compare", first, second, "--format", "json",
				"--output", output, "--name", "comparison", "--color="+raw,
			)
			if status != 0 || stdout == "" || stderr != "" {
				t.Fatalf("valid comparison color %q status=%d stdout=%q stderr=%q", raw, status, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(output, "comparison.json")); err != nil {
				entries, readErr := os.ReadDir(output)
				t.Fatalf("valid comparison color %q did not write output: %v (entries=%v, read=%v)", raw, err, entries, readErr)
			}
		})
	}
}

func TestResolveTerminalColorPreservesExplicitLevelsAndValidatesBeforeNoColor(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want termcolor.Level
	}{
		{raw: "none", want: termcolor.LevelNone},
		{raw: "basic", want: termcolor.LevelBasic},
		{raw: "256", want: termcolor.LevelANSI256},
		{raw: "truecolor", want: termcolor.LevelTrueColor},
	} {
		got, err := resolveTerminalColor(test.raw, false, &bytes.Buffer{})
		if err != nil || got != test.want {
			t.Fatalf("resolveTerminalColor(%q) = %s/%v, want %s/nil", test.raw, got, err, test.want)
		}
		got, err = resolveTerminalColor(test.raw, true, &bytes.Buffer{})
		if err != nil || got != termcolor.LevelNone {
			t.Fatalf("resolveTerminalColor(%q, no-color) = %s/%v, want none/nil", test.raw, got, err)
		}
	}
	for _, raw := range []string{"", "terminal-magic"} {
		if got, err := resolveTerminalColor(raw, true, &bytes.Buffer{}); err == nil || got != termcolor.LevelNone {
			t.Fatalf("resolveTerminalColor(%q, no-color) = %s/%v, want none/error", raw, got, err)
		}
	}
}

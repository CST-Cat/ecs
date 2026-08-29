package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/config"
)

func runInformationCommand(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func TestInformationCommandsSucceed(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "help", args: []string{"help", "--lang", "en"}, marker: "Usage:"},
		{name: "version", args: []string{"version"}, marker: "ecs "},
		{name: "list", args: []string{"list", "--lang", "en"}, marker: "Profiles:"},
		{name: "config example", args: []string{"config", "example"}, marker: `"profile": "standard"`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || stderr != "" || !strings.Contains(stdout, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRootHelpUsesDefaultRunCommand(t *testing.T) {
	for _, argument := range []string{"-h", "--help"} {
		t.Run(argument, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(argument, "--lang=en")
			if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs [run]") || strings.Contains(stderr, "ecs —") {
				t.Fatalf("root %s help status=%d stdout=%q stderr=%q", argument, status, stdout, stderr)
			}
		})
	}
}

func TestMainDispatchesGlobalLanguageBeforeOrAfterCommand(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		stdoutMarker   string
		languageMarker string
		stderrMarker   string
	}{
		{
			name:           "language before list",
			args:           []string{"--lang=en", "list"},
			stdoutMarker:   "Profiles:",
			languageMarker: "Standard profile:",
		},
		{
			name:           "language after list",
			args:           []string{"list", "--lang=en"},
			stdoutMarker:   "Profiles:",
			languageMarker: "Standard profile:",
		},
		{
			name:           "language before Chinese list",
			args:           []string{"--lang=zh", "list"},
			stdoutMarker:   "配置档:",
			languageMarker: "标准配置：",
		},
		{
			name:         "language before version",
			args:         []string{"--lang=zh", "version"},
			stdoutMarker: "ecs ",
		},
		{
			name:         "language after version",
			args:         []string{"version", "--lang=zh"},
			stdoutMarker: "ecs ",
		},
		{
			name:         "language before compare help",
			args:         []string{"--lang=en", "compare", "--help"},
			stderrMarker: "Usage: ecs compare",
		},
		{
			name:         "language after compare help",
			args:         []string{"compare", "--help", "--lang=en"},
			stderrMarker: "Usage: ecs compare",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || (test.stderrMarker == "" && stderr != "") ||
				(test.stdoutMarker != "" && !strings.Contains(stdout, test.stdoutMarker)) ||
				(test.languageMarker != "" && !strings.Contains(stdout, test.languageMarker)) ||
				(test.stderrMarker != "" && !strings.Contains(stderr, test.stderrMarker)) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestMainUsesLastGlobalLanguageAndRejectsLastInvalidValue(t *testing.T) {
	status, stdout, stderr := runInformationCommand("list", "--lang=zh", "--lang=en")
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Standard profile:") || strings.Contains(stdout, "标准配置：") {
		t.Fatalf("last global language (post-command) status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	status, stdout, stderr = runInformationCommand("--lang=en", "list", "--lang=zh")
	if status != 0 || stderr != "" || !strings.Contains(stdout, "标准配置：") || strings.Contains(stdout, "Standard profile:") {
		t.Fatalf("last global language (pre/post-command) status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	status, stdout, stderr = runInformationCommand("--lang=en", "list", "--lang=invalid")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "invalid --lang value \"invalid\"") {
		t.Fatalf("last invalid global language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestMainRejectsMissingOrEmptyGlobalLanguageValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		arg    string
		marker string
	}{
		{name: "long missing", arg: "--lang", marker: "--lang requires a value"},
		{name: "long empty", arg: "--lang=", marker: "--lang value must not be empty"},
		{name: "short missing", arg: "-lang", marker: "--lang requires a value"},
		{name: "short empty", arg: "-lang=", marker: "--lang value must not be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand("list", test.arg)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestMainStopsGlobalLanguageScanAtDoubleDash(t *testing.T) {
	status, stdout, stderr := runInformationCommand("list", "--lang=en", "--", "--lang=en")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "error: unexpected arguments") {
		t.Fatalf("double-dash language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestInformationCommandsReportDistinctFailures(t *testing.T) {
	// list's positional arguments use the shared extra-argument guard; unknown
	// flags and missing flag values are reported by the standard FlagSet.
	// config's missing and unknown subcommands share the same argument guard.
	cases := []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "unknown command", args: []string{"not-a-command", "--lang", "en"}, marker: "unknown command"},
		{name: "list extra argument", args: []string{"list", "--lang", "en", "unexpected"}, marker: "error: unexpected arguments"},
		{name: "config subcommand missing", args: []string{"config", "--lang", "en"}, marker: "Usage: ecs config example"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRemovedBaselineCommandIsUnknownAndDoesNotWriteOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "baseline.json")
	status, stdout, stderr := runInformationCommand("baseline", "--lang", "en", "--output", output)
	if status != 1 || stdout != "" || !strings.Contains(stderr, `unknown command "baseline"`) {
		t.Fatalf("removed baseline command status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if strings.Contains(stderr, "Usage: ecs baseline") {
		t.Fatalf("removed baseline help leaked into stderr: %q", stderr)
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("removed baseline command wrote output: %v", err)
	}
}

func TestListUsesStandardFlagFormsAndLastValue(t *testing.T) {
	for _, test := range []struct {
		name           string
		args           []string
		marker         string
		languageMarker string
	}{
		{name: "long equals", args: []string{"list", "--lang=en"}, marker: "Profiles:", languageMarker: "Standard profile:"},
		{name: "short equals", args: []string{"list", "-lang=en"}, marker: "Profiles:", languageMarker: "Standard profile:"},
		{name: "last repeated value", args: []string{"list", "--lang=zh", "-lang=en"}, marker: "Profiles:", languageMarker: "Standard profile:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || stderr != "" || !strings.Contains(stdout, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if !strings.Contains(stdout, test.languageMarker) {
				t.Fatalf("English language was not applied for %v: stdout=%q", test.args, stdout)
			}
		})
	}
}

func TestListReportsStandardFlagErrors(t *testing.T) {
	t.Run("unknown flag", func(t *testing.T) {
		status, stdout, stderr := runInformationCommand("list", "--unexpected", "--lang=en")
		if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined: -unexpected") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
	t.Run("missing language value", func(t *testing.T) {
		status, stdout, stderr := runInformationCommand("list", "--lang")
		if status != 1 || stdout != "" || !strings.Contains(stderr, "--lang requires a value") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
}

func TestListShowsCanonicalIPQualitySourceIDsInStableOrder(t *testing.T) {
	expected := "  " + strings.Join(config.IPQualitySourceIDs(), ", ") + "\n"
	for _, language := range []string{"zh", "en"} {
		t.Run(language, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand("list", "--lang="+language)
			if status != 0 || stderr != "" || !strings.Contains(stdout, expected) {
				t.Fatalf("status=%d stdout=%q stderr=%q, want canonical source list %q", status, stdout, stderr, expected)
			}
		})
	}
}

func TestListRejectsRemovedMachineManifestFormats(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "machine flag", args: []string{"list", "--machine", "--lang", "en"}},
		{name: "machine format value", args: []string{"list", "--format", "machine", "--lang", "en"}},
		{name: "machine format equals", args: []string{"list", "--format=machine", "--lang", "en"}},
		{name: "manifest format value", args: []string{"list", "--format", "manifest", "--lang", "en"}},
		{name: "manifest format equals", args: []string{"list", "--format=manifest", "--lang", "en"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined") {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "ecs-module-manifest") {
				t.Fatalf("removed module manifest output leaked: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

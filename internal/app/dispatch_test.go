package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"ecs/internal/config"
)

func runInformationCommand(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), args, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

func runListCommand(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	status := listCommand(args, &stdout, &stderr)
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
		status, stdout, stderr := runListCommand("--lang")
		if status != 1 || stdout != "" || !strings.Contains(stderr, "flag needs an argument: -lang") {
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

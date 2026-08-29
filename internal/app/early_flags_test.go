package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func TestScanEarlyFlagsSupportsLongShortAndEqualsForms(t *testing.T) {
	got := scanEarlyFlags([]string{
		"--name", "long",
		"-name", "short",
		"--name=long-equal",
		"-name=short-equal",
		"--lang", "en",
		"-config", "short.json",
		"--profile=full",
	}, "name", "lang", "config", "profile")
	want := []earlyFlagOccurrence{
		{Name: "name", Value: "long"},
		{Name: "name", Value: "short"},
		{Name: "name", Value: "long-equal"},
		{Name: "name", Value: "short-equal"},
		{Name: "lang", Value: "en"},
		{Name: "config", Value: "short.json"},
		{Name: "profile", Value: "full"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("early flag occurrences = %#v, want %#v", got, want)
	}
}

func TestScanEarlyFlagsStopsAtDelimiterAndDoesNotConsumeMissingValues(t *testing.T) {
	got := scanEarlyFlags([]string{
		"--name", "before",
		"--name", "--unrelated",
		"-name", "-other",
		"--name=",
		"--",
		"--name", "after",
		"--lang", "en",
	}, "name", "lang")
	want := []earlyFlagOccurrence{{Name: "name", Value: "before"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("early flag delimiter/missing-value handling = %#v, want %#v", got, want)
	}
}

func TestEarlyFlagsKeepLastValueAndLanguageCanSurroundCommand(t *testing.T) {
	configPath, profile := preparse([]string{
		"run", "--profile", "first", "--profile=second",
		"-config", "first.json", "--config=second.json",
	})
	if configPath != "second.json" || profile != "second" {
		t.Fatalf("last valid early values = %q/%q, want second.json/second", configPath, profile)
	}

	for _, args := range [][]string{
		{"--lang", "zh", "run", "--lang=en"},
		{"run", "-lang=zh", "--lang", "en"},
	} {
		if got := resolveLanguage(args); got != i18n.LangEN {
			t.Fatalf("language for %v = %s, want %s", args, got, i18n.LangEN)
		}
	}
	if err := validateExplicitLanguage([]string{"--lang=en", "--lang=invalid"}); err == nil {
		t.Fatal("last invalid language value was accepted")
	}
	if got := resolveLanguage([]string{"--lang=invalid", "--lang=en"}); got != i18n.LangEN {
		t.Fatalf("last valid language value = %s, want %s", got, i18n.LangEN)
	}
}

func TestEarlyFlagsUseLastExplicitLanguageValue(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want i18n.Lang
	}{
		{name: "long equals", args: []string{"--lang=en", "--lang=zh"}, want: i18n.LangZH},
		{name: "short equals", args: []string{"-lang=en", "-lang=zh"}, want: i18n.LangZH},
		{name: "separate values", args: []string{"--lang", "en", "--lang", "zh"}, want: i18n.LangZH},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateExplicitLanguage(test.args); err != nil {
				t.Fatalf("language validation: %v", err)
			}
			if got := resolveLanguage(test.args); got != test.want {
				t.Fatalf("resolveLanguage(%v) = %s, want %s", test.args, got, test.want)
			}
		})
	}
}

func TestResolveLanguageWithoutExplicitValueUsesEnvironment(t *testing.T) {
	for _, key := range []string{"ECS_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	t.Setenv("LANG", "en_US.UTF-8")
	if got := resolveLanguage([]string{"list"}); got != i18n.LangEN {
		t.Fatalf("resolveLanguage without --lang = %s, want %s", got, i18n.LangEN)
	}
}

func TestEarlyFlagsRejectInvalidMissingAndEmptyLanguage(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "long equals", args: []string{"--lang=invalid"}, marker: "invalid --lang value \"invalid\""},
		{name: "short equals", args: []string{"-lang=invalid"}, marker: "invalid --lang value \"invalid\""},
		{name: "separate value", args: []string{"--lang", "invalid"}, marker: "invalid --lang value \"invalid\""},
		{name: "missing value", args: []string{"--lang"}, marker: "--lang requires a value"},
		{name: "empty value", args: []string{"--lang="}, marker: "--lang value must not be empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateExplicitLanguage(test.args); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("validateExplicitLanguage(%v) = %v, want marker %q", test.args, err, test.marker)
			}
		})
	}
}

func TestMainUsesLastExplicitLanguageValue(t *testing.T) {
	status, stdout, stderr := invokeAppMain("list", "--lang=invalid", "--lang=en")
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Profiles:") {
		t.Fatalf("last valid language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	status, stdout, stderr = invokeAppMain("run", "--lang=en", "--lang=invalid")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "invalid --lang value \"invalid\"") {
		t.Fatalf("last invalid language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestMainRejectsInvalidLanguageAtCommandEntry(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "run", args: []string{`run`, `--lang=invalid`}},
		{name: "list", args: []string{`list`, `-lang=invalid`}},
		{name: "compare", args: []string{`compare`, `--lang`, `invalid`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "invalid --lang value \"invalid\"") {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestPreparseShortFormsAndCLIProfilePrecedence(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"profile":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	resolved, err := resolveRunConfig([]string{
		"-config", configPath, "-profile", config.ProfileStandard,
		"--only", "system",
	}, &stderr)
	if err != nil {
		t.Fatalf("short config/profile resolution: %v (stderr=%q)", err, stderr.String())
	}
	if resolved.Runtime.Profile != config.ProfileStandard {
		t.Fatalf("CLI profile did not override config profile: %q", resolved.Runtime.Profile)
	}

	var equalStderr bytes.Buffer
	equalResolved, err := resolveRunConfig([]string{
		"--config=" + configPath, "--only=system",
	}, &equalStderr)
	if err != nil {
		t.Fatalf("equal config resolution: %v (stderr=%q)", err, equalStderr.String())
	}
	if equalResolved.Runtime.Profile != config.ProfileFull {
		t.Fatalf("config profile from equal form = %q, want %q", equalResolved.Runtime.Profile, config.ProfileFull)
	}
}

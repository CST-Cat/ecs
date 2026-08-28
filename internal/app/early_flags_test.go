package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
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

func TestEarlyFlagsKeepLastValidValueAndLanguageCanSurroundCommand(t *testing.T) {
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
	if got := resolveLanguage([]string{"--lang=en", "--lang", "--profile", "full", "--lang=invalid"}); got != i18n.LangEN {
		t.Fatalf("invalid/missing language value changed result to %s", got)
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

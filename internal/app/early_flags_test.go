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

func TestGlobalLanguagePrefixConsumesOnlyLeadingLanguageFlags(t *testing.T) {
	args := []string{"--lang", "en", "-lang=zh", "compare", "--name", "--lang"}
	got, rest := globalLanguagePrefix(args)
	want := []languageFlagOccurrence{{Value: "en"}, {Value: "zh"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("global language prefix = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(rest, args[3:]) {
		t.Fatalf("remaining argv = %v, want %v", rest, args[3:])
	}
}

func TestGlobalLanguagePrefixDoesNotInspectCommandArguments(t *testing.T) {
	for _, args := range [][]string{
		{"compare", "--name", "--lang", "a.json", "b.json"},
		{"render", "--input", "--lang"},
		{"--", "--lang", "en"},
	} {
		got, rest := globalLanguagePrefix(args)
		if len(got) != 0 || !reflect.DeepEqual(rest, args) {
			t.Fatalf("globalLanguagePrefix(%v) = %#v, %v", args, got, rest)
		}
	}
}

func TestGlobalLanguagePrefixDistinguishesMissingAndEmptyValues(t *testing.T) {
	for _, test := range []struct {
		args []string
		want languageFlagOccurrence
	}{
		{args: []string{"--lang"}, want: languageFlagOccurrence{Missing: true}},
		{args: []string{"--lang", "--"}, want: languageFlagOccurrence{Missing: true}},
		{args: []string{"--lang="}, want: languageFlagOccurrence{}},
		{args: []string{"--lang", ""}, want: languageFlagOccurrence{}},
	} {
		got, _ := globalLanguagePrefix(test.args)
		if len(got) != 1 || got[0] != test.want {
			t.Fatalf("globalLanguagePrefix(%v) = %#v, want %#v", test.args, got, test.want)
		}
	}
}

func TestDispatchLeavesRunLikeLanguageForFormalParser(t *testing.T) {
	args := []string{"plan", "--name", "--lang=en", "--only", "system"}
	command, commandArgs := dispatchCommand(args)
	if command != "plan" || !reflect.DeepEqual(commandArgs, args[1:]) {
		t.Fatalf("run-like dispatch = %q %v, want plan %v", command, commandArgs, args[1:])
	}
}

func TestGlobalLanguagePrefixUsesLastExplicitValue(t *testing.T) {
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
			occurrences, rest := globalLanguagePrefix(test.args)
			if len(rest) != 0 {
				t.Fatalf("remaining argv = %v", rest)
			}
			if err := validateExplicitLanguage(occurrences); err != nil {
				t.Fatalf("language validation: %v", err)
			}
			if got := resolveLanguage(occurrences); got != test.want {
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
	if got := resolveLanguage(nil); got != i18n.LangEN {
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
			occurrences, _ := globalLanguagePrefix(test.args)
			err := validateExplicitLanguage(occurrences)
			if err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("validateExplicitLanguage(%v) = %v, want marker %q", test.args, err, test.marker)
			}
		})
	}
}

func TestMainUsesLastExplicitLanguageValue(t *testing.T) {
	status, stdout, stderr := invokeAppMain("--lang=invalid", "--lang=en", "list")
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
		{name: "list", args: []string{`-lang=invalid`, `list`}},
		{name: "compare", args: []string{`--lang`, `invalid`, `compare`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := invokeAppMain(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, "invalid --lang value \"invalid\"") {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestRunConfigShortFormsAndCLIProfilePrecedence(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"profile":"full","exposure":"local","reveal":true,"formats":["md"],"output":"from-file"}`), 0o600); err != nil {
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
	if resolved.Runtime.Exposure != config.ExposureLocal || !resolved.Runtime.Reveal || !reflect.DeepEqual(resolved.Runtime.Formats, []string{"md"}) || resolved.Runtime.Output != "from-file" {
		t.Fatalf("config values were not retained under CLI profile override: exposure=%s reveal=%t formats=%v output=%q", resolved.Runtime.Exposure, resolved.Runtime.Reveal, resolved.Runtime.Formats, resolved.Runtime.Output)
	}

	var overlayStderr bytes.Buffer
	overlay, err := resolveRunConfig([]string{
		"--config", configPath, "--profile", config.ProfileStandard,
		"--exposure", "any", "--reveal=false", "--format", "json", "--output", "from-cli",
		"--only", "system",
	}, &overlayStderr)
	if err != nil {
		t.Fatalf("explicit CLI overlay: %v (stderr=%q)", err, overlayStderr.String())
	}
	if overlay.Runtime.Exposure != config.ExposureConsent || overlay.Runtime.Reveal || !reflect.DeepEqual(overlay.Runtime.Formats, []string{"json"}) || overlay.Runtime.Output != "from-cli" {
		t.Fatalf("explicit CLI values did not override config: exposure=%s reveal=%t formats=%v output=%q", overlay.Runtime.Exposure, overlay.Runtime.Reveal, overlay.Runtime.Formats, overlay.Runtime.Output)
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

func TestRunConfigCanonicalizesEmptyExplicitProfile(t *testing.T) {
	for _, args := range [][]string{
		{"--profile="},
		{"--profile", ""},
	} {
		resolved, err := resolveRunConfig(args, &bytes.Buffer{})
		if err != nil {
			t.Fatalf("resolveRunConfig(%q) = %v, want empty profile to select standard", args, err)
		}
		if resolved.Runtime.Profile != config.ProfileStandard {
			t.Fatalf("resolveRunConfig(%q) profile = %q, want %q", args, resolved.Runtime.Profile, config.ProfileStandard)
		}
	}
	if _, err := resolveRunConfig([]string{"--profile=invalid"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("invalid non-empty profile error = %v, want rejection", err)
	}
}

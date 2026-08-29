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
		{Name: "name", Value: "long", Position: 0, End: 2, HasValue: true},
		{Name: "name", Value: "short", Position: 2, End: 4, HasValue: true},
		{Name: "name", Value: "long-equal", Position: 4, End: 5, HasValue: true, HasEquals: true},
		{Name: "name", Value: "short-equal", Position: 5, End: 6, HasValue: true, HasEquals: true},
		{Name: "lang", Value: "en", Position: 6, End: 8, HasValue: true},
		{Name: "config", Value: "short.json", Position: 8, End: 10, HasValue: true},
		{Name: "profile", Value: "full", Position: 10, End: 11, HasValue: true, HasEquals: true},
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
	want := []earlyFlagOccurrence{
		{Name: "name", Value: "before", Position: 0, End: 2, HasValue: true},
		{Name: "name", Position: 2, End: 3, Missing: true},
		{Name: "name", Position: 4, End: 5, Missing: true},
		{Name: "name", Position: 6, End: 7, HasValue: true, HasEquals: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("early flag delimiter/missing-value handling = %#v, want %#v", got, want)
	}
}

func TestScanEarlyFlagsDistinguishesEmptySeparateValues(t *testing.T) {
	got := scanEarlyFlags([]string{"--lang", "", "-lang", "--other", "-lang="}, "lang")
	want := []earlyFlagOccurrence{
		{Name: "lang", Position: 0, End: 2, HasValue: true},
		{Name: "lang", Position: 2, End: 3, Missing: true},
		{Name: "lang", Position: 4, End: 5, HasValue: true, HasEquals: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("language value states = %#v, want %#v", got, want)
	}
}

func TestScanEarlyFlagsLeavesOptionLikeValuesToFormalParser(t *testing.T) {
	if got := scanEarlyFlags([]string{"plan", "--name", "--lang=en", "--only", "system"}, "lang"); len(got) != 0 {
		t.Fatalf("language occurrences in --name value = %#v, want none", got)
	}

	got := scanEarlyFlags([]string{"plan", "--name", "value", "--lang=en"}, "lang")
	want := []earlyFlagOccurrence{{Name: "lang", Value: "en", Position: 3, End: 4, HasValue: true, HasEquals: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("language after formal option value = %#v, want %#v", got, want)
	}
}

func TestEarlyFlagsKeepLastLanguageValueAndLanguageCanSurroundCommand(t *testing.T) {
	for _, args := range [][]string{
		{"--lang", "zh", "run", "--lang=en"},
		{"run", "-lang=zh", "--lang", "en"},
	} {
		if got := resolveLanguage(scanEarlyFlags(args, "lang")); got != i18n.LangEN {
			t.Fatalf("language for %v = %s, want %s", args, got, i18n.LangEN)
		}
	}
	if err := validateExplicitLanguage(scanEarlyFlags([]string{"--lang=en", "--lang=invalid"}, "lang")); err == nil {
		t.Fatal("last invalid language value was accepted")
	}
	if got := resolveLanguage(scanEarlyFlags([]string{"--lang=invalid", "--lang=en"}, "lang")); got != i18n.LangEN {
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
			occurrences := scanEarlyFlags(test.args, "lang")
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
	if got := resolveLanguage(scanEarlyFlags([]string{"list"}, "lang")); got != i18n.LangEN {
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
			err := validateExplicitLanguage(scanEarlyFlags(test.args, "lang"))
			if err == nil || !strings.Contains(err.Error(), test.marker) {
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

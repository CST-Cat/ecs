package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"ecs/internal/config"
	"ecs/internal/i18n"
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
		{name: "help", args: []string{"--lang", "en", "help"}, marker: "Usage:"},
		{name: "version", args: []string{"version"}, marker: "ecs "},
		{name: "list", args: []string{"--lang", "en", "list"}, marker: "Profiles:"},
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

func TestPrintHelpTaglinesQualifyReportUploads(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	for _, test := range []struct {
		name      string
		language  i18n.Lang
		qualifier string
		forbidden []string
	}{
		{
			name:      "Chinese",
			language:  i18n.LangZH,
			qualifier: "默认不上传报告",
			forbidden: []string{"默认零上传", "全程零上传"},
		},
		{
			name:      "English",
			language:  i18n.LangEN,
			qualifier: "does not upload reports by default",
			forbidden: []string{"never uploads by default", "nothing was ever uploaded", "nothing is ever uploaded"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			i18n.Set(test.language)
			var output bytes.Buffer
			printHelp(&output)
			tagline := strings.SplitN(output.String(), "\n", 2)[0]
			if !strings.Contains(tagline, test.qualifier) {
				t.Fatalf("help tagline = %q, want report upload qualifier %q", tagline, test.qualifier)
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(tagline, forbidden) {
					t.Fatalf("help tagline contains absolute upload wording %q: %q", forbidden, tagline)
				}
			}
		})
	}
}

func TestRootHelpUsesDefaultRunCommand(t *testing.T) {
	for _, argument := range []string{"-h", "--help"} {
		t.Run(argument, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand("--lang=en", argument)
			if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs [run]") || strings.Contains(stderr, "ecs —") {
				t.Fatalf("root %s help status=%d stdout=%q stderr=%q", argument, status, stdout, stderr)
			}
		})
	}
}

func TestMainDispatchesGlobalLanguagePrefix(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		stdoutMarker   string
		languageMarker string
		stderrMarker   string
	}{
		{
			name:           "equals language before list",
			args:           []string{"--lang=en", "list"},
			stdoutMarker:   "Profiles:",
			languageMarker: "Standard profile:",
		},
		{
			name:           "separate language before list",
			args:           []string{"--lang", "en", "list"},
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
			name:         "language before compare help",
			args:         []string{"--lang=en", "compare", "--help"},
			stderrMarker: "Usage: ecs compare",
		},
		{
			name:         "language before render help",
			args:         []string{"--lang=en", "render", "--help"},
			stderrMarker: "Usage of ecs render",
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

func TestMainLocalizesRunHelpAfterFormalLanguage(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		args       []string
		want       string
		forbidden  []string
		noHanRunes bool
	}{
		{
			name:       "late English language",
			env:        "zh",
			args:       []string{"run", "--name", "value", "--lang=en", "--help"},
			want:       "Usage: ecs [run] [options]",
			forbidden:  []string{"用法:", "仅测试", "配置档", "三网回程", "报告文件名前缀"},
			noHanRunes: true,
		},
		{
			name:      "late Chinese language",
			env:       "en",
			args:      []string{"run", "--name", "value", "--lang=zh", "--help"},
			want:      "用法: ecs [run] [选项]",
			forbidden: []string{"Usage: ecs [run] [options]", "test IPv4 only", "profile: standard, full", "run only these modules", "report filename prefix"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ECS_LANG", test.env)
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("late language help status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(stderr, forbidden) {
					t.Fatalf("late language help contains %q: %q", forbidden, stderr)
				}
			}
			if test.noHanRunes {
				for _, runeValue := range stderr {
					if unicode.Is(unicode.Han, runeValue) {
						t.Fatalf("English late language help contains Han text: %q", stderr)
					}
				}
			}
		})
	}
}

func TestRunAndPlanFlagSetsOwnLateLanguage(t *testing.T) {
	t.Setenv("ECS_LANG", "zh")
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "implicit run", args: []string{"--profile", "full", "--lang=en", "--help"}},
		{name: "explicit run", args: []string{"run", "--profile", "full", "--lang=en", "--help"}},
		{name: "plan", args: []string{"plan", "--profile", "full", "--lang=en", "--help"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 0 || stdout != "" || !strings.Contains(stderr, "Usage: ecs [run] [options]") || strings.Contains(stderr, "用法:") {
				t.Fatalf("late run language args=%v status=%d stdout=%q stderr=%q", test.args, status, stdout, stderr)
			}
		})
	}
}

func TestMainUsesLastGlobalLanguageAndRejectsLastInvalidValue(t *testing.T) {
	status, stdout, stderr := runInformationCommand("--lang=zh", "--lang=en", "list")
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Standard profile:") || strings.Contains(stdout, "标准配置：") {
		t.Fatalf("last global language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	status, stdout, stderr = runInformationCommand("--lang=en", "--lang=invalid", "list")
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
			status, stdout, stderr := runInformationCommand(test.arg)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestMainStopsGlobalLanguageScanAtDoubleDash(t *testing.T) {
	status, stdout, stderr := runInformationCommand("--lang=en", "compare", "--", "--lang", "x.json")
	if status != 1 || stdout != "" || !strings.Contains(stderr, "--lang") ||
		strings.Contains(stderr, "invalid --lang") || strings.Contains(stderr, "--lang requires a value") {
		t.Fatalf("double-dash language status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestMainDoesNotStealOptionLookingCommandValues(t *testing.T) {
	t.Setenv("ECS_LANG", "en")
	for _, test := range []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "compare name", args: []string{"compare", "--name", "--lang", "a.json", "b.json"}, marker: "a.json"},
		{name: "render input", args: []string{"render", "--input", "--lang"}, marker: "open --lang"},
		{name: "render name", args: []string{"render", "--name", "--lang"}, marker: "--input is required"},
		{name: "submit note", args: []string{"submit", "--note", "--lang"}, marker: "--input is required"},
		{name: "leaderboard source", args: []string{"leaderboard", "--source", "--lang"}, marker: "at least one report"},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand(test.args...)
			if status != 1 || stdout != "" || !strings.Contains(stderr, test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			for _, forbidden := range []string{"invalid --lang", "--lang requires a value", "--lang value must not be empty"} {
				if strings.Contains(stderr, forbidden) {
					t.Fatalf("Main stole command value: args=%v stderr=%q", test.args, stderr)
				}
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
		{name: "unknown command", args: []string{"--lang", "en", "not-a-command"}, marker: "unknown command"},
		{name: "list extra argument", args: []string{"--lang", "en", "list", "unexpected"}, marker: "error: unexpected arguments"},
		{name: "config subcommand missing", args: []string{"--lang", "en", "config"}, marker: "Usage: ecs config example"},
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
	status, stdout, stderr := runInformationCommand("--lang", "en", "baseline", "--output", output)
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
		{name: "long equals", args: []string{"--lang=en", "list"}, marker: "Profiles:", languageMarker: "Standard profile:"},
		{name: "short equals", args: []string{"-lang=en", "list"}, marker: "Profiles:", languageMarker: "Standard profile:"},
		{name: "last repeated value", args: []string{"--lang=zh", "-lang=en", "list"}, marker: "Profiles:", languageMarker: "Standard profile:"},
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
		status, stdout, stderr := runInformationCommand("--lang=en", "list", "--unexpected")
		if status != 1 || stdout != "" || !strings.Contains(stderr, "flag provided but not defined: -unexpected") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
	t.Run("missing language value", func(t *testing.T) {
		status, stdout, stderr := runInformationCommand("--lang")
		if status != 1 || stdout != "" || !strings.Contains(stderr, "--lang requires a value") {
			t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	})
}

func TestRunReportsStandardMissingLanguageValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{"run", "--lang"}, &stdout, &stderr)
	if status != 1 || stdout.Len() != 0 {
		t.Fatalf("run --lang status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	if count := strings.Count(stderr.String(), "flag needs an argument: -lang"); count != 1 {
		t.Fatalf("run --lang parser error count=%d, want one: stderr=%q", count, stderr.String())
	}
}

func TestListShowsCanonicalIPQualitySourceIDsInStableOrder(t *testing.T) {
	expected := "  " + strings.Join(config.IPQualitySourceIDs(), ", ") + "\n"
	for _, language := range []string{"zh", "en"} {
		t.Run(language, func(t *testing.T) {
			status, stdout, stderr := runInformationCommand("--lang="+language, "list")
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
		{name: "machine flag", args: []string{"--lang", "en", "list", "--machine"}},
		{name: "machine format value", args: []string{"--lang", "en", "list", "--format", "machine"}},
		{name: "machine format equals", args: []string{"--lang", "en", "list", "--format=machine"}},
		{name: "manifest format value", args: []string{"--lang", "en", "list", "--format", "manifest"}},
		{name: "manifest format equals", args: []string{"--lang", "en", "list", "--format=manifest"}},
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

func TestRunConfigExplicitEmptyCLIOverridesFileCollections(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	content := `{"formats":["md"],"dns_resolvers":[{"name":"file-dns","address":"1.1.1.1:53"}],"iperf_targets":[{"name":"file-iperf","host":"example.com","port_start":5201,"port_end":5201}],"media_regions":["jp"],"backtrace_targets":[{"name":"file-backtrace","address":"1.1.1.1","kind":"telecom"}],"ookla_servers":[{"carrier":"telecom","id":1}]}`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	resolved, err := resolveRunConfig([]string{
		"--config", configPath,
		"--dns-resolvers", "",
		"--iperf-targets", "",
		"--media-region", "",
		"--backtrace-city", "",
		"--ookla-servers", "",
	}, &stderr)
	if err != nil {
		t.Fatalf("explicit empty CLI overlay: %v (stderr=%q)", err, stderr.String())
	}
	if len(resolved.Runtime.DNSResolvers) != 0 || len(resolved.Runtime.IPerfTargets) != 0 || len(resolved.Runtime.MediaRegions) != 0 || len(resolved.Runtime.BacktraceTargets) != 0 || len(resolved.Runtime.OoklaServers) != 0 {
		t.Fatalf("explicit empty CLI collections were not applied: dns=%v iperf=%v media=%v backtrace=%v ookla=%v", resolved.Runtime.DNSResolvers, resolved.Runtime.IPerfTargets, resolved.Runtime.MediaRegions, resolved.Runtime.BacktraceTargets, resolved.Runtime.OoklaServers)
	}
	if err := config.Validate(resolved.Runtime); err != nil {
		t.Fatalf("valid runtime with empty optional collections rejected: %v", err)
	}
	for _, raw := range []string{" ", ",,"} {
		resolved, err = resolveRunConfig([]string{
			"--config", configPath,
			"--backtrace-city", raw,
		}, &stderr)
		if err != nil {
			t.Fatalf("explicit effectively empty backtrace city %q: %v (stderr=%q)", raw, err, stderr.String())
		}
		if len(resolved.Runtime.BacktraceTargets) != 0 {
			t.Fatalf("backtrace city %q retained file targets: %v", raw, resolved.Runtime.BacktraceTargets)
		}
	}

	resolved, err = resolveRunConfig([]string{
		"--config", configPath,
		"--format", "",
	}, &stderr)
	if err != nil {
		t.Fatalf("explicit empty format overlay: %v (stderr=%q)", err, stderr.String())
	}
	if len(resolved.Runtime.Formats) != 0 {
		t.Fatalf("explicit empty formats = %#v, want empty", resolved.Runtime.Formats)
	}
	if err := config.Validate(resolved.Runtime); err == nil || !strings.Contains(err.Error(), "at least one output format") {
		t.Fatalf("explicit empty format validation error = %v, want no-formats error", err)
	}
}

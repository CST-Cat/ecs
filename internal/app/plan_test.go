package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestPlanJSONUsesRunResolverAndDescribesStaging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--profile", "full",
		"--only", "cpu,ookla,zstd", "--exposure", "any",
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("plan status=%d stderr=%q", status, stderr.String())
	}
	var plan struct {
		SchemaVersion string                         `json:"schema_version"`
		Tool          struct{ Name, Version string } `json:"tool"`
		Profile       string                         `json:"profile"`
		Exposure      string                         `json:"exposure"`
		Reveal        bool                           `json:"reveal"`
		IPVersion     string                         `json:"ip_version"`
		Modules       []struct {
			ID string `json:"id"`
		} `json:"modules"`
		RequiredTools    []string `json:"required_tools"`
		NeedsEgressIP    bool     `json:"needs_egress_ip"`
		ExternalServices []string `json:"external_services"`
		Staging          struct {
			ToolArchiveRequired  bool     `json:"tool_archive_required"`
			ToolArchiveTools     []string `json:"tool_archive_tools"`
			OoklaPackageRequired bool     `json:"ookla_package_required"`
			ZstdCorpusRequired   bool     `json:"zstd_corpus_required"`
		} `json:"staging"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != "ecs.plan/v1" || plan.Profile != "full" {
		t.Fatalf("plan identity = %#v", plan)
	}
	if plan.Tool.Name != "ecs" || plan.Tool.Version == "" || plan.Exposure != "any" || plan.Reveal || plan.IPVersion != config.IPVersionAuto {
		t.Fatalf("plan top-level machine facts = %#v", plan)
	}
	if got := []string{plan.Modules[0].ID, plan.Modules[1].ID, plan.Modules[2].ID}; !strings.EqualFold(strings.Join(got, ","), "cpu,zstd,ookla") {
		t.Fatalf("selected modules = %v", got)
	}
	if len(plan.RequiredTools) != 3 || !reflect.DeepEqual(plan.ExternalServices, []string{"third-party-provider", "ookla"}) || plan.NeedsEgressIP {
		t.Fatalf("machine tool/external metadata = %#v / %#v / %v", plan.RequiredTools, plan.ExternalServices, plan.NeedsEgressIP)
	}
	if !plan.Staging.ToolArchiveRequired || !plan.Staging.OoklaPackageRequired || !plan.Staging.ZstdCorpusRequired {
		t.Fatalf("staging = %#v", plan.Staging)
	}
	if strings.Contains(stdout.String(), "标准") || strings.Contains(stdout.String(), "完整配置") {
		t.Fatalf("machine plan contains localized prose: %s", stdout.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	wantTopLevel := []string{
		"exposure", "external_services", "ip_version", "modules", "needs_egress_ip",
		"profile", "required_tools", "reveal", "schema_version", "staging", "tool",
	}
	gotTopLevel := make([]string, 0, len(raw))
	for key := range raw {
		gotTopLevel = append(gotTopLevel, key)
	}
	sort.Strings(gotTopLevel)
	if !reflect.DeepEqual(gotTopLevel, wantTopLevel) {
		t.Fatalf("plan top-level keys = %v, want %v", gotTopLevel, wantTopLevel)
	}
	var rawModules []map[string]json.RawMessage
	if err := json.Unmarshal(raw["modules"], &rawModules); err != nil {
		t.Fatal(err)
	}
	for index, module := range rawModules {
		if len(module) != 1 {
			keys := make([]string, 0, len(module))
			for key := range module {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			t.Fatalf("module %d keys = %v, want [id]", index, keys)
		}
		if _, ok := module["id"]; !ok {
			t.Fatalf("module %d has no id: %#v", index, module)
		}
	}
}

func TestPlanJSONPreservesRevealSelection(t *testing.T) {
	for _, test := range []struct {
		name     string
		exposure string
		reveal   bool
	}{
		{name: "local false", exposure: "local", reveal: false},
		{name: "local true", exposure: "local", reveal: true},
		{name: "public false", exposure: "public", reveal: false},
		{name: "public true", exposure: "public", reveal: true},
		{name: "thirdparty false", exposure: "thirdparty", reveal: false},
		{name: "thirdparty true", exposure: "thirdparty", reveal: true},
		{name: "any false", exposure: "any", reveal: false},
		{name: "any true", exposure: "any", reveal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			revealArg := "--reveal=false"
			if test.reveal {
				revealArg = "--reveal=true"
			}
			status := Main(context.Background(), []string{
				"plan", "--lang", "en", "--profile", "standard",
				"--only", "system", "--exposure", test.exposure, revealArg,
			}, &stdout, &stderr)
			if status != 0 || stderr.Len() != 0 {
				t.Fatalf("plan status=%d stderr=%q", status, stderr.String())
			}
			var plan struct {
				SchemaVersion string `json:"schema_version"`
				Exposure      string `json:"exposure"`
				Reveal        *bool  `json:"reveal"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.SchemaVersion != "ecs.plan/v1" || plan.Exposure != test.exposure || plan.Reveal == nil || *plan.Reveal != test.reveal {
				t.Fatalf("plan identity/privacy = %#v", plan)
			}
			if strings.Contains(stdout.String(), "本机") || strings.Contains(stdout.String(), "完整") {
				t.Fatalf("machine plan contains localized prose: %s", stdout.String())
			}
		})
	}
}

func TestPlanCommandUsesFormalParserForFlagAsValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--name", "--profile=full", "--only", "system",
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("plan status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	var plan struct {
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Profile != config.ProfileStandard {
		t.Fatalf("--profile=full consumed as --name value changed profile to %q", plan.Profile)
	}
}

func TestPlanCommandPreservesConfigAndCLIPrecedence(t *testing.T) {
	root := t.TempDir()
	configs := map[string]string{
		"full":     `{"profile":"full","exposure":"local","reveal":true}`,
		"standard": `{"profile":"standard","exposure":"any","reveal":true}`,
	}
	paths := make(map[string]string, len(configs))
	for name, content := range configs {
		path := filepath.Join(root, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}

	for _, test := range []struct {
		name        string
		configArg   []string
		profileArg  []string
		exposure    string
		wantProfile string
		wantReveal  bool
	}{
		{
			name:        "space config and equals profile",
			configArg:   []string{"--config", paths["full"]},
			profileArg:  []string{"--profile=standard"},
			exposure:    "any",
			wantProfile: config.ProfileStandard,
			wantReveal:  false,
		},
		{
			name:        "equals config and space profile",
			configArg:   []string{"--config=" + paths["standard"]},
			profileArg:  []string{"--profile", "full"},
			exposure:    "any",
			wantProfile: config.ProfileFull,
			wantReveal:  false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"plan", "--lang", "en"}
			args = append(args, test.configArg...)
			args = append(args, test.profileArg...)
			args = append(args, "--exposure", test.exposure, "--reveal=false", "--only", "system")
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), args, &stdout, &stderr)
			if status != 0 || stderr.Len() != 0 {
				t.Fatalf("plan status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
			var plan struct {
				Profile  string `json:"profile"`
				Exposure string `json:"exposure"`
				Reveal   bool   `json:"reveal"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			if plan.Profile != test.wantProfile || plan.Exposure != test.exposure || plan.Reveal != test.wantReveal {
				t.Fatalf("resolved plan = %#v, want profile=%q exposure=%q reveal=%t", plan, test.wantProfile, test.exposure, test.wantReveal)
			}
		})
	}
}

func TestPlanCommandKeepsLastExplicitProfileAndConfig(t *testing.T) {
	root := t.TempDir()
	firstConfig := filepath.Join(root, "first.json")
	secondConfig := filepath.Join(root, "second.json")
	if err := os.WriteFile(firstConfig, []byte(`{"profile":"standard"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondConfig, []byte(`{"profile":"full"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--config", firstConfig, "--config=" + secondConfig,
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("repeated config plan status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	var plan struct {
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Profile != config.ProfileFull {
		t.Fatalf("last config profile = %q, want %q", plan.Profile, config.ProfileFull)
	}

	stdout.Reset()
	stderr.Reset()
	status = Main(context.Background(), []string{
		"plan", "--lang", "en", "--profile", "full", "--profile=standard", "--only", "system",
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("repeated profile plan status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	plan.Profile = ""
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Profile != config.ProfileStandard {
		t.Fatalf("last CLI profile = %q, want %q", plan.Profile, config.ProfileStandard)
	}
}

func TestPlanKeepsTopLevelEgressFactsWhenModulesOnlyExposeIDs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--only", "network", "--exposure", "thirdparty",
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("plan status=%d stderr=%q", status, stderr.String())
	}
	var plan struct {
		Modules          []struct{ ID string } `json:"modules"`
		NeedsEgressIP    bool                  `json:"needs_egress_ip"`
		ExternalServices []string              `json:"external_services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Modules) != 1 || plan.Modules[0].ID != "network" || !plan.NeedsEgressIP || !reflect.DeepEqual(plan.ExternalServices, []string{"egress-ip-discovery", "third-party-provider"}) {
		t.Fatalf("top-level egress facts = %#v", plan)
	}
}

func TestResolveRunConfigPlanOverridesAreLastWins(t *testing.T) {
	var stderr bytes.Buffer
	resolved, err := resolveRunConfig([]string{
		"--only", "system",
		"--exposure", "any", "--reveal=true",
		// The plan-derived values are appended after the original CLI values.
		"--exposure", "local", "--reveal=false",
	}, &stderr)
	if err != nil {
		t.Fatalf("resolve run config: %v (stderr=%q)", err, stderr.String())
	}
	if resolved.Runtime.Exposure != config.ExposureLocal || resolved.Runtime.Reveal {
		t.Fatalf("last exposure/reveal values did not win: exposure=%s reveal=%t", resolved.Runtime.Exposure, resolved.Runtime.Reveal)
	}
}

func TestBacktraceCLIUsesExplicitCarrierSyntax(t *testing.T) {
	var stderr bytes.Buffer
	resolved, err := resolveRunConfig([]string{
		"--only", "backtrace", "--exposure", "public",
		"--backtrace-targets", "telecom:Shanghai Telecom=202.96.209.133,unicom:IPv6 target=[2001:db8::1]",
	}, &stderr)
	if err != nil {
		t.Fatalf("resolve explicit backtrace targets: %v (stderr=%q)", err, stderr.String())
	}
	if len(resolved.Runtime.BacktraceTargets) != 2 || resolved.Runtime.BacktraceTargets[0].Kind != config.BacktraceCarrierTelecom || resolved.Runtime.BacktraceTargets[1].Kind != config.BacktraceCarrierUnicom || resolved.Runtime.BacktraceTargets[1].Family != config.IPVersion6 {
		t.Fatalf("resolved backtrace targets = %+v", resolved.Runtime.BacktraceTargets)
	}

	var stdout, planStderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--only", "backtrace", "--exposure", "public",
		"--backtrace-targets", "telecom:Shanghai Telecom=202.96.209.133",
	}, &stdout, &planStderr)
	if status != 0 || planStderr.Len() != 0 {
		t.Fatalf("explicit backtrace plan status=%d stdout=%q stderr=%q", status, stdout.String(), planStderr.String())
	}
}

func TestBacktraceCLIFailsClosedWithoutExplicitCarrier(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "legacy carrierless", args: []string{"--backtrace-targets", "Shanghai Telecom=202.96.209.133"}},
		{name: "invalid carrier", args: []string{"--backtrace-targets", "china:Shanghai Telecom=202.96.209.133"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if _, err := resolveRunConfig(append([]string{"--only", "backtrace", "--exposure", "public"}, test.args...), &stderr); err == nil || !strings.Contains(err.Error(), "backtrace") {
				t.Fatalf("resolve invalid backtrace target = %v (stderr=%q)", err, stderr.String())
			}
		})
	}
}

func TestBacktraceCLIHelpDocumentsExplicitCarrierSyntax(t *testing.T) {
	for _, test := range []struct {
		language string
		marker   string
	}{
		{language: "en", marker: "carrier:Name=IP/hostname"},
		{language: "zh", marker: "carrier:名称=IP/域名"},
	} {
		t.Run(test.language, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), []string{"run", "--lang", test.language, "--help"}, &stdout, &stderr)
			if status != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.marker) {
				t.Fatalf("help status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

func TestPlanRejectsRemovedJSONOption(t *testing.T) {
	for _, option := range []string{"--json", "--json=true"} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := Main(context.Background(), []string{"plan", "--lang", "en", option}, &stdout, &stderr)
			if status == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("removed plan JSON option status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

func TestPlanOutputIsLanguageIndependent(t *testing.T) {
	outputs := make([]string, 0, 2)
	for _, language := range []string{"zh", "en"} {
		var stdout, stderr bytes.Buffer
		status := Main(context.Background(), []string{"plan", "--lang", language, "--only", "system"}, &stdout, &stderr)
		if status != 0 || stderr.Len() != 0 {
			t.Fatalf("language=%s status=%d stderr=%q", language, status, stderr.String())
		}
		outputs = append(outputs, stdout.String())
	}
	if outputs[0] != outputs[1] {
		t.Fatalf("machine plan changed with language:\nzh=%s\nen=%s", outputs[0], outputs[1])
	}
}

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanJSONUsesRunResolverAndDescribesStaging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Main(context.Background(), []string{
		"plan", "--lang", "en", "--json", "--profile", "full",
		"--only", "cpu,ookla,zstd", "--exposure", "any",
	}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("plan status=%d stderr=%q", status, stderr.String())
	}
	var plan struct {
		SchemaVersion string `json:"schema_version"`
		Profile       string `json:"profile"`
		Modules       []struct {
			ID                  string   `json:"id"`
			TitleKey            string   `json:"title_key"`
			RetryOnInterference bool     `json:"retry_on_interference"`
			RequiredTools       []string `json:"required_tools"`
		} `json:"modules"`
		RequiredTools []string `json:"required_tools"`
		Staging       struct {
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
	if got := []string{plan.Modules[0].ID, plan.Modules[1].ID, plan.Modules[2].ID}; !strings.EqualFold(strings.Join(got, ","), "cpu,zstd,ookla") {
		t.Fatalf("selected modules = %v", got)
	}
	if plan.Modules[0].TitleKey != "module.cpu.title" || len(plan.RequiredTools) != 3 {
		t.Fatalf("module/tool metadata = %#v / %#v", plan.Modules, plan.RequiredTools)
	}
	if !plan.Modules[0].RetryOnInterference || !plan.Modules[1].RetryOnInterference || plan.Modules[2].RetryOnInterference {
		t.Fatalf("retry metadata = %#v", plan.Modules)
	}
	if !plan.Staging.ToolArchiveRequired || !plan.Staging.OoklaPackageRequired || !plan.Staging.ZstdCorpusRequired {
		t.Fatalf("staging = %#v", plan.Staging)
	}
	if strings.Contains(stdout.String(), "标准") || strings.Contains(stdout.String(), "完整配置") {
		t.Fatalf("machine plan contains localized prose: %s", stdout.String())
	}
}

func TestPlanRequiresJSON(t *testing.T) {
	for _, test := range []struct {
		name   string
		args   []string
		marker string
	}{
		{name: "missing json", args: []string{"plan", "--lang", "en"}, marker: "plan requires --json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := Main(context.Background(), test.args, &stdout, &stderr); status == 0 || !strings.Contains(stderr.String(), test.marker) {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

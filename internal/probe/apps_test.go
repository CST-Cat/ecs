package probe

import (
	"context"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestAppTargetCarriesPortAndCategory(t *testing.T) {
	targets := appTargets()
	if len(targets) == 0 {
		t.Fatal("app target list is empty")
	}
	target := targets[0]
	if target.Port != 443 || target.Host == "" || target.Category.Key != appCategoryTelegram.Key {
		t.Fatalf("representative app target = %+v", target)
	}
	seen := map[string]bool{}
	for _, item := range targets {
		if item.Host == "" || item.Port < 1 || item.Port > 65535 || seen[item.Host] {
			t.Fatalf("invalid or duplicate app target = %+v", item)
		}
		seen[item.Host] = true
	}
	for _, category := range []string{appCategoryTelegram.Key, appCategoryCodeAndImages.Key, appCategoryRepositories.Key, appCategoryInfrastructure.Key} {
		found := false
		for _, item := range targets {
			found = found || item.Category.Key == category
		}
		if !found {
			t.Fatalf("app category %q has no targets", category)
		}
	}
	results := []appResult{
		{Target: appTarget{Name: "slow", Host: "slow.example", Port: 443}, Reachable: true, Latency: 20 * time.Millisecond},
		{Target: appTarget{Name: "blocked", Host: "blocked.example", Port: 443}, Detail: "connection refused"},
		{Target: appTarget{Name: "fast", Host: "fast.example", Port: 443}, Reachable: true, Latency: 5 * time.Millisecond},
	}
	sortAppResults(results)
	if results[0].Target.Name != "blocked" || results[1].Latency != 5*time.Millisecond || results[2].Latency != 20*time.Millisecond {
		t.Fatalf("app result order = %+v", results)
	}
}

func TestAppsProducerBuildsStableSuccessDirectly(t *testing.T) {
	oldProbeTarget := appProbeTargetFunc
	t.Cleanup(func() { appProbeTargetFunc = oldProbeTarget })
	appProbeTargetFunc = func(_ context.Context, target appTarget, _ string) appResult {
		latency := 20 * time.Millisecond
		if target.Name == "DC2 Amsterdam" {
			latency = 5 * time.Millisecond
		}
		return appResult{Target: target, Reachable: true, Latency: latency}
	}

	result := (appsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{}})
	if result.Title != "module.apps.title" || result.Description != "probe.apps.description" || result.Status != model.StatusOK {
		t.Fatalf("apps result metadata/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.protocol-measurement" || result.Methodology.Profile != "probe.apps.profile" {
		t.Fatalf("apps methodology = %+v", result.Methodology)
	}
	if len(result.Tables) != 4 || result.Tables[0].Title != "probe.apps.table.telegram" {
		t.Fatalf("apps tables = %+v", result.Tables)
	}
	for _, table := range result.Tables {
		if len(table.Columns) != 5 {
			t.Fatalf("apps columns = %+v", table.Columns)
		}
		for _, row := range table.Rows {
			if len(row) != len(table.Columns) {
				t.Fatalf("apps row width = %d/%d", len(row), len(table.Columns))
			}
			if status, ok := row[3].Key(); !ok || status != "probe.apps.status.reachable" {
				t.Fatalf("apps reachable status = %#v", row[3])
			}
		}
	}
	if len(result.Measurements) != 1 || result.Measurements[0].Label != "probe.apps.metric.apps_reachable" || result.Measurements[0].Display.Text() != "17/17" {
		t.Fatalf("apps measurement = %+v", result.Measurements)
	}
	if len(result.Fields) != 1 || result.Fields[0].Label != "probe.apps.field.telegram_nearest_dc" || result.Fields[0].Value.Text() != "DC2 Amsterdam · 5.00 ms" {
		t.Fatalf("apps nearest field = %+v", result.Fields)
	}
	if result.Evidence == nil || result.Evidence.Valid != 17 || result.Evidence.Expected != 17 || len(result.Notes) != 3 {
		t.Fatalf("apps evidence/notes = %+v/%#v", result.Evidence, result.Notes)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.apps.summary.values" || result.SummaryMessages[0].Args[0] != "17/17" {
		t.Fatalf("apps summary = %+v", result.SummaryMessages)
	}
}

func TestAppsProducerPreservesUnreachableDiagnosticsDirectly(t *testing.T) {
	oldProbeTarget := appProbeTargetFunc
	t.Cleanup(func() { appProbeTargetFunc = oldProbeTarget })
	appProbeTargetFunc = func(_ context.Context, target appTarget, _ string) appResult {
		if target.Name == "DC2 Amsterdam" {
			return appResult{Target: target, Reachable: true, Latency: 5 * time.Millisecond}
		}
		if target.Name == "DC1 Miami" {
			return appResult{Target: target, Reachable: true, Latency: 10 * time.Millisecond}
		}
		return appResult{Target: target, Detail: "fixture connection refused"}
	}

	result := (appsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{}})
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 17 || result.Evidence.Expected != 17 {
		t.Fatalf("apps mixed status/evidence = %s/%+v", result.Status, result.Evidence)
	}
	if result.Measurements[0].Display.Text() != "2/17" || result.SummaryMessages[0].Args[0] != "2/17" {
		t.Fatalf("apps mixed summary = %+v/%+v", result.Measurements, result.SummaryMessages)
	}
	foundUnreachable := false
	for _, table := range result.Tables {
		for _, row := range table.Rows {
			if status, ok := row[3].Key(); !ok || (status != "probe.apps.status.reachable" && status != "probe.apps.status.unreachable") {
				t.Fatalf("apps mixed status = %#v", row[3])
			}
			if row[3].Text() == "probe.apps.status.unreachable" {
				foundUnreachable = true
				if row[4].Text() != "fixture connection refused" {
					t.Fatalf("apps diagnostic = %#v", row[4])
				}
			}
		}
	}
	if !foundUnreachable || len(result.Failures) != 15 || len(result.Fields) != 1 || result.Fields[0].Value.Text() != "DC2 Amsterdam · 5.00 ms" {
		t.Fatalf("apps mixed failures/field = %+v/%+v", result.Failures, result.Fields)
	}
}

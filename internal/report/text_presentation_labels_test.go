package report

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
)

func TestTextPresentationLabelsResolveFromCatalog(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })

	data := textPresentationLabelFixture()
	keys := []string{
		"report.reportTime", "report.scriptVersion", "report.runProfile", "report.reportStatus",
		"report.navigation.all", "report.navigation.selected", "report.navigation.basic",
		"report.navigation.hardware", "report.navigation.ipQuality", "report.navigation.networkQuality",
		"report.navigation.returnPath", "report.engine", "report.profileWorkload",
		"report.group.moduleDetails", "report.group.system.hardware", "report.group.system.storage",
		"report.group.system.kernel", "report.group.network.ip", "report.group.network.risk",
		"report.group.network.egress", "score.incompleteStatus", "score.matrixItemCount",
		"score.matrixMissingCount",
	}

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		language := language
		t.Run(string(language), func(t *testing.T) {
			i18n.Set(language)
			output := Text(data, TextOptions{Color: termcolor.LevelNone, Score: textPresentationScoreFixture(), Width: 160})
			for _, key := range keys {
				translated := i18n.T(key)
				if translated == key {
					t.Fatalf("catalog key %q is missing for %s", key, language)
				}
				switch key {
				case "score.matrixItemCount":
					translated = fmt.Sprintf(translated, 2)
				case "score.matrixMissingCount":
					translated = fmt.Sprintf(translated, 1)
				}
				if !strings.Contains(output, translated) {
					t.Errorf("%s Text omitted catalog value %q for %s:\n%s", language, translated, key, output)
				}
				if strings.Contains(output, key) {
					t.Errorf("%s Text leaked catalog key %q:\n%s", language, key, output)
				}
			}
		})
	}
}

func textPresentationLabelFixture() model.Report {
	when := time.Date(2026, 8, 28, 12, 34, 56, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run: model.RunInfo{
			Profile: "standard", StartedAt: when.Add(-time.Minute), CompletedAt: when,
			Requested: []string{"system", "network"},
		},
		Summary: model.Summary{
			Status:   model.StatusWarning,
			Messages: []model.Message{model.NewMessage("message.summary.withWarnings", 1, 1)},
		},
		Results: []model.Result{
			{
				ID: "system", Title: "module.system.title", Status: model.StatusOK,
				Methodology: model.Methodology{Engine: "fixture-engine", Profile: "fixture-workload", ComparisonScope: "fixture-scope"},
				Fields: []model.Field{
					{Key: "custom_hardware", Label: "fixture hardware", Value: model.RawValue("hardware")},
					{Key: "disk_total", Label: "fixture storage", Value: model.RawValue("storage")},
					{Key: "tcp_congestion", Label: "fixture kernel", Value: model.RawValue("kernel")},
				},
			},
			{
				ID: "network", Title: "module.network.title", Status: model.StatusOK,
				Fields: []model.Field{
					{Key: "provider", Label: "fixture IP", Value: model.RawValue("ip")},
					{Key: "risk_level", Label: "fixture risk", Value: model.RawValue("risk")},
				},
				Tables: []model.Table{{
					Key: "network.egress.overview", Title: "fixture egress",
					Columns: []model.TableColumn{{Key: "value", Label: "fixture value"}},
					Rows:    [][]model.Value{{model.RawValue("egress")}},
				}},
			},
			{ID: "custom", Status: model.StatusOK},
		},
	}
}

func textPresentationScoreFixture() *score.Report {
	return &score.Report{
		Total: 500, Covered: 1, Possible: 2, Complete: false,
		Dimensions: []score.DimensionScore{
			{
				Key: "disk", Score: 500, Ratio: 0.5,
				Groups:         []score.GroupScore{{Key: "crystal", MetricCount: 2}},
				MissingMetrics: []string{"crystal_rnd4k_q1_read_mib_s"},
			},
		},
	}
}

func TestTextScoreMetricLabelRequiresCanonicalMatrixKey(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	i18n.Set(i18n.LangEN)

	if got := metricLabel(score.MetricScore{Key: "crystal_custom_read_mib_s", Label: "custom metric"}); got != "custom metric" {
		t.Fatalf("near-miss matrix key received an inferred label: %q", got)
	}
	if got := metricLabel(score.MetricScore{Key: "crystal_rnd4k_q1_read_mib_s", Label: "matrix metric"}); !strings.HasPrefix(got, "Crystal · ") {
		t.Fatalf("canonical matrix key lost its explicit family label: %q", got)
	}
}

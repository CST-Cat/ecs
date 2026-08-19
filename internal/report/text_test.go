package report

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func textSampleReport() model.Report {
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test"},
		Run:           model.RunInfo{ID: "abc", Profile: "standard", StartedAt: time.Unix(0, 0).UTC(), Redacted: true},
		Summary:       model.Summary{Status: model.StatusOK, OK: 1, Headline: "1 项完成"},
		Results: []model.Result{{
			ID: "cpu", Title: "CPU 性能", Status: model.StatusOK,
			Fields:       []model.Field{{Key: "engine", Label: "引擎", Value: "sysbench"}},
			Measurements: []model.Measurement{{Key: "events", Label: "事件率", Value: 780, Unit: "events/s", Display: "780 events/s", HigherIsBetter: model.BoolPtr(true)}},
			Tables:       []model.Table{{Title: "明细", Columns: []string{"项目", "数值"}, Rows: [][]string{{"单线程", "780"}}}},
		}},
	}
}

func TestTextRendersBasicReportDetails(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)
	i18n.Set(i18n.LangZH)

	output := Text(textSampleReport(), TextOptions{Color: termcolor.LevelNone})
	if strings.Contains(output, "\x1b") {
		t.Fatal("plain text contains ANSI escapes")
	}
	for _, marker := range []string{"CPU 性能", "引擎", "sysbench", "780 events/s", "明细", "单线程", "780"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("text report missing %q:\n%s", marker, output)
		}
	}
}

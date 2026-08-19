package report

import (
	"bytes"
	"os"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func sampleReport() model.Report {
	start := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test", Commit: "abc"},
		Run: model.RunInfo{
			ID: "run-1", Profile: "standard", StartedAt: start, CompletedAt: start.Add(time.Second),
			DurationMS: 1000, Redacted: true,
		},
		Summary: model.Summary{Status: model.StatusOK, OK: 1, Headline: "1 项测试完成"},
		Results: []model.Result{{
			ID: "system", Title: "系统 | 信息", Status: model.StatusOK, Summary: "<safe>",
			Methodology:  model.Methodology{Kind: "inventory", Label: "事实采集", Engine: "OS inspection", ComparisonScope: "资源快照；不是基准"},
			Fields:       []model.Field{{Key: "os", Label: "系统", Value: "Linux"}},
			Measurements: []model.Measurement{{Key: "cpu", Label: "CPU", Value: 1, Display: "1 point"}},
			Tables:       []model.Table{{Columns: []string{"列"}, Rows: [][]string{{"值"}}}},
		}},
	}
}

func TestWriteFilesKeepsCanonicalJSONAcrossLanguages(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)

	data := sampleReport()
	data.Results[0].Fields = []model.Field{{Key: "state", Label: "系统", Value: "完成"}}
	data.Results[0].Tables = []model.Table{
		{Key: "system.state", Title: "当前值", Columns: []string{"状态"}, ColumnKeys: []string{"state"}, Rows: [][]string{{"完成"}}},
	}
	canonical, err := JSON(data)
	if err != nil {
		t.Fatal(err)
	}

	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		written, err := WriteFiles(data, t.TempDir(), "report", []string{"json"})
		if err != nil {
			t.Fatalf("write %s report: %v", language, err)
		}
		content, err := os.ReadFile(written["json"])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(content, canonical) {
			t.Fatalf("canonical JSON changed for %s", language)
		}
		loaded, err := LoadJSON(written["json"])
		if err != nil || loaded.Results[0].Fields[0].Value != "完成" || loaded.Results[0].Tables[0].Rows[0][0] != "完成" {
			t.Fatalf("canonical values were not preserved: report=%+v err=%v", loaded, err)
		}
	}
}

package report

import (
	"testing"

	"ecs/internal/i18n"
	"ecs/internal/model"
)

func TestLocalizeDeepCopiesAndTranslatesRetryAudit(t *testing.T) {
	originalLanguage := i18n.Current()
	defer i18n.Set(originalLanguage)
	i18n.Set(i18n.LangEN)
	data := sampleReport()
	data.Results[0].Methodology.Parameters = map[string]string{"target": "固定中文值"}
	data.Results[0].Retry = &model.RetryInfo{
		SelectionRule:  "先排除无有效证据的轮次，再选择干扰评分较低的一轮；同分保留首次结果，不按性能高低挑选",
		TriggerReasons: []string{"测试窗口 CPU steal 2.50%"},
		Attempts: []model.RetryAttempt{{
			Evidence: model.NewEvidence(1, 1, "run"),
			Interference: model.Interference{
				Reasons:      []string{"测试前 load 4.00 高于 2 CPU allowance 的 1.5 倍"},
				Measurements: []model.Measurement{{Label: "测试窗口 CPU steal", Display: "2.50 %"}},
			},
			Measurements: []model.Measurement{{Label: "cgroup CPU throttle 时间占比", Display: "3.00 %"}},
		}},
	}
	localized := Localize(data)
	if localized.Results[0].Methodology.Parameters["target"] != "固定中文值" {
		t.Fatalf("machine parameter was localized: %+v", localized.Results[0].Methodology.Parameters)
	}
	localized.Results[0].Methodology.Parameters["target"] = "changed"
	if data.Results[0].Methodology.Parameters["target"] != "固定中文值" {
		t.Fatal("Localize shared the machine parameter map")
	}
	retry := localized.Results[0].Retry
	if retry == data.Results[0].Retry {
		t.Fatal("Localize reused retry pointer")
	}
	if retry.SelectionRule != "Discard attempts without valid evidence first, then select the lower-interference attempt; keep the first on a tie and never select by benchmark score" {
		t.Fatalf("selection rule = %q", retry.SelectionRule)
	}
	if retry.TriggerReasons[0] != "CPU steal during the test window was 2.50%" {
		t.Fatalf("trigger reason = %q", retry.TriggerReasons[0])
	}
	if retry.Attempts[0].Interference.Reasons[0] != "Pre-test load 4.00 was above the 2-CPU allowance's 1.5× threshold" {
		t.Fatalf("attempt reason = %q", retry.Attempts[0].Interference.Reasons[0])
	}
	if retry.Attempts[0].Interference.Measurements[0].Label != "CPU steal during test window" ||
		retry.Attempts[0].Measurements[0].Label != "cgroup CPU throttled time ratio" {
		t.Fatalf("retry measurement labels = %+v / %+v", retry.Attempts[0].Interference.Measurements, retry.Attempts[0].Measurements)
	}
	retry.TriggerReasons[0] = "changed"
	retry.Attempts[0].Evidence.Valid = 0
	if data.Results[0].Retry.TriggerReasons[0] != "测试窗口 CPU steal 2.50%" || data.Results[0].Retry.Attempts[0].Evidence.Valid != 1 {
		t.Fatal("Localize mutated the source retry audit")
	}
}

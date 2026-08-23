package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestStabilizeMediaResultKeysVerdictEvidenceAndStrength(t *testing.T) {
	result := model.Result{
		ID:           "media",
		Status:       model.StatusOK,
		Measurements: []model.Measurement{{Key: "media_unlocked", Value: 1, Display: "1/1"}},
		Tables: []model.Table{{
			Key:     "network.media.streaming",
			Columns: []string{"旧"},
			Rows:    [][]string{{"Netflix", "解锁", "US", "页面正常返回", "强", "10 ms"}},
		}},
	}

	stabilizeMediaResult(&result)
	row := result.Tables[0].Rows[0]
	if row[1] != "probe.media.verdict.unlocked" || row[3] != "probe.media.evidence.observed" || row[4] != "probe.media.strength.strong" {
		t.Fatalf("media row = %#v", row)
	}
	if result.SummaryMessages[0].Key != "probe.media.summary.values" {
		t.Fatalf("summary = %#v", result.SummaryMessages)
	}
}

func TestStabilizeRouteAndBacktraceResultsKeyStatuses(t *testing.T) {
	route := model.Result{
		ID:     "route",
		Status: model.StatusWarning,
		Tables: []model.Table{{Key: "network.route.summary", Columns: []string{"旧"}, Rows: [][]string{{"target", "host", "完成", "1", "1", "0", "1 ms"}}}},
	}
	stabilizeRouteResult(&route)
	if route.Tables[0].Rows[0][2] != "probe.route.status.complete" || route.SummaryMessages[0].Key != "probe.route.summary.values" {
		t.Fatalf("route = %#v", route)
	}

	backtrace := model.Result{
		ID:           "backtrace",
		Status:       model.StatusOK,
		Measurements: []model.Measurement{{Key: "backtrace_identified", Value: 1, Display: "1/1"}},
		Tables:       []model.Table{{Key: "network.backtrace.summary", Columns: []string{"旧"}, Rows: [][]string{{"telecom", "target", "CN2", "1", "198.51.100.1", "已识别"}}}},
	}
	stabilizeBacktraceResult(&backtrace)
	if backtrace.Tables[0].Rows[0][5] != "probe.backtrace.status.identified" || backtrace.SummaryMessages[0].Key != "probe.backtrace.summary.values" {
		t.Fatalf("backtrace = %#v", backtrace)
	}
}

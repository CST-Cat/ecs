package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestStabilizeBacktraceResultKeyStatuses(t *testing.T) {
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

//go:build linux

package probe

import (
	"context"
	"reflect"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestSysbenchProducerIgnoresCanonicalHighCPUWindow(t *testing.T) {
	highBefore, highAfter := newCPUWindowFixture(100, cpuTimeSample{Total: 1_000}, cpuTimeSample{Total: 1_100, Steal: 100})
	highPressure := BuildPressureMeasurements(highBefore, highAfter)
	if load, ok := cpuMeasurementByKey(highPressure, "pretest_load_1m"); !ok || load.Value != 100 {
		t.Fatalf("high-load canonical measurement = %+v, want 100", highPressure)
	}
	if steal, ok := cpuMeasurementByKey(highPressure, "cpu_steal_percent_window"); !ok || steal.Value != 100 {
		t.Fatalf("high-steal canonical measurement = %+v, want 100%%", highPressure)
	}

	lowBefore, lowAfter := newCPUWindowFixture(0.1, cpuTimeSample{Total: 1_000}, cpuTimeSample{Total: 1_100})
	lowPressure := BuildPressureMeasurements(lowBefore, lowAfter)
	tool := writeSysbenchFixtureTool(t)
	env := Environment{}
	env.Config.CPUTime = time.Second
	allowance := cpuAllowance{Visible: 2, Threads: 2, Source: "fixture"}
	high := runSysbenchCPUWithAllowance(context.Background(), env, tool, allowance)
	low := runSysbenchCPUWithAllowance(context.Background(), env, tool, allowance)

	if got, want := cpuSemanticResult(high), cpuSemanticResult(low); !reflect.DeepEqual(got, want) {
		t.Fatalf("CPU result changed with canonical high load/steal fixture:\nhigh=%+v\nlow=%+v", got, want)
	}
	if high.Status != model.StatusOK || cpuResultValidity(t, high) != "probe.cpu.validity.valid" {
		t.Fatalf("high-window CPU status/validity = %s/%q", high.Status, cpuResultValidity(t, high))
	}
	if high.Evidence == nil || high.Evidence.Valid != 2 || high.Evidence.Expected != 2 {
		t.Fatalf("high-window CPU evidence = %+v", high.Evidence)
	}
	for _, key := range []string{"cpu_steal_percent_during_test", "cpu_steal_percent_window"} {
		if hasMeasurement(high, key) {
			t.Fatalf("CPU probe emitted host-window measurement %q: %+v", key, high.Measurements)
		}
	}
	for _, field := range high.Fields {
		if field.Key == "pretest_load_1m" {
			t.Fatalf("CPU probe emitted pre-test load field: %+v", high.Fields)
		}
	}
	for _, note := range high.Notes {
		if note == "probe.cpu.note.steal" || note == "probe.cpu.note.load" {
			t.Fatalf("CPU probe emitted host-state note %q: %v", note, high.Notes)
		}
	}
	if load, ok := cpuMeasurementByKey(lowPressure, "pretest_load_1m"); !ok || load.Value != 0.1 {
		t.Fatalf("low-load canonical measurement = %+v, want 0.1", lowPressure)
	}
}

package runner

import (
	"reflect"
	"testing"

	"ecs/internal/config"
)

func TestPlanSchedulePreservesOrderAroundExclusiveModule(t *testing.T) {
	bindings := []moduleBinding{
		{Descriptor: config.ModuleDescriptor{ID: "first", Concurrency: config.ModuleConcurrencyProbe}},
		{Descriptor: config.ModuleDescriptor{ID: "second", Concurrency: config.ModuleConcurrencyProbe}},
		{Descriptor: config.ModuleDescriptor{ID: "exclusive", Concurrency: config.ModuleConcurrencyExclusive}},
	}
	groups := planSchedule(bindings)
	if len(groups) != 2 || !reflect.DeepEqual(groups[0].Indices, []int{0, 1}) || !groups[0].Parallel {
		t.Fatalf("parallel group = %+v, want the first two indices in order", groups)
	}
	if !reflect.DeepEqual(groups[1].Indices, []int{2}) || groups[1].Parallel {
		t.Fatalf("exclusive group = %+v, want the final index alone", groups[1])
	}
}

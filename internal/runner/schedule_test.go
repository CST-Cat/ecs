package runner

import (
	"reflect"
	"testing"

	"ecs/internal/module"
	"ecs/internal/probe"
)

func TestPlanScheduleClassesAndOrder(t *testing.T) {
	makeDefinitions := func(classes ...module.Concurrency) []probe.Definition {
		definitions := make([]probe.Definition, len(classes))
		for index, class := range classes {
			definitions[index].Descriptor = module.Descriptor{ID: string(rune('a' + index)), Concurrency: class}
		}
		return definitions
	}
	cases := []struct {
		name string
		in   []probe.Definition
		want []scheduleGroup
	}{
		{name: "empty", want: nil},
		{name: "one probe", in: makeDefinitions(module.ConcurrencyProbe), want: []scheduleGroup{{Indices: []int{0}}}},
		{name: "continuous probes", in: makeDefinitions(module.ConcurrencyProbe, module.ConcurrencyProbe), want: []scheduleGroup{{Indices: []int{0, 1}}}},
		{name: "mixed groups", in: makeDefinitions(module.ConcurrencyProbe, module.ConcurrencyProbe, module.ConcurrencyExclusive, module.ConcurrencyProbe), want: []scheduleGroup{{Indices: []int{0, 1}}, {Indices: []int{2}}, {Indices: []int{3}}}},
		{name: "missing descriptor is exclusive", in: []probe.Definition{{}}, want: []scheduleGroup{{Indices: []int{0}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := planSchedule(test.in); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("schedule = %+v, want %+v", got, test.want)
			}
		})
	}
}

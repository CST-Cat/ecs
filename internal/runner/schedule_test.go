package runner

import (
	"reflect"
	"testing"

	"ecs/internal/config"
)

func TestPlanScheduleClassesAndOrder(t *testing.T) {
	makeBindings := func(classes ...config.ModuleConcurrency) []moduleBinding {
		bindings := make([]moduleBinding, len(classes))
		for index, class := range classes {
			bindings[index].Descriptor = config.ModuleDescriptor{ID: string(rune('a' + index)), Concurrency: class}
		}
		return bindings
	}
	cases := []struct {
		name string
		in   []moduleBinding
		want []scheduleGroup
	}{
		{name: "empty", want: nil},
		{name: "one probe", in: makeBindings(config.ModuleConcurrencyProbe), want: []scheduleGroup{{Indices: []int{0}}}},
		{name: "continuous probes", in: makeBindings(config.ModuleConcurrencyProbe, config.ModuleConcurrencyProbe), want: []scheduleGroup{{Parallel: true, Indices: []int{0, 1}}}},
		{name: "mixed groups", in: makeBindings(config.ModuleConcurrencyProbe, config.ModuleConcurrencyProbe, config.ModuleConcurrencyExclusive, config.ModuleConcurrencyProbe), want: []scheduleGroup{{Parallel: true, Indices: []int{0, 1}}, {Indices: []int{2}}, {Indices: []int{3}}}},
		{name: "missing descriptor is exclusive", in: []moduleBinding{{}}, want: []scheduleGroup{{Indices: []int{0}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := planSchedule(test.in); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("schedule = %+v, want %+v", got, test.want)
			}
		})
	}
}

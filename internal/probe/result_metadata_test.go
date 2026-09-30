package probe

import (
	"reflect"
	"testing"

	"ecs/internal/model"
)

func TestCompleteResultMetadataKeepsBuiltInMethodVariantsAndCanonicalIdentity(t *testing.T) {
	definitions := BuiltinDefinitions()
	find := func(id string) Definition {
		t.Helper()
		for _, definition := range definitions {
			if definition.Descriptor.ID == id {
				return definition
			}
		}
		t.Fatalf("built-in %q definition missing", id)
		return Definition{}
	}

	cpuDefinition := find("cpu")
	cpuDefinition.Probe = cpuProbe{}
	cpuParameters := map[string]string{"configured_duration": "1s"}
	cpu := CompleteResultMetadata(cpuDefinition, model.Result{
		Title: "producer title", Description: "producer description",
		Methodology: model.Methodology{
			Kind: "producer kind", Label: "producer label", Engine: "producer engine",
			Profile: "producer profile", ComparisonScope: cpuToolMissingComparisonScope,
			Parameters: cpuParameters,
		},
	})
	if cpu.Title != cpuDefinition.Descriptor.TitleKey || cpu.Description != cpuDefinition.Descriptor.DescriptionKey ||
		cpu.Methodology.Kind != cpuDefinition.Descriptor.Methodology.Kind || cpu.Methodology.Label != cpuDefinition.Descriptor.Methodology.Label ||
		cpu.Methodology.Engine != cpuDefinition.Descriptor.Methodology.Engine || cpu.Methodology.Profile != cpuDefinition.Descriptor.Methodology.Profile ||
		cpu.Methodology.ComparisonScope != cpuToolMissingComparisonScope || !reflect.DeepEqual(cpu.Methodology.Parameters, cpuParameters) {
		t.Fatalf("completed CPU metadata = %+v, descriptor = %+v", cpu, cpuDefinition.Descriptor)
	}

	memoryDefinition := find("memory")
	memoryDefinition.Probe = memoryProbe{}
	memoryParameters := map[string]string{"threads": "1 / 1"}
	memory := CompleteResultMetadata(memoryDefinition, model.Result{
		Title: "producer title", Description: memorySingleCoreDescription,
		Methodology: model.Methodology{
			Kind: "producer kind", Label: "producer label", Engine: "producer engine",
			Profile: memorySingleCoreProfile, ComparisonScope: "producer scope",
			Parameters: memoryParameters,
		},
	})
	if memory.Title != memoryDefinition.Descriptor.TitleKey || memory.Description != memorySingleCoreDescription ||
		memory.Methodology.Kind != memoryDefinition.Descriptor.Methodology.Kind || memory.Methodology.Label != memoryDefinition.Descriptor.Methodology.Label ||
		memory.Methodology.Engine != memoryDefinition.Descriptor.Methodology.Engine || memory.Methodology.Profile != memorySingleCoreProfile ||
		memory.Methodology.ComparisonScope != memoryDefinition.Descriptor.Methodology.ComparisonScope || !reflect.DeepEqual(memory.Methodology.Parameters, memoryParameters) {
		t.Fatalf("completed STREAM metadata = %+v, descriptor = %+v", memory, memoryDefinition.Descriptor)
	}

	for _, test := range []struct {
		name       string
		definition Definition
		result     model.Result
	}{
		{
			name:       "CPU variant from unknown producer",
			definition: Definition{Descriptor: cpuDefinition.Descriptor, Probe: definitionTestProbe{id: "cpu"}},
			result:     model.Result{Methodology: model.Methodology{ComparisonScope: cpuToolMissingComparisonScope}},
		},
		{
			name:       "STREAM variant from unknown producer",
			definition: Definition{Descriptor: memoryDefinition.Descriptor, Probe: definitionTestProbe{id: "memory"}},
			result: model.Result{
				Description: memorySingleCoreDescription,
				Methodology: model.Methodology{Profile: memorySingleCoreProfile},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := CompleteResultMetadata(test.definition, test.result)
			if got.Description != test.definition.Descriptor.DescriptionKey ||
				got.Methodology.ComparisonScope != test.definition.Descriptor.Methodology.ComparisonScope ||
				got.Methodology.Profile != test.definition.Descriptor.Methodology.Profile {
				t.Fatalf("unknown producer changed canonical metadata: %+v", got)
			}
		})
	}
}

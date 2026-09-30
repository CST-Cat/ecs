package probe

import "ecs/internal/model"

const (
	cpuToolMissingComparisonScope = "probe.cpu.comparison_scope.tool_missing"
	memorySingleCoreDescription   = "probe.memory.description.single_core"
	memorySingleCoreProfile       = "probe.memory.stream.profile.single_core"
)

// CanonicalTitle returns the descriptor's title key, falling back to the
// stable probe ID when presentation metadata is unavailable.
func (definition Definition) CanonicalTitle() string {
	if definition.Descriptor.TitleKey != "" {
		return definition.Descriptor.TitleKey
	}
	if definition.Probe != nil {
		return definition.Probe.ID()
	}
	return ""
}

// CompleteResultMetadata applies canonical module metadata to a producer or
// runner-generated result. The descriptor owns fixed identity fields;
// parameters describe runtime facts and remain result-owned. Only the built-in
// probe implementations that produce a method variant may select it.
func CompleteResultMetadata(definition Definition, result model.Result) model.Result {
	descriptor := definition.Descriptor
	producerDescription := result.Description
	result.Title = definition.CanonicalTitle()
	result.Description = descriptor.DescriptionKey

	parameters := result.Methodology.Parameters
	methodology := descriptor.Methodology
	methodology.Parameters = parameters
	switch definition.Probe.(type) {
	case cpuProbe:
		if result.Methodology.ComparisonScope == cpuToolMissingComparisonScope {
			methodology.ComparisonScope = cpuToolMissingComparisonScope
		}
	case memoryProbe:
		if result.Methodology.Profile == memorySingleCoreProfile {
			methodology.Profile = memorySingleCoreProfile
		}
		if producerDescription == memorySingleCoreDescription {
			result.Description = memorySingleCoreDescription
		}
	}
	result.Methodology = methodology
	return result
}

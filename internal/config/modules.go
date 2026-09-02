package config

import (
	"ecs/internal/i18n"
	"ecs/internal/module"
)

// ModuleDescriptors returns the descriptors supplied by the composition
// boundary in canonical order. Configuration owns profile and selection
// semantics, while module.Catalog owns descriptor storage and validation.
func ModuleDescriptors(catalog module.Catalog) []module.Descriptor {
	return catalog.Descriptors()
}

// ModuleDescriptorFor returns one descriptor by ID from the explicit catalog.
func ModuleDescriptorFor(catalog module.Catalog, id string) (module.Descriptor, bool) {
	return catalog.Lookup(id)
}

// ModuleIDs returns descriptor IDs in the catalog's canonical order.
func ModuleIDs(catalog module.Catalog) []string {
	return catalog.IDs()
}

// ValidateModuleSelection checks explicit --only/--skip IDs against the
// supplied canonical catalog.
func ValidateModuleSelection(catalog module.Catalog, only, skip []string) error {
	for _, ids := range [][]string{only, skip} {
		for _, id := range ids {
			if _, ok := catalog.Lookup(id); !ok {
				return i18n.Errorf("err.unknownModule", id)
			}
		}
	}
	return nil
}

// ModulesForProfile interprets user profile names and selects IDs from the
// supplied catalog. Unknown profiles return nil so Defaults can provide the
// localized validation error.
func ModulesForProfile(catalog module.Catalog, profile string) []string {
	switch profile {
	case ProfileStandard:
		return catalog.StandardIDs()
	case ProfileFull:
		return catalog.IDs()
	default:
		return nil
	}
}

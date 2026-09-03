package config

import (
	"ecs/internal/i18n"
	"ecs/internal/module"
)

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

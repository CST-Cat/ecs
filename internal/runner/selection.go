package runner

import "ecs/internal/probe"

// selectDefinitions applies the requested ID set while preserving the
// canonical order supplied by the validated definition catalog.
func selectDefinitions(definitions []probe.Definition, ids []string) []probe.Definition {
	requested := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	selected := make([]probe.Definition, 0, len(ids))
	for _, definition := range definitions {
		if _, ok := requested[definition.Descriptor.ID]; ok {
			selected = append(selected, definition)
		}
	}
	return selected
}

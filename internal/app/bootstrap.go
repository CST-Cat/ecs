package app

import (
	"fmt"

	"ecs/internal/module"
	"ecs/internal/probe"
	"ecs/internal/score"
	"ecs/internal/tool"
)

// application is the per-invocation composition root. Its fields are private
// and initialized once by composeApplication; command handlers receive the
// value explicitly so every consumer observes the same module catalog.
type application struct {
	commands    []commandDefinition
	definitions []probe.Definition
	modules     module.Catalog
}

// newApplication composes the built-in application graph. Built-in definitions
// are compile-time structure, so a malformed change is a programmer error
// rather than user input and fails closed with a panic.
func newApplication() application {
	composed, err := composeApplication(probe.BuiltinDefinitions())
	if err != nil {
		panic(fmt.Sprintf("invalid application composition: %v", err))
	}
	return composed
}

// composeApplication is kept separate from newApplication so tests can prove
// the composition boundary rejects malformed definitions without changing the
// production construction path.
func composeApplication(definitions []probe.Definition) (application, error) {
	catalog, err := probe.CatalogFromDefinitions(definitions)
	if err != nil {
		return application{}, fmt.Errorf("module definitions: %w", err)
	}
	if err := score.ValidateDimensions(catalog); err != nil {
		return application{}, fmt.Errorf("score dimensions: %w", err)
	}
	if err := validateRequiredToolReferences(catalog); err != nil {
		return application{}, err
	}
	return application{
		commands:    commandDefinitions(),
		definitions: definitions,
		modules:     catalog,
	}, nil
}

func validateRequiredToolReferences(catalog module.Catalog) error {
	for _, descriptor := range catalog.Descriptors() {
		for _, id := range descriptor.RequiredTools {
			if _, ok := tool.LookupBuiltin(id); !ok {
				return fmt.Errorf("module %q references unknown tool %q", descriptor.ID, id)
			}
		}
	}
	return nil
}

// definitionsInOrder returns the canonical probe definition order for the
// runner. The composition root owns this slice for the lifetime of the app.
func (app application) definitionsInOrder() []probe.Definition {
	return app.definitions
}

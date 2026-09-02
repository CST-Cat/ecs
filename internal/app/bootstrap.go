package app

import (
	"fmt"

	"ecs/internal/module"
	"ecs/internal/probe"
	"ecs/internal/score"
)

// application is the per-invocation composition root. Its fields are private
// and initialized once by composeApplication; command handlers receive the
// value explicitly so every consumer observes the same validated catalogs.
type application struct {
	commands    commandCatalog
	definitions []probe.Definition
	modules     module.Catalog
}

// newApplication composes the built-in application graph. Built-in
// definitions are compile-time structure, so a malformed change is a
// programmer error rather than user input and fails closed with a panic.
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
	commands, err := newCommandCatalog([]commandDefinition{
		{Name: "run", Handler: runCommand, UsageKey: "help.usageRun"},
		{Name: "plan", Handler: planCommand, UsageKey: "help.usagePlan"},
		{Name: "list", Handler: commandWithoutContext(listCommand), UsageKey: "help.usageList"},
		{Name: "render", Handler: commandWithoutContext(renderCommand), UsageKey: "help.usageRender"},
		{Name: "compare", Handler: commandWithoutContext(compareCommand), UsageKey: "help.usageCompare"},
		{Name: "config", Handler: commandWithoutContext(configCommand), UsageKey: "help.usageConfig"},
		{Name: "doctor", Handler: doctorCommand, UsageKey: "help.usageDoctor"},
		{Name: "leaderboard", Handler: commandWithoutContext(leaderboardCommand), UsageKey: "help.usageLeaderboard"},
		{Name: "submit", Handler: commandWithoutContext(submitCommand), UsageKey: "help.usageSubmit"},
		{Name: "version", Handler: versionCommand, UsageKey: "help.usageVersion"},
		{Name: "help", Handler: helpCommand, UsageKey: "help.usageHelp"},
	})
	if err != nil {
		return application{}, fmt.Errorf("command catalog: %w", err)
	}
	return application{
		commands:    commands,
		definitions: copyApplicationDefinitions(definitions, catalog),
		modules:     catalog,
	}, nil
}

// copyApplicationDefinitions detaches the application-owned definition slice
// and obtains fresh descriptor copies from the immutable module catalog.
func copyApplicationDefinitions(definitions []probe.Definition, catalog module.Catalog) []probe.Definition {
	result := make([]probe.Definition, len(definitions))
	for index, definition := range definitions {
		descriptor, ok := catalog.Lookup(definition.Descriptor.ID)
		if !ok {
			panic(fmt.Sprintf("missing application descriptor %q", definition.Descriptor.ID))
		}
		result[index] = definition
		result[index].Descriptor = descriptor
	}
	return result
}

// definitionsInOrder returns a detached definition slice for the runner.
func (app application) definitionsInOrder() []probe.Definition {
	return copyApplicationDefinitions(app.definitions, app.modules)
}

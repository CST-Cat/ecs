package app

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"ecs/internal/buildinfo"
)

// commandHandler is the one typed boundary between command selection and a
// command implementation. Every command receives the per-invocation
// application composition root, lifecycle context, and output writers.
type commandHandler func(application, context.Context, []string, io.Writer, io.Writer) int

// commandDefinition owns one command's identity, handler, and human-facing
// help metadata. UsageKey and DescriptionKey are stable i18n keys rather than
// localized text, so the command catalog remains language independent.
type commandDefinition struct {
	Name           string
	Handler        commandHandler
	UsageKey       string
	DescriptionKey string
}

// commandCatalog is immutable after construction. Its backing slice is
// private, construction copies its input, and the only read methods return
// values or defensive copies. There is intentionally no registration or
// replacement API.
type commandCatalog struct {
	definitions []commandDefinition
}

func commandWithoutContext(handler func(application, []string, io.Writer, io.Writer) int) commandHandler {
	return func(app application, _ context.Context, args []string, stdout, stderr io.Writer) int {
		return handler(app, args, stdout, stderr)
	}
}

// newCommandCatalog validates and copies an ordered set of command
// definitions. The input order is the canonical command/help order.
func newCommandCatalog(definitions []commandDefinition) (commandCatalog, error) {
	copyOfDefinitions := append([]commandDefinition(nil), definitions...)
	seen := make(map[string]struct{}, len(copyOfDefinitions))
	for index, definition := range copyOfDefinitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			return commandCatalog{}, fmt.Errorf("command definition %d has empty name", index)
		}
		if name != definition.Name {
			return commandCatalog{}, fmt.Errorf("command definition %d name %q has surrounding whitespace", index, definition.Name)
		}
		if _, exists := seen[name]; exists {
			return commandCatalog{}, fmt.Errorf("duplicate command definition %q", definition.Name)
		}
		seen[name] = struct{}{}
		if definition.Handler == nil {
			return commandCatalog{}, fmt.Errorf("command %q has nil handler", definition.Name)
		}
		if strings.TrimSpace(definition.UsageKey) == "" && strings.TrimSpace(definition.DescriptionKey) == "" {
			return commandCatalog{}, fmt.Errorf("command %q has missing help metadata", definition.Name)
		}
	}
	return commandCatalog{definitions: copyOfDefinitions}, nil
}

// definitionsInOrder returns the canonical definitions without exposing the
// catalog's backing storage to callers.
func (catalog commandCatalog) definitionsInOrder() []commandDefinition {
	return append([]commandDefinition(nil), catalog.definitions...)
}

// lookup returns only an exact catalog match. Unknown names never fall back to
// a default handler and do not mutate the catalog.
func (catalog commandCatalog) lookup(name string) (commandDefinition, bool) {
	for _, definition := range catalog.definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return commandDefinition{}, false
}

func versionCommand(_ application, _ context.Context, _ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "%s %s commit=%s built=%s go=%s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.Version())
	return 0
}

func helpCommand(app application, _ context.Context, _ []string, stdout, _ io.Writer) int {
	printHelp(app.commands, stdout)
	return 0
}

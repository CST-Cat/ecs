package app

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"ecs/internal/buildinfo"
	"ecs/internal/i18n"
)

// commandHandler is the one typed boundary between command selection and a
// command implementation. Every command receives the per-invocation
// application composition root, lifecycle context, and output writers.
type commandHandler func(application, context.Context, []string, io.Writer, io.Writer) int

// commandDefinition owns one command's identity, handler, and human-facing
// help metadata. UsageKey and DescriptionKey are stable i18n keys rather than
// localized text, so the command table remains language independent.
type commandDefinition struct {
	Name           string
	Handler        commandHandler
	UsageKey       string
	DescriptionKey string
}

// commandDefinitions returns the canonical command/help order.
func commandDefinitions() []commandDefinition {
	return []commandDefinition{
		{Name: "run", Handler: runCommand, UsageKey: "help.usageRun"},
		{Name: "plan", Handler: planCommand, UsageKey: "help.usagePlan"},
		{Name: "list", Handler: commandWithoutContext(listCommand), UsageKey: "help.usageList"},
		{Name: "render", Handler: commandWithoutContext(renderCommand), UsageKey: "help.usageRender"},
		{Name: "compare", Handler: commandWithoutContext(compareCommand), UsageKey: "help.usageCompare"},
		{Name: "config", Handler: commandWithoutContext(configCommand), UsageKey: "help.usageConfig"},
		{Name: "leaderboard", Handler: commandWithoutContext(leaderboardCommand), UsageKey: "help.usageLeaderboard"},
		{Name: "submit", Handler: commandWithoutContext(submitCommand), UsageKey: "help.usageSubmit"},
		{Name: "version", Handler: versionCommand, UsageKey: "help.usageVersion"},
		{Name: "help", Handler: helpCommand, UsageKey: "help.usageHelp"},
	}
}

func lookupCommand(definitions []commandDefinition, name string) (commandDefinition, bool) {
	for _, definition := range definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return commandDefinition{}, false
}

func commandWithoutContext(handler func(application, []string, io.Writer, io.Writer) int) commandHandler {
	return func(app application, _ context.Context, args []string, stdout, stderr io.Writer) int {
		return handler(app, args, stdout, stderr)
	}
}

func versionCommand(_ application, _ context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--bundle" {
		fmt.Fprintln(stdout, buildinfo.ToolsBundle)
		return 0
	}
	if len(args) != 0 {
		fmt.Fprintf(stderr, "%s: %s\n", i18n.T("cli.error"), i18n.T("help.extraArgs"))
		return 1
	}
	fmt.Fprintf(stdout, "%s %s commit=%s built=%s go=%s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.Version())
	return 0
}

func helpCommand(app application, _ context.Context, _ []string, stdout, _ io.Writer) int {
	printHelp(app.commands, stdout)
	return 0
}

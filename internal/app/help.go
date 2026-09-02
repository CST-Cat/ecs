package app

import (
	"fmt"
	"io"
	"strings"

	"ecs/internal/i18n"
)

func printHelp(writer io.Writer) {
	fmt.Fprintf(writer, "ecs — %s\n\n", i18n.T("cli.tagline"))
	fmt.Fprintf(writer, "%s:\n", i18n.T("cli.usage"))
	for _, definition := range defaultCommandCatalog().definitionsInOrder() {
		fmt.Fprintf(writer, "  %s\n", commandHelpText(definition))
	}
	fmt.Fprintln(writer)
	if i18n.Current() == i18n.LangEN {
		fmt.Fprintln(writer, `Examples:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,md,html --output ./compare

Run ecs run --help for all test options or ecs compare --help for comparison options.`)
		return
	}
	fmt.Fprintln(writer, `常用示例:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,md,html --output ./compare

运行 ecs run --help 查看测试参数，或运行 ecs compare --help 查看对比参数。`)
}

func commandHelpText(definition commandDefinition) string {
	parts := make([]string, 0, 2)
	if key := strings.TrimSpace(definition.UsageKey); key != "" {
		parts = append(parts, i18n.T(key))
	}
	if key := strings.TrimSpace(definition.DescriptionKey); key != "" {
		parts = append(parts, i18n.T(key))
	}
	return strings.Join(parts, " ")
}

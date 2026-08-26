package app

import (
	"encoding/json"
	"fmt"
	"io"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

func configCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "example" {
		fmt.Fprintln(stderr, i18n.T("help.configUsage"))
		return 1
	}
	content, err := json.MarshalIndent(config.ExampleFile(), "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	fmt.Fprintln(stdout, string(content))
	return 0
}

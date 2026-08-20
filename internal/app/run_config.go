package app

import (
	"strings"
)

// preparse extracts only the config/profile selectors required to establish
// defaults before the full run flag set is created. The complete resolver is
// intentionally introduced in the next refactor stage so this file move does
// not change CLI semantics.
func preparse(args []string) (configPath, profile string) {
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--config" && index+1 < len(args):
			configPath = args[index+1]
			index++
		case strings.HasPrefix(value, "--config="):
			configPath = strings.TrimPrefix(value, "--config=")
		case value == "--profile" && index+1 < len(args):
			profile = args[index+1]
			index++
		case strings.HasPrefix(value, "--profile="):
			profile = strings.TrimPrefix(value, "--profile=")
		}
	}
	return configPath, profile
}

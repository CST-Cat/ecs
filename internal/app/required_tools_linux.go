//go:build linux

package app

import "ecs/internal/tool"

// resolveRequiredTools projects logical requirements into the wrapper-managed
// dependencies for the Linux plan. Linux currently retains every known
// builtin, including speedtest's separate signed-package path.
func resolveRequiredTools(declared []string) []string {
	return tool.RequiredToolIDs(tool.PlatformLinux, declared)
}

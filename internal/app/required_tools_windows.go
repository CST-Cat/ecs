//go:build windows

package app

import "ecs/internal/tool"

// resolveRequiredTools projects logical requirements into wrapper-managed
// dependencies for the Windows plan. Native ICMP is platform-provided, and
// unsupported tools are omitted.
func resolveRequiredTools(declared []string) []string {
	return tool.RequiredToolIDs(tool.PlatformWindows, declared)
}

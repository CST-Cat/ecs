//go:build windows

package app

import "ecs/internal/tool"

// resolveRequiredTools applies the private staged-dependency projection.
// Windows native ICMP is represented by the base-system source, and
// unsupported tools are not staged.
func resolveRequiredTools(declared []string) []string {
	return tool.BundleToolIDs(tool.PlatformWindows, declared)
}

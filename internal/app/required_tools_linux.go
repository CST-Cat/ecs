//go:build linux

package app

import "ecs/internal/tool"

// resolveRequiredTools projects declared requirements through the canonical
// runtime source facts. Linux currently maps every known builtin to the
// private staged-dependency projection.
func resolveRequiredTools(declared []string) []string {
	return tool.BundleToolIDs(tool.PlatformLinux, declared)
}

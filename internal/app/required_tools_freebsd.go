//go:build freebsd

package app

import "ecs/internal/tool"

// resolveRequiredTools applies the private staged-dependency projection.
// FreeBSD's ping and NextTrace logical tools are base-system sources and are
// omitted; speedtest remains in required_tools for run.sh's separately
// verified signed-package path, which fails closed on FreeBSD.
func resolveRequiredTools(declared []string) []string {
	return tool.BundleToolIDs(tool.PlatformFreeBSD, declared)
}

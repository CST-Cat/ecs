//go:build freebsd

package app

import "ecs/internal/tool"

// resolveRequiredTools projects logical requirements into wrapper-managed
// dependencies for the FreeBSD plan. Platform-provided ping and traceroute
// are omitted; speedtest remains for run.sh's separately verified
// signed-package path, which fails closed on FreeBSD.
func resolveRequiredTools(declared []string) []string {
	return tool.RequiredToolIDs(tool.PlatformFreeBSD, declared)
}

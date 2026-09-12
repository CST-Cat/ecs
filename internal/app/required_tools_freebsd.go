//go:build freebsd

package app

// freeBSDBaseProvidedTools are the declared tool requirements that FreeBSD
// already satisfies with a base-system utility. ECS must never download, copy,
// or chmod a network utility to recreate a privilege boundary the OS already
// owns:
//
//	ping           -> /sbin/ping
//	nexttrace-tiny -> /usr/sbin/traceroute and /usr/sbin/traceroute6
//
// This is the executable-contract counterpart of the package-side rule in
// scripts/lib/common.sh (ecs_target_tool_names), which keeps the same two tools
// out of the FreeBSD frozen bundle. The plan must never ask the wrapper for a
// tool that the matching bundle deliberately does not ship.
var freeBSDBaseProvidedTools = map[string]struct{}{
	"ping":           {},
	"nexttrace-tiny": {},
}

// resolveRequiredTools drops declared tools that FreeBSD provides in its base
// system and preserves the declared order for everything else. The result is a
// fresh slice; the descriptor's own RequiredTools storage stays immutable.
func resolveRequiredTools(declared []string) []string {
	var resolved []string
	for _, toolID := range declared {
		if _, provided := freeBSDBaseProvidedTools[toolID]; provided {
			continue
		}
		resolved = append(resolved, toolID)
	}
	return resolved
}

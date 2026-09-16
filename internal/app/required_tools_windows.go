//go:build windows

package app

// Windows currently has no frozen runtime for these declared Unix adapters.
// The module definitions remain canonical so ecs list and selection retain
// every module ID, while this boundary keeps the wrapper contract limited to
// tools that a Windows bundle can actually provide in this phase.
var windowsUnsupportedTools = map[string]struct{}{
	"ping":           {},
	"sysbench":       {},
	"iperf3":         {},
	"speedtest":      {},
	"nexttrace-tiny": {},
}

func resolveRequiredTools(declared []string) []string {
	var resolved []string
	for _, toolID := range declared {
		if _, unsupported := windowsUnsupportedTools[toolID]; unsupported {
			continue
		}
		resolved = append(resolved, toolID)
	}
	return resolved
}

//go:build linux

package app

// resolveRequiredTools maps a module's declared tool requirements to the tools
// the wrapper must actually stage on this platform.
//
// Linux has no base-system substitute for any declared tool: the frozen bundle
// supplied through ECS_TOOL_BIN is the only source, so the declared contract is
// returned unchanged. The result is a fresh slice; the descriptor's own
// RequiredTools storage stays immutable.
func resolveRequiredTools(declared []string) []string {
	if len(declared) == 0 {
		return nil
	}
	resolved := make([]string, len(declared))
	copy(resolved, declared)
	return resolved
}

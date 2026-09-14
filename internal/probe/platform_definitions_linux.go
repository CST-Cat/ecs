//go:build linux

package probe

// applyPlatformDefinitions is the compile-time platform boundary for module
// definitions. Linux keeps the canonical definitions unchanged: all declared
// frozen tools remain part of the execution contract.
func applyPlatformDefinitions(definitions []Definition) []Definition {
	return definitions
}

//go:build freebsd

package probe

// applyPlatformDefinitions is the compile-time platform boundary for module
// definitions. FreeBSD keeps canonical module metadata unchanged; its
// base-system tool substitutions are resolved by app's required-tools
// boundary, not by deleting modules or rewriting their methodology.
func applyPlatformDefinitions(definitions []Definition) []Definition {
	return definitions
}

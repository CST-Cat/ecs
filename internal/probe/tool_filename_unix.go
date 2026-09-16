//go:build linux || freebsd

package probe

// toolFilename keeps the logical tool ID separate from the executable name
// used by the host platform. Unix frozen tool staging uses the ID verbatim.
func toolFilename(name string) string { return name }

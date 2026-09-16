//go:build windows

package probe

// toolFilename keeps the logical tool ID separate from the executable name
// used by the host platform. Windows frozen tools are staged as real .exe
// files; callers continue to pass IDs such as "fio" and "zstd".
func toolFilename(name string) string { return name + ".exe" }

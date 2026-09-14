//go:build linux || freebsd

package ui

import "io"

// Unix terminals already interpret the ANSI sequences used by the existing
// live progress renderer; the platform-specific isTerminalFile check remains
// the gate for redirected descriptors.
func terminalSupportsLiveProgress(io.Writer) bool { return true }

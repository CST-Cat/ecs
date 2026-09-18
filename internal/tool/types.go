// Package tool contains the low-level, feature-independent tool contract.
//
// It describes the executable identities and external service facts needed by the
// execution-plan projection. Package manifests and process execution remain
// owned by their respective higher-level packages.
package tool

// Definition is the complete application-facing identity of one logical
// executable tool. It contains no network locations, checksums, shell
// commands, or manifest build facts.
type Definition struct {
	ID              string
	ExternalService string
}

// Platform identifies one of the supported runtime operating systems.
type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformFreeBSD Platform = "freebsd"
	PlatformWindows Platform = "windows"
)

// ToolSource describes where a logical tool comes from at runtime. Build and
// package facts remain owned by tools/lock.json; this is only the runtime
// source used by the plan and platform definitions.
type ToolSource string

const (
	ToolSourceBundle      ToolSource = "bundle"
	ToolSourceBaseSystem  ToolSource = "base-system"
	ToolSourceUnsupported ToolSource = "unsupported"
)

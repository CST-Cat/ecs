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

// ToolSource classifies whether a logical tool is wrapper-managed,
// platform-provided, or unsupported. Package and download facts remain with
// the wrapper and tools/lock.json.
type ToolSource string

const (
	ToolSourceWrapperManaged   ToolSource = "wrapper-managed"
	ToolSourcePlatformProvided ToolSource = "platform-provided"
	ToolSourceUnsupported      ToolSource = "unsupported"
)

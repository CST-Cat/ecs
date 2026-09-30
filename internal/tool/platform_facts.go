package tool

// platformToolException records a platform/source classification exception to
// the default wrapper-managed classification. Entries include both
// platform-provided and unsupported tools. Keeping only exceptions here leaves
// the canonical builtin order in builtins.go and avoids a second tool manifest
// or registry.
type platformToolException struct {
	Platform Platform
	ID       string
	Source   ToolSource
}

var platformToolExceptions = []platformToolException{
	{Platform: PlatformFreeBSD, ID: "ping", Source: ToolSourcePlatformProvided},
	{Platform: PlatformFreeBSD, ID: "nexttrace-tiny", Source: ToolSourcePlatformProvided},
	// Windows native ICMP is platform-owned and is represented by the
	// platform-provided source so it is never staged or downloaded.
	{Platform: PlatformWindows, ID: "ping", Source: ToolSourcePlatformProvided},
	{Platform: PlatformWindows, ID: "sysbench", Source: ToolSourceUnsupported},
	{Platform: PlatformWindows, ID: "iperf3", Source: ToolSourceUnsupported},
	{Platform: PlatformWindows, ID: "speedtest", Source: ToolSourceUnsupported},
}

// PlatformToolSource returns the runtime source for one known builtin tool.
// Unknown platforms and IDs fail closed as unsupported. The returned source
// never consults tools/lock.json, so an installed ECS binary remains
// independent from the repository checkout.
func PlatformToolSource(platform Platform, id string) ToolSource {
	if _, ok := LookupBuiltin(id); !ok {
		return ToolSourceUnsupported
	}
	switch platform {
	case PlatformLinux, PlatformFreeBSD, PlatformWindows:
	default:
		return ToolSourceUnsupported
	}
	for _, exception := range platformToolExceptions {
		if exception.Platform == platform && exception.ID == id {
			return exception.Source
		}
	}
	// Unix speedtest intentionally remains in the plan's wrapper-managed
	// dependency projection. It is not a tools/lock.json archive member: the
	// wrapper prepares its separately verified signed client, while FreeBSD
	// fails closed before that path is used.
	return ToolSourceWrapperManaged
}

// RequiredToolIDs projects logical module requirements into the dependencies
// the platform's wrapper must handle. It includes wrapper-managed IDs such as
// speedtest's separately verified signed-package path; platform-provided and
// unsupported sources do not appear in the plan projection.
func RequiredToolIDs(platform Platform, declared []string) []string {
	var required []string
	for _, id := range declared {
		if PlatformToolSource(platform, id) == ToolSourceWrapperManaged {
			required = append(required, id)
		}
	}
	return required
}

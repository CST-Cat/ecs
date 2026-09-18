package tool

// platformToolException is a runtime source fact that differs from the
// normal bundle source for a supported platform. Keeping only exceptions here
// leaves the canonical builtin order in builtins.go and avoids a second tool
// manifest or registry.
type platformToolException struct {
	Platform Platform
	ID       string
	Source   ToolSource
}

var platformToolExceptions = []platformToolException{
	{Platform: PlatformFreeBSD, ID: "ping", Source: ToolSourceBaseSystem},
	{Platform: PlatformFreeBSD, ID: "nexttrace-tiny", Source: ToolSourceBaseSystem},
	// Windows native ICMP is platform-owned and is represented by the
	// base-system source so it is never staged or downloaded.
	{Platform: PlatformWindows, ID: "ping", Source: ToolSourceBaseSystem},
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
	// Unix speedtest intentionally remains in the plan's private staged
	// dependency projection. It is not a tools/lock.json archive member: the
	// wrapper places its separately verified signed client in the same private
	// staging directory, while FreeBSD fails closed before that path is used.
	return ToolSourceBundle
}

// BundleToolIDs projects declared module requirements into the private staged
// dependencies that the wrapper must prepare. ToolSourceBundle is a projection
// label here, not a claim that every returned ID is a tools/lock.json archive
// member: Unix speedtest uses the wrapper's separately verified signed package
// path. Base-system and unsupported sources are not staged.
func BundleToolIDs(platform Platform, declared []string) []string {
	var staged []string
	for _, id := range declared {
		if PlatformToolSource(platform, id) == ToolSourceBundle {
			staged = append(staged, id)
		}
	}
	return staged
}

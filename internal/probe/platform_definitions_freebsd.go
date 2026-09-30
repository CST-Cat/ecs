//go:build freebsd

package probe

// applyPlatformDefinitions is the compile-time platform boundary for module
// definitions. FreeBSD route execution uses the base-system traceroute backend,
// so its methodology names that backend while retaining the shared module
// contract.
func applyPlatformDefinitions(definitions []Definition) []Definition {
	for index := range definitions {
		if definitions[index].Descriptor.ID == "route" {
			definitions[index].Descriptor.Methodology.Engine = traceFreeBSDTracerouteName
		}
	}
	return definitions
}

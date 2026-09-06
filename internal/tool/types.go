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

func validExternalService(service string) bool {
	return service == "" || service == "ookla"
}

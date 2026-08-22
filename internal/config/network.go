package config

import "strings"

// IPVersions returns the protocol families selected for a run. The result is
// stable so callers can build deterministic per-family rows and errors.
func IPVersions(mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case IPVersion4:
		return []string{IPVersion4}
	case IPVersion6:
		return []string{IPVersion6}
	default:
		return []string{IPVersion4, IPVersion6}
	}
}

// AllowsIPVersion reports whether a concrete family is enabled by the run.
func AllowsIPVersion(mode, version string) bool {
	for _, candidate := range IPVersions(mode) {
		if candidate == version {
			return true
		}
	}
	return false
}

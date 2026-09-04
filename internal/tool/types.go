// Package tool contains the low-level, feature-independent tool contract.
//
// It describes the executable identities and staging facts needed by the
// execution-plan projection. Package manifests and process execution remain
// owned by their respective higher-level packages.
package tool

// StagingCategory identifies the plan staging behavior of a tool.
type StagingCategory string

const (
	StagingNone       StagingCategory = "none"
	StagingArchive    StagingCategory = "archive"
	StagingZstdCorpus StagingCategory = "zstd_corpus"
	StagingNextTrace  StagingCategory = "nexttrace"
	StagingOokla      StagingCategory = "ookla_package"
)

// StagingSource identifies stable plan/v1 source enum values for special
// staging categories. Ordinary archive tools have no source field.
type StagingSource string

const (
	StagingSourceNone                  StagingSource = ""
	StagingSourceNextTraceArchitecture StagingSource = "official-architecture-asset"
	StagingSourceOoklaSignedPackage    StagingSource = "official-signed-package-source"
)

// StagingPolicy contains the typed plan staging capability of a tool.
type StagingPolicy struct {
	Category StagingCategory
	Source   StagingSource
}

// Definition is the complete application-facing identity of one logical
// executable tool. It contains no network locations, checksums, shell
// commands, or manifest build facts.
type Definition struct {
	ID      string
	Staging StagingPolicy
}

func (category StagingCategory) valid() bool {
	switch category {
	case StagingNone, StagingArchive, StagingZstdCorpus, StagingNextTrace, StagingOokla:
		return true
	default:
		return false
	}
}

func (source StagingSource) valid() bool {
	return source == StagingSourceNone || source == StagingSourceNextTraceArchitecture || source == StagingSourceOoklaSignedPackage
}

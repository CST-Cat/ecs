// Package tool contains the low-level, feature-independent tool contract.
//
// It describes the executable facts needed by the application doctor and
// execution-plan projection. Package manifests and process execution remain
// owned by their respective higher-level packages.
package tool

import "strings"

// VerificationKind selects the application-owned verification behavior for a
// tool. The catalog carries the data for a policy; the application retains the
// concrete process and file inspection implementation.
type VerificationKind string

const (
	VerificationCommand        VerificationKind = "command"
	VerificationPinnedZstd     VerificationKind = "pinned_zstd"
	VerificationPinnedOpenSSL  VerificationKind = "pinned_openssl"
	VerificationNPB            VerificationKind = "npb"
	VerificationOfficialStream VerificationKind = "official_stream"
)

// NPBVariant identifies which NAS Parallel Benchmarks executable is checked.
type NPBVariant string

const (
	NPBVariantNone NPBVariant = ""
	NPBVariantEP   NPBVariant = "EP"
	NPBVariantFT   NPBVariant = "FT"
)

// VerificationPolicy contains typed, immutable-by-catalog verification facts.
// Arguments are copied by Catalog construction and every read operation.
type VerificationPolicy struct {
	Kind            VerificationKind
	Arguments       []string
	ExpectedVersion string
	SuccessLabel    string
	NPBVariant      NPBVariant
}

// DoctorPolicy describes whether a tool appears in the standard doctor and
// whether a reference from a standard module makes it mandatory. Order is the
// stable legacy doctor display order; it is data in the catalog, not a second
// tool list in the application package.
type DoctorPolicy struct {
	Standard bool
	Required bool
	Order    int
}

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
	ID           string
	PurposeKey   string
	Verification VerificationPolicy
	Doctor       DoctorPolicy
	Staging      StagingPolicy
}

func (kind VerificationKind) valid() bool {
	switch kind {
	case VerificationCommand, VerificationPinnedZstd, VerificationPinnedOpenSSL,
		VerificationNPB, VerificationOfficialStream:
		return true
	default:
		return false
	}
}

func (variant NPBVariant) valid() bool {
	return variant == NPBVariantEP || variant == NPBVariantFT
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

func nonblank(value string) bool {
	return strings.TrimSpace(value) != ""
}

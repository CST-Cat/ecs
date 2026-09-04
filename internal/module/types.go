// Package module contains the low-level, feature-independent module contract.
// It describes module metadata only; probe implementations and application
// configuration are owned by higher-level packages.
package module

import (
	"time"

	"ecs/internal/model"
)

// Concurrency describes the amount of interference a module can cause when it
// runs alongside another module.
type Concurrency string

const (
	ConcurrencyExclusive Concurrency = "exclusive"
	ConcurrencyProbe     Concurrency = "probe"
)

// EstimateMode identifies the generic shape of a module's runtime estimate.
// Fixed estimates use the descriptor's Estimate value; the remaining modes
// derive their duration from runtime settings that control the probe.
type EstimateMode string

const (
	EstimateModeFixed      EstimateMode = "fixed"
	EstimateModeTwoContext EstimateMode = "two_context"
	EstimateModeCPU        EstimateMode = "cpu"
	EstimateModeMemory     EstimateMode = "memory"
	EstimateModeDisk       EstimateMode = "disk"
	EstimateModeDNS        EstimateMode = "dns"
	EstimateModeLatency    EstimateMode = "latency"
	EstimateModeSpeed      EstimateMode = "speed"
	EstimateModeRoute      EstimateMode = "route"
)

// Exposure is the maximum permitted external-contact level. The CLI string
// parser remains in config; the value and ordering semantics belong here so
// low-level module descriptors do not depend on configuration parsing.
type Exposure int

const (
	ExposureLocal Exposure = iota
	ExposurePublic
	ExposureThirdParty
	ExposureConsent
)

// String returns the stable CLI/report name for an exposure level.
func (e Exposure) String() string {
	switch e {
	case ExposureLocal:
		return "local"
	case ExposurePublic:
		return "public"
	case ExposureThirdParty:
		return "thirdparty"
	case ExposureConsent:
		return "any"
	default:
		return "invalid"
	}
}

// Valid reports whether e is one of the declared exposure levels.
func (e Exposure) Valid() bool {
	return e >= ExposureLocal && e <= ExposureConsent
}

// ExposureMetadata is the exposure-related metadata consumed by selection and
// egress planning.
type ExposureMetadata struct {
	Level         Exposure
	NeedsEgressIP bool
}

// Descriptor is the feature-independent description of one module.
//
// It owns module identity, canonical selection metadata, exposure policy,
// scheduler class, methodology, tool requirements, presentation keys, and a
// rough estimate. Probe executors are deliberately outside this low-level
// contract; the probe package couples them to descriptors at composition.
type Descriptor struct {
	ID string

	// ProfileStandard records membership in the caller-defined standard set.
	// Callers map user-facing profile names to this metadata; the module package
	// does not define those names.
	ProfileStandard bool

	Exposure      Exposure
	NeedsEgressIP bool
	Concurrency   Concurrency

	Methodology model.Methodology

	// RequiredTools lists tools relevant to the module. Route probes have a
	// single execution contract: NextTrace Tiny. This metadata is also consumed
	// by the wrapper dependency planner.
	RequiredTools []string

	TitleKey       string
	DescriptionKey string
	// PrivacyNoticeKey is an optional stable presentation key for modules whose
	// external client or service has an independent privacy/data-processing
	// policy. Empty means that the module needs no additional notice.
	PrivacyNoticeKey string
	// WizardGroup and WizardQuestionKey describe the optional interactive
	// switch for costly/privacy-sensitive modules. Empty means that the module
	// is always included by the wizard and is not individually asked about.
	WizardGroup       string
	WizardQuestionKey string

	// Estimate is a rough standalone duration used by lightweight integrations.
	// Probe remains authoritative for runtime-sensitive estimates.
	Estimate     time.Duration
	EstimateMode EstimateMode
}

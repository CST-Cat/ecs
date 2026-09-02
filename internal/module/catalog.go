package module

import (
	"fmt"
	"strings"
)

// Catalog is an immutable ordered collection of validated module metadata.
// The constructor copies all reference-typed fields, and read methods return
// values or defensive copies. There is intentionally no runtime registration
// or replacement API.
type Catalog struct {
	descriptors []Descriptor
}

// NewCatalog validates and copies descriptors in their supplied canonical
// order. Order is explicit in the input and is never derived from map
// iteration.
func NewCatalog(descriptors []Descriptor) (Catalog, error) {
	copyOfDescriptors := make([]Descriptor, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for index, descriptor := range descriptors {
		if err := validateDescriptor(index, descriptor, seen); err != nil {
			return Catalog{}, err
		}
		seen[strings.TrimSpace(descriptor.ID)] = struct{}{}
		copyOfDescriptors[index] = copyDescriptor(descriptor)
	}
	return Catalog{descriptors: copyOfDescriptors}, nil
}

// Descriptors returns validated descriptors in canonical order. Every
// reference-typed field is copied before returning.
func (catalog Catalog) Descriptors() []Descriptor {
	result := make([]Descriptor, len(catalog.descriptors))
	for index, descriptor := range catalog.descriptors {
		result[index] = copyDescriptor(descriptor)
	}
	return result
}

// Lookup returns one descriptor by its exact ID. Unknown IDs never produce a
// default descriptor and do not mutate the catalog.
func (catalog Catalog) Lookup(id string) (Descriptor, bool) {
	for _, descriptor := range catalog.descriptors {
		if descriptor.ID == id {
			return copyDescriptor(descriptor), true
		}
	}
	return Descriptor{}, false
}

// IDs returns descriptor IDs in canonical order.
func (catalog Catalog) IDs() []string {
	ids := make([]string, 0, len(catalog.descriptors))
	for _, descriptor := range catalog.descriptors {
		ids = append(ids, descriptor.ID)
	}
	return ids
}

// StandardIDs returns IDs whose descriptors opt into the standard selection.
// The caller owns the returned slice; profile names and unknown-profile
// handling remain in config.
func (catalog Catalog) StandardIDs() []string {
	ids := make([]string, 0, len(catalog.descriptors))
	for _, descriptor := range catalog.descriptors {
		if descriptor.ProfileStandard {
			ids = append(ids, descriptor.ID)
		}
	}
	return ids
}

// ExposureFor returns exposure metadata for one known module.
func (catalog Catalog) ExposureFor(id string) (ExposureMetadata, bool) {
	for _, descriptor := range catalog.descriptors {
		if descriptor.ID == id {
			return ExposureMetadata{Level: descriptor.Exposure, NeedsEgressIP: descriptor.NeedsEgressIP}, true
		}
	}
	return ExposureMetadata{}, false
}

func validateDescriptor(index int, descriptor Descriptor, seen map[string]struct{}) error {
	trimmedID := strings.TrimSpace(descriptor.ID)
	if trimmedID == "" {
		return fmt.Errorf("module descriptor %d has empty ID", index)
	}
	if trimmedID != descriptor.ID {
		return fmt.Errorf("module descriptor %d ID %q has surrounding whitespace", index, descriptor.ID)
	}
	if _, exists := seen[trimmedID]; exists {
		return fmt.Errorf("duplicate module descriptor %q", descriptor.ID)
	}
	if !descriptor.Exposure.Valid() {
		return fmt.Errorf("module %q has unknown exposure level %d", descriptor.ID, descriptor.Exposure)
	}
	if descriptor.Concurrency != ConcurrencyExclusive && descriptor.Concurrency != ConcurrencyProbe {
		return fmt.Errorf("module %q has unknown concurrency class %q", descriptor.ID, descriptor.Concurrency)
	}
	switch descriptor.EstimateMode {
	case EstimateModeFixed, EstimateModeTwoContext, EstimateModeCPU, EstimateModeMemory, EstimateModeDisk,
		EstimateModeDNS, EstimateModeLatency, EstimateModeSpeed, EstimateModeRoute:
	default:
		return fmt.Errorf("module %q has unknown estimate mode %q", descriptor.ID, descriptor.EstimateMode)
	}
	if descriptor.Estimate < 0 {
		return fmt.Errorf("module %q has negative estimate %s", descriptor.ID, descriptor.Estimate)
	}
	if strings.TrimSpace(descriptor.Methodology.Kind) == "" ||
		strings.TrimSpace(descriptor.Methodology.Label) == "" ||
		strings.TrimSpace(descriptor.Methodology.Engine) == "" ||
		strings.TrimSpace(descriptor.Methodology.Profile) == "" ||
		strings.TrimSpace(descriptor.Methodology.ComparisonScope) == "" {
		return fmt.Errorf("module %q has incomplete methodology", descriptor.ID)
	}
	if strings.TrimSpace(descriptor.TitleKey) == "" || strings.TrimSpace(descriptor.DescriptionKey) == "" {
		return fmt.Errorf("module %q has incomplete display metadata", descriptor.ID)
	}
	groupPresent := strings.TrimSpace(descriptor.WizardGroup) != ""
	questionPresent := strings.TrimSpace(descriptor.WizardQuestionKey) != ""
	if groupPresent != questionPresent {
		return fmt.Errorf("module %q has unpaired wizard metadata", descriptor.ID)
	}
	return nil
}

func copyDescriptor(descriptor Descriptor) Descriptor {
	if descriptor.RequiredTools != nil {
		requiredTools := make([]string, len(descriptor.RequiredTools))
		copy(requiredTools, descriptor.RequiredTools)
		descriptor.RequiredTools = requiredTools
	}
	if descriptor.Methodology.Parameters != nil {
		parameters := make(map[string]string, len(descriptor.Methodology.Parameters))
		for key, value := range descriptor.Methodology.Parameters {
			parameters[key] = value
		}
		descriptor.Methodology.Parameters = parameters
	}
	return descriptor
}

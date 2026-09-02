package tool

import (
	"fmt"
	"strings"
)

// Catalog is an immutable ordered collection of validated tool metadata.
// NewCatalog copies every reference field, and all read methods return
// caller-owned copies. A zero Catalog is intentionally invalid so composition
// roots cannot silently substitute an uninitialized dependency.
type Catalog struct {
	definitions []Definition
	valid       bool
}

// NewCatalog validates and copies definitions in their supplied canonical
// order. Order is explicit input and never derived from map iteration.
func NewCatalog(definitions []Definition) (Catalog, error) {
	copyOfDefinitions := make([]Definition, len(definitions))
	seenIDs := make(map[string]struct{}, len(definitions))
	seenDoctorOrders := make(map[int]string, len(definitions))
	for index, definition := range definitions {
		if err := validateDefinition(index, definition, seenIDs, seenDoctorOrders); err != nil {
			return Catalog{}, err
		}
		seenIDs[definition.ID] = struct{}{}
		if definition.Doctor.Standard {
			seenDoctorOrders[definition.Doctor.Order] = definition.ID
		}
		copyOfDefinitions[index] = copyDefinition(definition)
	}
	return Catalog{definitions: copyOfDefinitions, valid: true}, nil
}

// Valid reports whether the catalog was produced by NewCatalog. It lets a
// composition root reject a zero value without exposing backing storage.
func (catalog Catalog) Valid() bool { return catalog.valid }

// Definitions returns all definitions in canonical catalog order.
func (catalog Catalog) Definitions() []Definition {
	result := make([]Definition, len(catalog.definitions))
	for index, definition := range catalog.definitions {
		result[index] = copyDefinition(definition)
	}
	return result
}

// DoctorDefinitions returns standard-doctor definitions in their stable
// display order. The returned definitions and nested argument slices are
// caller-owned.
func (catalog Catalog) DoctorDefinitions() []Definition {
	result := make([]Definition, 0, len(catalog.definitions))
	used := make([]bool, len(catalog.definitions))
	for len(result) < standardDoctorCount(catalog.definitions) {
		best := -1
		for index, definition := range catalog.definitions {
			if used[index] || !definition.Doctor.Standard {
				continue
			}
			if best < 0 || definition.Doctor.Order < catalog.definitions[best].Doctor.Order {
				best = index
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		result = append(result, copyDefinition(catalog.definitions[best]))
	}
	return result
}

func standardDoctorCount(definitions []Definition) int {
	count := 0
	for _, definition := range definitions {
		if definition.Doctor.Standard {
			count++
		}
	}
	return count
}

// Lookup performs an exact ID lookup. Unknown IDs return a zero definition and
// never select a default tool or mutate the catalog.
func (catalog Catalog) Lookup(id string) (Definition, bool) {
	for _, definition := range catalog.definitions {
		if definition.ID == id {
			return copyDefinition(definition), true
		}
	}
	return Definition{}, false
}

// IDs returns all IDs in canonical catalog order.
func (catalog Catalog) IDs() []string {
	ids := make([]string, 0, len(catalog.definitions))
	for _, definition := range catalog.definitions {
		ids = append(ids, definition.ID)
	}
	return ids
}

func validateDefinition(index int, definition Definition, seenIDs map[string]struct{}, seenDoctorOrders map[int]string) error {
	if !canonicalID(definition.ID) {
		if strings.TrimSpace(definition.ID) == "" {
			return fmt.Errorf("tool definition %d has empty ID", index)
		}
		return fmt.Errorf("tool definition %d ID %q is noncanonical", index, definition.ID)
	}
	if _, exists := seenIDs[definition.ID]; exists {
		return fmt.Errorf("duplicate tool definition %q", definition.ID)
	}
	if !nonblank(definition.PurposeKey) {
		return fmt.Errorf("tool %q has missing purpose key", definition.ID)
	}
	if err := validateVerification(definition.ID, definition.Verification); err != nil {
		return err
	}
	if definition.Doctor.Required && !definition.Doctor.Standard {
		return fmt.Errorf("tool %q is required but not in standard doctor", definition.ID)
	}
	if definition.Doctor.Standard {
		if definition.Doctor.Order < 0 {
			return fmt.Errorf("tool %q has negative doctor order", definition.ID)
		}
		if previous, exists := seenDoctorOrders[definition.Doctor.Order]; exists {
			return fmt.Errorf("duplicate standard doctor order %d for tools %q and %q", definition.Doctor.Order, previous, definition.ID)
		}
	} else if definition.Doctor.Order != -1 {
		return fmt.Errorf("tool %q has doctor order %d outside standard doctor", definition.ID, definition.Doctor.Order)
	}
	if err := validateStaging(definition.ID, definition.Staging); err != nil {
		return err
	}
	return nil
}

func validateVerification(id string, policy VerificationPolicy) error {
	if !policy.Kind.valid() {
		return fmt.Errorf("tool %q has invalid verification kind %q", id, policy.Kind)
	}
	for index, argument := range policy.Arguments {
		if !nonblank(argument) {
			return fmt.Errorf("tool %q has blank verification argument %d", id, index)
		}
	}
	switch policy.Kind {
	case VerificationCommand:
		if len(policy.Arguments) == 0 {
			return fmt.Errorf("tool %q command verification has no arguments", id)
		}
		if policy.ExpectedVersion != "" || policy.SuccessLabel != "" || policy.NPBVariant != NPBVariantNone {
			return fmt.Errorf("tool %q command verification has incomplete policy metadata", id)
		}
	case VerificationPinnedZstd, VerificationPinnedOpenSSL:
		if len(policy.Arguments) == 0 || !nonblank(policy.ExpectedVersion) || !nonblank(policy.SuccessLabel) || policy.NPBVariant != NPBVariantNone {
			return fmt.Errorf("tool %q pinned verification has incomplete policy metadata", id)
		}
	case VerificationNPB:
		if len(policy.Arguments) != 0 || !policy.NPBVariant.valid() || !nonblank(policy.ExpectedVersion) || !nonblank(policy.SuccessLabel) {
			return fmt.Errorf("tool %q NPB verification has incomplete policy metadata", id)
		}
	case VerificationOfficialStream:
		if len(policy.Arguments) != 0 || !nonblank(policy.SuccessLabel) || policy.ExpectedVersion != "" || policy.NPBVariant != NPBVariantNone {
			return fmt.Errorf("tool %q STREAM verification has incomplete policy metadata", id)
		}
	}
	return nil
}

func validateStaging(id string, policy StagingPolicy) error {
	if !policy.Category.valid() || !policy.Source.valid() {
		return fmt.Errorf("tool %q has invalid staging policy", id)
	}
	wantSource := StagingSourceNone
	switch policy.Category {
	case StagingNone, StagingArchive, StagingZstdCorpus:
		wantSource = StagingSourceNone
	case StagingNextTrace:
		wantSource = StagingSourceNextTraceArchitecture
	case StagingOokla:
		wantSource = StagingSourceOoklaSignedPackage
	}
	if policy.Source != wantSource {
		return fmt.Errorf("tool %q has staging source %q for category %q, want %q", id, policy.Source, policy.Category, wantSource)
	}
	return nil
}

func canonicalID(id string) bool {
	if id == "" || strings.TrimSpace(id) != id {
		return false
	}
	for index := 0; index < len(id); index++ {
		character := id[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			continue
		}
		if character == '-' && index > 0 && index+1 < len(id) && id[index-1] != '-' {
			next := id[index+1]
			if (next >= 'a' && next <= 'z') || (next >= '0' && next <= '9') {
				continue
			}
		}
		return false
	}
	return true
}

func copyDefinition(definition Definition) Definition {
	if definition.Verification.Arguments != nil {
		arguments := make([]string, len(definition.Verification.Arguments))
		copy(arguments, definition.Verification.Arguments)
		definition.Verification.Arguments = arguments
	}
	return definition
}

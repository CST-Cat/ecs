package tool

import (
	"fmt"
	"strings"
)

// Catalog is an immutable ordered collection of validated tool metadata.
// All read methods return caller-owned values. A zero Catalog is intentionally
// invalid so composition roots cannot silently substitute an uninitialized
// dependency.
type Catalog struct {
	definitions []Definition
	valid       bool
}

// NewCatalog validates and copies definitions in their supplied canonical
// order. Order is explicit input and never derived from map iteration.
func NewCatalog(definitions []Definition) (Catalog, error) {
	copyOfDefinitions := make([]Definition, len(definitions))
	seenIDs := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		if err := validateDefinition(index, definition, seenIDs); err != nil {
			return Catalog{}, err
		}
		seenIDs[definition.ID] = struct{}{}
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

func validateDefinition(index int, definition Definition, seenIDs map[string]struct{}) error {
	if !canonicalID(definition.ID) {
		if strings.TrimSpace(definition.ID) == "" {
			return fmt.Errorf("tool definition %d has empty ID", index)
		}
		return fmt.Errorf("tool definition %d ID %q is noncanonical", index, definition.ID)
	}
	if _, exists := seenIDs[definition.ID]; exists {
		return fmt.Errorf("duplicate tool definition %q", definition.ID)
	}
	if err := validateStaging(definition.ID, definition.Staging); err != nil {
		return err
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
	return definition
}

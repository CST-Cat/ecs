package app

import (
	"strings"
	"testing"

	"ecs/internal/probe"
)

func TestApplicationCompositionPreservesCanonicalDefinitions(t *testing.T) {
	definitions := probe.BuiltinDefinitions()
	application, err := composeApplication(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(application.definitions) == 0 || len(application.commands) == 0 {
		t.Fatal("application composition is missing definitions or commands")
	}
	if len(application.definitions) != len(application.modules.IDs()) {
		t.Fatalf("definition/catalog sizes = %d/%d", len(application.definitions), len(application.modules.IDs()))
	}
	for index, definition := range application.definitionsInOrder() {
		if definition.Probe == nil || definition.Descriptor.ID != definition.Probe.ID() {
			t.Fatalf("application definition[%d] is not a validated pair: %+v", index, definition)
		}
		if definition.Descriptor.ID != definitions[index].Descriptor.ID || definition.Descriptor.ID != application.modules.IDs()[index] {
			t.Fatalf("application definition[%d] changed canonical order: got %q, want %q", index, definition.Descriptor.ID, definitions[index].Descriptor.ID)
		}
	}
}

func TestComposeApplicationRejectsMalformedDefinitions(t *testing.T) {
	_, err := composeApplication([]probe.Definition{{}})
	if err == nil || !strings.Contains(err.Error(), "empty ID") {
		t.Fatalf("malformed application definitions error = %v, want empty ID", err)
	}
}

func TestComposeApplicationRejectsMissingToolReference(t *testing.T) {
	definitions := probe.BuiltinDefinitions()
	definitions[3].Descriptor.RequiredTools[0] = "missing-tool"
	if _, err := composeApplication(definitions); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("missing tool reference error = %v", err)
	}
}

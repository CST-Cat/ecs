package app

import (
	"strings"
	"testing"

	"ecs/internal/probe"
	"ecs/internal/tool"
)

func TestApplicationCompositionOwnsValidatedCatalogs(t *testing.T) {
	definitions := probe.BuiltinDefinitions()
	tools, err := tool.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	application, err := composeApplication(definitions, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(application.definitions) == 0 || len(application.commands.definitionsInOrder()) == 0 {
		t.Fatal("application composition is missing definitions or commands")
	}
	if len(application.definitions) != len(application.modules.IDs()) {
		t.Fatalf("definition/catalog sizes = %d/%d", len(application.definitions), len(application.modules.IDs()))
	}
	for index, definition := range application.definitionsInOrder() {
		if definition.Probe == nil || definition.Descriptor.ID != definition.Probe.ID() {
			t.Fatalf("application definition[%d] is not a validated pair: %+v", index, definition)
		}
	}

	cpuIndex := -1
	for index, definition := range definitions {
		if definition.Descriptor.ID == "cpu" {
			cpuIndex = index
			break
		}
	}
	if cpuIndex < 0 || len(definitions[cpuIndex].Descriptor.RequiredTools) == 0 {
		t.Fatal("cpu definition does not have a required tool fixture")
	}
	definitions[0].Descriptor.ID = "mutated-input"
	definitions[cpuIndex].Descriptor.RequiredTools[0] = "mutated-input"
	fromInput := application.definitionsInOrder()
	if fromInput[0].Descriptor.ID != application.modules.IDs()[0] || fromInput[0].Descriptor.ID == "mutated-input" {
		t.Fatalf("application copied definition identity from input: %+v", fromInput[0].Descriptor)
	}
	if got := fromInput[cpuIndex].Descriptor.RequiredTools[0]; got != "sysbench" {
		t.Fatalf("application copied nested input metadata = %q, want sysbench", got)
	}

	definitions = application.definitionsInOrder()
	definitions[0].Descriptor.ID = "mutated-output"
	definitions[cpuIndex].Descriptor.RequiredTools[0] = "mutated-output"
	ids := application.modules.IDs()
	ids[0] = "mutated"
	fresh := application.definitionsInOrder()
	if fresh[0].Descriptor.ID != application.modules.IDs()[0] || fresh[0].Descriptor.ID == "mutated-output" {
		t.Fatalf("application definition slice leaked backing state: %+v", fresh[0].Descriptor)
	}
	if got := fresh[cpuIndex].Descriptor.RequiredTools[0]; got != "sysbench" {
		t.Fatalf("application definition nested output metadata = %q, want sysbench", got)
	}
	if application.modules.IDs()[0] == "mutated" {
		t.Fatal("application module catalog leaked IDs backing state")
	}
	toolDefinitions := application.tools.Definitions()
	toolDefinitions[1].ID = "mutated-tool"
	freshTool, ok := application.tools.Lookup("zstd")
	if !ok || freshTool.ID != "zstd" {
		t.Fatalf("application tool catalog leaked returned state: %+v", freshTool)
	}
}

func TestComposeApplicationRejectsMalformedDefinitions(t *testing.T) {
	tools, err := tool.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	_, err = composeApplication([]probe.Definition{{}}, tools)
	if err == nil || !strings.Contains(err.Error(), "empty ID") {
		t.Fatalf("malformed application definitions error = %v, want empty ID", err)
	}
}

func TestComposeApplicationRejectsMissingToolReference(t *testing.T) {
	definitions := probe.BuiltinDefinitions()
	definitions[3].Descriptor.RequiredTools[0] = "missing-tool"
	tools, err := tool.BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := composeApplication(definitions, tools); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("missing tool reference error = %v", err)
	}
}

func TestComposeApplicationRejectsUninitializedToolCatalog(t *testing.T) {
	if _, err := composeApplication(probe.BuiltinDefinitions(), tool.Catalog{}); err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("zero tool catalog error = %v", err)
	}
}

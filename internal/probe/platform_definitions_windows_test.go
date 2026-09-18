//go:build windows

package probe

import (
	"context"
	"reflect"
	"testing"

	"ecs/internal/model"
	"ecs/internal/tool"
)

func TestWindowsUnsupportedDefinitionsPreserveIDsAndToolFacts(t *testing.T) {
	definitions := BuiltinDefinitions()
	seen := make(map[string]bool, len(definitions))
	unsupported := make(map[string]struct{})
	for _, definition := range definitions {
		seen[definition.Descriptor.ID] = true
		if _, isUnsupported := definition.Probe.(windowsUnsupportedProbe); isUnsupported {
			unsupported[definition.Descriptor.ID] = struct{}{}
			result := definition.Probe.Run(context.Background(), Environment{})
			if result.Status != model.StatusSkipped || len(result.Failures) != 1 {
				t.Fatalf("Windows unsupported result for %q = %+v", definition.Descriptor.ID, result)
			}
			failure := result.Failures[0]
			if failure.Category != model.FailureUnsupported || failure.Message != windowsUnsupportedMethodology || failure.Stage != "platform" {
				t.Fatalf("Windows unsupported failure for %q = %+v", definition.Descriptor.ID, failure)
			}
			if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.platform.summary.unsupported" {
				t.Fatalf("Windows unsupported summary for %q = %+v", definition.Descriptor.ID, result.SummaryMessages)
			}
		}
	}
	wantUnsupported := make(map[string]struct{})
	for _, definition := range definitions {
		if windowsModuleHasUnsupportedTool(definition.Descriptor.RequiredTools) {
			wantUnsupported[definition.Descriptor.ID] = struct{}{}
		}
	}
	if !reflect.DeepEqual(unsupported, wantUnsupported) {
		t.Fatalf("Windows unsupported IDs = %v, want source-derived %v", unsupported, wantUnsupported)
	}
	wantUnsupportedIDs := []string{"cpu", "speed", "ookla"}
	for _, id := range wantUnsupportedIDs {
		if _, ok := unsupported[id]; !ok {
			t.Fatalf("Windows unsupported IDs = %v, missing %q", unsupported, id)
		}
	}
	if len(unsupported) != len(wantUnsupportedIDs) {
		t.Fatalf("Windows unsupported IDs = %v, want exactly %v", unsupported, wantUnsupportedIDs)
	}
	for _, id := range []string{"cpu", "speed", "ookla", "latency", "route", "backtrace"} {
		if !seen[id] {
			t.Fatalf("Windows canonical module ID %q disappeared", id)
		}
	}
	for _, definition := range definitions {
		if definition.Descriptor.ID != "route" && definition.Descriptor.ID != "backtrace" {
			continue
		}
		for _, requiredTool := range definition.Descriptor.RequiredTools {
			if source := tool.PlatformToolSource(tool.PlatformWindows, requiredTool); source != tool.ToolSourceBundle {
				t.Fatalf("Windows %s requirement %q source = %q, want bundle", definition.Descriptor.ID, requiredTool, source)
			}
		}
	}
}

//go:build windows

package probe

import (
	"context"
	"reflect"
	"testing"

	"ecs/internal/model"
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
	wantUnsupported := map[string]struct{}{
		"cpu":   {},
		"speed": {},
		"ookla": {},
	}
	if !reflect.DeepEqual(unsupported, wantUnsupported) {
		t.Fatalf("Windows unsupported IDs = %v, want exactly %v", unsupported, wantUnsupported)
	}
	for _, id := range []string{"cpu", "speed", "ookla", "latency", "route", "backtrace"} {
		if !seen[id] {
			t.Fatalf("Windows canonical module ID %q disappeared", id)
		}
	}
}

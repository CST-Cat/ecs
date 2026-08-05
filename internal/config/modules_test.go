package config

import (
	"testing"
	"time"
)

func TestModuleDescriptorsAreCanonical(t *testing.T) {
	descriptors := ModuleDescriptors()
	if len(descriptors) != len(ModuleOrder) {
		t.Fatalf("descriptor count = %d, ModuleOrder count = %d", len(descriptors), len(ModuleOrder))
	}
	for index, descriptor := range descriptors {
		if descriptor.ID != ModuleOrder[index] {
			t.Fatalf("descriptor[%d] = %q, ModuleOrder = %q", index, descriptor.ID, ModuleOrder[index])
		}
		if got, ok := ModuleDescriptorFor(descriptor.ID); !ok || got.ID != descriptor.ID {
			t.Fatalf("ModuleDescriptorFor(%q) = %+v, ok=%v", descriptor.ID, got, ok)
		}
		if descriptor.TitleKey == "" || descriptor.DescriptionKey == "" || descriptor.Methodology.Kind == "" {
			t.Fatalf("descriptor %q is missing shared metadata: %+v", descriptor.ID, descriptor)
		}
		if descriptor.ProfileStandard && !descriptor.ProfileFull {
			t.Fatalf("standard module %q is absent from full profile", descriptor.ID)
		}
	}
	if err := ValidateModuleDescriptors(); err != nil {
		t.Fatal(err)
	}
	if got := ModulesForProfile(ProfileStandard); len(got) != 16 {
		t.Fatalf("standard profile module count = %d, want 16: %v", len(got), got)
	}
	if got := ModulesForProfile(ProfileFull); len(got) != 18 {
		t.Fatalf("full profile module count = %d, want 18: %v", len(got), got)
	}
}

func TestModuleDescriptorMetadataMatchesCompatibilityTables(t *testing.T) {
	scoreCount := 0
	for _, descriptor := range ModuleDescriptors() {
		info, ok := moduleExposure[descriptor.ID]
		if !ok {
			t.Fatalf("descriptor %q has no derived exposure entry", descriptor.ID)
		}
		if info.Level != descriptor.Exposure || info.NeedsEgressIP != descriptor.NeedsEgressIP {
			t.Fatalf("exposure mismatch for %q: %+v vs %+v", descriptor.ID, info, descriptor)
		}
		if descriptor.ScoreEnabled {
			scoreCount++
			if descriptor.ScoreKey == "" {
				t.Fatalf("score-enabled descriptor %q has no score key", descriptor.ID)
			}
		}
	}
	if scoreCount != 4 {
		t.Fatalf("score-enabled descriptor count = %d, want 4", scoreCount)
	}
}

func TestModuleDescriptorEstimateModes(t *testing.T) {
	wantModes := map[string]EstimateMode{
		"system":    EstimateModeFixed,
		"network":   EstimateModeFixed,
		"bgp":       EstimateModeFixed,
		"cpu":       EstimateModeCPU,
		"memory":    EstimateModeMemory,
		"disk":      EstimateModeDisk,
		"dns":       EstimateModeDNS,
		"latency":   EstimateModeLatency,
		"speed":     EstimateModeSpeed,
		"ports":     EstimateModeFixed,
		"nat":       EstimateModeFixed,
		"blacklist": EstimateModeFixed,
		"apps":      EstimateModeFixed,
		"cnspeed":   EstimateModeFixed,
		"ookla":     EstimateModeFixed,
		"media":     EstimateModeFixed,
		"route":     EstimateModeRoute,
		"backtrace": EstimateModeFixed,
	}
	runtime := Runtime{
		CPUTime:         time.Second,
		DiskMiB:         50,
		DNSAttempts:     2,
		LatencyAttempts: 2,
		IPerfDuration:   time.Second,
		IPerfTargets:    []IPerfEndpoint{{Name: "test", Host: "127.0.0.1"}},
		RouteTargets:    []Endpoint{{Name: "test", Address: "127.0.0.1"}},
	}
	wantDurations := map[EstimateMode]time.Duration{
		EstimateModeCPU:     3 * time.Second,
		EstimateModeMemory:  5 * time.Second,
		EstimateModeDisk:    8 * time.Second,
		EstimateModeDNS:     2 * time.Second,
		EstimateModeLatency: 3 * time.Second,
		EstimateModeSpeed:   2 * time.Second,
		EstimateModeRoute:   12 * time.Second,
	}
	seenModes := make(map[EstimateMode]bool)
	for _, descriptor := range ModuleDescriptors() {
		want, ok := wantModes[descriptor.ID]
		if !ok {
			t.Fatalf("descriptor %q is missing from estimate mode expectations", descriptor.ID)
		}
		if descriptor.EstimateMode != want {
			t.Fatalf("descriptor %q estimate mode = %q, want %q", descriptor.ID, descriptor.EstimateMode, want)
		}
		seenModes[descriptor.EstimateMode] = true
		got := estimateModuleDuration(runtime, descriptor)
		if expected, dynamic := wantDurations[descriptor.EstimateMode]; dynamic {
			if got != expected {
				t.Fatalf("descriptor %q estimate = %s, want %s", descriptor.ID, got, expected)
			}
		} else if got != descriptor.Estimate {
			t.Fatalf("descriptor %q fixed estimate = %s, want descriptor estimate %s", descriptor.ID, got, descriptor.Estimate)
		}
	}
	for _, mode := range []EstimateMode{
		EstimateModeFixed, EstimateModeCPU, EstimateModeMemory, EstimateModeDisk,
		EstimateModeDNS, EstimateModeLatency, EstimateModeSpeed, EstimateModeRoute,
	} {
		if !seenModes[mode] {
			t.Fatalf("estimate mode %q is not used by any descriptor", mode)
		}
	}
}

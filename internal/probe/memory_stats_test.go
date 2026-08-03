package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestMemoryUsageFromMemInfoPreservesUsageAndCgroupCap(t *testing.T) {
	memory := memoryUsageFromMemInfo(map[string]uint64{
		"MemTotal":     1024,
		"MemAvailable": 256,
	}, 512*1024)
	if memory.HostTotalBytes != 1024*1024 || memory.HostAvailableBytes != 256*1024 {
		t.Fatalf("host memory = %+v", memory)
	}
	if memory.HostUsedBytes != 768*1024 || memory.HostUsagePercent != 75 {
		t.Fatalf("host used/usage = %d/%f", memory.HostUsedBytes, memory.HostUsagePercent)
	}
	if !memory.LimitApplied || memory.EffectiveTotalBytes != 512*1024 ||
		memory.EffectiveAvailableBytes != 256*1024 || memory.EffectiveUsedBytes != 256*1024 ||
		memory.EffectiveUsagePercent != 50 {
		t.Fatalf("effective memory = %+v", memory)
	}
}

func TestMemoryUsageFallsBackWhenMemAvailableIsAbsent(t *testing.T) {
	memory := memoryUsageFromMemInfo(map[string]uint64{
		"MemTotal": 512,
		"MemFree":  128,
		"Buffers":  32,
		"Cached":   64,
	}, 0)
	if memory.AvailableKnown {
		t.Fatal("fallback availability must not be labelled as MemAvailable evidence")
	}
	if memory.HostAvailableBytes != (128+32+64)*1024 || memory.HostUsedBytes != (512-224)*1024 {
		t.Fatalf("fallback memory = %+v", memory)
	}
}

func TestOptionalMemoryFacilitiesAreUnavailableWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	balloon := detectBalloonReclaim(root, filepath.Join(root, "missing-vmstat"))
	if balloon.Available || balloon.Status() != "unavailable" || balloon.Evidence == "" {
		t.Fatalf("missing balloon facility should be explicit and unavailable: %+v", balloon)
	}
	ksm := detectKSM(root)
	if ksm.Available || ksm.Status() != "unavailable" || ksm.Evidence == "" {
		t.Fatalf("missing KSM facility should be explicit and unavailable: %+v", ksm)
	}

	result := modelResultForMemoryInventory()
	appendMemoryInventory(&result, memoryUsageSnapshot{EffectiveTotalBytes: 1024, EffectiveUsedBytes: 256, EffectiveAvailableBytes: 768, EffectiveUsagePercent: 25, AvailableKnown: true}, balloon, ksm)
	for _, key := range []string{"balloon_reclaim", "balloon_reclaim_available", "ksm_merging", "ksm_merging_available"} {
		if value := memoryInventoryField(result, key); value == "" {
			t.Fatalf("missing explicit facility field %q: %+v", key, result.Fields)
		}
	}
	if got := memoryInventoryField(result, "balloon_reclaim_available"); got != "false" {
		t.Fatalf("balloon availability = %q, want false", got)
	}
	if got := memoryInventoryField(result, "ksm_merging_available"); got != "false" {
		t.Fatalf("KSM availability = %q, want false", got)
	}
}

func TestOptionalMemoryFacilitiesUseOnlyLinuxEvidence(t *testing.T) {
	root := t.TempDir()
	balloonDir := filepath.Join(root, "class", "virtio-balloon", "balloon0")
	if err := os.MkdirAll(balloonDir, 0o700); err != nil {
		t.Fatal(err)
	}
	reclaimPath := filepath.Join(balloonDir, "reclaim")
	if err := os.WriteFile(reclaimPath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	balloon := detectBalloonReclaim(root, filepath.Join(root, "missing-vmstat"))
	if !balloon.Available || !strings.Contains(balloon.Evidence, "reclaim=1") {
		t.Fatalf("sysfs reclaim evidence not reported: %+v", balloon)
	}
	activityOnly := filepath.Join(root, "activity-only-vmstat")
	if err := os.WriteFile(activityOnly, []byte("balloon_inflate 9\nballoon_deflate 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if detected := detectBalloonReclaim(t.TempDir(), activityOnly); detected.Available {
		t.Fatalf("inflate/deflate activity alone must not claim reclaim availability: %+v", detected)
	}

	vmstat := filepath.Join(root, "vmstat")
	if err := os.WriteFile(vmstat, []byte("balloon_inflate 9\nballoon_reclaim 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if detected := detectBalloonReclaim(t.TempDir(), vmstat); !detected.Available || !strings.Contains(detected.Evidence, "balloon_reclaim=7") {
		t.Fatalf("proc reclaim evidence not reported: %+v", detected)
	}

	ksmDir := filepath.Join(root, "kernel", "mm", "ksm")
	if err := os.MkdirAll(ksmDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"run": "0\n", "pages_sharing": "42\n"} {
		if err := os.WriteFile(filepath.Join(ksmDir, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ksm := detectKSM(root)
	if !ksm.Available || !strings.Contains(ksm.Evidence, "run=0") || !strings.Contains(ksm.Evidence, "pages_sharing=42") {
		t.Fatalf("KSM evidence not reported: %+v", ksm)
	}
}

func TestSysbenchMemoryLatencyParsingAndFormula(t *testing.T) {
	output := `
Transferred (256.00 MiB/sec)
total time:                          2.0000s
total number of events:              2000
Latency (ms):
         min:                                    0.10
         avg:                                    1.25
         max:                                    5.00
         95th percentile:                        2.50
`
	rateMatch := sysbenchMemoryRatePattern.FindStringSubmatch(output)
	if len(rateMatch) != 3 || memoryRateToMiB(256, rateMatch[2]) != 256 {
		t.Fatalf("rate parse = %v", rateMatch)
	}
	avg, p95 := parseSysbenchMemoryLatency(output)
	if avg != 1.25 || p95 != 2.5 {
		t.Fatalf("native latency = %f/%f", avg, p95)
	}
	seconds, events, ok := parseSysbenchMemoryTiming(output)
	if !ok || seconds != 2 || events != 2000 {
		t.Fatalf("timing parse = %f/%d/%v", seconds, events, ok)
	}
	derived := seconds * 1000 / float64(events)
	if derived != 1 {
		t.Fatalf("derived latency = %f, want 1 ms per event", derived)
	}

	derivedOutput := "Transferred (128.00 MiB/sec)\ntotal time: 1.500s\ntotal number of events: 3000\n"
	if avg, _ := parseSysbenchMemoryLatency(derivedOutput); avg != 0 {
		t.Fatalf("missing native latency should remain missing, got %f", avg)
	}
	seconds, events, ok = parseSysbenchMemoryTiming(derivedOutput)
	if !ok || seconds*1000/float64(events) != 0.5 {
		t.Fatalf("derived-only timing = %f/%d/%v", seconds, events, ok)
	}
}

// This tiny constructor avoids coupling the facility test to the full probe
// runner while keeping the field assertions on the public report shape.
func modelResultForMemoryInventory() model.Result {
	return model.Result{ID: "memory", Status: model.StatusOK}
}

func memoryInventoryField(result model.Result, key string) string {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value
		}
	}
	return ""
}

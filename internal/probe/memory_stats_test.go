package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestMemoryInventoryAndFacilities(t *testing.T) {
	directory := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	meminfo := map[string]uint64{"MemTotal": 1024, "MemAvailable": 256}
	memory := memoryUsageFromMemInfo(meminfo, 512*1024)
	if !memory.AvailableKnown || !memory.LimitApplied || memory.HostUsedBytes != 768*1024 || memory.EffectiveUsedBytes != 256*1024 || memory.EffectiveUsagePercent != 50 {
		t.Fatalf("effective memory = %+v", memory)
	}
	fallback := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 100, "MemFree": 20, "Buffers": 30, "Cached": 60}, 0)
	if fallback.AvailableKnown || fallback.HostAvailableBytes != 100*1024 || fallback.HostUsedBytes != 0 {
		t.Fatalf("memory available fallback = %+v", fallback)
	}
	clamped := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 10, "MemAvailable": 20}, 0)
	if clamped.HostAvailableBytes != clamped.HostTotalBytes || clamped.HostUsedBytes != 0 {
		t.Fatalf("memory availability clamp = %+v", clamped)
	}

	balloonRoot := filepath.Join(directory, "sys", "class", "virtio-balloon", "balloon0")
	write(filepath.Join(balloonRoot, "reclaim"), "enabled\n")
	balloon := detectBalloonReclaim(filepath.Join(directory, "sys"), filepath.Join(directory, "vmstat"))
	if !balloon.Available || !strings.Contains(balloon.Evidence, "reclaim=enabled") {
		t.Fatalf("balloon facility = %+v", balloon)
	}
	write(filepath.Join(directory, "vmstat"), "balloon_reclaim 7\nballoon_inflate 2\ninvalid nope\n")
	balloon = detectBalloonReclaim(filepath.Join(directory, "absent-sys"), filepath.Join(directory, "vmstat"))
	if !balloon.Available || !strings.Contains(balloon.Evidence, "balloon_reclaim=7") {
		t.Fatalf("vmstat balloon facility = %+v", balloon)
	}
	write(filepath.Join(directory, "invalid-vmstat"), "balloon_reclaim nope\nballoon_inflate bad\n")
	if detectBalloonReclaim(filepath.Join(directory, "absent-sys"), filepath.Join(directory, "invalid-vmstat")).Available {
		t.Fatal("invalid balloon evidence reported as available")
	}
	ksmRoot := filepath.Join(directory, "ksm", "kernel", "mm", "ksm")
	write(filepath.Join(ksmRoot, "run"), "0\n")
	write(filepath.Join(ksmRoot, "pages_sharing"), "12\n")
	ksm := detectKSM(filepath.Join(directory, "ksm"))
	if !ksm.Available || !strings.Contains(ksm.Evidence, "pages_sharing=12") {
		t.Fatalf("KSM facility = %+v", ksm)
	}
	partialKSM := filepath.Join(directory, "partial-ksm")
	write(filepath.Join(partialKSM, "kernel", "mm", "ksm", "run"), "1\n")
	if detectKSM(partialKSM).Available {
		t.Fatal("incomplete KSM evidence reported as available")
	}
	if detectKSM(filepath.Join(directory, "absent-ksm")).Available {
		t.Fatal("missing KSM evidence reported as available")
	}

	result := model.NewResult("memory", "memory")
	appendMemoryInventory(&result, memory, balloon, ksm)
	labels := make(map[string]string)
	for _, field := range result.Fields {
		labels[field.Key] = field.Label
	}
	for key, wantLabel := range map[string]string{
		"memory_total":     "probe.memory.field.total",
		"memory_available": "probe.memory.field.available",
		"balloon_reclaim":  "probe.memory.field.balloon_reclaim",
		"ksm_merging":      "probe.memory.field.ksm_merging",
	} {
		if got := labels[key]; got != wantLabel {
			t.Fatalf("memory inventory field %q label = %q, want %q; fields=%+v", key, got, wantLabel, result.Fields)
		}
	}

	limitedFallback := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 1024, "MemFree": 100, "Buffers": 100, "Cached": 100}, 512*1024)
	limitedFallback.EffectiveCurrentKnown = true
	degraded := model.NewResult("memory", "memory")
	appendMemoryInventory(&degraded, limitedFallback, memoryFacility{Evidence: "none"}, memoryFacility{Evidence: "none"})
	values := make(map[string]string)
	for _, field := range degraded.Fields {
		values[field.Key] = field.Value
	}
	if values["balloon_reclaim"] != "unavailable" || values["ksm_merging"] != "unavailable" {
		t.Fatalf("degraded memory inventory values = %v", values)
	}
	for _, want := range []string{
		"probe.memory.note.cgroup_limit",
		"probe.memory.note.cgroup_current",
		"probe.memory.note.memavailable_legacy_fallback",
		"probe.memory.note.balloon_reclaim_unavailable",
		"probe.memory.note.ksm_unavailable",
	} {
		if !containsString(degraded.Notes, want) {
			t.Fatalf("degraded memory inventory missing note %q: %v", want, degraded.Notes)
		}
	}
}

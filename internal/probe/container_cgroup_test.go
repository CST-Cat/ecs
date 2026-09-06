package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func cgroupFixture(t *testing.T, self, mountinfo string, files map[string]string) func() {
	t.Helper()
	root := t.TempDir()
	selfPath := filepath.Join(root, "proc", "self", "cgroup")
	mountPath := filepath.Join(root, "proc", "self", "mountinfo")
	if err := os.MkdirAll(filepath.Dir(selfPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfPath, []byte(self), 0o644); err != nil {
		t.Fatal(err)
	}
	mountinfo = strings.ReplaceAll(mountinfo, " /sandbox/cgroup", " "+filepath.Join(root, "sandbox/cgroup"))
	mountinfo = strings.ReplaceAll(mountinfo, " /cg/", " "+filepath.Join(root, "cg")+"/")
	mountinfo = strings.ReplaceAll(mountinfo, " /cg\\", " "+filepath.Join(root, "cg")+"\\")
	if err := os.WriteFile(mountPath, []byte(mountinfo), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldSelf, oldMount := cgroupSelfPath, cgroupMountInfoPath
	cgroupSelfPath, cgroupMountInfoPath = selfPath, mountPath
	return func() { cgroupSelfPath, cgroupMountInfoPath = oldSelf, oldMount }
}

func TestCgroupV2VisibleAncestorsAndLeafUsage(t *testing.T) {
	self := "0::/parent/child\n"
	mount := "29 24 0:26 / /sandbox/cgroup rw,relatime - cgroup2 cgroup rw\n"
	restore := cgroupFixture(t, self, mount, map[string]string{
		"sandbox/cgroup/cpu.max":                     "max 100000\n",
		"sandbox/cgroup/parent/cpu.max":              "200000 100000\n",
		"sandbox/cgroup/parent/child/cpu.max":        "max 100000\n",
		"sandbox/cgroup/parent/cpu.stat":             "usage_usec 888\n nr_periods 8\n",
		"sandbox/cgroup/parent/child/cpu.stat":       "usage_usec 123\n nr_periods 1\n",
		"sandbox/cgroup/memory.max":                  "max\n",
		"sandbox/cgroup/parent/memory.max":           "8192\n",
		"sandbox/cgroup/parent/child/memory.max":     "max\n",
		"sandbox/cgroup/memory.current":              "999\n",
		"sandbox/cgroup/parent/memory.current":       "888\n",
		"sandbox/cgroup/parent/child/memory.current": "123\n",
	})
	defer restore()
	quota, source, ok := cgroupCPUQuota()
	if !ok || quota != 2 || !filepath.IsAbs(source) || filepath.Base(source) != "cpu.max" {
		t.Fatalf("quota=%v source=%q ok=%v", quota, source, ok)
	}
	limit, source, ok := cgroupMemoryLimit()
	if !ok || limit != 8192 || filepath.Base(source) != "memory.max" || !hasPathFragment(source, "parent") {
		t.Fatalf("limit=%d source=%q ok=%v", limit, source, ok)
	}
	current, source, ok := cgroupMemoryCurrent()
	if !ok || current != 123 || !hasPathFragment(source, "parent/child") {
		t.Fatalf("current=%d source=%q ok=%v", current, source, ok)
	}
	stats := readCgroupCPUStats()
	if !stats.Present || stats.UsageUS != 123 || !hasPathFragment(stats.Source, "parent/child") {
		t.Fatalf("leaf cpu stats=%+v", stats)
	}
}

func hasPathFragment(path, fragment string) bool {
	return strings.Contains(filepath.ToSlash(path), fragment)
}

func TestCgroupNamespaceMountRootBoundsAncestors(t *testing.T) {
	restore := cgroupFixture(t, "0::/host/ns/parent/child\n", "29 24 0:26 /host/ns /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/cpu.max":       "100000 100000\n",
		"sandbox/cgroup/parent/child/cpu.max": "max 100000\n",
		"sandbox/cgroup/cpu.max":              "max 100000\n",
	})
	defer restore()
	quota, _, ok := cgroupCPUQuota()
	if !ok || quota != 1 {
		t.Fatalf("namespace quota=%v ok=%v", quota, ok)
	}
}

func TestCgroupV1ControllerMountAndHierarchy(t *testing.T) {
	self := "7:cpu,cpuacct:/job/child\n8:memory:/job/child\n"
	mount := "30 24 0:30 / /cg/cpu rw - cgroup cgroup rw,cpu,cpuacct\n31 24 0:31 / /cg/memory rw - cgroup cgroup rw,memory\n"
	restore := cgroupFixture(t, self, mount, map[string]string{
		"cg/cpu/cpu.cfs_quota_us": "-1\n", "cg/cpu/cpu.cfs_period_us": "100000\n",
		"cg/cpu/job/cpu.cfs_quota_us": "300000\n", "cg/cpu/job/cpu.cfs_period_us": "100000\n",
		"cg/cpu/job/child/cpu.cfs_quota_us": "-1\n", "cg/cpu/job/child/cpu.cfs_period_us": "100000\n",
		"cg/memory/memory.limit_in_bytes":     "4611686018427387904\n",
		"cg/memory/job/memory.limit_in_bytes": "8192\n", "cg/memory/job/memory.use_hierarchy": "0\n",
		"cg/memory/job/child/memory.limit_in_bytes": "16384\n", "cg/memory/job/child/memory.use_hierarchy": "0\n",
	})
	defer restore()
	quota, _, ok := cgroupCPUQuota()
	if !ok || quota != 3 {
		t.Fatalf("v1 quota=%v ok=%v", quota, ok)
	}
	limit, _, ok := cgroupMemoryLimit()
	if !ok || limit != 16384 {
		t.Fatalf("v1 nonhier limit=%d ok=%v", limit, ok)
	}
	// Enabling hierarchy exposes the parent's stricter limit.
	// Locate the fixture root represented by the mount point.
	mountRoot := filepath.Dir(filepath.Dir(filepath.Dir(cgroupMountInfoPath)))
	if err := os.WriteFile(filepath.Join(mountRoot, "cg/memory/job/memory.use_hierarchy"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	limit, _, ok = cgroupMemoryLimit()
	if !ok || limit != 8192 {
		t.Fatalf("v1 hier limit=%d ok=%v", limit, ok)
	}
}

func TestMountInfoEscapesDoNotDecodeProcMembership(t *testing.T) {
	mount := "40 24 0:40 / /cg\\040root rw - cgroup2 cgroup rw\n"
	parsed := readCgroupMounts(writeCgroupText(t, mount))
	if len(parsed) != 1 || parsed[0].mountPoint != "/cg root" {
		t.Fatalf("mount=%+v", parsed)
	}
	if got := mapCgroupMember(parsed[0], "/literal\\040name"); got != "/cg root/literal\\040name" {
		t.Fatalf("mapped literal path=%q", got)
	}
}

func writeCgroupText(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCgroupMappingRejectsUnrelatedAndDotTraversal(t *testing.T) {
	mount := cgroupMount{root: "/unrelated", mountPoint: "/fixture"}
	if paths := visibleCgroupPaths(mount, "/parent/child"); len(paths) != 0 {
		t.Fatalf("unrelated mount accepted: %v", paths)
	}
	mount.root = "/.."
	if paths := visibleCgroupPaths(mount, "/"); len(paths) != 0 {
		t.Fatalf("dot root accepted: %v", paths)
	}
	mount.root = "/"
	if paths := visibleCgroupPaths(mount, "/../child"); len(paths) != 0 {
		t.Fatalf("dot member accepted: %v", paths)
	}
	if paths := visibleCgroupPaths(mount, "../child"); len(paths) != 0 {
		t.Fatalf("relative member accepted: %v", paths)
	}
}

func TestCgroupEscapedProcAndMountFixtureResolvesLiteralName(t *testing.T) {
	root := t.TempDir()
	self := filepath.Join(root, "self-cgroup")
	mountInfo := filepath.Join(root, "mountinfo")
	if err := os.WriteFile(self, []byte("0::/literal\\040name/child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mountPoint := filepath.Join(root, "cg root")
	escapedMountPoint := strings.ReplaceAll(filepath.ToSlash(mountPoint), " ", "\\040")
	mountText := "40 24 0:40 / " + escapedMountPoint + " rw - cgroup2 cgroup rw\n"
	if err := os.WriteFile(mountInfo, []byte(mountText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mountPoint, `literal\040name`, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountPoint, `literal\040name`, "child", "memory.max"), []byte("4096\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldSelf, oldMount := cgroupSelfPath, cgroupMountInfoPath
	cgroupSelfPath, cgroupMountInfoPath = self, mountInfo
	defer func() { cgroupSelfPath, cgroupMountInfoPath = oldSelf, oldMount }()
	limit, source, ok := cgroupMemoryLimit()
	if !ok || limit != 4096 || !strings.Contains(source, `literal\040name`) {
		t.Fatalf("escaped fixture limit=%d source=%q ok=%v", limit, source, ok)
	}
}

func TestCgroupNamespaceRootAndMemberBothSlash(t *testing.T) {
	restore := cgroupFixture(t, "0::/\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/cpu.max": "100000 100000\n",
	})
	defer restore()
	quota, _, ok := cgroupCPUQuota()
	if !ok || quota != 1 {
		t.Fatalf("root namespace quota=%v ok=%v", quota, ok)
	}
}

func TestCgroupMemoryHeadroomUsesEveryVisibleLimit(t *testing.T) {
	restore := cgroupFixture(t, "0::/parent/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/memory.max":           "4096\n",
		"sandbox/cgroup/parent/memory.current":       "3000\n",
		"sandbox/cgroup/parent/child/memory.max":     "8192\n",
		"sandbox/cgroup/parent/child/memory.current": "1000\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 16384, "MemAvailable": 7000}, 4096)
	limit, limitSource, ok := cgroupMemoryLimit()
	if !ok || limit != 4096 {
		t.Fatalf("limit=%d source=%q ok=%v", limit, limitSource, ok)
	}
	current, currentSource, ok := cgroupMemoryCurrent()
	if !ok {
		t.Fatal("leaf current unavailable")
	}
	memory = applyCgroupMemoryUsage(memory, currentSource, current, true, cgroupMemoryLimitCandidates())
	if !memory.EffectiveAvailableKnown || memory.EffectiveAvailableBytes != 1096 || memory.EffectiveUsedBytes != 1000 {
		t.Fatalf("headroom=%+v", memory)
	}
}

func TestCgroupMemoryMissingAncestorUsageIsUnknown(t *testing.T) {
	restore := cgroupFixture(t, "0::/parent/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/memory.max":           "4096\n",
		"sandbox/cgroup/parent/child/memory.max":     "8192\n",
		"sandbox/cgroup/parent/child/memory.current": "1000\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 16384, "MemAvailable": 0}, 4096)
	current, currentSource, _ := cgroupMemoryCurrent()
	memory = applyCgroupMemoryUsage(memory, currentSource, current, true, cgroupMemoryLimitCandidates())
	if memory.EffectiveAvailableKnown || memory.EffectiveAvailableBytes != 0 || memory.EffectiveUsedBytes != 1000 {
		t.Fatalf("missing ancestor usage was reported as known: %+v", memory)
	}
	result := model.NewResult("memory", "memory")
	appendMemoryInventory(&result, memory, memoryFacility{}, memoryFacility{})
	for _, field := range result.Fields {
		if field.Key == "memory_available" && field.Value.Text() != "unavailable" {
			t.Fatalf("unknown headroom rendered as %q", field.Value.Text())
		}
	}
}

func TestCgroupV1HierarchyStopsAtDisabledAncestor(t *testing.T) {
	restore := cgroupFixture(t, "8:memory:/grand/parent/child\n", "31 24 0:31 / /cg/memory rw - cgroup cgroup rw,memory\n", map[string]string{
		"cg/memory/grand/memory.limit_in_bytes":              "2048\n",
		"cg/memory/grand/memory.use_hierarchy":               "0\n",
		"cg/memory/grand/parent/memory.limit_in_bytes":       "4096\n",
		"cg/memory/grand/parent/memory.use_hierarchy":        "1\n",
		"cg/memory/grand/parent/child/memory.limit_in_bytes": "8192\n",
		"cg/memory/grand/parent/child/memory.use_hierarchy":  "1\n",
	})
	defer restore()
	limit, _, ok := cgroupMemoryLimit()
	if !ok || limit != 4096 {
		t.Fatalf("per-level hierarchy limit=%d ok=%v", limit, ok)
	}
}

func TestCgroupCPUChildLimitBeatsParent(t *testing.T) {
	restore := cgroupFixture(t, "0::/parent/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/cpu.max":       "200000 100000\n",
		"sandbox/cgroup/parent/child/cpu.max": "100000 100000\n",
	})
	defer restore()
	quota, _, ok := cgroupCPUQuota()
	if !ok || quota != 1 {
		t.Fatalf("child cpu limit=%v ok=%v", quota, ok)
	}
}

func TestCgroupZeroSwapLimitIsFinite(t *testing.T) {
	restore := cgroupFixture(t, "0::/child\n", "31 24 0:31 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/memory.swap.max":       "max\n",
		"sandbox/cgroup/child/memory.swap.max": "0\n",
	})
	defer restore()
	limit, source, unlimited, ok := readCgroupLimit("memory.swap.max", "memory.memsw.limit_in_bytes")
	if !ok || unlimited || limit != 0 || filepath.Base(source) != "memory.swap.max" {
		t.Fatalf("zero swap=(%d %s %v %v)", limit, source, unlimited, ok)
	}
}

func TestCgroupHostZeroAvailableStaysZeroWithLeafUsage(t *testing.T) {
	restore := cgroupFixture(t, "0::/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/child/memory.max":     "8192\n",
		"sandbox/cgroup/child/memory.current": "123\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 1024, "MemAvailable": 0, "MemFree": 512}, 8192)
	current, currentSource, ok := cgroupMemoryCurrent()
	if !ok {
		t.Fatal("leaf current unavailable")
	}
	memory = applyCgroupMemoryUsage(memory, currentSource, current, true, cgroupMemoryLimitCandidates())
	if !memory.EffectiveAvailableKnown || memory.EffectiveAvailableBytes != 0 {
		t.Fatalf("host zero availability became %+v", memory)
	}
}

func TestCgroupMissingLeafUsageInventoryIsUnavailable(t *testing.T) {
	restore := cgroupFixture(t, "0::/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/child/memory.max": "8192\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 1024, "MemAvailable": 512}, 8192)
	memory = applyCgroupMemoryUsage(memory, "", 0, false, cgroupMemoryLimitCandidates())
	if memory.EffectiveCurrentKnown || memory.EffectiveAvailableKnown {
		t.Fatalf("missing leaf current known: %+v", memory)
	}
	result := model.NewResult("memory", "memory")
	appendMemoryInventory(&result, memory, memoryFacility{}, memoryFacility{})
	fields := map[string]string{}
	for _, field := range result.Fields {
		fields[field.Key] = field.Value.Text()
	}
	if fields["memory_used"] != "unavailable" || fields["memory_usage_percent"] != "unavailable" || fields["memory_available"] != "unavailable" {
		t.Fatalf("missing leaf inventory=%v", fields)
	}
}

func TestCgroupReadableParentHeadroomSurvivesMissingLeafUsage(t *testing.T) {
	restore := cgroupFixture(t, "0::/parent/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/memory.max":       "4096\n",
		"sandbox/cgroup/parent/memory.current":   "3000\n",
		"sandbox/cgroup/parent/child/memory.max": "max\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 16384, "MemAvailable": 7000}, 4096)
	memory = applyCgroupMemoryUsage(memory, "", 0, false, cgroupMemoryLimitCandidates())
	if memory.EffectiveCurrentKnown || memory.EffectiveUsedBytes != 0 || !memory.EffectiveAvailableKnown || memory.EffectiveAvailableBytes != 1096 {
		t.Fatalf("parent headroom with missing leaf usage=%+v", memory)
	}
	result := model.NewResult("memory", "memory")
	appendMemoryInventory(&result, memory, memoryFacility{}, memoryFacility{})
	fields := map[string]string{}
	for _, field := range result.Fields {
		fields[field.Key] = field.Value.Text()
	}
	if fields["memory_used"] != "unavailable" || fields["memory_usage_percent"] != "unavailable" || fields["memory_available"] == "unavailable" {
		t.Fatalf("parent headroom inventory=%v", fields)
	}
}

func TestCgroupNonTightestLimitStillConstrainsHeadroom(t *testing.T) {
	restore := cgroupFixture(t, "0::/parent/child\n", "29 24 0:26 / /sandbox/cgroup rw - cgroup2 cgroup rw\n", map[string]string{
		"sandbox/cgroup/parent/memory.max":           "8192\n",
		"sandbox/cgroup/parent/memory.current":       "8100\n",
		"sandbox/cgroup/parent/child/memory.max":     "4096\n",
		"sandbox/cgroup/parent/child/memory.current": "1000\n",
	})
	defer restore()
	memory := memoryUsageFromMemInfo(map[string]uint64{"MemTotal": 16384, "MemAvailable": 7000}, 4096)
	current, currentSource, ok := cgroupMemoryCurrent()
	if !ok {
		t.Fatal("leaf current unavailable")
	}
	memory = applyCgroupMemoryUsage(memory, currentSource, current, true, cgroupMemoryLimitCandidates())
	if !memory.EffectiveAvailableKnown || memory.EffectiveAvailableBytes != 92 || memory.EffectiveUsedBytes != 1000 {
		t.Fatalf("non-tightest headroom=%+v", memory)
	}
}

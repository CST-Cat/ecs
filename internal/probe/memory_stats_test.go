package probe

import (
	"os"
	"testing"
)

func TestMemInfoFixtureProducesEffectiveUsage(t *testing.T) {
	path := t.TempDir() + "/meminfo"
	if err := os.WriteFile(path, []byte("MemTotal:       1024 kB\nMemAvailable:    256 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	memory := memoryUsageFromMemInfo(parseMemInfo(path), 512*1024)
	if memory.HostTotalBytes != 1024*1024 || memory.HostAvailableBytes != 256*1024 ||
		memory.HostUsedBytes != 768*1024 || memory.HostUsagePercent != 75 {
		t.Fatalf("host memory = %+v", memory)
	}
	if !memory.LimitApplied || memory.EffectiveTotalBytes != 512*1024 ||
		memory.EffectiveAvailableBytes != 256*1024 || memory.EffectiveUsedBytes != 256*1024 ||
		memory.EffectiveUsagePercent != 50 {
		t.Fatalf("effective memory = %+v", memory)
	}
}

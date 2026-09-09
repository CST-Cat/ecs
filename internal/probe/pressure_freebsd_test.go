//go:build freebsd

package probe

import (
	"strings"
	"testing"
	"time"
)

func TestFreeBSDPressureMeasurementsKeepOnlyNativeLoadFact(t *testing.T) {
	before := EnvironmentSnapshot{
		CapturedAt: time.Unix(100, 0),
		Load1:      1.25,
		LoadKnown:  true,
		CPUTracked: true,
		CPUStat:    cgroupCPUStats{Present: true},
		Memory:     cgroupMemoryEvents{Present: true},
		PSI: map[string]psiResource{
			"cpu": {Some: psiValues{Present: true}},
		},
	}
	after := before
	after.CapturedAt = before.CapturedAt.Add(time.Second)

	measurements := BuildPressureMeasurements(before, after)
	if len(measurements) != 1 {
		t.Fatalf("FreeBSD pressure measurements = %+v, want only native load average", measurements)
	}
	measurement := measurements[0]
	if measurement.Key != "pretest_load_1m" || measurement.Value != 1.25 || measurement.Method != "freebsd-sysctl-vm-loadavg-v1" {
		t.Fatalf("FreeBSD load measurement = %+v", measurement)
	}
	method := strings.ToLower(measurement.Method)
	for _, forbidden := range []string{"proc-", "cgroup", "linux-psi"} {
		if strings.Contains(method, forbidden) {
			t.Fatalf("FreeBSD load measurement used Linux provenance: %+v", measurement)
		}
	}
}

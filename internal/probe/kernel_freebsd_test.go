//go:build freebsd

package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestParseFreeBSDCCAvailableTable(t *testing.T) {
	// Captured from FreeBSD 15.1: sysctl -n net.inet.tcp.cc.available.
	const actual = "CCmod           D PCB count\ncubic           * 3\n"
	if got, ok := parseFreeBSDCCAvailable(actual); !ok || got != "cubic" {
		t.Fatalf("FreeBSD CC table %q = %q/%v, want cubic/true", actual, got, ok)
	}
	const multiple = "CCmod D PCB count\n\n cubic * 3\nnewreno 2\n cubic * 3\n"
	if got, ok := parseFreeBSDCCAvailable(multiple); !ok || got != "cubic newreno" {
		t.Fatalf("multiple FreeBSD CC rows = %q/%v, want cubic newreno/true", got, ok)
	}
	for _, input := range []string{
		"cubic * 3\n",
		"CCmod D PCB count\n",
		"CCmod D PCB count\ncubic * nope\n",
		"CCmod D PCB count\ncubic ! 3\n",
		"CCmod D PCB count\ncubic * 3 extra\n",
		"CCmod D PCB count\nnot/a/module * 3\n",
	} {
		if got, ok := parseFreeBSDCCAvailable(input); ok {
			t.Fatalf("malformed FreeBSD CC table %q parsed as %q", input, got)
		}
	}
}

func TestFreeBSDKernelOmitsLinuxReceiveBufferMaxAndBDP(t *testing.T) {
	for _, param := range platformKernelParams() {
		if param.Key == "rmem_max" || param.Key == "tcp_rmem" {
			t.Fatalf("FreeBSD retained a receive-buffer max key: %+v", param)
		}
	}
	result := model.NewResult("system", "system")
	appendKernelNetworkFacts(&result, map[string]string{
		"tcp_congestion_control":   "cubic",
		"tcp_available_congestion": "cubic",
		"somaxconn":                "128",
		// A value under the former Linux key must not become a FreeBSD fact.
		"rmem_max": "65536",
	})
	for _, measurement := range result.Measurements {
		if strings.Contains(measurement.Key, "rmem") || strings.Contains(measurement.Key, "single_flow_window") {
			t.Fatalf("receive-buffer max/BDP measurement emitted: %+v", measurement)
		}
	}
	for _, field := range result.Fields {
		if field.Key == "tcp_available_congestion" && field.Value.Text() != "cubic" {
			t.Fatalf("unexpected CC field: %+v", field)
		}
	}
}

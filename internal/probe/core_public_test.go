package probe

import (
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestProbeFailureWrappersAndHTTPClient(t *testing.T) {
	var result model.Result
	addFailure(nil, "ignored", "ignored", errors.New("ignored"))
	addFailure(&result, "stage", "target", errors.New("fixture failure"), 2)
	addFailure(&result, "stage", "target", errors.New("fixture failure"), 3)
	addFailureMessage(&result, "message", "other", "fixture warning", 2)
	addFailureMessage(&result, "ignored", "ignored", "")
	if len(result.Failures) != 2 || result.Failures[0].Stage != "stage" || result.Failures[0].Target != "target" || result.Failures[0].Message != "fixture failure" || result.Failures[0].Count != 5 {
		t.Fatalf("wrapped error failure = %+v", result.Failures)
	}
	if result.Failures[1].Stage != "message" || result.Failures[1].Target != "other" || result.Failures[1].Message != "fixture warning" || result.Failures[1].Count != 2 {
		t.Fatalf("wrapped message failure = %+v", result.Failures)
	}

	client := NewHTTPClient(250 * time.Millisecond)
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || client.Timeout != 250*time.Millisecond || transport.Proxy != nil || transport.MaxIdleConns != 16 || transport.MaxIdleConnsPerHost != 8 || transport.IdleConnTimeout != 30*time.Second || transport.TLSHandshakeTimeout != 250*time.Millisecond {
		t.Fatalf("HTTP client policy = timeout %s/transport %#v", client.Timeout, client.Transport)
	}
	request := &http.Request{}
	if err := client.CheckRedirect(request, make([]*http.Request, 7)); err != nil {
		t.Fatalf("redirect below limit = %v", err)
	}
	if err := client.CheckRedirect(request, make([]*http.Request, 8)); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect limit error = %v", err)
	}
}

func TestSystemAndInventoryParsersUseFixtureFiles(t *testing.T) {
	directory := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	osRelease := parseOSRelease(write("os-release", "NAME=\"Fixture OS\"\nPRETTY_NAME='Fixture Linux'\ninvalid line\n"))
	if osRelease["NAME"] != "Fixture OS" || osRelease["PRETTY_NAME"] != "Fixture Linux" || len(parseOSRelease(filepath.Join(directory, "missing-os"))) != 0 {
		t.Fatalf("os release = %v", osRelease)
	}
	memInfo := parseMemInfo(write("meminfo", "MemTotal:       123 kB\nMemAvailable:    45 kB\nbad: nope\nshort\n"))
	if memInfo["MemTotal"] != 123 || memInfo["MemAvailable"] != 45 || len(parseMemInfo(filepath.Join(directory, "missing-mem"))) != 0 {
		t.Fatalf("meminfo = %v", memInfo)
	}

	diskCases := []struct {
		name      string
		fields    []string
		wantOK    bool
		wantUsage float64
		wantUsed  uint64
		wantFree  uint64
	}{
		{name: "normal", fields: []string{"/dev/sda", "100", "40", "60", "40%", "/mnt"}, wantOK: true, wantUsage: 40, wantUsed: 40 * 1024, wantFree: 60 * 1024},
		{name: "short", fields: []string{"/dev/sda", "100"}},
		{name: "clamped", fields: []string{"/dev/sda", "100", "150", "200", "200%", "/mnt"}, wantOK: true, wantUsage: 100, wantUsed: 100 * 1024},
		{name: "percentage without total", fields: []string{"/dev/sda", "0", "0", "0", "37.5%", "/mnt"}, wantOK: true, wantUsage: 37.5},
	}
	for _, test := range diskCases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseDiskDFFields(test.fields)
			if ok != test.wantOK || (ok && (got.DiskUsage != test.wantUsage || got.DiskUsed != test.wantUsed || got.DiskFree != test.wantFree)) {
				t.Fatalf("df parse = %+v/%v", got, ok)
			}
		})
	}

	hardware := write("hardware", "Fixture Vendor\n")
	if readHardwareValue(hardware) != "Fixture Vendor" || readHardwareValue(write("none", "none\n")) != "unknown" || readHardwareValue(filepath.Join(directory, "missing-hardware")) != "unknown" {
		t.Fatal("hardware value fallback failed")
	}
	trimmed := write("trimmed", "  fixture value \n")
	if readTrimmed(trimmed, "fallback") != "fixture value" || readTrimmed(write("empty", "\n"), "fallback") != "fallback" || readTrimmed(filepath.Join(directory, "missing-trimmed"), "fallback") != "fallback" {
		t.Fatal("trimmed value fallback failed")
	}
	keyValues := parseKeyValueFile(write("key-values", "A=one\n B = two \ninvalid\n"))
	if keyValues["A"] != "one" || keyValues["B"] != "two" || len(parseKeyValueFile(filepath.Join(directory, "missing-kv"))) != 0 {
		t.Fatalf("key-value parse = %v", keyValues)
	}
	target := write("link-target", "target")
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if readLinkBase(link) != "link-target" || readLinkBase(filepath.Join(directory, "missing-link")) != "" {
		t.Fatal("link basename fallback failed")
	}
}

func TestProbeFormattingAndParsingHelpers(t *testing.T) {
	if joinHardwareValues("", "unknown", "GPU") != "GPU" || joinHardwareValues("A", "B") != "A · B" || joinHardwareList(nil) != "unknown" || joinHardwareList([]string{"A", "B"}) != "A · B" {
		t.Fatal("hardware joins do not preserve useful values")
	}
	if formatHardwareBytes(0) != "0 B" || formatHardwareBytes(1536) != "1.5 KiB" {
		t.Fatalf("hardware bytes = %q/%q", formatHardwareBytes(0), formatHardwareBytes(1536))
	}
	for _, test := range []struct {
		value time.Duration
		want  string
	}{{-time.Second, "unknown"}, {90 * time.Minute, "1 小时 30 分"}, {2*24*time.Hour + 3*time.Hour, "2 天 3 小时 0 分"}} {
		if got := formatDuration(test.value); got != test.want {
			t.Fatalf("duration %s = %q, want %q", test.value, got, test.want)
		}
	}
	if parseUintDefault("42", 7) != 42 || parseUintDefault("bad", 7) != 7 || fallback(" value ", "default") != "value" || fallback(" ", "default") != "default" {
		t.Fatal("uint parser success/fallback failed")
	}
	if describeSteal(systemSnapshot{}) != "unavailable（/proc/stat 不可读）" || describeSteal(systemSnapshot{StealKnown: true, StealPercent: 12.34}) != "12.34 %" {
		t.Fatal("steal description formatting failed")
	}
}

func TestContainerResourcePureSemantics(t *testing.T) {
	limited := cpuAllowance{Visible: 4, Quota: 2.5, Threads: 3, Source: "fixture quota"}
	if !limited.Limited() || !strings.Contains(describeCPUAllowance(limited), "fixture quota") {
		t.Fatalf("limited CPU allowance = %+v", limited)
	}
	if (cpuAllowance{Visible: 4, Threads: 4}).Limited() || !strings.Contains(describeCPUAllowance(cpuAllowance{Visible: 4, Threads: 4}), "无 cgroup 配额限制") {
		t.Fatal("unlimited CPU allowance description failed")
	}
	if !reflect.DeepEqual(distinctBenchmarkThreadCounts(0), []int{1}) || !reflect.DeepEqual(distinctBenchmarkThreadCounts(1), []int{1}) || !reflect.DeepEqual(distinctBenchmarkThreadCounts(4), []int{1, 4}) {
		t.Fatal("benchmark thread contexts are incorrect")
	}
	if value, ok := stealPercent(cpuTimeSample{Total: 100, Steal: 10}, cpuTimeSample{Total: 200, Steal: 20}); !ok || value != 10 {
		t.Fatalf("steal percent = %v/%v", value, ok)
	}
	if _, ok := stealPercent(cpuTimeSample{Total: 200, Steal: 10}, cpuTimeSample{Total: 100, Steal: 20}); ok {
		t.Fatal("backward total was accepted")
	}
	if _, ok := stealPercent(cpuTimeSample{Total: 100, Steal: 20}, cpuTimeSample{Total: 200, Steal: 10}); ok {
		t.Fatal("backward steal counter was accepted")
	}
	if _, ok := cumulativeStealPercent(cpuTimeSample{}); ok {
		t.Fatal("zero cumulative sample was accepted")
	}
	if value, ok := cumulativeStealPercent(cpuTimeSample{Total: 200, Steal: 10}); !ok || value != 5 {
		t.Fatalf("cumulative steal = %v/%v", value, ok)
	}
}

func TestProbeStatisticsAggregateWithoutMutatingInputs(t *testing.T) {
	durations := []time.Duration{3, 1, 2}
	if medianDuration(nil) != 0 || medianDuration(durations) != 2 || !reflect.DeepEqual(durations, []time.Duration{3, 1, 2}) || medianDuration([]time.Duration{2, 4}) != 3 {
		t.Fatalf("duration median/input = %s/%v", medianDuration(durations), durations)
	}
	values := []time.Duration{1, 2, 3, 4}
	if percentileDuration(nil, 0.5) != 0 || percentileDuration(values, 0) != 1 || percentileDuration(values, 0.5) != 2 || percentileDuration(values, 2) != 4 || !reflect.DeepEqual(values, []time.Duration{1, 2, 3, 4}) {
		t.Fatalf("percentiles/input = %v/%v", values, values)
	}
	floats := []float64{3, 1, 2}
	if medianFloat(nil) != 0 || medianFloat(floats) != 2 || !reflect.DeepEqual(floats, []float64{3, 1, 2}) || medianFloat([]float64{2, 4}) != 3 {
		t.Fatalf("float median/input = %v/%v", medianFloat(floats), floats)
	}
	if stddevFloat([]float64{1}) != 0 || math.Abs(stddevFloat([]float64{1, 2, 3})-math.Sqrt(2.0/3.0)) > 1e-12 {
		t.Fatal("standard deviation aggregate failed")
	}
}

func TestKernelBDPFormula(t *testing.T) {
	if got := bdpThroughputMbps(1_000_000, 100); got != 80 {
		t.Fatalf("BDP throughput = %v, want 80", got)
	}
	if bdpThroughputMbps(0, 100) != 0 || bdpThroughputMbps(1_000_000, 0) != 0 {
		t.Fatal("non-positive BDP inputs were not rejected")
	}
	// appendKernelNetworkParams intentionally reads fixed /proc/sys paths; its
	// host-facing behavior is covered by the system probe boundary, not here.
}

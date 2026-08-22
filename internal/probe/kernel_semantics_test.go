package probe

import (
	"strings"
	"testing"
)

func TestKernelMachineSemantics(t *testing.T) {
	seen := make(map[string]bool)
	for _, param := range kernelParams() {
		if param.Key == "" || param.Path == "" || seen[param.Key] {
			t.Fatalf("invalid or duplicate kernel parameter: %+v", param)
		}
		seen[param.Key] = true
		if label := kernelParamLabelKey(param.Key); !strings.HasPrefix(label, "probe.kernel.param.") || !strings.HasSuffix(label, ".label") {
			t.Fatalf("kernel label key = %q", label)
		}
		if why := kernelParamWhyKey(param.Key); !strings.HasPrefix(why, "probe.kernel.param.") || !strings.HasSuffix(why, ".why") {
			t.Fatalf("kernel rationale key = %q", why)
		}
	}

	for _, test := range []struct {
		current   string
		available string
		want      string
	}{
		{current: "bbr", available: "reno cubic bbr", want: "enabled"},
		{current: "cubic", available: "reno cubic bbr", want: "available_not_enabled"},
		{current: "cubic", available: "reno cubic", want: "unavailable"},
		{current: "cubic", available: "bbr2 cubic", want: "unavailable"},
	} {
		if got := bbrMachineStatus(test.current, test.available); got != test.want {
			t.Fatalf("bbrMachineStatus(%q, %q) = %q, want %q", test.current, test.available, got, test.want)
		}
	}
}

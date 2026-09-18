package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestToolSourceValuesAreExactlyThree(t *testing.T) {
	values := []ToolSource{ToolSourceBundle, ToolSourceBaseSystem, ToolSourceUnsupported}
	want := []ToolSource{"bundle", "base-system", "unsupported"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("tool source values = %v, want exactly %v", values, want)
	}
}

func TestPlatformToolSourcesFollowRuntimeContract(t *testing.T) {
	definitions := BuiltinDefinitions()
	tests := []struct {
		platform Platform
		want     []ToolSource
	}{
		{
			platform: PlatformLinux,
			want: []ToolSource{
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
			},
		},
		{
			platform: PlatformFreeBSD,
			want: []ToolSource{
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
				ToolSourceBaseSystem, ToolSourceBaseSystem, ToolSourceBundle,
			},
		},
		{
			platform: PlatformWindows,
			want: []ToolSource{
				ToolSourceUnsupported, ToolSourceBundle, ToolSourceBundle, ToolSourceBundle,
				ToolSourceBundle, ToolSourceBundle, ToolSourceBundle, ToolSourceUnsupported,
				ToolSourceBundle, ToolSourceBaseSystem, ToolSourceUnsupported,
			},
		},
	}
	for _, test := range tests {
		t.Run(string(test.platform), func(t *testing.T) {
			if len(test.want) != len(definitions) {
				t.Fatalf("expected %d source facts, got %d", len(definitions), len(test.want))
			}
			for index, definition := range definitions {
				got := PlatformToolSource(test.platform, definition.ID)
				if got != test.want[index] {
					t.Fatalf("%s source for %q = %q, want %q", test.platform, definition.ID, got, test.want[index])
				}
			}
		})
	}

	if got := PlatformToolSource(Platform("plan9"), "zstd"); got != ToolSourceUnsupported {
		t.Fatalf("unknown platform source = %q, want %q", got, ToolSourceUnsupported)
	}
	if got := PlatformToolSource(PlatformLinux, "missing"); got != ToolSourceUnsupported {
		t.Fatalf("unknown tool source = %q, want %q", got, ToolSourceUnsupported)
	}
}

func TestWindowsPingUsesNativeICMPPlatformFact(t *testing.T) {
	if got := PlatformToolSource(PlatformWindows, "ping"); got != ToolSourceBaseSystem {
		t.Fatalf("Windows ping source = %q, want base-system for native ICMP", got)
	}
	if got := BundleToolIDs(PlatformWindows, []string{"ping"}); len(got) != 0 {
		t.Fatalf("Windows native ICMP staging projection = %v, want empty", got)
	}
}

func TestFreeBSDSpeedtestUsesPrivatePlanProjection(t *testing.T) {
	if got := PlatformToolSource(PlatformFreeBSD, "speedtest"); got != ToolSourceBundle {
		t.Fatalf("FreeBSD speedtest source = %q, want bundle projection", got)
	}
	if got, want := BundleToolIDs(PlatformFreeBSD, []string{"speedtest"}), []string{"speedtest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FreeBSD speedtest plan projection = %v, want %v", got, want)
	}
	// The bundle label above means the private required_tools/staging
	// projection. FreeBSD speedtest is not a frozen tools/lock.json archive
	// member; run.sh owns its separately verified signed-package path and then
	// fails closed because no FreeBSD client exists.
}

func TestBundleToolIDsPreservesDeclaredOrder(t *testing.T) {
	declared := []string{"ping", "zstd", "nexttrace-tiny", "zstd", "sysbench"}
	if got, want := BundleToolIDs(PlatformFreeBSD, declared), []string{"zstd", "zstd", "sysbench"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FreeBSD bundle projection = %v, want %v", got, want)
	}
	if got, want := BundleToolIDs(PlatformWindows, declared), []string{"zstd", "nexttrace-tiny", "zstd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Windows bundle projection = %v, want %v", got, want)
	}
}

func TestWindowsBundleSourcesMatchLockManifest(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	lockPath := filepath.Join(filepath.Dir(sourceFile), "..", "..", "tools", "lock.json")
	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read tools/lock.json: %v", err)
	}
	var lock struct {
		Tools        []struct{ Name string } `json:"tools"`
		WindowsTools []string                `json:"windows_tools"`
	}
	if err := json.Unmarshal(content, &lock); err != nil {
		t.Fatalf("decode tools/lock.json: %v", err)
	}

	want := make([]string, 0, len(BuiltinDefinitions()))
	for _, definition := range BuiltinDefinitions() {
		if PlatformToolSource(PlatformWindows, definition.ID) == ToolSourceBundle {
			want = append(want, definition.ID)
		}
	}
	if !reflect.DeepEqual(lock.WindowsTools, want) {
		t.Fatalf("lock windows_tools = %v, want runtime bundle tools %v", lock.WindowsTools, want)
	}
	for _, tool := range lock.Tools {
		if tool.Name == "speedtest" {
			t.Fatal("tools/lock.json unexpectedly contains FreeBSD speedtest")
		}
	}
}

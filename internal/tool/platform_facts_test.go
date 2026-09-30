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
	values := []ToolSource{ToolSourceWrapperManaged, ToolSourcePlatformProvided, ToolSourceUnsupported}
	want := []ToolSource{"wrapper-managed", "platform-provided", "unsupported"}
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
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
			},
		},
		{
			platform: PlatformFreeBSD,
			want: []ToolSource{
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
				ToolSourcePlatformProvided, ToolSourcePlatformProvided, ToolSourceWrapperManaged,
			},
		},
		{
			platform: PlatformWindows,
			want: []ToolSource{
				ToolSourceUnsupported, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged,
				ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceWrapperManaged, ToolSourceUnsupported,
				ToolSourceWrapperManaged, ToolSourcePlatformProvided, ToolSourceUnsupported,
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
	if got := PlatformToolSource(PlatformWindows, "ping"); got != ToolSourcePlatformProvided {
		t.Fatalf("Windows ping source = %q, want platform-provided for native ICMP", got)
	}
	if got := RequiredToolIDs(PlatformWindows, []string{"ping"}); len(got) != 0 {
		t.Fatalf("Windows native ICMP plan projection = %v, want empty", got)
	}
}

func TestFreeBSDSpeedtestUsesWrapperManagedPlanProjection(t *testing.T) {
	if got := PlatformToolSource(PlatformFreeBSD, "speedtest"); got != ToolSourceWrapperManaged {
		t.Fatalf("FreeBSD speedtest source = %q, want wrapper-managed projection", got)
	}
	if got, want := RequiredToolIDs(PlatformFreeBSD, []string{"speedtest"}), []string{"speedtest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FreeBSD speedtest plan projection = %v, want %v", got, want)
	}
	// speedtest is not a frozen tools/lock.json archive member; run.sh owns its
	// separately verified signed-package path and then fails closed because no
	// FreeBSD client exists.
}

func TestRequiredToolIDsPreservesDeclaredOrder(t *testing.T) {
	declared := []string{"ping", "zstd", "nexttrace-tiny", "zstd", "sysbench"}
	if got, want := RequiredToolIDs(PlatformFreeBSD, declared), []string{"zstd", "zstd", "sysbench"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FreeBSD required tool projection = %v, want %v", got, want)
	}
	if got, want := RequiredToolIDs(PlatformWindows, declared), []string{"zstd", "nexttrace-tiny", "zstd"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Windows required tool projection = %v, want %v", got, want)
	}
}

func TestWindowsWrapperManagedToolsMatchLockManifest(t *testing.T) {
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
		if PlatformToolSource(PlatformWindows, definition.ID) == ToolSourceWrapperManaged {
			want = append(want, definition.ID)
		}
	}
	if !reflect.DeepEqual(lock.WindowsTools, want) {
		t.Fatalf("lock windows_tools = %v, want runtime wrapper-managed tools %v", lock.WindowsTools, want)
	}
	for _, tool := range lock.Tools {
		if tool.Name == "speedtest" {
			t.Fatal("tools/lock.json unexpectedly contains FreeBSD speedtest")
		}
	}
}

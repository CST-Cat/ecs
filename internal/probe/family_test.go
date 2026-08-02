package probe

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
)

func TestEndpointFamiliesRespectIPVersion(t *testing.T) {
	cases := []struct {
		name     string
		networks string
		hasV6    bool
		mode     string
		want     []string
	}{
		{"auto dual stack", "IPv4|IPv6", true, config.IPVersionAuto, []string{"IPv4", "IPv6"}},
		{"auto v4 only", "IPv4|IPv6", false, config.IPVersionAuto, []string{"IPv4"}},
		{"forced v4", "IPv4|IPv6", true, config.IPVersion4, []string{"IPv4"}},
		{"forced v6", "IPv4|IPv6", true, config.IPVersion6, []string{"IPv6"}},
		{"v4 target cannot serve v6", "IPv4", true, config.IPVersion6, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := endpointFamilies(testCase.networks, testCase.hasV6, testCase.mode); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("endpointFamilies = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestLatencyFamiliesForLiteralAddresses(t *testing.T) {
	if got := latencyFamilies("127.0.0.1:443", config.IPVersionAuto); !reflect.DeepEqual(got, []string{config.IPVersion4}) {
		t.Fatalf("IPv4 literal families = %v", got)
	}
	if got := latencyFamilies("[::1]:443", config.IPVersionAuto); !reflect.DeepEqual(got, []string{config.IPVersion6}) {
		t.Fatalf("IPv6 literal families = %v", got)
	}
	if got := latencyFamilies("127.0.0.1:443", config.IPVersion6); len(got) != 0 {
		t.Fatalf("forced IPv6 must not schedule IPv4 literal: %v", got)
	}
}

func TestEndpointsForIPVersionFiltersLiteralResolvers(t *testing.T) {
	endpoints := []config.Endpoint{
		{Name: "v4", Address: "1.1.1.1:53"},
		{Name: "v6", Address: "[2606:4700:4700::1111]:53"},
		{Name: "hostname", Address: "resolver.example:53"},
		{Name: "pinned-v6", Address: "resolver.example:53", Family: config.IPVersion6},
	}
	got := endpointsForIPVersion(endpoints, config.IPVersion6)
	want := []config.Endpoint{endpoints[1], endpoints[2], endpoints[3]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IPv6 resolver filter = %v, want %v", got, want)
	}
	if endpointMatchesIPVersion("1.1.1.1", config.IPVersion6) {
		t.Fatal("IPv4 literal without a port must not match IPv6")
	}
	if !endpointMatchesIPVersion("2001:db8::1", config.IPVersion6) {
		t.Fatal("IPv6 literal without a port must match IPv6")
	}
}

func TestEndpointFamilySelectsIPv6Hostname(t *testing.T) {
	target := config.Endpoint{Name: "v6 hostname", Address: "bj-ct-v6.ip.zstaticcdn.com", Family: config.IPVersion6}
	if got := endpointFamily(target, config.IPVersionAuto); got != config.IPVersion6 {
		t.Fatalf("endpoint family = %q, want 6", got)
	}
	args := routeCommandArgsForFamily(routeEngine{Name: "traceroute"}, target.Address, 20, endpointFamily(target, config.IPVersionAuto))
	if len(args) == 0 || args[0] != "-6" {
		t.Fatalf("IPv6 hostname route args = %v", args)
	}
	if got := latencyFamiliesForEndpoint(target, config.IPVersionAuto, true); !reflect.DeepEqual(got, []string{config.IPVersion6}) {
		t.Fatalf("IPv6 hostname latency families = %v", got)
	}
}

func TestFamilySpecificArguments(t *testing.T) {
	if got := strings.Join(pingArgumentsForFamily("::1", 1, time.Second, config.IPVersion6), " "); !strings.Contains(got, " -6 ") && !strings.HasPrefix(got, "-6 ") {
		t.Fatalf("IPv6 ping arguments = %q", got)
	}
	engine := routeEngine{Name: "traceroute"}
	args := routeCommandArgsForFamily(engine, "2001:db8::1", 5, config.IPVersion6)
	if len(args) == 0 || args[0] != "-6" {
		t.Fatalf("IPv6 route arguments = %v", args)
	}
	if got := routeCommandArgs(engine, "127.0.0.1", 5); len(got) == 0 || got[0] == "-4" || got[0] == "-6" {
		t.Fatalf("auto route arguments unexpectedly force a family: %v", got)
	}
}

func TestHardwareHelpers(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "uevent")
	if err := os.WriteFile(path, []byte("PCI_ID=8086:1234\nDRIVER=virtio_net\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values := parseKeyValueFile(path)
	if values["PCI_ID"] != "8086:1234" || values["DRIVER"] != "virtio_net" {
		t.Fatalf("uevent values = %v", values)
	}
	if got := joinHardwareValues("unknown", "Acme", "v1"); got != "Acme · v1" {
		t.Fatalf("hardware values = %q", got)
	}
	if got := joinHardwareList(nil); got != "unknown" {
		t.Fatalf("empty hardware list = %q", got)
	}
	if got := formatHardwareBytes(1 << 30); got != "1.0 GiB" {
		t.Fatalf("hardware size = %q", got)
	}
	if got := formatHardwareBytes(1024); got != "1.0 KiB" {
		t.Fatalf("small hardware size = %q", got)
	}
}

func TestTemperatureAndSMARTFormatting(t *testing.T) {
	if got, ok := formatTemperature("42500"); !ok || got != "42.5 °C" {
		t.Fatalf("temperature = %q, %v", got, ok)
	}
	if _, ok := formatTemperature("999"); ok {
		t.Fatal("unphysical direct temperature should be rejected")
	}
	passed := true
	temp := 38
	info := smartInfo{Device: "/dev/nvme0n1", ModelName: "Example SSD", Passed: &passed, Temperature: &temp}
	if got := formatSMARTSummary(info); got != "nvme0n1 model=Example SSD health=pass temp=38 °C" {
		t.Fatalf("SMART summary = %q", got)
	}
}

func TestSystemReportIncludesHardwareFacts(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	result := (systemProbe{}).Run(context.Background(), Environment{Config: cfg})
	keys := make(map[string]bool, len(result.Fields))
	for _, field := range result.Fields {
		keys[field.Key] = true
	}
	for _, key := range []string{"system_vendor", "product_name", "motherboard", "bios", "gpus", "network_adapters", "block_devices", "raid", "temperatures", "smart"} {
		if !keys[key] {
			t.Fatalf("system report missing hardware field %q", key)
		}
	}
}

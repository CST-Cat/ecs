//go:build freebsd

package probe

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ecs/internal/config"
)

func TestFreeBSDTraceBackendUsesOnlyBaseSystemPaths(t *testing.T) {
	backend := detectTraceBackend(context.Background())
	if backend.Name != "freebsd-traceroute" || backend.Adapter != traceFreeBSDTracerouteAdapter || backend.Version == "" {
		t.Fatalf("FreeBSD trace backend = %#v", backend)
	}
	if !traceBackendAvailableForFamily(backend, "4") || !traceBackendAvailableForFamily(backend, "6") {
		t.Fatal("FreeBSD base traceroute backend is not available for both IP families")
	}
	for _, test := range []struct {
		family  string
		maxHops int
		path    string
		args    []string
	}{
		{family: "4", maxHops: 12, path: "/usr/sbin/traceroute", args: []string{"-I", "-n", "-q", "1", "-m", "12", "-w", "1", "127.0.0.1"}},
		{family: "6", maxHops: 20, path: "/usr/sbin/traceroute6", args: []string{"-I", "-n", "-q", "1", "-m", "20", "-w", "1", "::1"}},
		{family: "auto", maxHops: 12, path: "/usr/sbin/traceroute", args: []string{"-I", "-n", "-q", "1", "-m", "12", "-w", "1", "example.com"}},
	} {
		spec := traceCommandSpecForFamily(backend, test.args[len(test.args)-1], test.maxHops, test.family)
		if spec.Path != test.path || !reflect.DeepEqual(spec.Args, test.args) {
			t.Fatalf("FreeBSD %s traceroute command = %#v, want path=%q args=%#v", test.family, spec, test.path, test.args)
		}
		if !traceBackendAvailableForFamily(backend, test.family) {
			t.Fatalf("FreeBSD %s traceroute path %q is unavailable", test.family, spec.Path)
		}
	}
}

func TestFreeBSDTraceArgumentsCoverMixedFamilies(t *testing.T) {
	backend := detectTraceBackend(context.Background())
	targets := []config.Endpoint{
		{Name: "IPv4 first", Address: "127.0.0.1", Family: config.IPVersion4},
		{Name: "IPv6 second", Address: "::1", Family: config.IPVersion6},
		{Name: "IPv4 duplicate", Address: "127.0.0.1", Family: config.IPVersion4},
	}
	arguments := traceArgumentsForTargets(backend, targets, config.IPVersionAuto)
	if arguments == "" || strings.Contains(arguments, "FreeBSD") || strings.Contains(arguments, "按目标") {
		t.Fatalf("FreeBSD mixed-family arguments contain non-machine prose: %q", arguments)
	}
	var variants []traceArgumentVariant
	if err := json.Unmarshal([]byte(arguments), &variants); err != nil {
		t.Fatalf("FreeBSD mixed-family arguments are not canonical JSON: %q: %v", arguments, err)
	}
	if len(variants) != 2 || variants[0].Family != "ipv4" || variants[1].Family != "ipv6" {
		t.Fatalf("FreeBSD mixed-family variants = %#v, want ordered ipv4/ipv6", variants)
	}
	wantIPv4 := []string{"-I", "-n", "-q", "1", "-m", "12", "-w", "1", "<target>"}
	wantIPv6 := []string{"-I", "-n", "-q", "1", "-m", "20", "-w", "1", "<target>"}
	if !reflect.DeepEqual(variants[0].Args, wantIPv4) || !reflect.DeepEqual(variants[1].Args, wantIPv6) {
		t.Fatalf("FreeBSD mixed-family argument arrays = %#v, want v4=%#v v6=%#v", variants, wantIPv4, wantIPv6)
	}
	if strings.Contains(arguments, freeBSDTracerouteIPv4Path) || strings.Contains(arguments, freeBSDTracerouteIPv6Path) {
		t.Fatalf("FreeBSD executable path leaked into arguments provenance: %q", arguments)
	}
	if single := traceArgumentsForTargets(backend, targets[:1], config.IPVersionAuto); single != strings.Join(wantIPv4, " ") {
		t.Fatalf("FreeBSD single-family arguments = %q, want %q", single, strings.Join(wantIPv4, " "))
	}
}

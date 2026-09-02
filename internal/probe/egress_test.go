package probe

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/module"
)

func TestEgressRecordsAndLocalDecisionContracts(t *testing.T) {
	egress := Egress{Attempted: true, ByVersion: map[string]EgressAddress{
		config.IPVersion4: {Version: config.IPVersion4, IP: "203.0.113.9", Source: "ipapi.is", Intel: ipAPIResponse{IP: "203.0.113.9"}, HasIntel: true},
		config.IPVersion6: {Version: config.IPVersion6, Source: "stun", IntelAttempted: true, IntelErr: errors.New("intel unavailable")},
	}}
	if ip, err := egress.IPFor(config.IPVersion4); err != nil || ip != "203.0.113.9" {
		t.Fatalf("IPFor success = %q/%v", ip, err)
	}
	if intel, ok := egress.IntelFor(config.IPVersion4); !ok || intel.IP != "203.0.113.9" {
		t.Fatalf("IntelFor success = %+v/%v", intel, ok)
	}
	if _, ok := egress.IntelFor(config.IPVersion6); ok {
		t.Fatal("IntelFor reported data for an unqueried record")
	}
	if _, err := egress.IPFor(config.IPVersion6); err == nil || !strings.Contains(err.Error(), "IPv6 出口地址为空") {
		t.Fatalf("empty IP diagnostic = %v", err)
	}
	if _, err := egress.IPFor("9"); err == nil || !strings.Contains(err.Error(), "未发现 IPv9") {
		t.Fatalf("missing family diagnostic = %v", err)
	}
	failed := Egress{ByVersion: map[string]EgressAddress{config.IPVersion4: {Err: errors.New("fixture failure")}}}
	if _, err := failed.IPFor(config.IPVersion4); err == nil || err.Error() != "fixture failure" {
		t.Fatalf("stored failure = %v", err)
	}

	if !moduleSelected([]string{"network", "bgp"}, "bgp") || moduleSelected([]string{"network"}, "dns") || !modulesNeedEgressBGP([]string{"network"}) || modulesNeedEgressBGP([]string{"dns"}) {
		t.Fatal("egress module selection contract failed")
	}
	if got := combineEgressErrors(nil, errors.New("stun")); got == nil || got.Error() != "stun" {
		t.Fatalf("single STUN error = %v", got)
	}
	if got := combineEgressErrors(errors.New("intel"), errors.New("stun")); got == nil || !strings.Contains(got.Error(), "ipapi.is: intel；STUN: stun") {
		t.Fatalf("combined egress error = %v", got)
	}
	for _, test := range []struct {
		value, version string
		want           bool
	}{
		{"203.0.113.9", config.IPVersion4, true}, {"2001:db8::9", config.IPVersion6, true}, {"2001:db8::9", config.IPVersion4, false}, {"bad", config.IPVersion4, false},
	} {
		if got := egressAddressMatchesVersion(test.value, test.version); got != test.want {
			t.Errorf("egress address %q/%s = %v, want %v", test.value, test.version, got, test.want)
		}
	}

	local := Egress{ByVersion: map[string]EgressAddress{config.IPVersion4: {Version: config.IPVersion4, Err: errors.New("missing")}}}
	cacheEgressBGP(context.Background(), Environment{}, &local, []string{config.IPVersion4})
	if !local.ByVersion[config.IPVersion4].BGPQueried || local.ByVersion[config.IPVersion4].BGPError == nil {
		t.Fatal("unavailable egress BGP cache state was not recorded")
	}
	if got := DiscoverEgress(context.Background(), Environment{Config: config.Runtime{Exposure: module.ExposureLocal}}); got.Attempted || len(got.ByVersion) != 0 {
		t.Fatalf("offline egress discovery = %+v", got)
	}
	if address := discoverEgressForVersion(context.Background(), Environment{}, config.IPVersion4, false); address.Err == nil || address.Source != "stun" {
		t.Fatalf("empty STUN configuration = %+v", address)
	}
}

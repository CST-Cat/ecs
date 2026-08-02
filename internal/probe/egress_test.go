package probe

import (
	"context"
	"errors"
	"testing"
	"time"

	"ecs/internal/config"
)

func TestDiscoverEgressSkippedWhenNobodyNeedsIt(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"cpu", "memory", "dns"}
	egress := DiscoverEgress(context.Background(), Environment{Config: cfg})
	if egress.Attempted {
		t.Fatal("没有模块需要出口 IP 时不该发起发现")
	}
}

func TestDiscoverEgressSkippedWhenOffline(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"network", "blacklist"}
	cfg.Exposure = config.ExposureLocal
	egress := DiscoverEgress(context.Background(), Environment{Config: cfg})
	if egress.Attempted {
		t.Fatal("local 级别下不该发起任何出口发现")
	}
}

// 报告里要能区分"查了但没结果"和"这次压根没查"：前者是数据缺失，
// 后者是用户主动选择的隐私边界。
func TestIntelForDistinguishesMissingFromNotQueried(t *testing.T) {
	stunOnly := Egress{
		Attempted: true,
		ByVersion: map[string]EgressAddress{
			config.IPVersion4: {Version: config.IPVersion4, IP: "203.0.113.7", Source: "stun"},
		},
	}
	if _, ok := stunOnly.IntelFor(config.IPVersion4); ok {
		t.Fatal("STUN 路径不该报告有情报记录")
	}
	ip, err := stunOnly.IPFor(config.IPVersion4)
	if err != nil || ip != "203.0.113.7" {
		t.Fatalf("IPFor = %q, %v", ip, err)
	}

	withIntel := Egress{
		Attempted: true,
		ByVersion: map[string]EgressAddress{
			config.IPVersion4: {
				Version:  config.IPVersion4,
				IP:       "203.0.113.9",
				Source:   "ipapi.is",
				Intel:    ipAPIResponse{IP: "203.0.113.9"},
				HasIntel: true,
			},
		},
	}
	intel, ok := withIntel.IntelFor(config.IPVersion4)
	if !ok || intel.IP != "203.0.113.9" {
		t.Fatalf("IntelFor = %+v, %v", intel, ok)
	}
}

func TestIPForSurfacesDiscoveryFailure(t *testing.T) {
	failed := Egress{
		Attempted: true,
		ByVersion: map[string]EgressAddress{
			config.IPVersion6: {Version: config.IPVersion6, Err: errors.New("no route")},
		},
	}
	if _, err := failed.IPFor(config.IPVersion6); err == nil {
		t.Fatal("发现失败时 IPFor 应当返回错误")
	}
	// 未查询过的协议族与查询失败要分开报告。
	if _, err := failed.IPFor(config.IPVersion4); err == nil {
		t.Fatal("未发现的协议族应当返回错误")
	}
}

func TestEgressAddressMatchesVersion(t *testing.T) {
	if !egressAddressMatchesVersion("198.51.100.4", config.IPVersion4) {
		t.Fatal("IPv4 字面量应当匹配 IPv4")
	}
	if egressAddressMatchesVersion("198.51.100.4", config.IPVersion6) {
		t.Fatal("IPv4 字面量不该匹配 IPv6")
	}
	if !egressAddressMatchesVersion("2001:db8::1", config.IPVersion6) {
		t.Fatal("IPv6 字面量应当匹配 IPv6")
	}
	if egressAddressMatchesVersion("not-an-ip", config.IPVersion4) {
		t.Fatal("非法地址不该匹配任何协议族")
	}
}

// STUN 路径必须在没有可用服务器时干净地失败，而不是挂起或 panic。
func TestEgressViaSTUNFailsWithoutServers(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileQuick)
	if err != nil {
		t.Fatal(err)
	}
	cfg.STUNServers = nil
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := egressViaSTUN(ctx, Environment{Config: cfg}, config.IPVersion4); err == nil {
		t.Fatal("没有 STUN 服务器时应当返回错误")
	}
}

package config

import (
	"net"
	"reflect"
	"testing"
)

func TestParseBacktraceCities(t *testing.T) {
	if got, err := ParseBacktraceCities(""); err != nil || !reflect.DeepEqual(got, defaultBacktraceCities) {
		t.Fatalf("空值应回落到默认：%v, %v", got, err)
	}
	if got, err := ParseBacktraceCities("all"); err != nil || len(got) != len(BacktraceCityOrder) {
		t.Fatalf("all = %v, %v", got, err)
	}
	if got, err := ParseBacktraceCities("shanghai,chengdu"); err != nil || len(got) != 2 {
		t.Fatalf("多城市 = %v, %v", got, err)
	}
	if _, err := ParseBacktraceCities("wuhan"); err == nil {
		t.Fatal("未知城市必须报错")
	}
	if _, err := ParseBacktraceCities("all,beijing"); err == nil {
		t.Fatal("all 与具体城市组合必须报错")
	}
}

func TestBacktraceTargetsFor(t *testing.T) {
	all := BacktraceTargetsFor(BacktraceCityOrder)
	// 四城市各三大运营商，IPv4 + IPv6 两组。
	if len(all) != 24 {
		t.Fatalf("全部城市应有 24 个目标，实际 %d", len(all))
	}
	carriers := map[string]int{}
	seen := map[string]bool{}
	for _, target := range all {
		if net.ParseIP(target.Address) == nil && target.Address == "" {
			t.Fatalf("回程目标必须是 IP 或域名：%q", target.Address)
		}
		if seen[target.Address] {
			t.Fatalf("回程目标重复：%s", target.Address)
		}
		seen[target.Address] = true
		if target.Name == "" || target.Kind == "" {
			t.Fatalf("回程目标缺字段：%+v", target)
		}
		carriers[target.Kind]++
	}
	for _, carrier := range []string{"电信", "联通", "移动"} {
		if carriers[carrier] != 8 {
			t.Errorf("%s 应有 8 个目标（每城市 IPv4/IPv6 各一个），实际 %d", carrier, carriers[carrier])
		}
	}

	// 顺序必须按 BacktraceCityOrder 固定，不能随 map 遍历漂移。
	first := BacktraceTargetsFor([]string{"chengdu", "beijing"})
	if len(first) != 12 || first[0].Kind != "电信" || first[0].Name != "北京电信" {
		t.Fatalf("城市顺序未按 BacktraceCityOrder 固定：%+v", first[:1])
	}

	if got := BacktraceTargetsFor(nil); len(got) != 0 {
		t.Fatalf("空城市列表应无目标：%+v", got)
	}
}

func TestDefaultsUsesBeijingAndGuangzhou(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.BacktraceTargets) != 12 {
		t.Fatalf("默认应为北京+广州 IPv4/IPv6 共 12 个目标，实际 %d", len(cfg.BacktraceTargets))
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("默认回程目标未通过校验：%v", err)
	}
}

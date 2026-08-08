package config

import "testing"

func TestParseExposureAcceptsEveryLevel(t *testing.T) {
	cases := map[string]Exposure{
		"local":      ExposureLocal,
		"public":     ExposurePublic,
		"thirdparty": ExposureThirdParty,
		"any":        ExposureConsent,
		"  ANY  ":    ExposureConsent,
	}
	for raw, want := range cases {
		got, err := ParseExposure(raw)
		if err != nil {
			t.Fatalf("ParseExposure(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParseExposure(%q) = %v, want %v", raw, got, want)
		}
	}
	if _, err := ParseExposure("everything"); err == nil {
		t.Fatal("expected an error for an unknown level")
	}
}

func TestExposureNamesRoundTrip(t *testing.T) {
	for _, name := range ExposureNames() {
		level, err := ParseExposure(name)
		if err != nil {
			t.Fatalf("ParseExposure(%q) error: %v", name, err)
		}
		if level.String() != name {
			t.Fatalf("round trip %q -> %v -> %q", name, level, level.String())
		}
	}
}

// 每个已注册模块都必须在外联表里有条目，否则 ExposureFor 会把它按最高级
// 兜底，用户会看到一个默认永远不跑的模块。
func TestEveryModuleHasAnExposureEntry(t *testing.T) {
	for _, id := range ModuleOrder {
		if _, ok := moduleExposure[id]; !ok {
			t.Fatalf("模块 %q 没有登记外联级别", id)
		}
	}
	for id := range moduleExposure {
		if !contains(ModuleOrder, id) {
			t.Fatalf("外联表里的 %q 不是已注册模块", id)
		}
	}
}

func TestFilterModulesByExposure(t *testing.T) {
	all := []string{"system", "dns", "network", "ookla"}

	local := FilterModulesByExposure(all, ExposureLocal)
	if len(local) != 1 || local[0] != "system" {
		t.Fatalf("local = %v", local)
	}

	public := FilterModulesByExposure(all, ExposurePublic)
	if len(public) != 2 || public[1] != "dns" {
		t.Fatalf("public = %v", public)
	}

	third := FilterModulesByExposure(all, ExposureThirdParty)
	if len(third) != 4 || third[2] != "network" || third[3] != "ookla" {
		t.Fatalf("thirdparty = %v", third)
	}

	any := FilterModulesByExposure(all, ExposureConsent)
	if len(any) != 4 || any[3] != "ookla" {
		t.Fatalf("any = %v", any)
	}
}

func TestOoklaIsAnOrdinaryThirdPartyModule(t *testing.T) {
	if !AllowsModule(ExposureThirdParty, "ookla") {
		t.Fatal("Ookla should be available at the thirdparty exposure level")
	}
	if AllowsModule(ExposurePublic, "ookla") {
		t.Fatal("Ookla should remain above the public-only exposure level")
	}
}

func TestCheckModuleExposureReportsActionableErrors(t *testing.T) {
	if err := CheckModuleExposure([]string{"ookla"}, ExposureThirdParty); err != nil {
		t.Fatalf("thirdparty Ookla should be allowed: %v", err)
	}

	err := CheckModuleExposure([]string{"network"}, ExposurePublic)
	if err == nil {
		t.Fatal("点名越级模块时应当报错")
	}
	if got := err.Error(); !containsSubstring(got, "thirdparty") {
		t.Fatalf("错误信息未给出应使用的级别: %q", got)
	}

	if err := CheckModuleExposure([]string{"dns"}, ExposurePublic); err != nil {
		t.Fatalf("级别之内的模块不该报错: %v", err)
	}
}

// 出口发现的选路：只有真正要用 ASN/地理字段的模块才值得把待查 IP 交给第三方。
func TestEgressIntelOnlyWhenAThirdPartyModuleNeedsIt(t *testing.T) {
	if !EgressNeedsIPIntel([]string{"network"}, ExposureThirdParty) {
		t.Fatal("network 需要情报字段")
	}
	if EgressNeedsIPIntel([]string{"blacklist", "bgp"}, ExposureThirdParty) {
		t.Fatal("只需要出口 IP 的模块不该触发情报查询")
	}
	if EgressNeedsIPIntel([]string{"network"}, ExposurePublic) {
		t.Fatal("级别不足时不该走情报接口")
	}
	if !RequiresEgressIP([]string{"blacklist"}) {
		t.Fatal("blacklist 需要出口 IP")
	}
	if RequiresEgressIP([]string{"cpu", "dns"}) {
		t.Fatal("不需要出口 IP 的模块集不该触发发现")
	}
}

func TestProfileOoklaMembershipIsExplicit(t *testing.T) {
	standard, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	if contains(standard.Modules, "ookla") {
		t.Fatal("standard 档默认不应包含 Ookla")
	}
	cfg, err := Defaults(ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(cfg.Modules, "ookla") {
		t.Fatal("full 档默认应包含 Ookla")
	}
	descriptor, ok := ModuleDescriptorFor("ookla")
	if !ok || descriptor.ProfileExplicitOnly || descriptor.ProfileStandard || !descriptor.ProfileFull {
		t.Fatalf("Ookla profile metadata = %+v, want full-only default module", descriptor)
	}
}

func TestValidateRejectsModulesBeyondExposure(t *testing.T) {
	cfg, err := Defaults(ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"system", "network"}
	cfg.Exposure = ExposurePublic
	if err := Validate(cfg); err == nil {
		t.Fatal("Validate 应当挡住越级的模块集")
	}
	cfg.Exposure = ExposureThirdParty
	if err := Validate(cfg); err != nil {
		t.Fatalf("级别足够时不该报错: %v", err)
	}
}

func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

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

	local := FilterModulesByExposure(all, ExposureLocal, nil)
	if len(local) != 1 || local[0] != "system" {
		t.Fatalf("local = %v", local)
	}

	public := FilterModulesByExposure(all, ExposurePublic, nil)
	if len(public) != 2 || public[1] != "dns" {
		t.Fatalf("public = %v", public)
	}

	third := FilterModulesByExposure(all, ExposureThirdParty, nil)
	if len(third) != 3 || third[2] != "network" {
		t.Fatalf("thirdparty = %v", third)
	}

	// any 只放开上限，闭源模块仍要单独签字。
	any := FilterModulesByExposure(all, ExposureConsent, nil)
	if len(any) != 3 {
		t.Fatalf("any without consent = %v", any)
	}
	accepted := FilterModulesByExposure(all, ExposureConsent, []string{"ookla"})
	if len(accepted) != 4 || accepted[3] != "ookla" {
		t.Fatalf("any with consent = %v", accepted)
	}
}

// --accept 比级别更强：签了字的模块在任何 --exposure 下都能跑，否则用户得同时
// 记住两个开关才能启用一个模块。
func TestAcceptOverridesExposureLimit(t *testing.T) {
	if !AllowsModule(ExposureLocal, []string{"ookla"}, "ookla") {
		t.Fatal("已同意的模块应当不受级别上限限制")
	}
	if AllowsModule(ExposureConsent, nil, "ookla") {
		t.Fatal("未同意的闭源模块不该只因级别放开就运行")
	}
}

func TestCheckModuleExposureReportsActionableErrors(t *testing.T) {
	err := CheckModuleExposure([]string{"ookla"}, ExposureThirdParty, nil)
	if err == nil {
		t.Fatal("点名 ookla 而未同意时应当报错")
	}
	if got := err.Error(); got == "" || !containsSubstring(got, "--accept ookla") {
		t.Fatalf("错误信息未给出可执行建议: %q", got)
	}

	err = CheckModuleExposure([]string{"network"}, ExposurePublic, nil)
	if err == nil {
		t.Fatal("点名越级模块时应当报错")
	}
	if got := err.Error(); !containsSubstring(got, "thirdparty") {
		t.Fatalf("错误信息未给出应使用的级别: %q", got)
	}

	if err := CheckModuleExposure([]string{"dns"}, ExposurePublic, nil); err != nil {
		t.Fatalf("级别之内的模块不该报错: %v", err)
	}
}

func TestValidateAcceptedRejectsUnrelatedModules(t *testing.T) {
	if err := ValidateAccepted([]string{"ookla"}); err != nil {
		t.Fatalf("ookla 应当可以被同意: %v", err)
	}
	if err := ValidateAccepted([]string{"dns"}); err == nil {
		t.Fatal("不需要同意的模块不该出现在 --accept 里")
	}
	if err := ValidateAccepted([]string{"nope"}); err == nil {
		t.Fatal("未知模块应当报错")
	}
}

func TestMergeAcceptedIsIdempotent(t *testing.T) {
	merged := MergeAccepted([]string{"system", "ookla"}, []string{"ookla"})
	if len(merged) != 2 {
		t.Fatalf("已存在的模块不该重复加入: %v", merged)
	}
	merged = MergeAccepted([]string{"system"}, []string{"ookla"})
	if len(merged) != 2 || merged[1] != "ookla" {
		t.Fatalf("merged = %v", merged)
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

func TestFullProfileExcludesConsentModulesByDefault(t *testing.T) {
	cfg, err := Defaults(ProfileFull)
	if err != nil {
		t.Fatal(err)
	}
	// full 档给出全集，过滤发生在外联层。
	if !contains(cfg.Modules, "ookla") {
		t.Fatal("full 档的模块全集应当包含 ookla")
	}
	filtered := FilterModulesByExposure(cfg.Modules, cfg.Exposure, cfg.Accepted)
	if contains(filtered, "ookla") {
		t.Fatal("默认级别下 ookla 不该进入运行集")
	}
}

func TestValidateRejectsModulesBeyondExposure(t *testing.T) {
	cfg, err := Defaults(ProfileQuick)
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

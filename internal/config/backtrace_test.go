package config

import (
	"reflect"
	"strings"
	"testing"

	"ecs/internal/i18n"
)

func TestBacktraceCitySelectionAndTargets(t *testing.T) {
	useEnglish(t)
	cityOrder := BacktraceCityOrder()
	cases := []struct {
		name   string
		raw    string
		want   []string
		marker string
	}{
		{name: "default", raw: "", want: []string{"beijing", "guangzhou"}},
		{name: "selected", raw: "shanghai", want: []string{"shanghai"}},
		{name: "all", raw: "all", want: cityOrder},
		{name: "unknown", raw: "unknown", marker: "unknown backtrace city"},
		{name: "all combination", raw: "all,beijing", marker: "cannot be combined"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseBacktraceCities(test.raw)
			if test.marker != "" {
				requireError(t, err, test.marker)
				return
			}
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseBacktraceCities(%q) = %v, %v", test.raw, got, err)
			}
		})
	}
	targets := BacktraceTargetsFor([]string{"chengdu", "beijing"})
	var wantTargets []Endpoint
	for _, city := range backtraceCities {
		if city.ID == "beijing" || city.ID == "chengdu" {
			wantTargets = append(wantTargets, city.Targets...)
		}
	}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("BacktraceTargetsFor reverse selection = %+v, want canonical %v", targets, wantTargets)
	}
	all := BacktraceTargetsFor(cityOrder)
	if len(all) != 24 {
		t.Fatalf("built-in backtrace target count = %d, want 24", len(all))
	}
	for _, target := range all {
		if !strings.HasPrefix(target.Name, "probe.backtrace.target.") || !validBacktraceCarrier(target.Kind) {
			t.Fatalf("built-in target is not machine-shaped: %+v", target)
		}
		for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
			if !i18n.Has(language, target.Name) {
				t.Fatalf("%s target catalog key missing: %s", language, target.Name)
			}
		}
	}
}

func TestBacktraceDefaultSelectionUsesCanonicalCatalog(t *testing.T) {
	useEnglish(t)
	cities, err := ParseBacktraceCities("")
	if err != nil {
		t.Fatalf("ParseBacktraceCities(\"\") failed: %v", err)
	}
	wantCities := []string{"beijing", "guangzhou"}
	if !reflect.DeepEqual(cities, wantCities) {
		t.Fatalf("default backtrace cities = %v, want %v", cities, wantCities)
	}

	var wantTargets []Endpoint
	for _, city := range backtraceCities {
		if contains(cities, city.ID) {
			wantTargets = append(wantTargets, city.Targets...)
		}
	}
	gotTargets := BacktraceTargetsFor(cities)
	if !reflect.DeepEqual(gotTargets, wantTargets) {
		t.Fatalf("default backtrace targets = %+v, want canonical %+v", gotTargets, wantTargets)
	}
}

func TestBacktraceCityCatalogEntriesStaySelectable(t *testing.T) {
	for _, city := range backtraceCities {
		selected, err := ParseBacktraceCities(city.ID)
		if err != nil {
			t.Fatalf("ParseBacktraceCities(%q) failed: %v", city.ID, err)
		}
		targets := BacktraceTargetsFor(selected)
		for _, want := range city.Targets {
			found := false
			for _, got := range targets {
				if reflect.DeepEqual(got, want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("city %q target missing after selection: %+v", city.ID, want)
			}
		}
	}
}

func TestBacktraceCityOrderReturnsCopy(t *testing.T) {
	original := BacktraceCityOrder()
	if len(original) == 0 {
		t.Fatal("backtrace city order must not be empty")
	}
	mutated := append([]string(nil), original...)
	mutated[0] = "mutated"
	got := BacktraceCityOrder()
	if !reflect.DeepEqual(got, original) || reflect.DeepEqual(got, mutated) {
		t.Fatalf("BacktraceCityOrder returned mutable canonical data: %v", got)
	}
}

func TestBacktraceTargetsForReturnsCopy(t *testing.T) {
	targets := BacktraceTargetsFor([]string{"beijing"})
	if len(targets) == 0 {
		t.Fatal("backtrace targets must not be empty")
	}
	want := append([]Endpoint(nil), targets...)
	targets[0].Name = "mutated"
	got := BacktraceTargetsFor([]string{"beijing"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BacktraceTargetsFor returned mutable canonical data: %v", got)
	}
}

func TestBacktraceCarrierValidationAndCustomNames(t *testing.T) {
	original := i18n.Current()
	t.Cleanup(func() { i18n.Set(original) })
	for _, test := range []struct {
		language i18n.Lang
		marker   string
	}{
		{language: i18n.LangZH, marker: "kind"},
		{language: i18n.LangEN, marker: "backtrace target kind"},
	} {
		i18n.Set(test.language)
		for _, kind := range []string{"", "carrier"} {
			runtime := validRuntime(t)
			runtime.BacktraceTargets = []Endpoint{{Name: "custom 汉字", Address: "1.1.1.1", Kind: kind}}
			if err := Validate(runtime); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("%s kind %q validation error = %v", test.language, kind, err)
			}
		}
		runtime := validRuntime(t)
		runtime.BacktraceTargets = []Endpoint{{Name: "custom 汉字", Address: "1.1.1.1", Kind: BacktraceCarrierTelecom}}
		if err := Validate(runtime); err != nil {
			t.Fatalf("%s custom target with valid carrier rejected: %v", test.language, err)
		}
	}
}

func TestParseBacktraceTargetListRequiresExplicitMachineCarrier(t *testing.T) {
	original := i18n.Current()
	t.Cleanup(func() { i18n.Set(original) })
	i18n.Set(i18n.LangEN)
	targets, err := ParseBacktraceTargetList("telecom:Shanghai Telecom=202.96.209.133,unicom:IPv6 target=[2001:db8::1],mobile:Mobile=mobile.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []Endpoint{
		{Name: "Shanghai Telecom", Address: "202.96.209.133", Kind: BacktraceCarrierTelecom, Family: IPVersion4},
		{Name: "IPv6 target", Address: "[2001:db8::1]", Kind: BacktraceCarrierUnicom, Family: IPVersion6},
		{Name: "Mobile", Address: "mobile.example", Kind: BacktraceCarrierMobile},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("ParseBacktraceTargetList = %+v, want %+v", targets, want)
	}

	cases := []struct {
		name   string
		raw    string
		marker string
	}{
		{name: "legacy carrierless", raw: "Shanghai Telecom=202.96.209.133", marker: "carrier:Name=host"},
		{name: "invalid carrier", raw: "china:Shanghai Telecom=202.96.209.133", marker: "invalid carrier"},
		{name: "missing name", raw: "telecom:=202.96.209.133", marker: "carrier:Name=host"},
		{name: "missing address", raw: "telecom:Shanghai Telecom=", marker: "carrier:Name=host"},
		{name: "unsafe address", raw: "telecom:Shanghai Telecom=bad host", marker: "not a safe IP or hostname"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseBacktraceTargetList(test.raw); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("ParseBacktraceTargetList(%q) = %v, want %q", test.raw, err, test.marker)
			}
		})
	}

	i18n.Set(i18n.LangZH)
	if _, err := ParseBacktraceTargetList("Shanghai Telecom=202.96.209.133"); err == nil || !strings.Contains(err.Error(), "carrier:名称=主机") {
		t.Fatalf("Chinese carrierless error = %v", err)
	}
}

func TestMediaAndBacktraceConfigurationHelpers(t *testing.T) {
	useEnglish(t)
	if err := ValidateMediaRegions([]string{"global", "jp", "cn"}); err != nil {
		t.Fatal(err)
	}
	requireError(t, ValidateMediaRegions([]string{"moon"}), "unknown streaming region")
}

func TestParseOoklaServerList(t *testing.T) {
	useEnglish(t)
	servers, err := ParseOoklaServerList("telecom=1,cu=2,mobile=3,")
	if err != nil || len(servers) != 3 || servers[0].Carrier != OoklaCarrierTelecom || servers[1].Carrier != OoklaCarrierUnicom || servers[2].Carrier != OoklaCarrierMobile || servers[0].ID != 1 || servers[1].ID != 2 || servers[2].ID != 3 {
		t.Fatalf("valid Ookla list = %+v, %v", servers, err)
	}
	for _, test := range []struct {
		alias, want string
	}{
		{alias: "电信", want: OoklaCarrierTelecom}, {alias: "telecom", want: OoklaCarrierTelecom}, {alias: "ct", want: OoklaCarrierTelecom}, {alias: "chinatelecom", want: OoklaCarrierTelecom},
		{alias: "联通", want: OoklaCarrierUnicom}, {alias: "unicom", want: OoklaCarrierUnicom}, {alias: "cu", want: OoklaCarrierUnicom}, {alias: "chinaunicom", want: OoklaCarrierUnicom},
		{alias: "移动", want: OoklaCarrierMobile}, {alias: "mobile", want: OoklaCarrierMobile}, {alias: "cm", want: OoklaCarrierMobile}, {alias: "chinamobile", want: OoklaCarrierMobile},
	} {
		servers, err := ParseOoklaServerList(test.alias + "=42")
		if err != nil || len(servers) != 1 || servers[0].Carrier != test.want {
			t.Errorf("ParseOoklaServerList(%q) = %+v, %v; want carrier %q", test.alias, servers, err, test.want)
		}
	}
	for _, test := range []struct {
		raw, marker string
	}{
		{raw: "telecom", marker: "carrier=server-id"},
		{raw: "other=1", marker: "unknown Ookla carrier"},
		{raw: "telecom=nope", marker: "invalid"},
		{raw: "telecom=1,ct=2", marker: "duplicate"},
	} {
		_, err := ParseOoklaServerList(test.raw)
		if err == nil || !strings.Contains(err.Error(), test.marker) {
			t.Errorf("ParseOoklaServerList(%q) = %v, want %q", test.raw, err, test.marker)
		}
	}
}

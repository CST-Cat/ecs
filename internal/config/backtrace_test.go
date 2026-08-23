package config

import (
	"reflect"
	"strings"
	"testing"

	"ecs/internal/i18n"
)

func TestBacktraceCitySelectionAndTargets(t *testing.T) {
	useEnglish(t)
	cases := []struct {
		name   string
		raw    string
		want   []string
		marker string
	}{
		{name: "default", raw: "", want: defaultBacktraceCities},
		{name: "selected", raw: "shanghai", want: []string{"shanghai"}},
		{name: "all", raw: "all", want: BacktraceCityOrder},
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
	wantTargets := append(append([]Endpoint(nil), backtraceCityTargets["beijing"]...), backtraceCityTargets["chengdu"]...)
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("BacktraceTargetsFor reverse selection = %+v, want canonical %v", targets, wantTargets)
	}
	all := BacktraceTargetsFor(BacktraceCityOrder)
	if len(all) != 24 {
		t.Fatalf("built-in backtrace target count = %d, want 24", len(all))
	}
	for _, target := range all {
		if !strings.HasPrefix(target.Name, "probe.backtrace.target.") || !ValidBacktraceCarrier(target.Kind) {
			t.Fatalf("built-in target is not machine-shaped: %+v", target)
		}
		for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
			if !i18n.Has(language, target.Name) {
				t.Fatalf("%s target catalog key missing: %s", language, target.Name)
			}
		}
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
	if err != nil || len(servers) != 3 || servers[0].Carrier != "电信" || servers[1].Carrier != "联通" || servers[2].Carrier != "移动" || servers[0].ID != 1 || servers[1].ID != 2 || servers[2].ID != 3 {
		t.Fatalf("valid Ookla list = %+v, %v", servers, err)
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

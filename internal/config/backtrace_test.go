package config

import (
	"reflect"
	"strings"
	"testing"
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

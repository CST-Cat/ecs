package carrier

import "testing"

func TestParseCanonicalAliases(t *testing.T) {
	for _, test := range []struct {
		alias string
		want  ID
	}{
		{alias: "电信", want: Telecom}, {alias: "中国电信", want: Telecom}, {alias: "telecom", want: Telecom}, {alias: "ct", want: Telecom}, {alias: "chinatelecom", want: Telecom}, {alias: "China Telecom", want: Telecom},
		{alias: "联通", want: Unicom}, {alias: "中国联通", want: Unicom}, {alias: "unicom", want: Unicom}, {alias: "cu", want: Unicom}, {alias: "chinaunicom", want: Unicom}, {alias: "China Unicom", want: Unicom},
		{alias: "移动", want: Mobile}, {alias: "中国移动", want: Mobile}, {alias: "mobile", want: Mobile}, {alias: "cm", want: Mobile}, {alias: "chinamobile", want: Mobile}, {alias: "China Mobile", want: Mobile},
	} {
		got, ok := Parse(test.alias)
		if !ok || got != test.want {
			t.Errorf("Parse(%q) = %q, %v; want %q, true", test.alias, got, ok, test.want)
		}
	}
}

func TestParseLeavesUnknownOutsideCanonicalSet(t *testing.T) {
	for _, raw := range []string{"", "auto", "provider/unknown", "中国电网"} {
		got, ok := Parse(raw)
		if ok || got == Telecom || got == Unicom || got == Mobile {
			t.Errorf("Parse(%q) = %q, %v; want unknown", raw, got, ok)
		}
	}
}

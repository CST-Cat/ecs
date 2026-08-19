package probe

import "testing"

func TestPTRFixtureProvidesHintAndBasicVerdict(t *testing.T) {
	if hits := matchResidentialHints("pppoe-dynamic.example.net"); len(hits) == 0 {
		t.Fatal("residential PTR hint was not recognized")
	}
	confirmed := rdnsResult{
		IP: "203.0.113.1", Names: []string{"mail.example.net."},
		Forward: []string{"203.0.113.1"}, Confirmed: true,
	}
	if !confirmed.Confirmed || len(confirmed.Names) != 1 || len(confirmed.Forward) != 1 {
		t.Fatalf("PTR fixture verdict = %+v", confirmed)
	}
}

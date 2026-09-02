package module

import "testing"

func TestExposureStringAndValidity(t *testing.T) {
	cases := []struct {
		level Exposure
		name  string
	}{
		{level: ExposureLocal, name: "local"},
		{level: ExposurePublic, name: "public"},
		{level: ExposureThirdParty, name: "thirdparty"},
		{level: ExposureConsent, name: "any"},
	}
	for _, test := range cases {
		if !test.level.Valid() || test.level.String() != test.name {
			t.Errorf("exposure %d = valid:%v/string:%q, want valid/%q", test.level, test.level.Valid(), test.level.String(), test.name)
		}
	}
	for _, invalid := range []Exposure{-1, 4, 99} {
		if invalid.Valid() || invalid.String() != "invalid" {
			t.Errorf("invalid exposure %d = valid:%v/string:%q, want invalid/invalid", invalid, invalid.Valid(), invalid.String())
		}
	}
}

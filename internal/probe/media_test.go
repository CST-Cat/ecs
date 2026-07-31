package probe

import (
	"errors"
	"strings"
	"testing"
)

// Netflix 的三种典型情形必须区分开，这是旧实现最大的误报来源。
func TestNetflixDistinguishesOriginalsOnly(t *testing.T) {
	check := netflixCheck()

	unlocked := check.Decide([]mediaResponse{
		{Status: 200, Body: `{"requestCountry":{"id":"JP"}}`},
		{Status: 200},
	})
	if unlocked.State != stateUnlocked || unlocked.Region != "JP" {
		t.Fatalf("unlocked verdict = %+v", unlocked)
	}

	originals := check.Decide([]mediaResponse{
		{Status: 404, Body: `{"requestCountry":{"id":"SG"}}`},
		{Status: 404},
	})
	if originals.State != stateOriginals || originals.Region != "SG" {
		t.Fatalf("originals verdict = %+v", originals)
	}

	// 403 可能是反爬，绝不能判成不解锁。
	forbidden := check.Decide([]mediaResponse{{Status: 403}, {Status: 403}})
	if forbidden.State != stateUnknown {
		t.Fatalf("403 verdict = %+v, want unknown", forbidden)
	}

	failed := check.Decide([]mediaResponse{
		{Err: errors.New("dial timeout")},
		{Err: errors.New("dial timeout")},
	})
	if failed.State != stateUnknown {
		t.Fatalf("failed verdict = %+v, want unknown", failed)
	}
}

func TestYouTubePremiumReadsCountryNotice(t *testing.T) {
	check := youtubePremiumCheck()

	locked := check.Decide([]mediaResponse{{
		Status: 200,
		Body:   `<div>Premium is not available in your country</div>{"countryCode":"CN"}`,
	}})
	if locked.State != stateLocked || locked.Region != "CN" {
		t.Fatalf("locked verdict = %+v", locked)
	}

	unlocked := check.Decide([]mediaResponse{{Status: 200, Body: `{"countryCode":"US"}`}})
	if unlocked.State != stateUnlocked || unlocked.Region != "US" {
		t.Fatalf("unlocked verdict = %+v", unlocked)
	}
}

func TestChatGPTUsesUnsupportedCountrySignal(t *testing.T) {
	check := chatGPTCheck()

	locked := check.Decide([]mediaResponse{
		{Status: 200, Body: "fl=abc\nloc=CN\ntls=TLSv1.3\n"},
		{Status: 403, Body: `{"error":{"code":"unsupported_country"}}`},
	})
	if locked.State != stateLocked || locked.Region != "CN" {
		t.Fatalf("locked verdict = %+v", locked)
	}

	// 没有明确的 unsupported_country 时，403 只能判未知。
	ambiguous := check.Decide([]mediaResponse{
		{Status: 200, Body: "loc=DE\n"},
		{Status: 403, Body: "just a cloudflare challenge"},
	})
	if ambiguous.State != stateUnknown || ambiguous.Region != "DE" {
		t.Fatalf("ambiguous verdict = %+v", ambiguous)
	}
}

func TestHTTPFallbackNeverTurns403IntoLocked(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{401, stateNeedLogin},
		{403, stateUnknown},
		// 通用规则只请求首页，404 说明入口变了而不是地区不可用。
		{404, stateUnknown},
		{406, stateUnknown},
		{429, stateUnknown},
		{451, stateRestricted},
		{500, stateUnknown},
		{302, stateUnknown},
		{0, stateUnknown},
	}
	for _, testCase := range cases {
		got := httpFallbackVerdict(mediaResponse{Status: testCase.status}, "")
		if got.State != testCase.want {
			t.Fatalf("status %d = %q, want %q", testCase.status, got.State, testCase.want)
		}
	}
}

func TestNormalizeCountryRejectsPathNoise(t *testing.T) {
	// 实测中这些是 URL 路径噪声，不是国家码。
	for _, invalid := range []string{"KU", "EU", "ku", "", "XX", "ZZ"} {
		if got := normalizeCountry(invalid); got != "" {
			t.Fatalf("normalizeCountry(%q) = %q, want empty", invalid, got)
		}
	}
	for input, want := range map[string]string{"jp": "JP", "US": "US", " hk ": "HK"} {
		if got := normalizeCountry(input); got != want {
			t.Fatalf("normalizeCountry(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMediaChecksAreWellFormed(t *testing.T) {
	checks := mediaChecks()
	if len(checks) < 30 {
		t.Fatalf("expected at least 30 platform rules, got %d", len(checks))
	}
	seen := make(map[string]bool)
	for _, check := range checks {
		if check.Name == "" || check.Category == "" {
			t.Fatalf("rule missing name or category: %+v", check)
		}
		if seen[check.Name] {
			t.Fatalf("duplicate rule for %q", check.Name)
		}
		seen[check.Name] = true
		if len(check.Requests) == 0 {
			t.Fatalf("rule %q declares no request", check.Name)
		}
		for _, request := range check.Requests {
			if !strings.HasPrefix(request.URL, "https://") {
				t.Fatalf("rule %q must use https: %s", check.Name, request.URL)
			}
		}
		if check.Decide == nil {
			t.Fatalf("rule %q has no decision function", check.Name)
		}
		// 每条规则在完全没有响应内容时都必须给出保守结论，不能 panic。
		verdict := check.Decide(make([]mediaResponse, len(check.Requests)))
		if verdict.State == stateUnlocked {
			t.Fatalf("rule %q claims unlocked from an empty response", check.Name)
		}
	}
}

package probe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMediaRulesCoverPlatformAndHTTPVerdicts(t *testing.T) {
	cases := []struct {
		name          string
		check         mediaCheck
		responses     []mediaResponse
		state, region string
		evidence      string
	}{
		{name: "Netflix unlocked", check: netflixCheck(), responses: []mediaResponse{{Status: 200, Body: `{"requestCountry":{"id":"US"}}`}, {Status: 404}}, state: stateUnlocked, region: "US"},
		{name: "Netflix originals", check: netflixCheck(), responses: []mediaResponse{{Status: 404}, {Status: 404}}, state: stateOriginals},
		{name: "ChatGPT unsupported", check: chatGPTCheck(), responses: []mediaResponse{{Status: 200, Body: "loc=US\n"}, {Status: 403, Body: "unsupported_country"}}, state: stateLocked, region: "US"},
		{name: "ChatGPT unlocked", check: chatGPTCheck(), responses: []mediaResponse{{Status: 200, Body: "loc=GB\n"}, {Status: 200, Body: "home"}}, state: stateUnlocked, region: "GB"},
		{name: "YouTube unlocked", check: youtubePremiumCheck(), responses: []mediaResponse{{Status: 200, Body: `{"countryCode":"US","premium":"available"}`}}, state: stateUnlocked, region: "US"},
		{name: "YouTube locked", check: youtubePremiumCheck(), responses: []mediaResponse{{Status: 200, Body: `{"countryCode":"US","message":"Premium is not available in your country"}`}}, state: stateLocked, region: "US"},
		{name: "TikTok region", check: tiktokCheck(), responses: []mediaResponse{{Status: 200, Body: `{"region":"JP"}`}}, state: stateUnlocked, region: "JP"},
		{name: "TikTok missing region", check: tiktokCheck(), responses: []mediaResponse{{Status: 200, Body: "ok"}}, state: stateUnknown, evidence: "缺少地区信号"},
	}
	for _, test := range cases {
		verdict := test.check.Decide(test.responses)
		if verdict.State != test.state || verdict.Region != test.region || (test.evidence != "" && !strings.Contains(verdict.Evidence, test.evidence)) {
			t.Errorf("%s verdict = %+v", test.name, verdict)
		}
	}
	generic := genericChecks()[0].Decide([]mediaResponse{{Status: 200, Body: `{"countryCode":"GB"}`}})
	if generic.State != stateUnlocked || generic.Region != "GB" {
		t.Fatalf("generic 2xx verdict = %+v", generic)
	}
	for _, test := range []struct {
		name            string
		response        mediaResponse
		state, evidence string
	}{
		{name: "login", response: mediaResponse{Status: 401}, state: stateNeedLogin, evidence: "401"},
		{name: "anti bot", response: mediaResponse{Status: 403}, state: stateUnknown, evidence: "403"},
		{name: "missing entry", response: mediaResponse{Status: 404}, state: stateUnknown, evidence: "404"},
		{name: "rate limit", response: mediaResponse{Status: 429}, state: stateUnknown, evidence: "429"},
		{name: "legal restriction", response: mediaResponse{Status: 451}, state: stateRestricted, evidence: "451"},
		{name: "server failure", response: mediaResponse{Status: 500}, state: stateUnknown, evidence: "服务端错误"},
		{name: "network", response: mediaResponse{Err: errors.New("fixture network")}, state: stateUnknown, evidence: "fixture network"},
	} {
		verdict := httpFallbackVerdict(test.response, "GB")
		if verdict.State != test.state || !strings.Contains(verdict.Evidence, test.evidence) {
			t.Errorf("%s verdict = %+v", test.name, verdict)
		}
	}
	selected := mediaChecksForRegions([]string{"jp"})
	if len(selected) == 0 {
		t.Fatalf("regional media selection = %+v", selected)
	}
	for _, check := range selected {
		if check.Category.Key != mediaCategoryJapan.Key {
			t.Fatalf("regional media selection = %+v", selected)
		}
	}
	var seen []*http.Request
	largeBody := strings.Repeat("x", 512*1024+1)
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request)
		body := largeBody
		if len(seen) > 1 {
			body = "ok"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: request, Header: make(http.Header)}, nil
	})}
	env := Environment{HTTPClient: client, UserAgent: "fixture"}
	check := mediaCheck{
		Requests: []mediaRequest{
			{URL: "https://fixture.invalid/large", Headers: map[string]string{"X-Fixture": "yes"}},
			{URL: "https://fixture.invalid/decide"},
		},
		Decide: func(responses []mediaResponse) mediaVerdict {
			if len(responses) != 2 || len(responses[0].Body) != 512*1024 || !responses[1].OK() {
				return mediaVerdict{State: stateUnknown, Evidence: "fixture response mismatch"}
			}
			return mediaVerdict{State: stateUnlocked, Evidence: "responses received"}
		},
	}
	result := runMediaCheck(context.Background(), env, check)
	if result.Verdict.State != stateUnlocked || len(result.Statuses) != 2 || result.Statuses[0] != 200 || len(seen) != 2 {
		t.Fatalf("media check result = %+v, requests=%d", result, len(seen))
	}
	if seen[0].Header.Get("User-Agent") != "fixture" || seen[0].Header.Get("X-Fixture") != "yes" {
		t.Fatalf("media request headers = %+v", seen[0].Header)
	}
	if empty := runMediaCheck(context.Background(), env, mediaCheck{}); empty.Verdict.State != stateUnknown || empty.Verdict.Evidence != "规则未声明请求" {
		t.Fatalf("empty media check = %+v", empty)
	}
}

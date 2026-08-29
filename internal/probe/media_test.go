package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/report"
	"ecs/internal/termcolor"
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
		{name: "TikTok missing region", check: tiktokCheck(), responses: []mediaResponse{{Status: 200, Body: "ok"}}, state: stateUnknown, evidence: mediaEvidenceMissingRegion},
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
		{name: "login", response: mediaResponse{Status: 401}, state: stateNeedLogin, evidence: mediaEvidenceLoginRequired},
		{name: "anti bot", response: mediaResponse{Status: 403}, state: stateUnknown, evidence: mediaEvidenceForbiddenAmbiguous},
		{name: "missing entry", response: mediaResponse{Status: 404}, state: stateUnknown, evidence: mediaEvidenceChangedEntry},
		{name: "rate limit", response: mediaResponse{Status: 429}, state: stateUnknown, evidence: mediaEvidenceHTTPRejected},
		{name: "legal restriction", response: mediaResponse{Status: 451}, state: stateRestricted, evidence: mediaEvidenceLegalRestriction},
		{name: "server failure", response: mediaResponse{Status: 500}, state: stateUnknown, evidence: mediaEvidenceServerError},
		{name: "network", response: mediaResponse{Err: errors.New("fixture network")}, state: stateUnknown, evidence: mediaEvidenceTransportError},
	} {
		verdict := httpFallbackVerdict(test.response, "GB")
		if verdict.State != test.state || verdict.Evidence != test.evidence {
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
				return mediaVerdict{State: stateUnknown, Evidence: mediaEvidenceUnknownPattern}
			}
			return mediaVerdict{State: stateUnlocked, Evidence: mediaEvidenceAvailable}
		},
	}
	result := runMediaCheck(context.Background(), env, check)
	if result.Verdict.State != stateUnlocked || len(result.Statuses) != 2 || result.Statuses[0] != 200 || len(seen) != 2 {
		t.Fatalf("media check result = %+v, requests=%d", result, len(seen))
	}
	if seen[0].Header.Get("User-Agent") != "fixture" || seen[0].Header.Get("X-Fixture") != "yes" {
		t.Fatalf("media request headers = %+v", seen[0].Header)
	}
	if empty := runMediaCheck(context.Background(), env, mediaCheck{}); empty.Verdict.State != stateUnknown || empty.Verdict.Evidence != mediaEvidenceMissingRequests {
		t.Fatalf("empty media check = %+v", empty)
	}
}

func TestMediaRegionOrderReturnsCopy(t *testing.T) {
	original := MediaRegionOrder()
	want := []string{"global", "jp", "tw", "hk", "cn"}
	if !reflect.DeepEqual(original, want) {
		t.Fatalf("MediaRegionOrder = %v, want %v", original, want)
	}
	mutated := append([]string(nil), original...)
	if len(mutated) == 0 {
		t.Fatal("media region order must not be empty")
	}
	mutated[0] = "mutated"
	got := MediaRegionOrder()
	if !reflect.DeepEqual(got, original) || reflect.DeepEqual(got, mutated) {
		t.Fatalf("MediaRegionOrder returned mutable canonical data: %v", got)
	}
}

func TestMediaFallbackPreservesFiniteEvidence(t *testing.T) {
	cases := []struct {
		name     string
		response mediaResponse
		state    string
		evidence string
	}{
		{name: "login", response: mediaResponse{Status: 401}, state: stateNeedLogin, evidence: mediaEvidenceLoginRequired},
		{name: "forbidden", response: mediaResponse{Status: 403}, state: stateUnknown, evidence: mediaEvidenceForbiddenAmbiguous},
		{name: "changed entry", response: mediaResponse{Status: 404}, state: stateUnknown, evidence: mediaEvidenceChangedEntry},
		{name: "rate limit", response: mediaResponse{Status: 429}, state: stateUnknown, evidence: mediaEvidenceHTTPRejected},
		{name: "legal restriction", response: mediaResponse{Status: 451}, state: stateRestricted, evidence: mediaEvidenceLegalRestriction},
		{name: "server failure", response: mediaResponse{Status: 503}, state: stateUnknown, evidence: mediaEvidenceServerError},
		{name: "redirect limit", response: mediaResponse{Status: 302}, state: stateUnknown, evidence: mediaEvidenceRedirectLimit},
		{name: "no response", response: mediaResponse{}, state: stateUnknown, evidence: mediaEvidenceNoResponse},
		{name: "unreachable status", response: mediaResponse{Status: 101}, state: stateUnreachable, evidence: mediaEvidenceUnreachable},
		{name: "transport", response: mediaResponse{Err: errors.New("fixture transport")}, state: stateUnknown, evidence: mediaEvidenceTransportError},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			verdict := httpFallbackVerdict(test.response, "GB")
			if verdict.State != test.state || verdict.Evidence != test.evidence {
				t.Fatalf("fallback verdict = %+v", verdict)
			}
		})
	}
}

type mediaDeadlineError struct{ message string }

func (err mediaDeadlineError) Error() string   { return err.message }
func (err mediaDeadlineError) Timeout() bool   { return true }
func (err mediaDeadlineError) Temporary() bool { return true }
func (err mediaDeadlineError) Unwrap() error   { return context.DeadlineExceeded }

func TestMediaPreservesTypedRequestErrorsAcrossMixedVerdicts(t *testing.T) {
	deadline := mediaDeadlineError{message: "fixture deadline " + strings.Repeat("x", 180)}
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() == "www.peacocktv.com" || strings.Contains(request.URL.Path, "70143836") {
			return nil, deadline
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"countryCode":"US"}`)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	env := Environment{Config: config.Runtime{MediaRegions: []string{"global"}}, HTTPClient: client, UserAgent: "media-error-fixture"}
	wantMessages := make(map[string]string)
	for _, request := range []mediaRequest{
		{URL: "https://www.netflix.com/title/70143836"},
		{URL: "https://www.peacocktv.com/"},
	} {
		response := performMediaRequest(context.Background(), env, request)
		if response.Err == nil {
			t.Fatalf("fixture request unexpectedly succeeded: %s", request.URL)
		}
		wantMessages[request.URL] = response.Err.Error()
	}
	result := (mediaProbe{}).Run(context.Background(), env)
	if result.Status != model.StatusOK {
		t.Fatalf("mixed transport errors changed partial status: %s", result.Status)
	}
	seen := make(map[string]int)
	for _, failure := range result.Failures {
		if failure.Target != "netflix/request_2" && failure.Target != "peacock/request_1" {
			t.Fatalf("unexpected media failure target: %+v", failure)
		}
		seen[failure.Target]++
		if len(failure.Message) <= 100 || failure.Category != model.FailureTimeout || !failure.Retryable {
			t.Fatalf("typed timeout failure was compacted or misclassified: %+v", failure)
		}
		var url string
		switch failure.Target {
		case "netflix/request_2":
			url = "https://www.netflix.com/title/70143836"
		case "peacock/request_1":
			url = "https://www.peacocktv.com/"
		}
		if failure.Message != wantMessages[url] {
			t.Fatalf("failure message changed: target=%s got=%q want=%q", failure.Target, failure.Message, wantMessages[url])
		}
	}
	if !reflect.DeepEqual(seen, map[string]int{"netflix/request_2": 1, "peacock/request_1": 1}) {
		t.Fatalf("transport errors were duplicated or dropped: %v", seen)
	}
	for _, table := range result.Tables {
		for _, row := range table.Rows {
			if row[0].Text() == mediaPlatformNameKey("netflix") && row[1].Text() != stateUnlocked {
				t.Fatalf("mixed Netflix success/error was not retained as available: %#v", row)
			}
		}
	}
}

func TestMediaVerdictCountsAndWarningThreshold(t *testing.T) {
	states := []string{stateUnlocked, stateOriginals, stateLocked, stateNeedLogin, stateRestricted, stateUnknown, stateUnreachable}
	results := make([]mediaResult, 0, len(states))
	for _, state := range states {
		results = append(results, mediaResult{Verdict: mediaVerdict{State: state}})
	}
	unlocked, locked, unknown := mediaVerdictCounts(results)
	if unlocked != 2 || locked != 3 || unknown != 2 || unlocked+locked+unknown != len(states) {
		t.Fatalf("finite media verdict buckets = %d/%d/%d", unlocked, locked, unknown)
	}

	allUnknownClient := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	result := (mediaProbe{}).Run(context.Background(), Environment{
		Config: config.Runtime{MediaRegions: []string{"global"}}, HTTPClient: allUnknownClient, UserAgent: "media-all-unknown",
	})
	if result.Status != model.StatusWarning || result.SummaryMessages[0].Args[0] != "0" || result.SummaryMessages[0].Args[2] != "0" || result.SummaryMessages[0].Args[3] != "20" {
		t.Fatalf("all-unknown media status/summary = %s/%v", result.Status, result.SummaryMessages)
	}
}

func TestMediaProducerEmitsMachineSemanticsAndLocalizedRenderers(t *testing.T) {
	originalLanguage := i18n.Current()
	t.Cleanup(func() { i18n.Set(originalLanguage) })
	client := &http.Client{Transport: fixtureRoundTripper(func(request *http.Request) (*http.Response, error) {
		host := request.URL.Hostname()
		body := `{"countryCode":"GB"}`
		status := http.StatusOK
		switch host {
		case "www.netflix.com":
			status = http.StatusNotFound
		case "www.youtube.com":
			body = `{"countryCode":"US","message":"Premium is not available in your country"}`
		case "chatgpt.com":
			if strings.Contains(request.URL.Path, "trace") {
				body = "loc=US\n"
			} else {
				status = http.StatusForbidden
				body = "anti-bot fixture"
			}
		case "www.tiktok.com":
			body = `{"region":"US"}`
		case "www.disneyplus.com":
			status = 451
		case "www.primevideo.com":
			status = http.StatusUnauthorized
		case "www.max.com":
			status = http.StatusNotFound
		case "www.hulu.com":
			status = http.StatusTooManyRequests
		case "www.paramountplus.com":
			status = http.StatusInternalServerError
		case "www.peacocktv.com":
			return nil, errors.New("fixture transport")
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	env := Environment{
		Config:     config.Runtime{MediaRegions: []string{"global"}},
		HTTPClient: client,
		UserAgent:  "media-fixture",
	}
	result := (mediaProbe{}).Run(context.Background(), env)
	if result.Title != "module.media.title" || result.Description != "probe.media.description" || len(result.SummaryMessages) != 1 {
		t.Fatalf("media result presentation contract = %+v", result)
	}
	if result.SummaryMessages[0].Key != "probe.media.summary.values" || len(result.SummaryMessages[0].Args) != 4 {
		t.Fatalf("media summary message = %+v", result.SummaryMessages)
	}
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Expected != len(mediaChecksForRegions([]string{"global"})) {
		t.Fatalf("media status/evidence = %s/%+v", result.Status, result.Evidence)
	}
	if got, want := result.SummaryMessages[0].Args, []string{"12", "20", "2", "6"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("media summary counts = %v, want %v", got, want)
	}
	if result.Measurements[0].Display.Text() != "12/20" || result.Measurements[1].Display.Text() != "6/20" || result.Evidence.Valid != 14 {
		t.Fatalf("media count/evidence consistency = measurements=%+v evidence=%+v", result.Measurements, result.Evidence)
	}
	if result.Methodology.Parameters["scope_revision"] != "1" || result.Methodology.Parameters["regions_sha256"] == "" || result.Measurements[0].Method != "media-rules-"+mediaRulesVersion {
		t.Fatalf("media method metadata = methodology=%+v measurement=%+v", result.Methodology, result.Measurements[0])
	}
	finiteStates := map[string]bool{
		stateUnlocked: true, stateOriginals: true, stateLocked: true, stateNeedLogin: true,
		stateRestricted: true, stateUnknown: true, stateUnreachable: true,
	}
	finiteEvidence := map[string]bool{
		mediaEvidenceAvailable: true, mediaEvidenceOriginalsOnly: true, mediaEvidenceCountryRestriction: true,
		mediaEvidenceUnsupportedCountry: true, mediaEvidenceLoginRequired: true, mediaEvidenceForbiddenAmbiguous: true,
		mediaEvidenceLegalRestriction: true, mediaEvidenceChangedEntry: true, mediaEvidenceHTTPRejected: true,
		mediaEvidenceServerError: true, mediaEvidenceRedirectLimit: true, mediaEvidenceNoResponse: true,
		mediaEvidenceTransportError: true, mediaEvidenceMissingRequests: true, mediaEvidenceRequestCount: true,
		mediaEvidenceMissingRegion: true, mediaEvidenceUnknownPattern: true, mediaEvidenceUnreachable: true,
	}
	finiteStrength := map[string]bool{string(strengthStrong): true, string(strengthWeak): true}
	for _, table := range result.Tables {
		if !strings.HasPrefix(table.Title, "probe.media.table.") || len(table.Columns) != 7 {
			t.Fatalf("media table is not machine-shaped: %+v", table)
		}
		for _, row := range table.Rows {
			if len(row) != 7 || !strings.HasPrefix(row[0].Text(), "probe.media.platform.") || !finiteStates[row[1].Text()] || !finiteEvidence[row[3].Text()] || !finiteStrength[row[4].Text()] {
				t.Fatalf("media row is not machine-shaped: %#v", row)
			}
			for _, index := range []int{0, 1, 3, 4} {
				if _, ok := row[index].Key(); !ok {
					t.Fatalf("media row cell %d is not a tagged key: %#v", index, row)
				}
			}
			for _, index := range []int{2, 5, 6} {
				if _, ok := row[index].Raw(); !ok {
					t.Fatalf("media row cell %d is not raw data: %#v", index, row)
				}
			}
		}
	}
	if len(result.Tables) != 4 {
		t.Fatalf("global media categories = %d", len(result.Tables))
	}
	var foundTransport bool
	for _, failure := range result.Failures {
		if strings.Contains(failure.Message, "fixture transport") && failure.Target == "peacock/request_1" {
			foundTransport = true
		}
		if strings.Contains(failure.Target, "probe.media.") || strings.Contains(failure.Message, "probe.media.") {
			t.Fatalf("presentation key entered canonical failure: %+v", failure)
		}
	}
	if !foundTransport {
		t.Fatalf("raw transport failure was not retained: %+v", result.Failures)
	}

	data := model.Report{
		SchemaVersion: buildinfo.SchemaVersion,
		Tool:          model.ToolInfo{Name: "ecs", Version: "fixture"},
		Run:           model.RunInfo{ID: "media-render", Profile: "fixture", Exposure: "local"},
		Summary:       model.Summary{Status: result.Status, Warnings: 1},
		Results:       []model.Result{result},
	}
	canonical, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if containsHan(string(canonical)) || strings.Contains(string(canonical), "流媒体") || strings.Contains(string(canonical), "解锁") || strings.Contains(string(canonical), "probe.media.evidence.observed") {
		t.Fatalf("canonical media output contains display prose: %s", canonical)
	}
	for _, value := range []string{result.Title, result.Description, result.Methodology.Engine, result.Methodology.Profile, result.Methodology.ComparisonScope} {
		if containsHan(value) {
			t.Fatalf("canonical ECS-owned media value contains Han: %q", value)
		}
	}
	for _, language := range []i18n.Lang{i18n.LangZH, i18n.LangEN} {
		i18n.Set(language)
		outputs := []string{
			report.Text(data, report.TextOptions{Color: termcolor.LevelNone, Width: 120}),
			report.Markdown(data, nil),
		}
		html, err := report.HTML(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, string(html))
		for _, output := range outputs {
			if strings.Contains(output, "probe.media.") || strings.Contains(output, "module.media.title") {
				t.Fatalf("%s output leaked stable media key: %s", language, output)
			}
			if strings.Contains(output, "%!") {
				t.Fatalf("%s output contains fmt diagnostic: %s", language, output)
			}
			if language == i18n.LangEN && containsHan(output) {
				t.Fatalf("English media output contains Han: %s", output)
			}
		}
	}
	after, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, after) {
		t.Fatal("media renderers mutated canonical report")
	}
}

func TestMediaRuleInventoryUsesFiniteMachineKeysAndCatalogParity(t *testing.T) {
	checks := mediaChecks()
	categories := make(map[string]bool)
	strengths := make(map[mediaEvidenceStrength]bool)
	for _, check := range checks {
		categories[check.Category.Key] = true
		strengths[check.Strength] = true
		if check.Category.Key == "" || check.ID == "" || check.Decide == nil {
			t.Fatalf("incomplete media check: %+v", check)
		}
	}
	if len(categories) != 8 || !strengths[strengthStrong] || !strengths[strengthWeak] {
		t.Fatalf("media category/strength inventory = %v/%v", categories, strengths)
	}
	keys := []string{
		"module.media.title", "probe.media.description", "probe.media.profile", "probe.media.comparison_scope",
		"probe.media.methodology.engine", "probe.media.summary.values", "probe.media.metric.unlocked", "probe.media.metric.unknown",
		"probe.media.column.platform", "probe.media.column.verdict", "probe.media.column.region", "probe.media.column.evidence",
		"probe.media.column.strength", "probe.media.column.http_status", "probe.media.column.duration",
		"probe.media.note.public_evidence", "probe.media.note.account_scope", "probe.media.note.unknown_semantics",
		stateUnlocked, stateOriginals, stateLocked, stateNeedLogin, stateRestricted, stateUnknown, stateUnreachable,
		string(strengthStrong), string(strengthWeak), mediaEvidenceAvailable, mediaEvidenceOriginalsOnly,
		mediaEvidenceCountryRestriction, mediaEvidenceUnsupportedCountry, mediaEvidenceLoginRequired,
		mediaEvidenceForbiddenAmbiguous, mediaEvidenceLegalRestriction, mediaEvidenceChangedEntry,
		mediaEvidenceHTTPRejected, mediaEvidenceServerError, mediaEvidenceRedirectLimit, mediaEvidenceNoResponse,
		mediaEvidenceTransportError, mediaEvidenceMissingRequests, mediaEvidenceRequestCount, mediaEvidenceMissingRegion,
		mediaEvidenceUnknownPattern, mediaEvidenceUnreachable,
	}
	for _, category := range []string{"streaming", "ai_services", "social", "music", "japan", "taiwan", "hong_kong", "mainland_china"} {
		keys = append(keys, "probe.media.table."+category)
	}
	for _, platform := range []string{
		"netflix", "youtube_premium", "chatgpt", "tiktok", "disney_plus", "amazon_prime_video", "hbo_max", "hulu",
		"paramount_plus", "peacock", "crunchyroll", "dazn", "claude", "gemini", "copilot", "spotify", "apple_music",
		"instagram", "x", "reddit", "abema", "niconico", "u_next", "dmm", "bahamut", "kktv", "litv", "viu",
		"now_e", "bilibili_hk", "iqiyi", "youku", "netease_music",
	} {
		keys = append(keys, "probe.media.platform."+platform)
	}
	for _, key := range keys {
		for _, language := range i18n.Supported() {
			if !i18n.Has(language, key) {
				t.Errorf("missing %s catalog key %q", language, key)
			}
		}
	}

	unknown := netflixCheck().Decide([]mediaResponse{{Status: 418}, {Status: 418}})
	if unknown.State != stateUnknown || unknown.Evidence != mediaEvidenceUnknownPattern {
		t.Fatalf("unknown media pattern = %+v", unknown)
	}
	chatGPTForbidden := chatGPTCheck().Decide([]mediaResponse{{Status: http.StatusOK, Body: "loc=US\n"}, {Status: http.StatusForbidden, Body: "anti-bot"}})
	if chatGPTForbidden.State != stateUnknown || chatGPTForbidden.Evidence != mediaEvidenceForbiddenAmbiguous {
		t.Fatalf("403 media semantics = %+v", chatGPTForbidden)
	}
}

func containsHan(value string) bool {
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

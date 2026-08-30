package probe

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// 流媒体与区域服务检测规则。
//
// 规则包版本：mediaRulesVersion。每条规则声明自己需要哪些请求，以及如何把响应
// 翻译成结论。判定原则：
//
//   - 只有拿到明确的正面证据才判"解锁"；
//   - 只有拿到明确的地区限制证据才判"不解锁"；
//   - 反爬、验证码、超时、限流一律判"未知"，绝不当作"不解锁"；
//   - 每条结论都带证据串，说明依据的是状态码、页面信号还是重定向。
//
// 平台页面会持续变化，规则失效是常态而不是意外。EvidenceStrength 标明该规则的
// 判定强度，弱规则只说明公开页可达性，不足以证明账号可用。

const mediaRulesVersion = "v2 (2026-07)"

// mediaCategory is only a machine identity. Presentation titles are resolved
// from the category key at the report boundary.
type mediaCategory struct {
	Key string
}

var (
	mediaCategoryStreaming     = mediaCategory{Key: "streaming"}
	mediaCategoryAIServices    = mediaCategory{Key: "ai_services"}
	mediaCategorySocial        = mediaCategory{Key: "social"}
	mediaCategoryMusic         = mediaCategory{Key: "music"}
	mediaCategoryJapan         = mediaCategory{Key: "japan"}
	mediaCategoryTaiwan        = mediaCategory{Key: "taiwan"}
	mediaCategoryHongKong      = mediaCategory{Key: "hong_kong"}
	mediaCategoryMainlandChina = mediaCategory{Key: "mainland_china"}
)

// mediaEvidenceStrength 表示一条规则的判定强度。
type mediaEvidenceStrength string

const (
	// Strength values are stable catalog keys, not display labels.
	strengthStrong mediaEvidenceStrength = "probe.media.strength.strong"
	strengthWeak   mediaEvidenceStrength = "probe.media.strength.weak"
)

// mediaRequest 是规则需要发起的一次请求。
type mediaRequest struct {
	URL string
	// Headers 覆盖默认请求头。
	Headers map[string]string
}

// mediaResponse 是一次请求的结果。
type mediaResponse struct {
	Status   int
	Body     string
	FinalURL string
	Err      error
}

// OK 表示拿到了 2xx 响应。
func (r mediaResponse) OK() bool { return r.Status >= 200 && r.Status < 300 }

// mediaVerdict 是一个平台的检测结论。
type mediaVerdict struct {
	State    string
	Region   string
	Evidence string
}

// mediaCheck 是一条平台检测规则。
type mediaCheck struct {
	// ID is a stable platform identity. Presentation derives its catalog key
	// from this value; raw failures use the ID as their target.
	ID       string
	Category mediaCategory
	Strength mediaEvidenceStrength
	Requests []mediaRequest
	// Decide 把所有响应翻译成结论。
	Decide func([]mediaResponse) mediaVerdict
}

// 结论取值。这些字符串会直接进入报告表格。
const (
	stateUnlocked    = "probe.media.verdict.unlocked"
	stateOriginals   = "probe.media.verdict.originals"
	stateLocked      = "probe.media.verdict.locked"
	stateNeedLogin   = "probe.media.verdict.login"
	stateRestricted  = "probe.media.verdict.restricted"
	stateUnknown     = "probe.media.verdict.unknown"
	stateUnreachable = "probe.media.verdict.unreachable"
)

// Evidence values are stable machine keys. HTTP status and raw transport
// errors are retained separately by mediaResult and the final Result.
const (
	mediaEvidenceAvailable          = "probe.media.evidence.available"
	mediaEvidenceOriginalsOnly      = "probe.media.evidence.originals_only"
	mediaEvidenceCountryRestriction = "probe.media.evidence.country_restriction"
	mediaEvidenceUnsupportedCountry = "probe.media.evidence.unsupported_country"
	mediaEvidenceLoginRequired      = "probe.media.evidence.login_required"
	mediaEvidenceForbiddenAmbiguous = "probe.media.evidence.forbidden_ambiguous"
	mediaEvidenceLegalRestriction   = "probe.media.evidence.legal_restriction"
	mediaEvidenceChangedEntry       = "probe.media.evidence.changed_entry"
	mediaEvidenceHTTPRejected       = "probe.media.evidence.http_rejected"
	mediaEvidenceServerError        = "probe.media.evidence.server_error"
	mediaEvidenceRedirectLimit      = "probe.media.evidence.redirect_limit"
	mediaEvidenceNoResponse         = "probe.media.evidence.no_response"
	mediaEvidenceTransportError     = "probe.media.evidence.transport_error"
	mediaEvidenceMissingRequests    = "probe.media.evidence.missing_requests"
	mediaEvidenceRequestCount       = "probe.media.evidence.request_count"
	mediaEvidenceMissingRegion      = "probe.media.evidence.missing_region"
	mediaEvidenceUnknownPattern     = "probe.media.evidence.unknown_pattern"
	mediaEvidenceUnreachable        = "probe.media.evidence.unreachable"
)

var (
	// Netflix 在页面里回传请求国别。
	netflixCountryPattern = regexp.MustCompile(`"requestCountry"\s*:\s*\{\s*"id"\s*:\s*"([A-Za-z]{2})"`)
	// YouTube 与多数 Google 前端使用 contentRegion/countryCode。
	youtubeCountryPattern = regexp.MustCompile(`"(?:countryCode|contentRegion|gl)"\s*:\s*"([A-Za-z]{2})"`)
	// TikTok 在初始状态里带 region。
	tiktokRegionPattern = regexp.MustCompile(`"region"\s*:\s*"([A-Za-z]{2})"`)
	// Cloudflare trace 端点的 loc 字段。
	cloudflareLocPattern = regexp.MustCompile(`(?m)^loc=([A-Za-z]{2})\s*$`)
	// 最终 URL 里的地区路径，如 /jp/ 或 /zh-hk/。
	urlRegionPattern = regexp.MustCompile(`/([a-z]{2})(?:-[a-z]{2})?/`)
)

// isoCountryCodes 是用于校验区域信号的 ISO 3166-1 alpha-2 子集。
//
// 不做校验会让 URL 路径里的任意两字母段被当成国家码：实测中 youku.com/ku/ 会被
// 读成 "KU"、primevideo 的 /eu/ 会被读成 "EU"。宁可少报也不能报错。
var isoCountryCodes = map[string]bool{
	"AE": true, "AR": true, "AT": true, "AU": true, "BE": true, "BR": true,
	"CA": true, "CH": true, "CL": true, "CN": true, "CO": true, "CZ": true,
	"DE": true, "DK": true, "EG": true, "ES": true, "FI": true, "FR": true,
	"GB": true, "GR": true, "HK": true, "HU": true, "ID": true, "IE": true,
	"IL": true, "IN": true, "IT": true, "JP": true, "KR": true, "MO": true,
	"MX": true, "MY": true, "NL": true, "NO": true, "NZ": true, "PH": true,
	"PL": true, "PT": true, "RO": true, "RU": true, "SA": true, "SE": true,
	"SG": true, "TH": true, "TR": true, "TW": true, "UA": true, "US": true,
	"VN": true, "ZA": true,
}

// normalizeCountry 只接受已知的国家码，其余一律丢弃。
func normalizeCountry(value string) string {
	code := strings.ToUpper(strings.TrimSpace(value))
	if isoCountryCodes[code] {
		return code
	}
	return ""
}

// mediaChecks 返回本次要执行的全部规则。
func mediaChecks() []mediaCheck {
	checks := []mediaCheck{
		netflixCheck(),
		youtubePremiumCheck(),
		chatGPTCheck(),
		tiktokCheck(),
	}
	checks = append(checks, genericChecks()...)
	return checks
}

// mediaRegionCategories 把地区选项映射到显式的规则分类描述。
//
// global 是不分地区的通用平台（流媒体、AI、社交、音乐），其余按 machine key 对应。
var mediaRegionCategories = map[string][]mediaCategory{
	"global": {mediaCategoryStreaming, mediaCategoryAIServices, mediaCategorySocial, mediaCategoryMusic},
	"jp":     {mediaCategoryJapan},
	"tw":     {mediaCategoryTaiwan},
	"hk":     {mediaCategoryHongKong},
	"cn":     {mediaCategoryMainlandChina},
}

// mediaChecksForRegions 按地区筛选规则，保持声明顺序。
func mediaChecksForRegions(regions []string) []mediaCheck {
	if len(regions) == 0 {
		return mediaChecks()
	}
	allowed := make(map[string]bool)
	for _, region := range regions {
		for _, category := range mediaRegionCategories[region] {
			allowed[category.Key] = true
		}
	}
	// Runtime validation rejects unknown regions before execution. Keep this
	// boundary defensive: an unknown-only direct caller must not expand its
	// request set to every media check.
	if len(allowed) == 0 {
		return nil
	}
	var selected []mediaCheck
	for _, check := range mediaChecks() {
		if allowed[check.Category.Key] {
			selected = append(selected, check)
		}
	}
	return selected
}

// netflixCheck 用两部非自制剧区分"完全解锁"与"仅自制剧"。
//
// Netflix 对未购买版权的地区会让非自制剧返回 404，但自制剧全球可看。只测首页
// 或单一 title 会把"仅自制剧"误报成完全解锁，这是旧实现最大的误报来源。
func netflixCheck() mediaCheck {
	return mediaCheck{
		ID:       "netflix",
		Category: mediaCategoryStreaming,
		Strength: strengthStrong,
		Requests: []mediaRequest{
			{URL: "https://www.netflix.com/title/81280792"}, // LEGO Ninjago，非自制
			{URL: "https://www.netflix.com/title/70143836"}, // Breaking Bad，非自制
		},
		Decide: func(responses []mediaResponse) mediaVerdict {
			if len(responses) < 2 {
				return mediaVerdict{State: stateUnknown, Evidence: mediaEvidenceRequestCount}
			}
			region := ""
			for _, response := range responses {
				if found := normalizeCountry(matchFirst(netflixCountryPattern, response.Body)); found != "" {
					region = found
					break
				}
			}
			for _, response := range responses {
				if response.Status == http.StatusForbidden {
					return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceForbiddenAmbiguous}
				}
				if response.Status == 451 {
					return mediaVerdict{State: stateRestricted, Region: region, Evidence: mediaEvidenceLegalRestriction}
				}
			}
			unlocked, notFound, failed := 0, 0, 0
			for _, response := range responses {
				switch {
				case response.Err != nil:
					failed++
				case response.OK():
					unlocked++
				case response.Status == http.StatusNotFound:
					notFound++
				}
			}
			switch {
			case unlocked > 0:
				return mediaVerdict{State: stateUnlocked, Region: region, Evidence: mediaEvidenceAvailable}
			case notFound >= 2:
				return mediaVerdict{State: stateOriginals, Region: region, Evidence: mediaEvidenceOriginalsOnly}
			case failed > 0:
				return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceTransportError}
			default:
				return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceUnknownPattern}
			}
		},
	}
}

// youtubePremiumCheck 依据页面上的不可用提示判定。
func youtubePremiumCheck() mediaCheck {
	return mediaCheck{
		ID:       "youtube_premium",
		Category: mediaCategoryStreaming,
		Strength: strengthStrong,
		Requests: []mediaRequest{{URL: "https://www.youtube.com/premium"}},
		Decide: func(responses []mediaResponse) mediaVerdict {
			response := responses[0]
			region := normalizeCountry(matchFirst(youtubeCountryPattern, response.Body))
			if response.Err != nil {
				return transportVerdict(region)
			}
			lower := strings.ToLower(response.Body)
			if strings.Contains(lower, "premium is not available in your country") ||
				strings.Contains(lower, "not available in your country") {
				return mediaVerdict{State: stateLocked, Region: region, Evidence: mediaEvidenceCountryRestriction}
			}
			if response.OK() {
				return mediaVerdict{State: stateUnlocked, Region: region, Evidence: mediaEvidenceAvailable}
			}
			return httpFallbackVerdict(response, region)
		},
	}
}

// chatGPTCheck 通过 Cloudflare trace 拿到 OpenAI 边缘识别到的国别。
func chatGPTCheck() mediaCheck {
	return mediaCheck{
		ID:       "chatgpt",
		Category: mediaCategoryAIServices,
		Strength: strengthStrong,
		Requests: []mediaRequest{
			{URL: "https://chatgpt.com/cdn-cgi/trace"},
			{URL: "https://chatgpt.com/"},
		},
		Decide: func(responses []mediaResponse) mediaVerdict {
			region := ""
			if len(responses) > 0 {
				region = normalizeCountry(matchFirst(cloudflareLocPattern, responses[0].Body))
			}
			if len(responses) < 2 {
				return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceRequestCount}
			}
			main := responses[1]
			switch {
			case main.Err != nil:
				return transportVerdict(region)
			case main.Status == 403:
				// OpenAI 对不支持地区返回 403 并带明确提示。
				if strings.Contains(strings.ToLower(main.Body), "unsupported_country") {
					return mediaVerdict{State: stateLocked, Region: region, Evidence: mediaEvidenceUnsupportedCountry}
				}
				return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceForbiddenAmbiguous}
			case main.OK():
				return mediaVerdict{State: stateUnlocked, Region: region, Evidence: mediaEvidenceAvailable}
			default:
				return httpFallbackVerdict(main, region)
			}
		},
	}
}

// tiktokCheck 从初始状态里读取 region。
func tiktokCheck() mediaCheck {
	return mediaCheck{
		ID:       "tiktok",
		Category: mediaCategorySocial,
		Strength: strengthStrong,
		Requests: []mediaRequest{{URL: "https://www.tiktok.com/"}},
		Decide: func(responses []mediaResponse) mediaVerdict {
			response := responses[0]
			region := normalizeCountry(matchFirst(tiktokRegionPattern, response.Body))
			if response.Err != nil {
				return transportVerdict(region)
			}
			if response.OK() && region != "" {
				return mediaVerdict{State: stateUnlocked, Region: region, Evidence: mediaEvidenceAvailable}
			}
			if response.OK() {
				return mediaVerdict{State: stateUnknown, Evidence: mediaEvidenceMissingRegion}
			}
			return httpFallbackVerdict(response, region)
		},
	}
}

// genericChecks 覆盖其余平台。
//
// 这些平台没有稳定且免登录的解锁判定接口，规则只检查公开页可达性并尽力提取
// 区域信号，因此统一标为弱证据：能打开首页不等于账号能播放。
func genericChecks() []mediaCheck {
	targets := []struct {
		name     string
		category mediaCategory
		url      string
	}{
		{"disney_plus", mediaCategoryStreaming, "https://www.disneyplus.com/"},
		{"amazon_prime_video", mediaCategoryStreaming, "https://www.primevideo.com/"},
		{"hbo_max", mediaCategoryStreaming, "https://www.max.com/"},
		{"hulu", mediaCategoryStreaming, "https://www.hulu.com/"},
		{"paramount_plus", mediaCategoryStreaming, "https://www.paramountplus.com/"},
		{"peacock", mediaCategoryStreaming, "https://www.peacocktv.com/"},
		{"crunchyroll", mediaCategoryStreaming, "https://www.crunchyroll.com/"},
		{"dazn", mediaCategoryStreaming, "https://www.dazn.com/"},

		{"claude", mediaCategoryAIServices, "https://claude.ai/"},
		{"gemini", mediaCategoryAIServices, "https://gemini.google.com/"},
		{"copilot", mediaCategoryAIServices, "https://copilot.microsoft.com/"},

		{"spotify", mediaCategoryMusic, "https://open.spotify.com/"},
		{"apple_music", mediaCategoryMusic, "https://music.apple.com/"},

		{"instagram", mediaCategorySocial, "https://www.instagram.com/"},
		{"x", mediaCategorySocial, "https://x.com/"},
		{"reddit", mediaCategorySocial, "https://www.reddit.com/"},

		{"abema", mediaCategoryJapan, "https://abema.tv/"},
		{"niconico", mediaCategoryJapan, "https://www.nicovideo.jp/"},
		{"u_next", mediaCategoryJapan, "https://video.unext.jp/"},
		{"dmm", mediaCategoryJapan, "https://www.dmm.com/"},

		{"bahamut", mediaCategoryTaiwan, "https://ani.gamer.com.tw/"},
		{"kktv", mediaCategoryTaiwan, "https://www.kktv.me/"},
		{"litv", mediaCategoryTaiwan, "https://www.litv.tv/"},

		{"viu", mediaCategoryHongKong, "https://www.viu.com/"},
		{"now_e", mediaCategoryHongKong, "https://www.nowe.com/"},
		{"bilibili_hk", mediaCategoryHongKong, "https://www.bilibili.com/"},

		{"iqiyi", mediaCategoryMainlandChina, "https://www.iq.com/"},
		{"youku", mediaCategoryMainlandChina, "https://www.youku.com/"},
		{"netease_music", mediaCategoryMainlandChina, "https://music.163.com/"},
	}
	checks := make([]mediaCheck, 0, len(targets))
	for _, target := range targets {
		checks = append(checks, mediaCheck{
			ID:       target.name,
			Category: target.category,
			Strength: strengthWeak,
			Requests: []mediaRequest{{URL: target.url}},
			Decide: func(responses []mediaResponse) mediaVerdict {
				response := responses[0]
				if response.Err != nil {
					return transportVerdict("")
				}
				region := firstNonEmpty(
					normalizeCountry(matchFirst(youtubeCountryPattern, response.Body)),
					normalizeCountry(matchFirst(urlRegionPattern, response.FinalURL)),
				)
				if response.OK() {
					return mediaVerdict{State: stateUnlocked, Region: region, Evidence: mediaEvidenceAvailable}
				}
				return httpFallbackVerdict(response, region)
			},
		})
	}
	return checks
}

// httpFallbackVerdict 把状态码翻译成保守结论。
//
// 403 一律归为未知：它既可能是地区封锁，也可能是 Cloudflare 质询或反爬，
// 把它当作"不解锁"会产生大量误报。404 同理——通用规则只请求首页，404 通常说明
// 入口 URL 变了而不是该地区不可用，实测中 Disney+ 与 Hulu 都因此被误判过。
// 只有平台规则自己能解释 404 语义时（如 Netflix 的 title 页），才在该规则内判定。
func httpFallbackVerdict(response mediaResponse, region string) mediaVerdict {
	switch {
	case response.Err != nil:
		return transportVerdict(region)
	case response.Status == http.StatusUnauthorized:
		return mediaVerdict{State: stateNeedLogin, Region: region, Evidence: mediaEvidenceLoginRequired}
	case response.Status == http.StatusForbidden:
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceForbiddenAmbiguous}
	case response.Status == 451:
		return mediaVerdict{State: stateRestricted, Region: region, Evidence: mediaEvidenceLegalRestriction}
	case response.Status == http.StatusNotFound:
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceChangedEntry}
	case response.Status >= 500:
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceServerError}
	case response.Status >= 400:
		// 406、429 等多为请求头或限流问题，不构成地区判定依据。
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceHTTPRejected}
	case response.Status >= 300:
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceRedirectLimit}
	case response.Status == 0:
		return mediaVerdict{State: stateUnknown, Region: region, Evidence: mediaEvidenceNoResponse}
	default:
		return mediaVerdict{State: stateUnreachable, Region: region, Evidence: mediaEvidenceUnreachable}
	}
}

func transportVerdict(region string) mediaVerdict {
	return mediaVerdict{
		State:    stateUnknown,
		Region:   region,
		Evidence: mediaEvidenceTransportError,
	}
}

// performMediaRequest 执行一次检测请求。
func performMediaRequest(ctx context.Context, env Environment, request mediaRequest) mediaResponse {
	result := mediaResponse{}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		result.Err = err
		return result
	}
	httpRequest.Header.Set("User-Agent", env.UserAgent)
	httpRequest.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for key, value := range request.Headers {
		httpRequest.Header.Set(key, value)
	}
	response, err := env.HTTPClient.Do(httpRequest)
	if err != nil {
		result.Err = err
		return result
	}
	defer response.Body.Close()
	result.Status = response.StatusCode
	result.FinalURL = response.Request.URL.String()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 512*1024))
	if readErr != nil {
		result.Err = readErr
		return result
	}
	result.Body = string(body)
	return result
}

// matchFirst 返回正则第一个捕获组的值。
func matchFirst(pattern *regexp.Regexp, text string) string {
	if text == "" {
		return ""
	}
	if match := pattern.FindStringSubmatch(text); len(match) == 2 {
		return match[1]
	}
	return ""
}

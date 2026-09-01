package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func findingFromIPAPI(data ipAPIResponse, latency time.Duration) qualityFinding {
	finding := qualityFinding{
		ID:        "ipapi",
		Enabled:   true,
		Access:    networkChannelDirect,
		Country:   strings.ToUpper(data.Location.CountryCode),
		Usage:     normalizeNetworkType(data.ASN.Type),
		Company:   normalizeNetworkType(data.Company.Type),
		ScoreKind: networkScoreKindCompanyAbuse,
		Risk:      translateRiskLabel(scoreLabel(data.Company.AbuserScore)),
		Proxy:     knownIPAPISignal(data.IsProxy, data.BooleanPresence.IsProxy),
		Tor:       knownIPAPISignal(data.IsTor, data.BooleanPresence.IsTor),
		VPN:       knownIPAPISignal(data.IsVPN, data.BooleanPresence.IsVPN),
		Server:    knownIPAPISignal(data.IsDatacenter, data.BooleanPresence.IsDatacenter),
		Abuser:    knownIPAPISignal(data.IsAbuser, data.BooleanPresence.IsAbuser),
		Robot:     knownIPAPISignal(data.IsCrawler, data.BooleanPresence.IsCrawler),
		Latency:   latency,
	}
	finding.Score = parseProbabilityScore(data.Company.AbuserScore)
	if finding.Score == nil {
		finding.Score = parseProbabilityScore(data.ASN.AbuserScore)
		finding.ScoreKind = networkScoreKindASNAbuse
		finding.Risk = translateRiskLabel(scoreLabel(data.ASN.AbuserScore))
	}
	return finding
}

func knownIPAPISignal(value, present bool) qualitySignal {
	if !present {
		return qualitySignal{}
	}
	return knownSignal(value)
}

func fetchMaxMindOrigin(ctx context.Context, client *http.Client, userAgent, ip string) originAssessment {
	assessment := originAssessment{
		Enabled: true,
		Label:   "probe.network.ip_type.unknown",
		Access:  networkChannelCommunity,
	}
	endpoint := communityURL(ip, "")
	body, latency, err := requestBytes(ctx, client, userAgent, endpoint, nil, 1024*1024)
	assessment.Latency = latency
	if err != nil {
		assessment.Err = err
		return assessment
	}
	var response struct {
		City struct {
			Country struct {
				ISOCode string `json:"IsoCode"`
			} `json:"Country"`
		} `json:"City"`
		Country struct {
			ISOCode           string `json:"IsoCode"`
			RegisteredCountry struct {
				ISOCode string `json:"IsoCode"`
			} `json:"RegisteredCountry"`
		} `json:"Country"`
	}
	if err := decodeJSON(body, &response); err != nil {
		assessment.Err = err
		return assessment
	}
	assessment.UsageCountry = strings.ToUpper(firstNonEmpty(response.Country.ISOCode, response.City.Country.ISOCode))
	assessment.RegisteredCountry = strings.ToUpper(response.Country.RegisteredCountry.ISOCode)
	switch {
	case assessment.UsageCountry == "" || assessment.RegisteredCountry == "":
		assessment.Label = "probe.network.ip_type.unknown"
		assessment.Err = errors.New("缺少使用地或注册地")
	case assessment.UsageCountry == assessment.RegisteredCountry:
		assessment.Label = "probe.network.ip_type.native"
	default:
		assessment.Label = "probe.network.ip_type.broadcast"
	}
	return assessment
}

func fetchIPinfo(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipinfo")
	token := strings.TrimSpace(os.Getenv("IPINFO_TOKEN"))
	endpoint := "https://ipinfo.io/widget/demo/" + url.PathEscape(ip)
	if token != "" {
		values := url.Values{"token": []string{token}}
		endpoint = "https://api.ipinfo.io/lookup/" + url.PathEscape(ip) + "?" + values.Encode()
		finding.Access = networkChannelAPIKey
	} else {
		finding.Access = networkChannelPublicDemo
	}
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var wrapper struct {
		Data json.RawMessage `json:"data"`
	}
	payload := body
	if json.Unmarshal(body, &wrapper) == nil && len(wrapper.Data) > 0 && string(wrapper.Data) != "null" {
		payload = wrapper.Data
	}
	var response struct {
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		ASN         struct {
			Type string `json:"type"`
		} `json:"asn"`
		Company struct {
			Type string `json:"type"`
		} `json:"company"`
		Privacy struct {
			VPN     *bool `json:"vpn"`
			Proxy   *bool `json:"proxy"`
			Tor     *bool `json:"tor"`
			Hosting *bool `json:"hosting"`
		} `json:"privacy"`
		IsAnonymous *bool `json:"is_anonymous"`
		IsHosting   *bool `json:"is_hosting"`
	}
	if err := decodeJSON(payload, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(firstNonEmpty(response.CountryCode, response.Country))
	finding.Usage = normalizeNetworkType(response.ASN.Type)
	finding.Company = normalizeNetworkType(response.Company.Type)
	finding.Proxy = pointerSignal(response.Privacy.Proxy)
	finding.Tor = pointerSignal(response.Privacy.Tor)
	finding.VPN = pointerSignal(response.Privacy.VPN)
	finding.Server = firstKnownSignal(pointerSignal(response.Privacy.Hosting), pointerSignal(response.IsHosting))
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	if !anyKnownSignal(finding.Proxy, finding.Tor, finding.VPN, finding.Server) {
		finding.Partial = networkPartialPrivacy
	}
	return finding
}

func fetchIPregistry(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	apiKey := strings.TrimSpace(os.Getenv("IPREGISTRY_API_KEY"))
	key := apiKey
	access := networkChannelAPIKey
	if key == "" {
		key = "tryout"
		access = networkChannelTryout
	}
	values := url.Values{
		"key":    []string{key},
		"fields": []string{"location.country.code,connection.type,company.type,security"},
	}
	endpoint := "https://api.ipregistry.co/" + url.PathEscape(ip) + "?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
	if err == nil {
		finding := parseIPregistryJSON(body)
		finding.Access = access
		finding.Latency = latency
		if finding.Err == nil {
			return finding
		}
	}

	body, fallbackLatency, fallbackErr := requestBytes(
		ctx,
		communityClient,
		env.UserAgent,
		communityURL(ip, "ipregistry"),
		nil,
		1024*1024,
	)
	latency += fallbackLatency
	if fallbackErr == nil {
		finding := parseIPregistryJSON(body)
		finding.Access = networkChannelCommunity
		finding.Latency = latency
		if finding.Err == nil {
			if apiKey != "" {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	finding := newFinding("ipregistry")
	finding.Access = networkChannelMixedFallback
	finding.Latency = latency
	if apiKey == "" {
		finding.Err = errors.New("官方 tryout 和社区查询均不可用")
	} else {
		finding.Err = errors.New("官方 API 和社区查询均不可用")
	}
	return finding
}

func parseIPregistryJSON(body []byte) qualityFinding {
	finding := newFinding("ipregistry")
	var response struct {
		Location struct {
			Country struct {
				Code string `json:"code"`
			} `json:"country"`
		} `json:"location"`
		Connection struct {
			Type string `json:"type"`
		} `json:"connection"`
		Company struct {
			Type string `json:"type"`
		} `json:"company"`
		Security struct {
			IsAbuser        *bool `json:"is_abuser"`
			IsAttacker      *bool `json:"is_attacker"`
			IsCloudProvider *bool `json:"is_cloud_provider"`
			IsProxy         *bool `json:"is_proxy"`
			IsTor           *bool `json:"is_tor"`
			IsTorExit       *bool `json:"is_tor_exit"`
			IsVPN           *bool `json:"is_vpn"`
		} `json:"security"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.Location.Country.Code)
	finding.Usage = normalizeNetworkType(response.Connection.Type)
	finding.Company = normalizeNetworkType(response.Company.Type)
	finding.Proxy = pointerSignal(response.Security.IsProxy)
	finding.Tor = anyPointerSignal(response.Security.IsTor, response.Security.IsTorExit)
	finding.VPN = pointerSignal(response.Security.IsVPN)
	finding.Server = pointerSignal(response.Security.IsCloudProvider)
	finding.Abuser = anyPointerSignal(response.Security.IsAbuser, response.Security.IsAttacker)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

func fetchIP2Location(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	apiKey := strings.TrimSpace(os.Getenv("IP2LOCATION_API_KEY"))
	var totalLatency time.Duration
	keyedAttemptFailed := false
	if apiKey != "" {
		values := url.Values{"ip": []string{ip}, "format": []string{"json"}}
		endpoint := "https://api.ip2location.io/?" + values.Encode()
		headers := map[string]string{"Authorization": "Bearer " + apiKey}
		body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, headers, 1024*1024)
		totalLatency += latency
		if err == nil {
			finding := parseIP2LocationJSON(body)
			finding.Access = networkChannelAPIKey
			finding.Latency = totalLatency
			if finding.Err == nil {
				return finding
			}
		}
		keyedAttemptFailed = true
	}

	values := url.Values{"ip": []string{ip}, "format": []string{"json"}}
	keylessEndpoint := "https://api.ip2location.io/?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, keylessEndpoint, nil, 1024*1024)
	totalLatency += latency
	var keylessFinding qualityFinding
	keylessAvailable := false
	if err == nil {
		keylessFinding = parseIP2LocationJSON(body)
		keylessFinding.Access = networkChannelOfficialFree
		keylessFinding.Latency = totalLatency
		keylessAvailable = keylessFinding.Err == nil
	}

	body, latency, err = requestBytes(
		ctx,
		communityClient,
		env.UserAgent,
		communityURL(ip, "ip2location"),
		nil,
		1024*1024,
	)
	totalLatency += latency
	if err == nil {
		finding := parseIP2LocationJSON(body)
		finding.Access = networkChannelCommunity
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	if keylessAvailable {
		keylessFinding.Latency = totalLatency
		keylessFinding.Partial = appendPartial(keylessFinding.Partial, networkPartialFallback)
		if keyedAttemptFailed {
			keylessFinding.Partial = appendPartial(keylessFinding.Partial, networkPartialFallback)
		}
		return keylessFinding
	}

	finding := newFinding("ip2location")
	finding.Access = networkChannelMixedFallback
	finding.Latency = totalLatency
	finding.Err = errors.New("官方免密接口和社区查询均不可用")
	return finding
}

func parseIP2LocationJSON(body []byte) qualityFinding {
	finding := newFinding("ip2location")
	var response struct {
		CountryCode string   `json:"country_code"`
		UsageType   string   `json:"usage_type"`
		IsProxy     *bool    `json:"is_proxy"`
		FraudScore  *float64 `json:"fraud_score"`
		ASInfo      struct {
			UsageType string `json:"as_usage_type"`
		} `json:"as_info"`
		Proxy struct {
			IsPublicProxy *bool `json:"is_public_proxy"`
			IsWebProxy    *bool `json:"is_web_proxy"`
			IsTor         *bool `json:"is_tor"`
			IsVPN         *bool `json:"is_vpn"`
			IsDataCenter  *bool `json:"is_data_center"`
			IsSpammer     *bool `json:"is_spammer"`
			IsWebCrawler  *bool `json:"is_web_crawler"`
			IsScanner     *bool `json:"is_scanner"`
			IsBotnet      *bool `json:"is_botnet"`
		} `json:"proxy"`
		Error any `json:"error"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Usage = normalizeIP2LocationType(response.UsageType)
	finding.Company = normalizeIP2LocationType(response.ASInfo.UsageType)
	finding.Score = validScore(response.FraudScore)
	finding.ScoreKind = networkScoreKindIP2Proxy
	finding.Risk = riskIP2Location(finding.Score)
	finding.Proxy = anyPointerSignal(response.IsProxy, response.Proxy.IsPublicProxy, response.Proxy.IsWebProxy)
	finding.Tor = pointerSignal(response.Proxy.IsTor)
	finding.VPN = pointerSignal(response.Proxy.IsVPN)
	finding.Server = pointerSignal(response.Proxy.IsDataCenter)
	finding.Abuser = pointerSignal(response.Proxy.IsSpammer)
	finding.Robot = anyPointerSignal(response.Proxy.IsWebCrawler, response.Proxy.IsScanner, response.Proxy.IsBotnet)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	if finding.Score == nil {
		finding.Partial = networkPartialScore
	}
	return finding
}

func fetchAbuseIPDB(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	finding := newFinding("abuseipdb")
	apiKey := strings.TrimSpace(os.Getenv("ABUSEIPDB_API_KEY"))
	client := communityClient
	endpoint := communityURL(ip, "abuseipdb")
	headers := map[string]string(nil)
	if apiKey != "" {
		values := url.Values{"ipAddress": []string{ip}, "maxAgeInDays": []string{"90"}}
		endpoint = "https://api.abuseipdb.com/api/v2/check?" + values.Encode()
		headers = map[string]string{"Key": apiKey, "Accept": "application/json"}
		client = env.HTTPClient
		finding.Access = networkChannelAPIKey
	} else {
		finding.Access = networkChannelCommunity
	}
	body, latency, err := requestBytes(ctx, client, env.UserAgent, endpoint, headers, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Data struct {
			CountryCode          string   `json:"countryCode"`
			UsageType            string   `json:"usageType"`
			AbuseConfidenceScore *float64 `json:"abuseConfidenceScore"`
		} `json:"data"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.Data.CountryCode)
	finding.Usage = normalizeAbuseIPDBType(response.Data.UsageType)
	finding.Score = validScore(response.Data.AbuseConfidenceScore)
	finding.ScoreKind = networkScoreKindAbuse
	finding.Risk = riskAbuseIPDB(finding.Score)
	if finding.Score != nil {
		value := *finding.Score >= 25
		finding.Abuser = knownSignal(value)
	}
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

func fetchScamalytics(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	finding := newFinding("scamalytics")
	username := strings.TrimSpace(os.Getenv("SCAMALYTICS_USER"))
	apiKey := strings.TrimSpace(os.Getenv("SCAMALYTICS_API_KEY"))
	client := communityClient
	endpoint := communityURL(ip, "scamalytics")
	if username != "" && apiKey != "" {
		values := url.Values{"key": []string{apiKey}, "ip": []string{ip}}
		endpoint = "https://api11.scamalytics.com/" + url.PathEscape(username) + "/?" + values.Encode()
		client = env.HTTPClient
		finding.Access = networkChannelAPIKey
	} else {
		finding.Access = networkChannelCommunity
	}
	body, latency, err := requestBytes(ctx, client, env.UserAgent, endpoint, nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Scamalytics struct {
			Score                 *float64 `json:"scamalytics_score"`
			IsBlacklistedExternal *bool    `json:"is_blacklisted_external"`
			Proxy                 struct {
				IsVPN        *bool `json:"is_vpn"`
				IsDataCenter *bool `json:"is_datacenter"`
			} `json:"scamalytics_proxy"`
		} `json:"scamalytics"`
		External struct {
			MaxMind struct {
				CountryCode string `json:"ip_country_code"`
			} `json:"maxmind_geolite2"`
			FireHOL struct {
				IsProxy *bool `json:"is_proxy"`
			} `json:"firehol"`
			X4BNet struct {
				IsTor            *bool `json:"is_tor"`
				IsBlacklistedBot *bool `json:"is_blacklisted_spambot"`
				IsBotOperaMini   *bool `json:"is_bot_operamini"`
				IsBotSEMrush     *bool `json:"is_bot_semrush"`
			} `json:"x4bnet"`
		} `json:"external_datasources"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.External.MaxMind.CountryCode)
	finding.Score = validScore(response.Scamalytics.Score)
	finding.ScoreKind = networkScoreKindWebFraud
	finding.Risk = riskScamalytics(finding.Score)
	finding.Proxy = pointerSignal(response.External.FireHOL.IsProxy)
	finding.Tor = pointerSignal(response.External.X4BNet.IsTor)
	finding.VPN = pointerSignal(response.Scamalytics.Proxy.IsVPN)
	finding.Server = pointerSignal(response.Scamalytics.Proxy.IsDataCenter)
	finding.Abuser = pointerSignal(response.Scamalytics.IsBlacklistedExternal)
	finding.Robot = anyPointerSignal(
		response.External.X4BNet.IsBlacklistedBot,
		response.External.X4BNet.IsBotOperaMini,
		response.External.X4BNet.IsBotSEMrush,
	)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

func fetchIPQS(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	apiKey := strings.TrimSpace(os.Getenv("IPQS_API_KEY"))
	var totalLatency time.Duration
	keyedAttemptFailed := false

	if apiKey != "" {
		values := url.Values{"strictness": []string{"1"}, "allow_public_access_points": []string{"true"}}
		endpoint := "https://ipqualityscore.com/api/json/ip/" + url.PathEscape(apiKey) + "/" + url.PathEscape(ip) + "?" + values.Encode()
		body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
		totalLatency += latency
		if err == nil {
			finding := parseIPQSJSON(body)
			finding.Access = networkChannelAPIKey
			finding.Latency = totalLatency
			if finding.Err == nil {
				return finding
			}
		}
		keyedAttemptFailed = true
	}

	// IPQuality's community endpoint currently redirects this one provider to
	// IPQS. The redirect is allowed only for this keyless request and only to
	// the two exact IPQS hostnames; requestBytes remains strict everywhere else.
	body, latency, err := requestBytesAllowingRedirectHosts(
		ctx,
		communityClient,
		env.UserAgent,
		communityURL(ip, "ipqualityscore"),
		nil,
		1024*1024,
		[]string{"ipqualityscore.com", "www.ipqualityscore.com"},
	)
	totalLatency += latency
	if err == nil {
		finding := parseIPQSJSON(body)
		finding.Access = networkChannelCommunity
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	publicEndpoint := "https://www.ipqualityscore.com/free-ip-lookup-proxy-vpn-test/lookup/" + url.PathEscape(ip)
	body, latency, err = requestBytes(ctx, env.HTTPClient, browserUserAgent(), publicEndpoint, nil, 2*1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseIPQSPublicPage(body, ip)
		// 通道披露到底：这一级用浏览器 UA 读官方公开页，而不是 ecs 自己的 UA。
		finding.Access = networkChannelPublicPage
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, networkPartialPublicFields)
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	// The official public page has a small per-egress lookup allowance. Jina
	// Reader is a disclosed last resort that reads the same public IPQS page
	// and can return its short-lived cache without requiring a user key.
	readerEndpoint := "https://r.jina.ai/http://www.ipqualityscore.com/free-ip-lookup-proxy-vpn-test/lookup/" + url.PathEscape(ip)
	readerHeaders := map[string]string{
		"Accept":            "text/plain",
		"X-Cache-Tolerance": "3600",
	}
	readerClient := env.HTTPClient
	readerAccess := networkChannelJina
	if proxyEnvironmentEnabled() {
		readerClient = newProxyFallbackHTTPClient(max(env.Config.HTTPTimeout, 25*time.Second))
		defer readerClient.CloseIdleConnections()
		readerAccess = networkChannelJinaProxy
	}
	body, latency, err = requestBytes(ctx, readerClient, env.UserAgent, readerEndpoint, readerHeaders, 2*1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseIPQSPublicPage(body, ip)
		finding.Access = readerAccess
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, networkPartialCachedFields)
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	finding := newFinding("ipqs")
	finding.Access = networkChannelMixedFallback
	finding.Latency = totalLatency
	finding.Err = errors.New("官方 API、社区额度和公开查询页均不可用")
	return finding
}

func parseIPQSJSON(body []byte) qualityFinding {
	finding := newFinding("ipqs")
	var response struct {
		Success        *bool    `json:"success"`
		FraudScore     *float64 `json:"fraud_score"`
		CountryCode    string   `json:"country_code"`
		Proxy          *bool    `json:"proxy"`
		Tor            *bool    `json:"tor"`
		VPN            *bool    `json:"vpn"`
		RecentAbuse    *bool    `json:"recent_abuse"`
		BotStatus      *bool    `json:"bot_status"`
		ConnectionType string   `json:"connection_type"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if response.Success != nil && !*response.Success {
		finding.Err = errors.New("上游未返回风险数据")
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Usage = normalizeNetworkType(response.ConnectionType)
	finding.Score = validScore(response.FraudScore)
	finding.ScoreKind = networkScoreKindIPFraud
	finding.Risk = riskIPQS(finding.Score)
	finding.Proxy = pointerSignal(response.Proxy)
	finding.Tor = pointerSignal(response.Tor)
	finding.VPN = pointerSignal(response.VPN)
	finding.Abuser = pointerSignal(response.RecentAbuse)
	finding.Robot = pointerSignal(response.BotStatus)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	if finding.Score == nil {
		finding.Partial = networkPartialScore
	}
	return finding
}

func parseIPQSPublicPage(body []byte, ip string) qualityFinding {
	finding := newFinding("ipqs")
	page := html.UnescapeString(string(body))
	plain := providerPageText(page)
	lower := strings.ToLower(plain)
	if !strings.Contains(page, ip) && !strings.Contains(plain, ip) {
		finding.Err = errors.New("公开页未返回目标 IP")
		return finding
	}

	scoreText := firstMatch(ipqsPublicScorePattern, plain)
	if scoreText == "" {
		if strings.Contains(lower, "daily max lookups") {
			finding.Err = errors.New("公开页达到查询上限")
		} else {
			finding.Err = errors.New("公开页未返回欺诈分")
		}
		return finding
	}
	score, err := strconv.ParseFloat(scoreText, 64)
	if err != nil {
		finding.Err = errors.New("公开页欺诈分无效")
		return finding
	}
	finding.Score = validScore(&score)
	if finding.Score == nil {
		finding.Err = errors.New("公开页欺诈分超出范围")
		return finding
	}
	finding.ScoreKind = networkScoreKindIPFraud
	finding.Risk = riskIPQS(finding.Score)
	finding.Country = strings.ToUpper(firstMatch(ipqsPublicCountryPattern, plain))

	privacyValues := markdownTableValues(page, "proxy")
	finding.Proxy = yesNoSignal(privacyValues["proxy"])
	finding.VPN = yesNoSignal(privacyValues["vpn"])
	torValues := markdownTableValues(page, "tor")
	finding.Tor = yesNoSignal(torValues["tor"])

	target := regexp.QuoteMeta(ip)
	if !finding.Proxy.Known {
		finding.Proxy = signalFromPageText(
			plain,
			`(?i)this IP address\s*\(?`+target+`\)?\s+is\s+(?:a\s+)?proxy connection`,
			`(?i)this IP address\s*\(?`+target+`\)?\s+is\s+not\s+(?:a\s+)?proxy connection`,
		)
	}
	if !finding.VPN.Known {
		finding.VPN = signalFromPageText(
			plain,
			`(?i)identified\s+`+target+`\s+as\s+(?:a\s+)?VPN connection`,
			`(?i)identified\s+`+target+`\s+as\s+not\s+(?:a\s+)?VPN connection`,
		)
	}
	switch {
	case strings.Contains(lower, "has recently detected abusive behavior from this connection"):
		finding.Abuser = knownSignal(true)
	case strings.Contains(lower, "no recent abuse detected from this connection"):
		finding.Abuser = knownSignal(false)
	}
	return finding
}

func fetchDBIP(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	apiKey := strings.TrimSpace(os.Getenv("DBIP_API_KEY"))
	var totalLatency time.Duration
	keyedAttemptFailed := false
	if apiKey != "" {
		endpoint := "https://api.db-ip.com/v2/" + url.PathEscape(apiKey) + "/" + url.PathEscape(ip)
		body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
		totalLatency += latency
		if err == nil {
			finding := parseDBIPExtendedJSON(body)
			finding.Access = networkChannelExtendedAPIKey
			finding.Latency = totalLatency
			if finding.Err == nil {
				return finding
			}
		}
		keyedAttemptFailed = true
	}

	endpoint := "https://db-ip.com/" + url.PathEscape(ip)
	body, latency, err := requestBytes(ctx, env.HTTPClient, browserUserAgent(), endpoint, nil, 2*1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseDBIPPublicPage(body)
		// 同上：公开页只接受浏览器 UA，报告如实标出这一点。
		finding.Access = networkChannelPublicPage
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	// DB-IP's documented free API does not expose threatLevel, but it is a
	// reliable, keyless DB-IP response and prevents a Cloudflare challenge on
	// the richer public page from turning the whole provider into a failure.
	freeEndpoint := "https://api.db-ip.com/v2/free/" + url.PathEscape(ip)
	body, latency, err = requestBytes(ctx, env.HTTPClient, env.UserAgent, freeEndpoint, nil, 1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseDBIPFreeJSON(body)
		finding.Access = networkChannelFreeFallback
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, networkPartialThreat)
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, networkPartialFallback)
			}
			return finding
		}
	}

	finding := newFinding("dbip")
	finding.Access = networkChannelFreeFallback
	finding.Latency = totalLatency
	finding.Err = errors.New("公开风险页和官方 free API 均不可用")
	return finding
}

func parseDBIPExtendedJSON(body []byte) qualityFinding {
	finding := newFinding("dbip")
	var response struct {
		CountryCode   string   `json:"countryCode"`
		UsageType     string   `json:"usageType"`
		IsCrawler     *bool    `json:"isCrawler"`
		IsProxy       *bool    `json:"isProxy"`
		ProxyType     string   `json:"proxyType"`
		ThreatLevel   string   `json:"threatLevel"`
		ThreatDetails []string `json:"threatDetails"`
		ErrorCode     string   `json:"errorCode"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if response.ErrorCode != "" {
		finding.Err = errors.New("DB-IP API 拒绝查询")
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Usage = normalizeNetworkType(response.UsageType)
	finding.Proxy = pointerSignal(response.IsProxy)
	finding.Tor = knownWhenNonEmpty(strings.EqualFold(response.ProxyType, "tor"), response.ProxyType)
	finding.VPN = knownWhenNonEmpty(strings.EqualFold(response.ProxyType, "vpn"), response.ProxyType)
	finding.Server = knownWhenNonEmpty(strings.EqualFold(response.UsageType, "hosting"), response.UsageType)
	finding.Robot = pointerSignal(response.IsCrawler)
	finding.Abuser = threatDetailSignal(response.ThreatDetails)
	setDBIPRisk(&finding, response.ThreatLevel)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	} else if finding.Score == nil {
		finding.Partial = networkPartialThreat
	}
	return finding
}

func parseDBIPPublicPage(body []byte) qualityFinding {
	finding := newFinding("dbip")
	page := html.UnescapeString(string(body))
	level := firstMatch(dbIPThreatPattern, page)
	finding.Country = strings.ToUpper(firstMatch(dbIPCountryPattern, page))
	setDBIPRisk(&finding, level)
	if finding.Score == nil && finding.Country == "" {
		finding.Err = errors.New("公开页未返回可解析的风险字段")
	} else if finding.Score == nil {
		finding.Partial = networkPartialThreat
	}
	return finding
}

func parseDBIPFreeJSON(body []byte) qualityFinding {
	finding := newFinding("dbip")
	var response struct {
		CountryCode string `json:"countryCode"`
		ErrorCode   string `json:"errorCode"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if response.ErrorCode != "" {
		finding.Err = errors.New("DB-IP free API 拒绝查询")
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	if finding.Country == "" {
		finding.Err = errors.New("DB-IP free API 未返回国家字段")
	}
	return finding
}

func fetchIPdata(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	finding := newFinding("ipdata")
	finding.Access = networkChannelCommunity
	body, latency, err := requestBytes(ctx, communityClient, env.UserAgent, communityURL(ip, "ipdata"), nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		CountryCode string `json:"country_code"`
		Threat      struct {
			IsProxy         *bool `json:"is_proxy"`
			IsTor           *bool `json:"is_tor"`
			IsDataCenter    *bool `json:"is_datacenter"`
			IsThreat        *bool `json:"is_threat"`
			IsKnownAbuser   *bool `json:"is_known_abuser"`
			IsKnownAttacker *bool `json:"is_known_attacker"`
		} `json:"threat"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Proxy = pointerSignal(response.Threat.IsProxy)
	finding.Tor = pointerSignal(response.Threat.IsTor)
	finding.Server = pointerSignal(response.Threat.IsDataCenter)
	finding.Abuser = anyPointerSignal(
		response.Threat.IsThreat,
		response.Threat.IsKnownAbuser,
		response.Threat.IsKnownAttacker,
	)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

func fetchIPWhois(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipwhois")
	apiKey := strings.TrimSpace(os.Getenv("IPWHOIS_API_KEY"))
	values := url.Values{"security": []string{"1"}}
	endpoint := "https://ipwho.is/" + url.PathEscape(ip) + "?" + values.Encode()
	finding.Access = networkChannelDirect
	if apiKey != "" {
		values.Set("key", apiKey)
		endpoint = "https://ipwhois.pro/" + url.PathEscape(ip) + "?" + values.Encode()
		finding.Access = networkChannelAPIKey
	}
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Success     *bool  `json:"success"`
		CountryCode string `json:"country_code"`
		Message     string `json:"message"`
		Security    struct {
			Proxy   *bool `json:"proxy"`
			VPN     *bool `json:"vpn"`
			Tor     *bool `json:"tor"`
			Hosting *bool `json:"hosting"`
		} `json:"security"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if response.Success != nil && !*response.Success {
		finding.Err = errors.New("IPWHOIS 未返回安全数据")
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Proxy = pointerSignal(response.Security.Proxy)
	finding.Tor = pointerSignal(response.Security.Tor)
	finding.VPN = pointerSignal(response.Security.VPN)
	finding.Server = pointerSignal(response.Security.Hosting)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	if !anyKnownSignal(finding.Proxy, finding.Tor, finding.VPN, finding.Server) {
		finding.Partial = networkPartialSecurity
	}
	return finding
}

func newFinding(id string) qualityFinding {
	return qualityFinding{
		ID:      id,
		Enabled: true,
	}
}

func communityURL(ip, database string) string {
	values := url.Values{}
	if database != "" {
		values.Set("db", database)
	} else {
		values.Set("lang", "en")
	}
	return communityIPInfoBase + "/" + url.PathEscape(ip) + "?" + values.Encode()
}

func requestBytes(
	ctx context.Context,
	client *http.Client,
	userAgent string,
	endpoint string,
	headers map[string]string,
	limit int64,
) ([]byte, time.Duration, error) {
	return requestBytesAllowingRedirectHosts(ctx, client, userAgent, endpoint, headers, limit, nil)
}

func requestBytesAllowingRedirectHosts(
	ctx context.Context,
	client *http.Client,
	userAgent string,
	endpoint string,
	headers map[string]string,
	limit int64,
	allowedRedirectHosts []string,
) ([]byte, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, errors.New("创建请求失败")
	}
	if request.URL.Hostname() == "ipinfo.check.place" {
		// The upstream script queries this community service sequentially.
		// Serializing here avoids turning one ecs run into a burst that trips
		// its shared anti-abuse limit.
		select {
		case communityRequestGate <- struct{}{}:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
		defer func() {
			time.Sleep(100 * time.Millisecond)
			<-communityRequestGate
		}()
	}
	requestUserAgent := firstNonEmpty(userAgent, "ecs")
	if request.URL.Hostname() == "ipinfo.check.place" {
		// The IPQuality community gateway currently accepts curl-compatible
		// clients only. Keep ecs identifiable instead of pretending to be the
		// upstream script.
		requestUserAgent = "curl/8.7.1 " + requestUserAgent
	}
	request.Header.Set("User-Agent", requestUserAgent)
	request.Header.Set("Accept", "application/json, text/html;q=0.8")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	start := time.Now()
	safeClient := *client
	previousRedirectPolicy := client.CheckRedirect
	safeClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 0 &&
			!strings.EqualFold(next.URL.Host, via[len(via)-1].URL.Host) &&
			!hostAllowed(next.URL.Hostname(), allowedRedirectHosts) {
			return errors.New("跨主机重定向已拒绝")
		}
		if previousRedirectPolicy != nil {
			return previousRedirectPolicy(next, via)
		}
		if len(via) >= 5 {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := safeClient.Do(request)
	latency := time.Since(start)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, latency, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, latency, context.DeadlineExceeded
		}
		return nil, latency, fmt.Errorf("网络请求失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, latency, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, latency, errors.New("读取响应失败")
	}
	if int64(len(body)) > limit {
		return nil, latency, errors.New("响应超过大小限制")
	}
	return body, latency, nil
}

func hostAllowed(host string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(host), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func decodeJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("响应不是有效 JSON")
	}
	return nil
}

package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/model"
)

const communityIPInfoBase = "https://ipinfo.check.place"

var (
	communityRequestGate = make(chan struct{}, 1)
	qualitySourceOrder   = []string{
		"maxmind",
		"ipinfo",
		"ipregistry",
		"ipapi",
		"ip2location",
		"abuseipdb",
		"scamalytics",
		"ipqs",
		"dbip",
		"ipdata",
		"ipwhois",
	}
	typeSourceOrder   = []string{"ipinfo", "ipregistry", "ipapi", "ip2location", "abuseipdb"}
	scoreSourceOrder  = []string{"ip2location", "scamalytics", "ipapi", "abuseipdb", "ipqs", "dbip"}
	factorSourceOrder = []string{
		"ip2location",
		"ipapi",
		"ipregistry",
		"ipqs",
		"scamalytics",
		"ipdata",
		"ipinfo",
		"ipwhois",
		"dbip",
	}
	qualitySourceLabels = map[string]string{
		"maxmind":     "MaxMind",
		"ipinfo":      "IPinfo",
		"ipregistry":  "ipregistry",
		"ipapi":       "ipapi",
		"ip2location": "IP2Location",
		"abuseipdb":   "AbuseIPDB",
		"scamalytics": "Scamalytics",
		"ipqs":        "IPQS",
		"dbip":        "DB-IP",
		"ipdata":      "ipdata",
		"ipwhois":     "IPWHOIS",
	}
)

type qualitySignal struct {
	Known bool
	Value bool
}

type qualityFinding struct {
	ID        string
	Name      string
	Enabled   bool
	Access    string
	Country   string
	Usage     string
	Company   string
	Score     *float64
	ScoreKind string
	Risk      string
	Proxy     qualitySignal
	Tor       qualitySignal
	VPN       qualitySignal
	Server    qualitySignal
	Abuser    qualitySignal
	Robot     qualitySignal
	Latency   time.Duration
	Err       error
	Partial   string
}

type originAssessment struct {
	Enabled           bool
	Label             string
	UsageCountry      string
	RegisteredCountry string
	Access            string
	Latency           time.Duration
	Err               error
}

type ipQualityBundle struct {
	Version  string
	Origin   originAssessment
	Findings map[string]qualityFinding
}

func collectIPQuality(ctx context.Context, env Environment, lookup ipLookup) ipQualityBundle {
	bundle := ipQualityBundle{
		Version:  lookup.Version,
		Findings: make(map[string]qualityFinding, len(qualitySourceOrder)),
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		bundle.Findings[id] = qualityFinding{
			ID:      id,
			Name:    qualitySourceLabels[id],
			Enabled: qualitySourceEnabled(env.Config.IPQualitySources, id),
		}
	}

	if qualitySourceEnabled(env.Config.IPQualitySources, "ipapi") {
		finding := findingFromIPAPI(lookup.Data, lookup.Latency)
		finding.Enabled = true
		bundle.Findings["ipapi"] = finding
	}

	communityClient := newIPVersionHTTPClient(env.Config.HTTPTimeout, lookup.Version)
	defer communityClient.CloseIdleConnections()

	type outcome struct {
		id      string
		finding qualityFinding
		origin  *originAssessment
	}
	outcomes := make(chan outcome, len(qualitySourceOrder))
	var wg sync.WaitGroup

	if qualitySourceEnabled(env.Config.IPQualitySources, "maxmind") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value := fetchMaxMindOrigin(ctx, communityClient, env.UserAgent, lookup.Data.IP)
			outcomes <- outcome{id: "maxmind", origin: &value}
		}()
	}

	fetchers := map[string]func(context.Context, Environment, *http.Client, string) qualityFinding{
		"ipinfo":      fetchIPinfo,
		"ipregistry":  fetchIPregistry,
		"ip2location": fetchIP2Location,
		"abuseipdb":   fetchAbuseIPDB,
		"scamalytics": fetchScamalytics,
		"ipqs":        fetchIPQS,
		"dbip":        fetchDBIP,
		"ipdata":      fetchIPdata,
		"ipwhois":     fetchIPWhois,
	}
	for _, id := range qualitySourceOrder {
		fetcher, ok := fetchers[id]
		if !ok || !qualitySourceEnabled(env.Config.IPQualitySources, id) {
			continue
		}
		wg.Add(1)
		go func(id string, fetcher func(context.Context, Environment, *http.Client, string) qualityFinding) {
			defer wg.Done()
			outcomes <- outcome{id: id, finding: fetcher(ctx, env, communityClient, lookup.Data.IP)}
		}(id, fetcher)
	}

	go func() {
		wg.Wait()
		close(outcomes)
	}()
	for item := range outcomes {
		if item.origin != nil {
			bundle.Origin = *item.origin
			continue
		}
		item.finding.Enabled = true
		bundle.Findings[item.id] = item.finding
	}
	return bundle
}

func qualitySourceEnabled(configured []string, source string) bool {
	for _, item := range configured {
		switch item {
		case "all":
			return true
		case "none":
			return false
		case source:
			return true
		}
	}
	return false
}

func newIPVersionHTTPClient(timeout time.Duration, version string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	network := "tcp" + version
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
	}
	transport.MaxIdleConnsPerHost = 8
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func newProxyFallbackHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.MaxIdleConnsPerHost = 4
	transport.TLSHandshakeTimeout = timeout
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func findingFromIPAPI(data ipAPIResponse, latency time.Duration) qualityFinding {
	finding := qualityFinding{
		ID:        "ipapi",
		Name:      qualitySourceLabels["ipapi"],
		Enabled:   true,
		Access:    "官方免密直连",
		Country:   strings.ToUpper(data.Location.CountryCode),
		Usage:     normalizeNetworkType(data.ASN.Type),
		Company:   normalizeNetworkType(data.Company.Type),
		ScoreKind: "公司滥用概率",
		Risk:      translateRiskLabel(scoreLabel(data.Company.AbuserScore)),
		Proxy:     knownSignal(data.IsProxy),
		Tor:       knownSignal(data.IsTor),
		VPN:       knownSignal(data.IsVPN),
		Server:    knownSignal(data.IsDatacenter),
		Abuser:    knownSignal(data.IsAbuser),
		Robot:     knownSignal(data.IsCrawler),
		Latency:   latency,
	}
	finding.Score = parseProbabilityScore(data.Company.AbuserScore)
	if finding.Score == nil {
		finding.Score = parseProbabilityScore(data.ASN.AbuserScore)
		finding.ScoreKind = "ASN 滥用概率"
		finding.Risk = translateRiskLabel(scoreLabel(data.ASN.AbuserScore))
	}
	return finding
}

func fetchMaxMindOrigin(ctx context.Context, client *http.Client, userAgent, ip string) originAssessment {
	assessment := originAssessment{
		Enabled: true,
		Access:  "IPQuality/check.place 中转",
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
		assessment.Label = "无法判定"
		assessment.Err = errors.New("缺少使用地或注册地")
	case assessment.UsageCountry == assessment.RegisteredCountry:
		assessment.Label = "原生 IP"
	default:
		assessment.Label = "广播 IP"
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
		finding.Access = "官方 API（用户密钥）"
	} else {
		finding.Access = "官方公开演示接口"
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
		finding.Partial = "套餐未返回细分隐私字段"
	}
	return finding
}

func fetchIPregistry(ctx context.Context, env Environment, communityClient *http.Client, ip string) qualityFinding {
	apiKey := strings.TrimSpace(os.Getenv("IPREGISTRY_API_KEY"))
	key := apiKey
	access := "官方 API（用户密钥）"
	if key == "" {
		key = "tryout"
		access = "官方 tryout 免密接口"
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
		finding.Access = "IPQuality/check.place 中转"
		finding.Latency = latency
		if finding.Err == nil {
			if apiKey != "" {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用社区兜底")
			}
			return finding
		}
	}

	finding := newFinding("ipregistry")
	finding.Access = access + "与社区兜底"
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
			finding.Access = "官方 API（用户密钥）"
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
		keylessFinding.Access = "官方免密接口"
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
		finding.Access = "IPQuality/check.place 中转"
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
			}
			return finding
		}
	}

	if keylessAvailable {
		keylessFinding.Latency = totalLatency
		keylessFinding.Partial = appendPartial(keylessFinding.Partial, "社区增强不可用")
		if keyedAttemptFailed {
			keylessFinding.Partial = appendPartial(keylessFinding.Partial, "用户密钥直连失败，已使用免密兜底")
		}
		return keylessFinding
	}

	finding := newFinding("ip2location")
	finding.Access = "官方免密接口与社区兜底"
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
	finding.ScoreKind = "IP2Proxy 欺诈分"
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
		finding.Partial = "套餐未返回 IP2Proxy 欺诈分"
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
		finding.Access = "官方 API（用户密钥）"
	} else {
		finding.Access = "IPQuality/check.place 中转"
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
	finding.ScoreKind = "90 天滥用置信度"
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
		finding.Access = "官方 API（用户密钥）"
	} else {
		finding.Access = "IPQuality/check.place 中转"
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
	finding.ScoreKind = "Web 流量欺诈分"
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
			finding.Access = "官方 API（用户密钥）"
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
		finding.Access = "IPQuality/check.place 中转"
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
			}
			return finding
		}
	}

	publicEndpoint := "https://www.ipqualityscore.com/free-ip-lookup-proxy-vpn-test/lookup/" + url.PathEscape(ip)
	body, latency, err = requestBytes(ctx, env.HTTPClient, browserUserAgent(), publicEndpoint, nil, 2*1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseIPQSPublicPage(body, ip)
		finding.Access = "官方公开查询页"
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, "公开页只提供部分 API 字段")
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
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
	readerAccess := "官方公开页（Jina Reader 只读兜底）"
	if proxyEnvironmentEnabled() {
		readerClient = newProxyFallbackHTTPClient(maxDuration(env.Config.HTTPTimeout, 25*time.Second))
		defer readerClient.CloseIdleConnections()
		readerAccess = "官方公开页（Jina Reader，经系统代理兜底）"
	}
	body, latency, err = requestBytes(ctx, readerClient, env.UserAgent, readerEndpoint, readerHeaders, 2*1024*1024)
	totalLatency += latency
	if err == nil {
		finding := parseIPQSPublicPage(body, ip)
		finding.Access = readerAccess
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, "公开页字段不完整，且结果可能来自 1 小时缓存")
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
			}
			return finding
		}
	}

	finding := newFinding("ipqs")
	finding.Access = "官方与免密兜底链路"
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
	finding.ScoreKind = "IP 欺诈分"
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
		finding.Partial = "未返回欺诈分"
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
	finding.ScoreKind = "IP 欺诈分"
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
			finding.Access = "官方 Extended API（用户密钥）"
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
		finding.Access = "官方公开查询页"
		finding.Latency = totalLatency
		if finding.Err == nil {
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
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
		finding.Access = "官方 free API（免密保底）"
		finding.Latency = totalLatency
		if finding.Err == nil {
			finding.Partial = appendPartial(finding.Partial, "免费 API 不提供威胁等级，公开风险页不可用")
			if keyedAttemptFailed {
				finding.Partial = appendPartial(finding.Partial, "用户密钥直连失败，已使用免密兜底")
			}
			return finding
		}
	}

	finding := newFinding("dbip")
	finding.Access = "官方公开页与 free API"
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
		finding.Partial = "套餐未返回威胁等级"
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
		finding.Partial = "公开页未返回威胁等级"
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
	finding.Access = "IPQuality/check.place 中转"
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
	finding.Access = "官方免密直连"
	if apiKey != "" {
		values.Set("key", apiKey)
		endpoint = "https://ipwhois.pro/" + url.PathEscape(ip) + "?" + values.Encode()
		finding.Access = "官方 API（用户密钥）"
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
		finding.Partial = "套餐未返回安全字段"
	}
	return finding
}

func newFinding(id string) qualityFinding {
	return qualityFinding{
		ID:      id,
		Name:    qualitySourceLabels[id],
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
		return nil, latency, errors.New("网络请求失败")
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
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(destination); err != nil {
		return errors.New("响应不是有效 JSON")
	}
	return nil
}

func (bundle ipQualityBundle) typeTable() model.Table {
	table := model.Table{
		Title:   "IPv" + bundle.Version + " · IP 类型属性",
		Columns: []string{"数据库", "使用类型", "公司类型", "国家/地区", "通道"},
	}
	for _, id := range typeSourceOrder {
		finding := bundle.Findings[id]
		table.Rows = append(table.Rows, []string{
			qualitySourceLabels[id],
			findingValue(finding, finding.Usage),
			findingValue(finding, finding.Company),
			findingValue(finding, finding.Country),
			findingAccess(finding),
		})
	}
	return table
}

func (bundle ipQualityBundle) scoreTable() model.Table {
	table := model.Table{
		Title:   "IPv" + bundle.Version + " · 风险评分",
		Columns: []string{"数据库", "原始/等效值", "风险等级", "可视化", "指标口径", "分段规则", "通道"},
	}
	for _, id := range scoreSourceOrder {
		finding := bundle.Findings[id]
		value := scoreText(finding)
		bar := "────────────"
		if finding.Score != nil {
			bar = scoreBar(*finding.Score)
		}
		table.Rows = append(table.Rows, []string{
			qualitySourceLabels[id],
			value,
			fallback(finding.Risk, "—"),
			bar,
			fallback(finding.ScoreKind, "—"),
			scoreBands(id),
			findingAccess(finding),
		})
	}
	return table
}

func scoreBands(id string) string {
	switch id {
	case "ip2location":
		return "<33 低 / 33–65 中 / ≥66 高"
	case "scamalytics":
		return "<20 低 / 20–59 中 / 60–89 高 / ≥90 极高"
	case "ipapi":
		return "采用上游 Very Low–Very High 标签"
	case "abuseipdb":
		return "<25 低 / 25–74 注意 / ≥75 高"
	case "ipqs":
		return "<75 低 / 75–84 可疑 / 85–89 高 / ≥90 极高"
	case "dbip":
		return "low/medium/high → 0/50/100*"
	default:
		return "—"
	}
}

func (bundle ipQualityBundle) factorTable() model.Table {
	columns := []string{"因子"}
	for _, id := range factorSourceOrder {
		columns = append(columns, qualitySourceLabels[id])
	}
	table := model.Table{
		Title:   "IPv" + bundle.Version + " · 风险因子矩阵",
		Columns: columns,
	}
	type factor struct {
		label string
		value func(qualityFinding) string
	}
	factors := []factor{
		{"国家/地区", func(f qualityFinding) string { return factorCountry(f) }},
		{"代理", func(f qualityFinding) string { return factorSignal(f, f.Proxy) }},
		{"Tor", func(f qualityFinding) string { return factorSignal(f, f.Tor) }},
		{"VPN", func(f qualityFinding) string { return factorSignal(f, f.VPN) }},
		{"机房", func(f qualityFinding) string { return factorSignal(f, f.Server) }},
		{"滥用", func(f qualityFinding) string { return factorSignal(f, f.Abuser) }},
		{"机器人", func(f qualityFinding) string { return factorSignal(f, f.Robot) }},
	}
	for _, item := range factors {
		row := []string{item.label}
		for _, id := range factorSourceOrder {
			row = append(row, item.value(bundle.Findings[id]))
		}
		table.Rows = append(table.Rows, row)
	}
	return table
}

func (bundle ipQualityBundle) statusTable() model.Table {
	table := model.Table{
		Title:   "IPv" + bundle.Version + " · 数据源状态",
		Columns: []string{"数据源", "状态", "通道", "耗时"},
	}
	if bundle.Origin.Enabled {
		table.Rows = append(table.Rows, []string{
			qualitySourceLabels["maxmind"],
			originStatus(bundle.Origin),
			bundle.Origin.Access,
			durationText(bundle.Origin.Latency),
		})
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		table.Rows = append(table.Rows, []string{
			qualitySourceLabels[id],
			findingStatus(finding),
			findingAccess(finding),
			durationText(finding.Latency),
		})
	}
	return table
}

func (bundle ipQualityBundle) measurements() []model.Measurement {
	var measurements []model.Measurement
	for _, id := range scoreSourceOrder {
		finding := bundle.Findings[id]
		if finding.Score == nil {
			continue
		}
		value := *finding.Score
		method := finding.ScoreKind + "；" + finding.Access
		if id == "dbip" {
			method += "；low/medium/high 映射为 0/50/100，仅用于展示"
		}
		label := "IPv" + bundle.Version + " " + qualitySourceLabels[id] + " 风险值"
		display := formatScore(value) + "/100"
		if id == "dbip" {
			label = "IPv" + bundle.Version + " " + qualitySourceLabels[id] + " 等效风险值"
			display = formatScore(value) + "*/100"
		}
		measurements = append(measurements, model.Measurement{
			Key:            "ipv" + bundle.Version + "_" + id + "_risk_score",
			Label:          label,
			Value:          value,
			Unit:           "/100",
			Display:        display,
			Rating:         finding.Risk,
			Method:         method,
			HigherIsBetter: model.BoolPtr(false),
		})
	}
	return measurements
}

func (bundle ipQualityBundle) successfulSources() (successful, enabled int) {
	if bundle.Origin.Enabled {
		enabled++
		if bundle.Origin.Err == nil {
			successful++
		}
	}
	for _, finding := range bundle.Findings {
		if !finding.Enabled {
			continue
		}
		enabled++
		if finding.Err == nil {
			successful++
		}
	}
	return successful, enabled
}

func (bundle ipQualityBundle) failedSourceNames() []string {
	var names []string
	if bundle.Origin.Enabled && bundle.Origin.Err != nil {
		names = append(names, qualitySourceLabels["maxmind"])
	}
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		if finding.Enabled && finding.Err != nil {
			names = append(names, finding.Name)
		}
	}
	return names
}

func (bundle ipQualityBundle) partialSourceNames() []string {
	var names []string
	for _, id := range qualitySourceOrder {
		if id == "maxmind" {
			continue
		}
		finding := bundle.Findings[id]
		if finding.Enabled && finding.Err == nil && finding.Partial != "" {
			names = append(names, finding.Name)
		}
	}
	return names
}

func findingValue(finding qualityFinding, value string) string {
	switch {
	case !finding.Enabled:
		return "未启用"
	case finding.Err != nil:
		return "失败"
	case strings.TrimSpace(value) == "":
		return "—"
	default:
		return value
	}
}

func findingAccess(finding qualityFinding) string {
	switch {
	case !finding.Enabled:
		return "未启用"
	case finding.Access != "":
		return finding.Access
	default:
		return "—"
	}
}

func findingStatus(finding qualityFinding) string {
	switch {
	case !finding.Enabled:
		return "未启用"
	case finding.Err != nil:
		return "失败：" + finding.Err.Error()
	case finding.Partial != "":
		return "部分：" + finding.Partial
	default:
		return "成功"
	}
}

func originStatus(origin originAssessment) string {
	if origin.Err != nil {
		return "失败：" + origin.Err.Error()
	}
	return "成功"
}

func factorCountry(finding qualityFinding) string {
	if !finding.Enabled || finding.Err != nil || finding.Country == "" {
		return "—"
	}
	return finding.Country
}

func factorSignal(finding qualityFinding, signal qualitySignal) string {
	if !finding.Enabled || finding.Err != nil || !signal.Known {
		return "—"
	}
	if signal.Value {
		return "是"
	}
	return "否"
}

func scoreText(finding qualityFinding) string {
	switch {
	case !finding.Enabled:
		return "未启用"
	case finding.Err != nil:
		return "失败"
	case finding.Score == nil:
		return "未返回"
	case finding.ID == "dbip":
		return formatScore(*finding.Score) + "*/100"
	default:
		return formatScore(*finding.Score) + "/100"
	}
}

func scoreBar(score float64) string {
	const width = 12
	score = math.Max(0, math.Min(100, score))
	filled := int(math.Round(score / 100 * width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatScore(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.005 {
		return strconv.FormatInt(int64(math.Round(value)), 10)
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func durationText(value time.Duration) string {
	if value <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f ms", float64(value)/float64(time.Millisecond))
}

func normalizeNetworkType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "isp", "line isp", "fixed line isp", "consumer":
		return "家宽"
	case "hosting", "data center", "datacenter", "data center/web hosting/transit":
		return "机房"
	case "business", "commercial":
		return "商业"
	case "education", "university/college/school":
		return "教育"
	case "government":
		return "政府"
	case "banking":
		return "银行"
	case "organization":
		return "组织"
	case "military":
		return "军队"
	case "library":
		return "图书馆"
	case "content delivery network", "cdn":
		return "CDN"
	case "mobile", "mobile isp":
		return "移动网络"
	case "search engine spider", "spider":
		return "搜索引擎"
	case "reserved":
		return "保留地址"
	case "", "unknown", "null":
		return ""
	default:
		return "其他"
	}
}

func normalizeIP2LocationType(value string) string {
	code := strings.ToUpper(strings.TrimSpace(strings.Split(value, "/")[0]))
	switch code {
	case "COM":
		return "商业"
	case "DCH":
		return "机房"
	case "EDU":
		return "教育"
	case "GOV":
		return "政府"
	case "ORG":
		return "组织"
	case "MIL":
		return "军队"
	case "LIB":
		return "图书馆"
	case "CDN":
		return "CDN"
	case "ISP":
		return "家宽"
	case "MOB":
		return "移动网络"
	case "SES":
		return "搜索引擎"
	case "RSV":
		return "保留地址"
	case "":
		return ""
	default:
		return normalizeNetworkType(value)
	}
}

func normalizeAbuseIPDBType(value string) string {
	return normalizeNetworkType(value)
}

func knownSignal(value bool) qualitySignal {
	return qualitySignal{Known: true, Value: value}
}

func pointerSignal(value *bool) qualitySignal {
	if value == nil {
		return qualitySignal{}
	}
	return knownSignal(*value)
}

func anyPointerSignal(values ...*bool) qualitySignal {
	known := false
	for _, value := range values {
		if value == nil {
			continue
		}
		known = true
		if *value {
			return knownSignal(true)
		}
	}
	if known {
		return knownSignal(false)
	}
	return qualitySignal{}
}

func firstKnownSignal(values ...qualitySignal) qualitySignal {
	for _, value := range values {
		if value.Known {
			return value
		}
	}
	return qualitySignal{}
}

func anyKnownSignal(values ...qualitySignal) bool {
	for _, value := range values {
		if value.Known {
			return true
		}
	}
	return false
}

func findingHasEvidence(finding qualityFinding) bool {
	return finding.Country != "" ||
		finding.Usage != "" ||
		finding.Company != "" ||
		finding.Score != nil ||
		anyKnownSignal(
			finding.Proxy,
			finding.Tor,
			finding.VPN,
			finding.Server,
			finding.Abuser,
			finding.Robot,
		)
}

func knownWhenNonEmpty(value bool, evidence string) qualitySignal {
	if strings.TrimSpace(evidence) == "" {
		return qualitySignal{}
	}
	return knownSignal(value)
}

func threatDetailSignal(details []string) qualitySignal {
	if details == nil {
		return qualitySignal{}
	}
	for _, detail := range details {
		detail = strings.ToLower(detail)
		if detail == "attack-source" || detail == "bot" || strings.HasPrefix(detail, "bot-") {
			return knownSignal(true)
		}
	}
	return knownSignal(false)
}

func yesNoSignal(value string) qualitySignal {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(value, "*_`"))) {
	case "yes", "true", "detected":
		return knownSignal(true)
	case "no", "false", "not detected":
		return knownSignal(false)
	default:
		return qualitySignal{}
	}
}

func signalFromPageText(value, positivePattern, negativePattern string) qualitySignal {
	if pattern, err := regexp.Compile(negativePattern); err == nil && pattern.MatchString(value) {
		return knownSignal(false)
	}
	if pattern, err := regexp.Compile(positivePattern); err == nil && pattern.MatchString(value) {
		return knownSignal(true)
	}
	return qualitySignal{}
}

func providerPageText(value string) string {
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = strings.NewReplacer("**", "", "__", "", "`", "").Replace(value)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
}

func markdownTableValues(page, requiredHeader string) map[string]string {
	lines := strings.Split(page, "\n")
	requiredHeader = strings.ToLower(strings.TrimSpace(requiredHeader))
	for index, line := range lines {
		headers := markdownCells(line)
		if len(headers) == 0 || !containsFold(headers, requiredHeader) {
			continue
		}
		for next := index + 1; next < len(lines); next++ {
			values := markdownCells(lines[next])
			if len(values) == 0 || markdownSeparator(values) {
				continue
			}
			if len(values) != len(headers) {
				break
			}
			result := make(map[string]string, len(headers))
			for cell := range headers {
				result[strings.ToLower(headers[cell])] = values[cell]
			}
			return result
		}
	}
	return nil
}

func markdownCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	raw := strings.Split(line, "|")
	cells := make([]string, 0, len(raw))
	for _, value := range raw {
		cells = append(cells, strings.TrimSpace(strings.Trim(value, "*_`")))
	}
	return cells
}

func markdownSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, value := range cells {
		if strings.Trim(value, " :-") != "" {
			return false
		}
	}
	return true
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func appendPartial(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	switch {
	case existing == "":
		return addition
	case addition == "":
		return existing
	default:
		return existing + "；" + addition
	}
}

func parseProbabilityScore(value string) *float64 {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return nil
	}
	number, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "%"), 64)
	if err != nil {
		return nil
	}
	if strings.Contains(fields[0], "%") {
		return validScore(&number)
	}
	number *= 100
	return validScore(&number)
}

func scoreLabel(value string) string {
	start := strings.Index(value, "(")
	end := strings.LastIndex(value, ")")
	if start < 0 || end <= start {
		return ""
	}
	return strings.TrimSpace(value[start+1 : end])
}

func translateRiskLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "very low":
		return "极低"
	case "low":
		return "低"
	case "elevated":
		return "中等"
	case "high":
		return "高"
	case "very high":
		return "极高"
	default:
		return value
	}
}

func validScore(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100 {
		return nil
	}
	copy := *value
	return &copy
}

func riskIP2Location(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 33:
		return "低"
	case *score < 66:
		return "中等"
	default:
		return "高"
	}
}

func riskScamalytics(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 20:
		return "低"
	case *score < 60:
		return "中等"
	case *score < 90:
		return "高"
	default:
		return "极高"
	}
}

func riskAbuseIPDB(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 25:
		return "低"
	case *score < 75:
		return "需留意"
	default:
		return "高"
	}
}

func riskIPQS(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 75:
		return "低"
	case *score < 85:
		return "可疑"
	case *score < 90:
		return "高"
	default:
		return "极高"
	}
}

func setDBIPRisk(finding *qualityFinding, level string) {
	level = strings.ToLower(strings.TrimSpace(level))
	var score float64
	switch level {
	case "low":
		score = 0
		finding.Risk = "低"
	case "medium":
		score = 50
		finding.Risk = "中等"
	case "high":
		score = 100
		finding.Risk = "高"
	default:
		return
	}
	finding.Score = &score
	finding.ScoreKind = "威胁等级（0/50/100 映射*）"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func firstMatch(pattern *regexp.Regexp, value string) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func browserUserAgent() string {
	return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36"
}

var (
	dbIPThreatPattern        = regexp.MustCompile(`(?is)Estimated threat level for this IP address is\s*<span[^>]*>\s*([^<]+)`)
	dbIPCountryPattern       = regexp.MustCompile(`(?is)"countryCode"\s*:\s*"([A-Za-z]{2})"`)
	ipqsPublicScorePattern   = regexp.MustCompile(`(?i)(?:scoring|scored|score(?:\s+of|\s+is)?)\s*([0-9]{1,3}(?:\.[0-9]+)?)\s+out of 100`)
	ipqsPublicCountryPattern = regexp.MustCompile(`(?i)\blocated in\b.{0,240}?\b([A-Z]{2})\b\s+that is assigned`)
	htmlTagPattern           = regexp.MustCompile(`(?s)<[^>]+>`)
	whitespacePattern        = regexp.MustCompile(`\s+`)
)

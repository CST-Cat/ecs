package probe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// 扩充数据源。
//
// IP 信誉判断本质上就是数据库业务，个人无法自建；既然已经接入了十余家闭源
// 商业 API，再以"闭源"为由拒绝其余的只是双标。真正要守住的是别的：
// 保留每家的原始语义与查询通道、失败如实标记、绝不平均成一个总分、
// 绝不用别家的分数顶替失败的那家。
//
// 下面每一家的可用性都在 2026-08-01 实测过，通道与限制写在各自的注释里。

// fetchIPAPICom 查询 ip-api.com。
//
// 免密可用，且直接给出 proxy / hosting / mobile 三个风险布尔值——这是所有免密
// 数据源里字段最实用的一家。免费版限制为 HTTP 且有频率限制（约 45 次/分钟）。
func fetchIPAPICom(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipapicom")
	finding.Access = "官方免密接口（HTTP）"
	// 免费端点只支持 HTTP；付费的 pro 端点才提供 HTTPS。这里如实披露而不是假装加密。
	values := url.Values{"fields": []string{
		"status,message,countryCode,isp,org,as,asname,reverse,mobile,proxy,hosting,query",
	}}
	endpoint := "http://ip-api.com/json/" + url.PathEscape(ip) + "?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 512*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		CountryCode string `json:"countryCode"`
		ISP         string `json:"isp"`
		Org         string `json:"org"`
		ASName      string `json:"asname"`
		Mobile      *bool  `json:"mobile"`
		Proxy       *bool  `json:"proxy"`
		Hosting     *bool  `json:"hosting"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if response.Status != "success" {
		finding.Err = errors.New(fallback(response.Message, "查询未成功"))
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Proxy = pointerSignal(response.Proxy)
	finding.Server = pointerSignal(response.Hosting)
	if response.Mobile != nil && *response.Mobile {
		finding.Usage = "移动网络"
	} else if response.Hosting != nil && *response.Hosting {
		finding.Usage = "机房"
	} else if response.ISP != "" {
		finding.Usage = "家宽"
	}
	finding.Company = normalizeNetworkType(response.Org)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	finding.Partial = "免费端点仅 HTTP，且不提供欺诈分"
	return finding
}

// fetchIPSB 查询 ip.sb。
//
// 免密、无频率限制说明，但只提供地理与 ISP，没有任何风险字段。
// 价值在于给国家与运营商归属提供一个独立的交叉验证来源。
func fetchIPSB(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipsb")
	finding.Access = "官方免密接口"
	endpoint := "https://api.ip.sb/geoip/" + url.PathEscape(ip)
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 512*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		CountryCode  string `json:"country_code"`
		ISP          string `json:"isp"`
		Organization string `json:"organization"`
		ASN          int    `json:"asn"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode)
	finding.Company = normalizeNetworkType(response.Organization)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	finding.Partial = "仅提供地理与运营商，无风险字段"
	return finding
}

// fetchVirusTotal 查询 VirusTotal 的 IP 信誉。
//
// 需要免费注册获取 API key（VIRUSTOTAL_API_KEY）。它的价值与其他风险库不同：
// 给出的是数十家安全厂商对该 IP 的投票统计，而不是单一模型的评分。
// 因此这里把"判为恶意的厂商占比"作为分值，并在口径里写清楚。
func fetchVirusTotal(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("virustotal")
	apiKey := strings.TrimSpace(os.Getenv("VIRUSTOTAL_API_KEY"))
	if apiKey == "" {
		finding.Err = errors.New("需要 VIRUSTOTAL_API_KEY（免费注册可得）")
		finding.Access = "官方 API（需用户密钥）"
		return finding
	}
	finding.Access = "官方 API（用户密钥）"
	endpoint := "https://www.virustotal.com/api/v3/ip_addresses/" + url.PathEscape(ip)
	headers := map[string]string{"x-apikey": apiKey}
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, headers, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Data struct {
			Attributes struct {
				Country           string `json:"country"`
				LastAnalysisStats struct {
					Harmless   int `json:"harmless"`
					Malicious  int `json:"malicious"`
					Suspicious int `json:"suspicious"`
					Undetected int `json:"undetected"`
				} `json:"last_analysis_stats"`
				Reputation int `json:"reputation"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	stats := response.Data.Attributes.LastAnalysisStats
	total := stats.Harmless + stats.Malicious + stats.Suspicious + stats.Undetected
	finding.Country = strings.ToUpper(response.Data.Attributes.Country)
	if total > 0 {
		flagged := float64(stats.Malicious+stats.Suspicious) / float64(total) * 100
		finding.Score = validScore(&flagged)
		finding.ScoreKind = "厂商判为恶意/可疑的占比"
		finding.Risk = riskVirusTotal(finding.Score)
		finding.Abuser = knownSignal(stats.Malicious > 0)
	}
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	finding.Partial = "分值是厂商投票占比，与单模型欺诈分不可直接比较"
	return finding
}

// fetchIPGeolocation 查询 ipgeolocation.io。
//
// 免费额度需要注册获取 key（IPGEOLOCATION_API_KEY）。无 key 时返回 401，
// 因此不做无谓的请求。
func fetchIPGeolocation(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipgeolocation")
	apiKey := strings.TrimSpace(os.Getenv("IPGEOLOCATION_API_KEY"))
	if apiKey == "" {
		finding.Err = errors.New("需要 IPGEOLOCATION_API_KEY（免费注册可得）")
		finding.Access = "官方 API（需用户密钥）"
		return finding
	}
	finding.Access = "官方 API（用户密钥）"
	values := url.Values{"apiKey": []string{apiKey}, "ip": []string{ip}, "include": []string{"security"}}
	endpoint := "https://api.ipgeolocation.io/ipgeo?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		CountryCode2 string `json:"country_code2"`
		Security     struct {
			ThreatScore     *float64 `json:"threat_score"`
			IsTor           *bool    `json:"is_tor"`
			IsProxy         *bool    `json:"is_proxy"`
			IsAnonymous     *bool    `json:"is_anonymous"`
			IsKnownAttacker *bool    `json:"is_known_attacker"`
			IsCloudProvider *bool    `json:"is_cloud_provider"`
		} `json:"security"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.CountryCode2)
	finding.Score = validScore(response.Security.ThreatScore)
	finding.ScoreKind = "威胁分"
	finding.Risk = riskGenericHundred(finding.Score)
	finding.Proxy = pointerSignal(response.Security.IsProxy)
	finding.Tor = pointerSignal(response.Security.IsTor)
	finding.VPN = pointerSignal(response.Security.IsAnonymous)
	finding.Server = pointerSignal(response.Security.IsCloudProvider)
	finding.Abuser = pointerSignal(response.Security.IsKnownAttacker)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

// fetchBigDataCloud 查询 bigdatacloud.com。
//
// 实测其免密端点返回 403（配额耗尽），因此必须提供 key（BIGDATACLOUD_API_KEY）。
func fetchBigDataCloud(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("bigdatacloud")
	apiKey := strings.TrimSpace(os.Getenv("BIGDATACLOUD_API_KEY"))
	if apiKey == "" {
		finding.Err = errors.New("需要 BIGDATACLOUD_API_KEY（免密端点实测返回 403 配额超限）")
		finding.Access = "官方 API（需用户密钥）"
		return finding
	}
	finding.Access = "官方 API（用户密钥）"
	values := url.Values{"key": []string{apiKey}, "ip": []string{ip}}
	endpoint := "https://api.bigdatacloud.net/data/ip-geolocation-full?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 1024*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Country struct {
			ISOAlpha2 string `json:"isoAlpha2"`
		} `json:"country"`
		Network struct {
			IsBogon *bool `json:"isBogon"`
		} `json:"network"`
		HazardReport struct {
			IsKnownAsTorServer    *bool `json:"isKnownAsTorServer"`
			IsKnownAsProxy        *bool `json:"isKnownAsProxy"`
			IsKnownAsVPN          *bool `json:"isKnownAsVpn"`
			IsKnownAsPublicRouter *bool `json:"isKnownAsPublicRouter"`
			IsKnownAsMailServer   *bool `json:"isKnownAsMailServer"`
			IsSpamhausDrop        *bool `json:"isSpamhausDrop"`
		} `json:"hazardReport"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	finding.Country = strings.ToUpper(response.Country.ISOAlpha2)
	finding.Proxy = pointerSignal(response.HazardReport.IsKnownAsProxy)
	finding.Tor = pointerSignal(response.HazardReport.IsKnownAsTorServer)
	finding.VPN = pointerSignal(response.HazardReport.IsKnownAsVPN)
	finding.Abuser = pointerSignal(response.HazardReport.IsSpamhausDrop)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

// fetchGetIPIntel 查询 getipintel.net 的代理/VPN 概率。
//
// 免密，但其服务条款要求在请求里附带联系邮箱，且明确对数据中心 IP 段设限——
// 实测本机（DigitalOcean 出口）直接被拒："Your connecting IP has been banned"。
// 因此邮箱只从环境变量读取，用户没提供就不发请求：拿别人的邮箱去满足对方条款
// 是不合适的。
func fetchGetIPIntel(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("getipintel")
	contact := strings.TrimSpace(os.Getenv("GETIPINTEL_CONTACT"))
	if contact == "" {
		finding.Err = errors.New("需要 GETIPINTEL_CONTACT（其条款要求提供联系邮箱）")
		finding.Access = "官方免密接口（需联系邮箱）"
		return finding
	}
	finding.Access = "官方免密接口（已提供联系邮箱）"
	values := url.Values{"ip": []string{ip}, "contact": []string{contact}, "format": []string{"json"}}
	endpoint := "https://check.getipintel.net/check.php?" + values.Encode()
	body, latency, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 256*1024)
	finding.Latency = latency
	if err != nil {
		finding.Err = err
		return finding
	}
	var response struct {
		Status  string `json:"status"`
		Result  string `json:"result"`
		Message string `json:"message"`
	}
	if err := decodeJSON(body, &response); err != nil {
		finding.Err = err
		return finding
	}
	if !strings.EqualFold(response.Status, "success") {
		finding.Err = errors.New(compactMessage(response.Message))
		return finding
	}
	probability, parseErr := strconv.ParseFloat(response.Result, 64)
	if parseErr != nil || probability < 0 {
		finding.Err = errors.New("未返回有效概率")
		return finding
	}
	percent := probability * 100
	finding.Score = validScore(&percent)
	finding.ScoreKind = "代理/VPN 概率"
	finding.Risk = riskGenericHundred(finding.Score)
	finding.Proxy = knownSignal(probability >= 0.95)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
	}
	return finding
}

// compactMessage 截断上游返回的错误文本。
func compactMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "查询未成功"
	}
	if len(message) > 120 {
		return message[:120] + "…"
	}
	return message
}

// riskVirusTotal 按厂商投票占比给出风险等级。
//
// VirusTotal 的语义与欺诈分完全不同：只要有一两家厂商标记就值得留意，
// 因此阈值远低于欺诈分类的库。
func riskVirusTotal(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score <= 0:
		return "低"
	case *score < 3:
		return "需留意"
	case *score < 10:
		return "高"
	default:
		return "极高"
	}
}

// riskGenericHundred 用于本身就是 0–100 风险刻度的数据源。
func riskGenericHundred(score *float64) string {
	if score == nil {
		return ""
	}
	switch {
	case *score < 25:
		return "低"
	case *score < 50:
		return "中等"
	case *score < 75:
		return "高"
	default:
		return "极高"
	}
}

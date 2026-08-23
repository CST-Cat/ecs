package probe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// 扩充数据源。
//
// IP 信誉判断本质上就是数据库业务，个人无法自建，因此接入闭源商业 API 本身
// 不是问题——项目已经接了十余家。要守住的是别的：保留每家的原始语义与查询
// 通道、失败如实标记、绝不平均成一个总分、绝不用别家的分数顶替失败的那家。
//
// 但**纯密钥源不收**：VirusTotal、ipgeolocation、BigDataCloud、getipintel 都
// 曾接入又移除，因为它们没有任何免密兜底路径——没有 key 就完全不能用。
// 工具作者无法替使用者提供密钥，绝大多数人也不会去申请，结果是每个人的报告里
// 都多出几行"需要 XXX_API_KEY"的噪音，既无信息量又违背"默认不要求 API Key"。
// 现有各源之所以保留，是因为它们都有免密或社区兜底通道，没 key 也能出数据。
//
// 下面每一家的可用性都在 2026-08-01 实测过，通道与限制写在各自的注释里。

// fetchIPAPICom 查询 ip-api.com。
//
// 免密可用，且直接给出 proxy / hosting / mobile 三个风险布尔值——这是所有免密
// 数据源里字段最实用的一家。免费版限制为 HTTP 且有频率限制（约 45 次/分钟）。
func fetchIPAPICom(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipapicom")
	finding.Access = networkChannelOfficialFree
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
		finding.Usage = "probe.network.network_type.mobile"
	} else if response.Hosting != nil && *response.Hosting {
		finding.Usage = "probe.network.network_type.datacenter"
	} else if response.ISP != "" {
		finding.Usage = "probe.network.network_type.residential"
	}
	finding.Company = normalizeNetworkType(response.Org)
	if !findingHasEvidence(finding) {
		finding.Err = errors.New("响应缺少所需字段")
		return finding
	}
	finding.Partial = networkPartialScore
	return finding
}

// fetchIPSB 查询 ip.sb。
//
// 免密、无频率限制说明，但只提供地理与 ISP，没有任何风险字段。
// 价值在于给国家与运营商归属提供一个独立的交叉验证来源。
func fetchIPSB(ctx context.Context, env Environment, _ *http.Client, ip string) qualityFinding {
	finding := newFinding("ipsb")
	finding.Access = networkChannelOfficialFree
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
	finding.Partial = networkPartialScore
	return finding
}

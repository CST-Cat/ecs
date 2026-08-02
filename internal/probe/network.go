package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type networkProbe struct{}

func (networkProbe) ID() string         { return "network" }
func (networkProbe) Title() string      { return "网络与 IP 质量" }
func (networkProbe) NeedsNetwork() bool { return true }

type ipAPIResponse struct {
	IP           string  `json:"ip"`
	RIR          string  `json:"rir"`
	IsBogon      bool    `json:"is_bogon"`
	IsMobile     bool    `json:"is_mobile"`
	IsSatellite  bool    `json:"is_satellite"`
	IsCrawler    bool    `json:"is_crawler"`
	IsDatacenter bool    `json:"is_datacenter"`
	IsTor        bool    `json:"is_tor"`
	IsProxy      bool    `json:"is_proxy"`
	IsVPN        bool    `json:"is_vpn"`
	IsAbuser     bool    `json:"is_abuser"`
	ElapsedMS    float64 `json:"elapsed_ms"`
	Error        string  `json:"error"`
	ASN          struct {
		ASN          int    `json:"asn"`
		AbuserScore  string `json:"abuser_score"`
		Route        string `json:"route"`
		Organization string `json:"org"`
		Domain       string `json:"domain"`
		Type         string `json:"type"`
		Country      string `json:"country"`
		RIR          string `json:"rir"`
	} `json:"asn"`
	Company struct {
		Name        string `json:"name"`
		AbuserScore string `json:"abuser_score"`
		Domain      string `json:"domain"`
		Type        string `json:"type"`
		Network     string `json:"network"`
	} `json:"company"`
	Location struct {
		Continent   string `json:"continent"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		State       string `json:"state"`
		City        string `json:"city"`
		Timezone    string `json:"timezone"`
		Accuracy    string `json:"accuracy"`
	} `json:"location"`
}

type ipLookup struct {
	Version string
	Data    ipAPIResponse
	Err     error
	Latency time.Duration
}

func (networkProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("network", "网络与 IP 质量")
	result.Description = "IPv4/IPv6 出口、原生/广播判定、五库类型、六库风险分与九库风险因子"
	result.Methodology = model.Methodology{
		Kind:            "provider-assessment",
		Label:           "第三方评估",
		Engine:          "multi-provider IP intelligence",
		Profile:         "source-preserving cross-check v1",
		ComparisonScope: "同一数据源、字段口径与查询时间；不同供应商分数不可直接混算",
	}

	versions := config.IPVersions(env.Config.IPVersion)
	lookups := make(chan ipLookup, len(versions))
	var wg sync.WaitGroup
	for _, version := range versions {
		wg.Add(1)
		go func(version string) {
			defer wg.Done()
			data, latency, err := lookupIP(ctx, env, version)
			lookups <- ipLookup{Version: version, Data: data, Err: err, Latency: latency}
		}(version)
	}
	go func() {
		wg.Wait()
		close(lookups)
	}()

	var found []ipLookup
	for lookup := range lookups {
		if lookup.Err == nil && lookup.Data.IP != "" {
			found = append(found, lookup)
		} else {
			reason := "unavailable"
			if lookup.Err != nil {
				reason = lookup.Err.Error()
			}
			// 错误原文（Go 的网络错误）拼进句子会让整句无法翻译也无法对照，
			// 因此说明句保持固定，具体原因单独成字段。
			result.Notes = append(result.Notes, fmt.Sprintf("IPv%s 出口查询失败，详见下方失败原因。", lookup.Version))
			result.Fields = append(result.Fields, model.Field{
				Key: "ipv" + lookup.Version + "_lookup_error", Label: "IPv" + lookup.Version + " 失败原因", Value: reason,
			})
		}
	}
	if len(found) == 0 {
		result.Fail(fmt.Errorf("IPv4 与 IPv6 出口信息均无法查询"))
		result.Finish(start)
		return result
	}
	sort.Slice(found, func(i, j int) bool {
		return found[i].Version < found[j].Version
	})

	type qualityResult struct {
		version string
		bundle  ipQualityBundle
	}
	qualityResults := make(chan qualityResult, len(found))
	var qualityWG sync.WaitGroup
	for _, lookup := range found {
		qualityWG.Add(1)
		go func(lookup ipLookup) {
			defer qualityWG.Done()
			qualityResults <- qualityResult{
				version: lookup.Version,
				bundle:  collectIPQuality(ctx, env, lookup),
			}
		}(lookup)
	}
	go func() {
		qualityWG.Wait()
		close(qualityResults)
	}()
	bundles := make(map[string]ipQualityBundle, len(found))
	for item := range qualityResults {
		bundles[item.version] = item.bundle
	}

	overview := model.Table{
		Title:   "出口概览",
		Columns: []string{"协议", "网络类型", "机房", "代理", "VPN", "Tor", "滥用记录", "数据源耗时"},
	}
	var summaries []string
	var providerNotes []string
	for _, lookup := range found {
		prefix := "ipv" + lookup.Version
		data := lookup.Data
		bundle := bundles[lookup.Version]
		location := strings.Trim(strings.Join([]string{data.Location.Country, data.Location.State, data.Location.City}, " / "), " /")
		if location == "" {
			location = "unknown"
		}
		asn := "unknown"
		if data.ASN.ASN > 0 {
			asn = fmt.Sprintf("AS%d %s", data.ASN.ASN, data.ASN.Organization)
		}
		ipType := "未启用"
		if bundle.Origin.Enabled {
			ipType = bundle.Origin.Label
			if bundle.Origin.Err != nil {
				ipType = "无法判定"
			}
			if bundle.Origin.UsageCountry != "" || bundle.Origin.RegisteredCountry != "" {
				ipType += fmt.Sprintf(
					"（使用地 %s / 注册地 %s）",
					fallback(bundle.Origin.UsageCountry, "—"),
					fallback(bundle.Origin.RegisteredCountry, "—"),
				)
			}
		}
		result.Fields = append(result.Fields,
			model.Field{Key: prefix, Label: "IPv" + lookup.Version + " 出口", Value: data.IP, Sensitive: true},
			model.Field{Key: prefix + "_asn", Label: "IPv" + lookup.Version + " ASN", Value: asn},
			model.Field{Key: prefix + "_route", Label: "IPv" + lookup.Version + " 路由段", Value: fallback(data.ASN.Route, "unknown")},
			model.Field{Key: prefix + "_location", Label: "IPv" + lookup.Version + " 地理", Value: location},
			model.Field{Key: prefix + "_owner", Label: "IPv" + lookup.Version + " 所有者", Value: fallback(data.Company.Name, data.ASN.Organization)},
			model.Field{Key: prefix + "_ip_type", Label: "IPv" + lookup.Version + " IP 类型", Value: ipType},
		)
		overview.Rows = append(overview.Rows, []string{
			"IPv" + lookup.Version,
			fallback(normalizeNetworkType(firstNonEmpty(data.Company.Type, data.ASN.Type)), fallback(data.Company.Type, data.ASN.Type)),
			yesNo(data.IsDatacenter),
			yesNo(data.IsProxy),
			yesNo(data.IsVPN),
			yesNo(data.IsTor),
			yesNo(data.IsAbuser),
			fmt.Sprintf("%.0f ms", float64(lookup.Latency)/float64(time.Millisecond)),
		})
		result.Measurements = append(result.Measurements, bundle.measurements()...)
		result.Tables = append(
			result.Tables,
			bundle.typeTable(),
			bundle.scoreTable(),
			bundle.factorTable(),
			bundle.statusTable(),
		)
		successful, enabled := bundle.successfulSources()
		sourceSummary := "附加源未启用"
		if enabled > 0 {
			sourceSummary = fmt.Sprintf("数据源响应 %d/%d", successful, enabled)
		}
		summaries = append(
			summaries,
			fmt.Sprintf(
				"IPv%s %s AS%d · %s · %s",
				lookup.Version,
				data.Location.CountryCode,
				data.ASN.ASN,
				fallback(bundle.Origin.Label, "类型未判定"),
				sourceSummary,
			),
		)
		if failed := bundle.failedSourceNames(); len(failed) > 0 {
			providerNotes = append(providerNotes, fmt.Sprintf("IPv%s 数据源失败：%s。详见数据源状态表。", lookup.Version, strings.Join(failed, "、")))
		}
		if partial := bundle.partialSourceNames(); len(partial) > 0 {
			providerNotes = append(providerNotes, fmt.Sprintf("IPv%s 数据源字段不完整：%s。通常由免费套餐字段限制导致。", lookup.Version, strings.Join(partial, "、")))
		}
	}
	result.Tables = append([]model.Table{overview}, result.Tables...)
	result.Fields = append([]model.Field{{
		Key: "ip_version_mode", Label: "协议族模式", Value: fallback(env.Config.IPVersion, config.IPVersionAuto),
	}}, result.Fields...)
	result.Sources = []model.Source{
		{Name: "ipapi.is", URL: "https://ipapi.is/", Purpose: "出口 IP、ASN、地理、网络类型与滥用概率"},
		{Name: "IPQuality", URL: "https://github.com/xykt/IPQuality", Purpose: "多源覆盖、字段映射与社区兼容通道（AGPL-3.0）"},
		{Name: "check.place", URL: "https://check.place/", Purpose: "无用户密钥时的 MaxMind、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQS、ipdata 社区中转"},
		{Name: "IPinfo", URL: "https://ipinfo.io/developers", Purpose: "网络类型、公司类型与隐私信号"},
		{Name: "ipregistry", URL: "https://ipregistry.co/docs/", Purpose: "网络类型与威胁信号"},
		{Name: "IP2Location.io", URL: "https://www.ip2location.io/ip2location-documentation", Purpose: "IP 类型、代理因子与 IP2Proxy 欺诈分"},
		{Name: "AbuseIPDB", URL: "https://docs.abuseipdb.com/", Purpose: "90 天滥用置信度与使用类型"},
		{Name: "Scamalytics", URL: "https://scamalytics.com/", Purpose: "Web 流量欺诈分与匿名化信号"},
		{Name: "IPQualityScore", URL: "https://www.ipqualityscore.com/documentation/proxy-detection-api/overview", Purpose: "IP 欺诈分、代理、VPN、Tor 与近期滥用"},
		{Name: "DB-IP", URL: "https://db-ip.com/api/doc.php", Purpose: "威胁等级、代理、爬虫与网络属性"},
		{Name: "ipdata", URL: "https://ipdata.co/", Purpose: "代理、Tor、机房与已知威胁因子"},
		{Name: "IPWHOIS", URL: "https://ipwhois.io/documentation", Purpose: "国家、代理、VPN、Tor 与托管信号"},
		{Name: "Jina Reader", URL: "https://github.com/jina-ai/reader", Purpose: "IPQS 官方公开页的最后一级免密只读兜底（可能读取 1 小时缓存）"},
	}
	result.Notes = append(result.Notes,
		"本模块会把待查询的出口 IP 发送给表内数据源；不会上传 CPU、磁盘、路由等其他测试结果。",
		"默认不要求 API 密钥；官方免密/公开接口优先，付费字段再由 IPQuality/check.place 或已披露的公开页读取链路补充，报告会逐行标明通道。",
		"各家分值口径不同，ecs 不做无依据的平均总分；风险评分表保留原始语义，因子矩阵保留冲突。",
		"“原生/广播 IP”仅按 MaxMind 使用地与注册地是否一致判定，是地理一致性线索，不等于住宅 IP、解锁能力或低欺诈风险。",
		"DB-IP 公开风险页返回 low/medium/high 时，0/50/100 后带 * 的值仅用于画进度条；若只能访问官方 free API，则不填造威胁分。",
	)
	result.Notes = append(result.Notes, providerNotes...)
	if proxyEnvironmentEnabled() {
		result.Notes = append(result.Notes, "检测到 HTTP(S)_PROXY 环境变量；出口发现与性能探针仍直连，仅 IPQS 对明确目标 IP 的 Jina Reader 最后兜底可使用系统代理，并在通道列披露。")
	}
	if len(providerNotes) > 0 {
		result.Status = model.StatusWarning
	}
	result.Summary = strings.Join(summaries, " · ")
	result.Finish(start)
	return result
}

func lookupIP(ctx context.Context, env Environment, version string) (ipAPIResponse, time.Duration, error) {
	var data ipAPIResponse
	network := "tcp" + version
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: env.Config.HTTPTimeout}).DialContext(ctx, network, address)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   env.Config.HTTPTimeout,
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipapi.is/", nil)
	if err != nil {
		return data, 0, err
	}
	request.Header.Set("User-Agent", env.UserAgent)
	start := time.Now()
	response, err := client.Do(request)
	latency := time.Since(start)
	if err != nil {
		return data, latency, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return data, latency, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	reader := io.LimitReader(response.Body, 512*1024)
	if err := json.NewDecoder(reader).Decode(&data); err != nil {
		return data, latency, err
	}
	if data.Error != "" {
		return data, latency, fmt.Errorf("%s", data.Error)
	}
	if net.ParseIP(data.IP) == nil {
		return data, latency, fmt.Errorf("数据源未返回有效 IP")
	}
	return data, latency, nil
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func proxyEnvironmentEnabled() bool {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

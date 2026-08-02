package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

// bgpProbe is deliberately a light public-observation adapter.  It asks
// RouteViews about the current RIB view of the machine's egress prefix; it
// does not download MRT dumps and it never claims to see the provider's full
// private peering graph.
type bgpProbe struct{}

func (bgpProbe) ID() string         { return "bgp" }
func (bgpProbe) Title() string      { return "BGP 与互联观测" }
func (bgpProbe) NeedsNetwork() bool { return true }

const routeViewsAPI = "https://api.routeviews.org"

var (
	routeViewsBaseURL     = routeViewsAPI
	routeViewsMinInterval = time.Second
	routeViewsGate        struct {
		mu   sync.Mutex
		last time.Time
	}
)

type routeViewsPeer struct {
	PeerASN   int    `json:"peer_asn"`
	Collector string `json:"collector"`
	ASPath    string `json:"as_path"`
	Timestamp string `json:"timestamp"`
}

type routeViewsPrefix struct {
	Prefix         string           `json:"prefix"`
	OriginASN      int              `json:"origin_asn"`
	RPKIState      string           `json:"rpki_state"`
	ReportingPeers []routeViewsPeer `json:"reporting_peers"`
}

func (bgpProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("bgp", "BGP 与互联观测")
	result.Description = "查询当前出口前缀在 RouteViews 公共 RIB 中的可见性、起源 ASN 与观测到的 AS 路径"
	result.Methodology = model.Methodology{
		Kind:            "provider-assessment",
		Label:           "第三方评估",
		Engine:          "RouteViews current RIB API",
		Profile:         "one prefix observation per enabled IP family",
		ComparisonScope: "当前公共观测；不是运营商私有互联全图，也不是历史 BGP 事件分析",
	}

	result.Sources = []model.Source{{
		Name:    "RouteViews API",
		URL:     routeViewsAPI + "/docs/",
		Purpose: "当前 RIB 前缀、起源 ASN、RPKI 状态与观测 AS 路径",
	}}
	table := model.Table{
		Title:   "公共 BGP / 互联观测",
		Columns: []string{"协议族", "匹配前缀", "起源 ASN", "RPKI", "观测 peer/collector", "AS 路径样本"},
		// The matched prefix identifies the egress network even when the full
		// address is not shown; keep the same default redaction boundary as IP.
		SensitiveColumns: []int{1},
	}

	versions := config.IPVersions(env.Config.IPVersion)
	successes := 0
	for _, version := range versions {
		lookup, _, err := lookupIP(ctx, env, version)
		if err != nil || net.ParseIP(lookup.IP) == nil {
			result.Fields = append(result.Fields, model.Field{
				Key:   "ipv" + version + "_error",
				Label: "IPv" + version + " 失败原因",
				Value: "出口 IP 不可用",
			})
			continue
		}
		result.Fields = append(result.Fields,
			model.Field{Key: "ipv" + version + "_ip", Label: "IPv" + version + " 出口 IP", Value: lookup.IP, Sensitive: true},
			model.Field{Key: "ipv" + version + "_ipapi_asn", Label: "IPv" + version + " ipapi ASN", Value: formatASN(lookup.ASN.ASN)},
		)

		observations, err := queryRouteViewsPrefix(ctx, env, lookup.IP)
		if err != nil {
			result.Fields = append(result.Fields, model.Field{
				Key: "ipv" + version + "_routeviews_error", Label: "IPv" + version + " RouteViews 失败原因", Value: compactError(err),
			})
			continue
		}
		successes++
		if len(observations) == 0 {
			table.Rows = append(table.Rows, []string{"IPv" + version, "未找到匹配前缀", "—", "—", "—", "—"})
			continue
		}
		for index, observation := range observations {
			peerText, pathText, collectors, adjacent := summarizeRouteViews(observation.ReportingPeers)
			if index == 0 {
				result.Fields = append(result.Fields,
					model.Field{Key: "ipv" + version + "_prefix", Label: "IPv" + version + " 匹配前缀", Value: observation.Prefix, Sensitive: true},
					model.Field{Key: "ipv" + version + "_origin_asn", Label: "IPv" + version + " 起源 ASN", Value: formatASN(observation.OriginASN)},
					model.Field{Key: "ipv" + version + "_rpki", Label: "IPv" + version + " RPKI", Value: fallback(observation.RPKIState, "unknown")},
					model.Field{Key: "ipv" + version + "_collectors", Label: "IPv" + version + " 观测 collectors", Value: collectors},
					model.Field{Key: "ipv" + version + "_adjacent_asns", Label: "IPv" + version + " 推断相邻 ASN", Value: adjacent},
				)
			}
			table.Rows = append(table.Rows, []string{
				"IPv" + version,
				observation.Prefix,
				formatASN(observation.OriginASN),
				fallback(observation.RPKIState, "unknown"),
				peerText,
				pathText,
			})
		}
	}

	result.Tables = []model.Table{table}
	result.Measurements = append(result.Measurements, model.Measurement{
		Key: "bgp_families_observed", Label: "BGP 可观测协议族", Value: float64(successes), Unit: "项",
		Display: fmt.Sprintf("%d/%d", successes, len(versions)), Method: "routeviews-current-rib-v1", HigherIsBetter: model.BoolPtr(true),
	})
	if successes == 0 {
		result.Status = model.StatusWarning
		result.Summary = "没有取得当前公共 BGP 观测"
	} else {
		result.Summary = fmt.Sprintf("%d/%d 个协议族取得公共 RIB 观测", successes, len(versions))
	}
	result.Notes = append(result.Notes,
		"该模块最多每个启用协议族查询一次 RouteViews 当前 RIB，不下载历史 MRT，不向 ecs 服务器上传报告。",
		"AS 路径中的相邻 ASN 是基于公开路径样本的推断；RouteViews reporting peer 是观测点的 peer，不等于你的 VPS 提供商直接互联。",
		"没有公共观测不等于前缀没有发布：可能是出口 IP 查询、RIB 收敛、过滤或 RouteViews 当前覆盖范围造成的。",
	)
	result.Finish(start)
	return result
}

func queryRouteViewsPrefix(ctx context.Context, env Environment, ip string) ([]routeViewsPrefix, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, errors.New("出口 IP 无效")
	}
	bits := 128
	if parsed.To4() != nil {
		bits = 32
	}
	if err := waitRouteViews(ctx); err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(routeViewsBaseURL, "/") + "/prefix/" + url.PathEscape(ip) + "/" + strconv.Itoa(bits) + "?strict-match=yes"
	body, _, err := requestBytes(ctx, env.HTTPClient, env.UserAgent, endpoint, nil, 4*1024*1024)
	if err != nil {
		return nil, err
	}
	var observations []routeViewsPrefix
	if err := json.Unmarshal(body, &observations); err != nil {
		return nil, fmt.Errorf("解析 RouteViews 响应失败")
	}
	return observations, nil
}

func waitRouteViews(ctx context.Context) error {
	routeViewsGate.mu.Lock()
	defer routeViewsGate.mu.Unlock()
	if routeViewsMinInterval > 0 && !routeViewsGate.last.IsZero() {
		wait := routeViewsMinInterval - time.Since(routeViewsGate.last)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
	}
	routeViewsGate.last = time.Now()
	return nil
}

func summarizeRouteViews(peers []routeViewsPeer) (peerText, pathText, collectors, adjacent string) {
	peerSet := make(map[string]bool)
	collectorSet := make(map[string]bool)
	pathSet := make(map[string]bool)
	adjacentSet := make(map[string]bool)
	for _, peer := range peers {
		if peer.PeerASN > 0 {
			peerSet["AS"+strconv.Itoa(peer.PeerASN)] = true
		}
		if peer.Collector != "" {
			collectorSet[peer.Collector] = true
		}
		if path := strings.TrimSpace(peer.ASPath); path != "" && len(pathSet) < 4 {
			pathSet[path] = true
		}
		for _, asn := range adjacentASNs(peer.ASPath) {
			adjacentSet["AS"+strconv.Itoa(asn)] = true
		}
	}
	peerValues := sortedKeys(peerSet)
	collectorValues := sortedKeys(collectorSet)
	pathValues := sortedKeys(pathSet)
	adjacentValues := sortedKeys(adjacentSet)
	return joinOrDash(peerValues), joinOrDash(pathValues), joinOrDash(collectorValues), joinOrDash(adjacentValues)
}

func adjacentASNs(path string) []int {
	var result []int
	previous := 0
	for _, raw := range strings.Fields(path) {
		raw = strings.Trim(raw, "{}(),")
		if strings.HasPrefix(strings.ToUpper(raw), "AS") {
			raw = raw[2:]
		}
		asn, err := strconv.Atoi(raw)
		if err != nil || asn <= 0 || asn == previous {
			continue
		}
		result = append(result, asn)
		previous = asn
	}
	return result
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return strings.Join(values, " · ")
}

func formatASN(asn int) string {
	if asn <= 0 {
		return "unknown"
	}
	return "AS" + strconv.Itoa(asn)
}

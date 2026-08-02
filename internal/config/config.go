package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ecs/internal/i18n"
)

const (
	ProfileQuick    = "quick"
	ProfileStandard = "standard"
	ProfileFull     = "full"

	// IPVersionAuto keeps the historical behaviour: each probe chooses the
	// usable protocol family it supports, while dual-stack probes keep results
	// separate instead of collapsing them into one score.
	IPVersionAuto = "auto"
	IPVersion4    = "4"
	IPVersion6    = "6"
)

var ModuleOrder = []string{
	"system",
	"network",
	"bgp",
	"cpu",
	"memory",
	"disk",
	"dns",
	"latency",
	"speed",
	"ports",
	"nat",
	"blacklist",
	"apps",
	"cnspeed",
	"ookla",
	"media",
	"route",
	"backtrace",
}

// stunServerPool 是 NAT 行为发现使用的公共 STUN 服务器。
//
// RFC 5780 的映射行为判定需要服务器提供**另一个 IP** 的备用地址，只换端口不够。
// 下面三台是 2026-08-01 各探测三次、每次都返回不同 IP 备用地址的：
//
//	stun.miwifi.com  111.206.174.2  → 111.206.174.3:3479   北京
//	stun.1und1.de    212.227.67.33  → 212.227.67.34:3479   德国
//	stun.hoiio.com   52.76.91.67    → 52.74.211.13:3479    新加坡
//
// 同组的 stun.schlund.de 与 stun.gmx.net 因 DNS 轮询会落到 212.227.67.34，
// 此时备用地址等于自身、测不了映射行为，故不收录。
// stun.l.google.com 与 stun.cloudflare.com 可用但不提供备用地址，且会忽略
// CHANGE-REQUEST 直接回包，只能做基础映射发现，排在后面兜底。
// 清单照抄实测结果，不凭记忆填写；用 `go test -tags=live -run TestLiveSTUNServers` 复核。
func stunServerPool() []Endpoint {
	return []Endpoint{
		{Name: "Xiaomi", Address: "stun.miwifi.com:3478", Kind: "双 IP"},
		{Name: "1&1", Address: "stun.1und1.de:3478", Kind: "双 IP"},
		{Name: "Hoiio", Address: "stun.hoiio.com:3478", Kind: "双 IP"},
		{Name: "Google", Address: "stun.l.google.com:19302", Kind: "仅映射"},
		{Name: "Cloudflare", Address: "stun.cloudflare.com:3478", Kind: "仅映射"},
	}
}

type Endpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind,omitempty"`
	// Family optionally pins hostname resolution and command-line routing to
	// IPv4 or IPv6.  Empty means the probe may choose automatically.
	Family string `json:"family,omitempty"`
}

type IPerfEndpoint struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	PortStart int    `json:"port_start"`
	PortEnd   int    `json:"port_end"`
	Location  string `json:"location,omitempty"`
	Networks  string `json:"networks,omitempty"`
	// Region 用于按地区裁剪节点集，避免长档位把所有公共节点跑一遍。
	Region string `json:"region,omitempty"`
}

// OoklaServer pins an official Ookla server ID to a carrier label. Server IDs
// are user/config supplied because the external catalogue changes over time.
type OoklaServer struct {
	Carrier string `json:"carrier"`
	ID      int    `json:"id"`
}

// iperfNodePool 是 YABS 公共节点清单里的 iperf3 服务器。
//
// 主机名、端口范围、友好名与位置逐条抄自 YABS v2026-07-24 的 IPERF_LOCS 数组，
// 不凭记忆填写——此前编造的 ams.speedtest.eranium.net 与 speedtest.tyo1.jp.leaseweb.net
// 在三个独立 DNS 上都无记录，是实网测试才发现的。
// Region 是 ecs 自己加的分区，用于按地区裁剪节点集；YABS 本身不分区。
//
// 这些节点由第三方免费提供，随时可能繁忙、限速、换端口或下线；ecs 只复用清单，
// 不保证可用性。standard 档按地区各取一个，full 档跑全部。
// 可用性用 `go test -tags=live -run TestLiveIPerfNodeReachability` 复核。
func iperfNodePool() []IPerfEndpoint {
	return []IPerfEndpoint{
		{Name: "Clouvider", Host: "lon.speedtest.clouvider.net", PortStart: 5200, PortEnd: 5209, Location: "London, UK (10G)", Networks: "IPv4|IPv6", Region: "europe"},
		{Name: "Eranium", Host: "iperf-ams-nl.eranium.net", PortStart: 5201, PortEnd: 5210, Location: "Amsterdam, NL (100G)", Networks: "IPv4|IPv6", Region: "europe"},

		{Name: "Uztelecom", Host: "speedtest.uztelecom.uz", PortStart: 5200, PortEnd: 5209, Location: "Tashkent, UZ (10G)", Networks: "IPv4|IPv6", Region: "asia"},
		{Name: "Leaseweb", Host: "speedtest.sin1.sg.leaseweb.net", PortStart: 5201, PortEnd: 5210, Location: "Singapore, SG (10G)", Networks: "IPv4|IPv6", Region: "asia"},

		{Name: "Clouvider", Host: "la.speedtest.clouvider.net", PortStart: 5200, PortEnd: 5209, Location: "Los Angeles, CA, US (10G)", Networks: "IPv4|IPv6", Region: "america"},
		{Name: "Leaseweb", Host: "speedtest.nyc1.us.leaseweb.net", PortStart: 5201, PortEnd: 5210, Location: "NYC, NY, US (10G)", Networks: "IPv4|IPv6", Region: "america"},
		{Name: "Edgoo", Host: "speedtest.sao1.edgoo.net", PortStart: 9204, PortEnd: 9240, Location: "Sao Paulo, BR (1G)", Networks: "IPv4|IPv6", Region: "america"},
	}
}

// selectIPerfTargets 按地区各取前 perRegion 个节点，保持地区覆盖均衡。
func selectIPerfTargets(perRegion int) []IPerfEndpoint {
	if perRegion < 1 {
		return nil
	}
	counts := make(map[string]int)
	var selected []IPerfEndpoint
	for _, node := range iperfNodePool() {
		if counts[node.Region] >= perRegion {
			continue
		}
		counts[node.Region]++
		selected = append(selected, node)
	}
	return selected
}

type Runtime struct {
	Profile string
	Modules []string
	// Exposure 是本次运行允许的最高外联级别，见 exposure.go。
	Exposure Exposure
	// Accepted 是经 --accept 逐个放行的模块，优先于 Exposure。
	Accepted         []string
	Reveal           bool
	IPVersion        string
	IPQualitySources []string
	Formats          []string
	Output           string
	NoColor          bool
	CPUTime          time.Duration
	DiskMiB          int
	DiskPath         string
	DiskMulti        bool
	IPerfDuration    time.Duration
	IPerfTargets     []IPerfEndpoint
	HTTPTimeout      time.Duration
	DNSAttempts      int
	LatencyAttempts  int
	SpeedThreads     int
	DNSResolvers     []Endpoint
	LatencyTargets   []Endpoint
	RouteTargets     []Endpoint
	BacktraceTargets []Endpoint
	STUNServers      []Endpoint
	MediaRegions     []string
	OoklaServers     []OoklaServer
}

type Estimate struct {
	DurationText string
	DiskMiB      int
	NetworkMiB   int
	Notes        []string
}

type File struct {
	Profile          string          `json:"profile,omitempty"`
	Only             []string        `json:"only,omitempty"`
	Skip             []string        `json:"skip,omitempty"`
	Exposure         string          `json:"exposure,omitempty"`
	Accept           []string        `json:"accept,omitempty"`
	Reveal           *bool           `json:"reveal,omitempty"`
	IPVersion        string          `json:"ip_version,omitempty"`
	IPQualitySources []string        `json:"ip_quality_sources,omitempty"`
	Formats          []string        `json:"formats,omitempty"`
	Output           string          `json:"output,omitempty"`
	NoColor          *bool           `json:"no_color,omitempty"`
	CPUTime          string          `json:"cpu_time,omitempty"`
	DiskMiB          *int            `json:"disk_mib,omitempty"`
	DiskPath         string          `json:"disk_path,omitempty"`
	DiskMulti        *bool           `json:"disk_multi,omitempty"`
	IPerfDuration    string          `json:"iperf_duration,omitempty"`
	IPerfTargets     []IPerfEndpoint `json:"iperf_targets,omitempty"`
	HTTPTimeout      string          `json:"http_timeout,omitempty"`
	DNSAttempts      *int            `json:"dns_attempts,omitempty"`
	LatencyAttempts  *int            `json:"latency_attempts,omitempty"`
	SpeedThreads     *int            `json:"speed_threads,omitempty"`
	DNSResolvers     []Endpoint      `json:"dns_resolvers,omitempty"`
	LatencyTargets   []Endpoint      `json:"latency_targets,omitempty"`
	RouteTargets     []Endpoint      `json:"route_targets,omitempty"`
	BacktraceTargets []Endpoint      `json:"backtrace_targets,omitempty"`
	STUNServers      []Endpoint      `json:"stun_servers,omitempty"`
	MediaRegions     []string        `json:"media_regions,omitempty"`
	OoklaServers     []OoklaServer   `json:"ookla_servers,omitempty"`
}

// backtraceCityTargets 是各城市的三网回程参考目标。
//
// 北京与广州两组沿用 zhanghanyun/backtrace 的选点；上海与成都两组是本项目补充，
// 地址于 2026-08-01 实测 ICMP 可达后才写入（上海电信最初候选 180.153.28.5
// 完全不响应，已换成实测 0% 丢包的 202.96.209.133）。
//
// 需要说明：北京电信、广州电信、广州联通这三个上游选点实测不响应 ICMP echo。
// 这不影响回程识别——traceroute 依赖沿途路由器的 TTL 超时应答，不需要终点响应，
// 骨干特征恰恰出现在中间跳上。
var backtraceCityTargets = map[string][]Endpoint{
	"beijing": {
		{Name: "北京电信", Address: "219.141.136.12", Kind: "电信"},
		{Name: "北京联通", Address: "202.106.50.1", Kind: "联通"},
		{Name: "北京移动", Address: "221.179.155.161", Kind: "移动"},
		{Name: "北京电信 IPv6", Address: "bj-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "北京联通 IPv6", Address: "bj-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "北京移动 IPv6", Address: "bj-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"guangzhou": {
		{Name: "广州电信", Address: "58.60.188.222", Kind: "电信"},
		{Name: "广州联通", Address: "210.21.196.6", Kind: "联通"},
		{Name: "广州移动", Address: "120.196.165.24", Kind: "移动"},
		{Name: "广州电信 IPv6", Address: "gd-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "广州联通 IPv6", Address: "gd-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "广州移动 IPv6", Address: "gd-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"shanghai": {
		{Name: "上海电信", Address: "202.96.209.133", Kind: "电信"},
		{Name: "上海联通", Address: "210.22.97.1", Kind: "联通"},
		{Name: "上海移动", Address: "211.136.112.200", Kind: "移动"},
		{Name: "上海电信 IPv6", Address: "sh-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "上海联通 IPv6", Address: "sh-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "上海移动 IPv6", Address: "sh-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
	"chengdu": {
		{Name: "成都电信", Address: "61.139.2.69", Kind: "电信"},
		{Name: "成都联通", Address: "119.6.6.6", Kind: "联通"},
		{Name: "成都移动", Address: "211.137.96.205", Kind: "移动"},
		{Name: "成都电信 IPv6", Address: "sc-ct-v6.ip.zstaticcdn.com", Kind: "电信", Family: IPVersion6},
		{Name: "成都联通 IPv6", Address: "sc-cu-v6.ip.zstaticcdn.com", Kind: "联通", Family: IPVersion6},
		{Name: "成都移动 IPv6", Address: "sc-cm-v6.ip.zstaticcdn.com", Kind: "移动", Family: IPVersion6},
	},
}

// BacktraceCityOrder 固定城市的展示与选择顺序。
var BacktraceCityOrder = []string{"beijing", "guangzhou", "shanghai", "chengdu"}

// defaultBacktraceCities 是默认测试的城市。
//
// 南北各一组即可反映不同的骨干接入；四个城市全测会让回程模块耗时翻倍，
// 需要时用 --backtrace-city 显式扩展。
var defaultBacktraceCities = []string{"beijing", "guangzhou"}

// BacktraceTargetsFor 按城市列表汇总回程目标。
func BacktraceTargetsFor(cities []string) []Endpoint {
	var targets []Endpoint
	for _, city := range BacktraceCityOrder {
		if !contains(cities, city) {
			continue
		}
		targets = append(targets, backtraceCityTargets[city]...)
	}
	return targets
}

// ValidateMediaRegions 校验流媒体地区选项。
func ValidateMediaRegions(regions []string) error {
	known := map[string]bool{"global": true, "jp": true, "tw": true, "hk": true, "cn": true}
	for _, region := range regions {
		if !known[region] {
			return i18n.Errorf("err.unknownMediaRegion", region)
		}
	}
	return nil
}

// ParseBacktraceCities 解析城市选项，all 表示全部。
func ParseBacktraceCities(raw string) ([]string, error) {
	items := ParseList(raw)
	if len(items) == 0 {
		return defaultBacktraceCities, nil
	}
	if contains(items, "all") {
		if len(items) > 1 {
			return nil, i18n.Errorf("err.cityAllCombo")
		}
		return append([]string(nil), BacktraceCityOrder...), nil
	}
	for _, item := range items {
		if _, ok := backtraceCityTargets[item]; !ok {
			return nil, i18n.Errorf("err.unknownCity", item)
		}
	}
	return items, nil
}

func Defaults(profile string) (Runtime, error) {
	if profile == "" {
		profile = ProfileStandard
	}
	base := Runtime{
		Profile:          profile,
		Exposure:         ExposureThirdParty,
		Reveal:           false,
		IPVersion:        IPVersionAuto,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		DiskPath:         ".",
		HTTPTimeout:      10 * time.Second,
		IPerfTargets:     selectIPerfTargets(1),
		STUNServers:      stunServerPool(),
		DNSResolvers: []Endpoint{
			{Name: "Cloudflare", Address: "1.1.1.1:53"},
			{Name: "Cloudflare IPv6", Address: "[2606:4700:4700::1111]:53"},
			{Name: "Google", Address: "8.8.8.8:53"},
			{Name: "Google IPv6", Address: "[2001:4860:4860::8888]:53"},
			{Name: "Quad9", Address: "9.9.9.9:53"},
			{Name: "Quad9 IPv6", Address: "[2620:fe::fe]:53"},
			{Name: "AliDNS", Address: "223.5.5.5:53"},
			{Name: "DNSPod", Address: "119.29.29.29:53"},
		},
		LatencyTargets: []Endpoint{
			{Name: "Cloudflare", Address: "www.cloudflare.com:443", Kind: "Global CDN"},
			{Name: "Google", Address: "www.google.com:443", Kind: "Global"},
			{Name: "Aliyun", Address: "www.aliyun.com:443", Kind: "中国大陆"},
			{Name: "Tencent", Address: "www.tencent.com:443", Kind: "中国大陆"},
			{Name: "Amazon", Address: "www.amazon.com:443", Kind: "Global"},
		},
		RouteTargets: []Endpoint{
			{Name: "Cloudflare", Address: "1.1.1.1", Kind: "Global"},
			{Name: "Google", Address: "8.8.8.8", Kind: "Global"},
			{Name: "AliDNS", Address: "223.5.5.5", Kind: "中国大陆"},
		},
		BacktraceTargets: BacktraceTargetsFor(defaultBacktraceCities),
	}

	switch profile {
	case ProfileQuick:
		base.Modules = []string{"system", "network", "cpu", "memory", "disk", "dns", "latency"}
		base.CPUTime = 5 * time.Second
		base.DiskMiB = 256
		base.DNSAttempts = 3
		base.LatencyAttempts = 4
		base.SpeedThreads = 2
		base.IPerfDuration = 3 * time.Second
	case ProfileStandard:
		base.Modules = []string{"system", "network", "bgp", "cpu", "memory", "disk", "dns", "latency", "speed", "ports", "nat", "blacklist", "apps", "media", "route", "backtrace"}
		base.CPUTime = 10 * time.Second
		base.DiskMiB = 1024
		base.DNSAttempts = 5
		base.LatencyAttempts = 6
		base.SpeedThreads = 8
		base.IPerfDuration = 10 * time.Second
	case ProfileFull:
		// 全集直接给出，需显式同意的模块由外联过滤器挡住（见 exposure.go），
		// 不必在这里逐个特判。
		base.Modules = append([]string(nil), ModuleOrder...)
		base.CPUTime = 15 * time.Second
		base.DiskMiB = 2048
		base.DNSAttempts = 8
		base.LatencyAttempts = 10
		base.SpeedThreads = 8
		base.IPerfDuration = 15 * time.Second
		// full 档跑遍公共节点池，覆盖欧洲、北美、亚洲各方向。
		base.IPerfTargets = selectIPerfTargets(3)
	default:
		return Runtime{}, i18n.Errorf("err.unknownProfile", profile)
	}
	return base, nil
}

func LoadFile(path string) (File, error) {
	var cfg File
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, i18n.Errorf("err.configRead", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, i18n.Errorf("err.configParse", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return cfg, i18n.Errorf("err.configSingle")
		}
		return cfg, i18n.Errorf("err.configTrailing", err)
	}
	return cfg, nil
}

func ApplyFile(runtime *Runtime, file File) error {
	if file.Exposure != "" {
		level, err := ParseExposure(file.Exposure)
		if err != nil {
			return err
		}
		runtime.Exposure = level
	}
	if len(file.Accept) > 0 {
		accepted := normalizeList(file.Accept)
		if err := ValidateAccepted(accepted); err != nil {
			return err
		}
		runtime.Accepted = accepted
	}
	if file.Reveal != nil {
		runtime.Reveal = *file.Reveal
	}
	if file.IPVersion != "" {
		runtime.IPVersion = strings.ToLower(strings.TrimSpace(file.IPVersion))
	}
	if len(file.IPQualitySources) > 0 {
		runtime.IPQualitySources = normalizeList(file.IPQualitySources)
	}
	if file.NoColor != nil {
		runtime.NoColor = *file.NoColor
	}
	if len(file.Formats) > 0 {
		runtime.Formats = append([]string(nil), file.Formats...)
	}
	if file.Output != "" {
		runtime.Output = file.Output
	}
	if file.CPUTime != "" {
		value, err := time.ParseDuration(file.CPUTime)
		if err != nil {
			return fmt.Errorf("cpu_time: %w", err)
		}
		runtime.CPUTime = value
	}
	if file.DiskMiB != nil {
		runtime.DiskMiB = *file.DiskMiB
	}
	if file.DiskPath != "" {
		runtime.DiskPath = file.DiskPath
	}
	if file.DiskMulti != nil {
		runtime.DiskMulti = *file.DiskMulti
	}
	if file.IPerfDuration != "" {
		value, err := time.ParseDuration(file.IPerfDuration)
		if err != nil {
			return fmt.Errorf("iperf_duration: %w", err)
		}
		runtime.IPerfDuration = value
	}
	if len(file.IPerfTargets) > 0 {
		runtime.IPerfTargets = append([]IPerfEndpoint(nil), file.IPerfTargets...)
	}
	if file.HTTPTimeout != "" {
		value, err := time.ParseDuration(file.HTTPTimeout)
		if err != nil {
			return fmt.Errorf("http_timeout: %w", err)
		}
		runtime.HTTPTimeout = value
	}
	if file.DNSAttempts != nil {
		runtime.DNSAttempts = *file.DNSAttempts
	}
	if file.LatencyAttempts != nil {
		runtime.LatencyAttempts = *file.LatencyAttempts
	}
	if file.SpeedThreads != nil {
		runtime.SpeedThreads = *file.SpeedThreads
	}
	if len(file.DNSResolvers) > 0 {
		runtime.DNSResolvers = append([]Endpoint(nil), file.DNSResolvers...)
	}
	if len(file.LatencyTargets) > 0 {
		runtime.LatencyTargets = append([]Endpoint(nil), file.LatencyTargets...)
	}
	if len(file.RouteTargets) > 0 {
		runtime.RouteTargets = append([]Endpoint(nil), file.RouteTargets...)
	}
	if len(file.BacktraceTargets) > 0 {
		runtime.BacktraceTargets = append([]Endpoint(nil), file.BacktraceTargets...)
	}
	if len(file.STUNServers) > 0 {
		runtime.STUNServers = append([]Endpoint(nil), file.STUNServers...)
	}
	if len(file.MediaRegions) > 0 {
		runtime.MediaRegions = normalizeList(file.MediaRegions)
	}
	if len(file.OoklaServers) > 0 {
		runtime.OoklaServers = append([]OoklaServer(nil), file.OoklaServers...)
	}
	runtime.Modules = SelectModules(runtime.Modules, file.Only, file.Skip)
	return nil
}

func SelectModules(base, only, skip []string) []string {
	selected := make(map[string]bool)
	if len(only) > 0 {
		for _, id := range only {
			selected[id] = true
		}
	} else {
		for _, id := range base {
			selected[id] = true
		}
	}
	for _, id := range skip {
		delete(selected, id)
	}
	out := make([]string, 0, len(selected))
	for _, id := range ModuleOrder {
		if selected[id] {
			out = append(out, id)
		}
	}
	return out
}

func ParseList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// ParseOoklaServerList parses carrier=server-id pairs. IDs are not fetched
// from the network because the official catalogue is volatile.
func ParseOoklaServerList(raw string) ([]OoklaServer, error) {
	var result []OoklaServer
	seen := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, i18n.Errorf("err.ooklaFormat")
		}
		carrier := normalizeOoklaCarrier(parts[0])
		if carrier == "" {
			return nil, i18n.Errorf("err.ooklaCarrier", parts[0])
		}
		id, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || id < 1 || id > 99999999 {
			return nil, i18n.Errorf("err.ooklaIDInvalid", parts[1])
		}
		if seen[carrier] {
			return nil, i18n.Errorf("err.ooklaDuplicate", carrier)
		}
		seen[carrier] = true
		result = append(result, OoklaServer{Carrier: carrier, ID: id})
	}
	return result, nil
}

func normalizeOoklaCarrier(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "电信", "telecom", "ct", "chinatelecom":
		return "电信"
	case "联通", "unicom", "cu", "chinaunicom":
		return "联通"
	case "移动", "mobile", "cm", "chinamobile":
		return "移动"
	default:
		return ""
	}
}

func Validate(runtime Runtime) error {
	knownModules := make(map[string]bool)
	for _, id := range ModuleOrder {
		knownModules[id] = true
	}
	if len(runtime.Modules) == 0 {
		return i18n.Errorf("err.noModules")
	}
	switch runtime.IPVersion {
	case "", IPVersionAuto, IPVersion4, IPVersion6:
		// Empty is accepted for callers constructing Runtime directly; it has
		// the same meaning as auto and keeps older integrations compatible.
	default:
		return i18n.Errorf("err.unknownIPVersion", runtime.IPVersion)
	}
	for _, id := range runtime.Modules {
		if !knownModules[id] {
			return i18n.Errorf("err.unknownModule", id)
		}
	}
	if err := ValidateAccepted(runtime.Accepted); err != nil {
		return err
	}
	// 到这一步模块集已经过滤过；仍然复核一遍，挡住直接构造 Runtime 的调用方
	// 绕过外联上限的可能。
	for _, id := range runtime.Modules {
		if !AllowsModule(runtime.Exposure, runtime.Accepted, id) {
			info := ExposureFor(id)
			if info.RequiresConsent {
				return i18n.Errorf("err.moduleConsentShort", id, id)
			}
			return i18n.Errorf("err.moduleAboveLimitFix", id, info.Level.String(), runtime.Exposure.String())
		}
	}
	if runtime.CPUTime < 100*time.Millisecond || runtime.CPUTime > 30*time.Second {
		return i18n.Errorf("err.cpuTimeRange")
	}
	if runtime.DiskMiB < 16 || runtime.DiskMiB > 16384 {
		return i18n.Errorf("err.diskSizeRange")
	}
	if runtime.HTTPTimeout < time.Second || runtime.HTTPTimeout > time.Minute {
		return i18n.Errorf("err.httpTimeoutRange")
	}
	if runtime.DNSAttempts < 1 || runtime.DNSAttempts > 20 || runtime.LatencyAttempts < 1 || runtime.LatencyAttempts > 20 {
		return i18n.Errorf("err.attemptsRange")
	}
	if runtime.SpeedThreads < 1 || runtime.SpeedThreads > 32 {
		return i18n.Errorf("err.threadsRange")
	}
	allowedFormats := map[string]bool{"json": true, "md": true, "html": true}
	if len(runtime.Formats) == 0 {
		return i18n.Errorf("err.noFormats")
	}
	for _, format := range runtime.Formats {
		if !allowedFormats[format] {
			return i18n.Errorf("err.unknownFormat", format)
		}
	}
	allowedIPSources := map[string]bool{
		"all": true, "none": true, "maxmind": true, "ipinfo": true,
		"ipregistry": true, "ipapi": true, "ip2location": true,
		"abuseipdb": true, "scamalytics": true, "ipqs": true,
		"dbip": true, "ipdata": true, "ipwhois": true,
		"ipapicom": true, "ipsb": true,
	}
	if len(runtime.IPQualitySources) == 0 {
		return i18n.Errorf("err.ipSourceEmpty")
	}
	for _, source := range runtime.IPQualitySources {
		if !allowedIPSources[source] {
			return i18n.Errorf("err.ipSourceUnknown", source)
		}
	}
	if len(runtime.IPQualitySources) > 1 && (contains(runtime.IPQualitySources, "all") || contains(runtime.IPQualitySources, "none")) {
		return i18n.Errorf("err.ipSourceCombo")
	}
	for _, group := range [][]Endpoint{runtime.DNSResolvers, runtime.LatencyTargets} {
		for _, endpoint := range group {
			if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
				return i18n.Errorf("err.endpointNameAddress")
			}
			if !validEndpointFamily(endpoint.Family) {
				return i18n.Errorf("err.endpointFamily", endpoint.Name)
			}
		}
	}
	for _, endpoint := range runtime.RouteTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.routeNameAddress")
		}
		if !validRouteTarget(endpoint.Address) {
			return i18n.Errorf("err.routeUnsafe", endpoint.Address)
		}
		if !validEndpointFamily(endpoint.Family) {
			return i18n.Errorf("err.routeFamily", endpoint.Name)
		}
	}
	for _, endpoint := range runtime.STUNServers {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.stunNameAddress")
		}
		host, port, err := net.SplitHostPort(endpoint.Address)
		if err != nil || !validRouteTarget(host) || port == "" {
			return i18n.Errorf("err.stunHostPort", endpoint.Address)
		}
	}
	for _, endpoint := range runtime.BacktraceTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return i18n.Errorf("err.backtraceNameAddress")
		}
		if !validRouteTarget(endpoint.Address) {
			return i18n.Errorf("err.backtraceUnsafe", endpoint.Address)
		}
		if !validEndpointFamily(endpoint.Family) {
			return i18n.Errorf("err.backtraceFamily", endpoint.Name)
		}
	}
	seenOoklaCarriers := make(map[string]bool)
	for _, server := range runtime.OoklaServers {
		if server.Carrier != "电信" && server.Carrier != "联通" && server.Carrier != "移动" {
			return i18n.Errorf("err.ooklaCarrierField", server.Carrier)
		}
		if server.ID < 1 || server.ID > 99999999 {
			return i18n.Errorf("err.ooklaIDField", server.Carrier)
		}
		if seenOoklaCarriers[server.Carrier] {
			return i18n.Errorf("err.ooklaDupField", server.Carrier)
		}
		seenOoklaCarriers[server.Carrier] = true
	}
	if runtime.DiskPath == "" {
		return i18n.Errorf("err.diskPathEmpty")
	}
	if runtime.IPerfDuration < time.Second || runtime.IPerfDuration > 30*time.Second {
		return i18n.Errorf("err.iperfDuration")
	}
	for _, endpoint := range runtime.IPerfTargets {
		if strings.TrimSpace(endpoint.Name) == "" || !validRouteTarget(endpoint.Host) {
			return i18n.Errorf("err.iperfNodeName", endpoint.Host)
		}
		if endpoint.PortStart < 1 || endpoint.PortStart > 65535 ||
			endpoint.PortEnd < endpoint.PortStart || endpoint.PortEnd > 65535 {
			return i18n.Errorf("err.iperfNodeRange", endpoint.Name)
		}
		switch endpoint.Networks {
		case "", "IPv4", "IPv6", "IPv4|IPv6":
		default:
			return i18n.Errorf("err.iperfNodeNetwork", endpoint.Name)
		}
	}
	abs, err := filepath.Abs(runtime.DiskPath)
	if err != nil {
		return i18n.Errorf("err.diskPathWrap", err)
	}
	if strings.TrimSpace(abs) == "" {
		return i18n.Errorf("err.diskPathInvalid")
	}
	return nil
}

var routeHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?\.?$`)

func validRouteTarget(value string) bool {
	value = strings.TrimSpace(value)
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return true
	}
	return len(value) <= 253 &&
		!strings.Contains(value, "..") &&
		routeHostnamePattern.MatchString(value)
}

func validEndpointFamily(value string) bool {
	return value == "" || value == IPVersion4 || value == IPVersion6
}

func EstimateFor(runtime Runtime) Estimate {
	estimate := Estimate{
		DiskMiB: runtime.DiskMiB,
	}
	if contains(runtime.Modules, "speed") {
		estimate.NetworkMiB = -1
		estimate.Notes = append(estimate.Notes,
			fmt.Sprintf("iperf3 为按时长跑满：%d 节点、每方向 %s、%d 并发流；实际流量随带宽变化",
				len(runtime.IPerfTargets), runtime.IPerfDuration, runtime.SpeedThreads))
	}
	if !contains(runtime.Modules, "disk") {
		estimate.DiskMiB = 0
	}
	typical := estimateTypicalDuration(runtime)
	estimate.DurationText = durationEstimateText(typical*3/5, typical*2)
	if runtime.OfflineOnly() {
		estimate.NetworkMiB = 0
		estimate.Notes = append(estimate.Notes, i18n.T("estimate.offline"))
	}
	if contains(runtime.Modules, "route") {
		estimate.Notes = append(estimate.Notes, i18n.T("estimate.route"))
	}
	return estimate
}

func estimateTypicalDuration(runtime Runtime) time.Duration {
	networkModule := map[string]bool{
		"network": true, "bgp": true, "dns": true, "latency": true, "speed": true,
		"ports": true, "media": true, "route": true, "backtrace": true,
		"ookla": true,
	}
	var total time.Duration
	for _, module := range runtime.Modules {
		if runtime.OfflineOnly() && networkModule[module] {
			total += 100 * time.Millisecond
			continue
		}
		switch module {
		case "system":
			total += time.Second
		case "network":
			total += 5 * time.Second
		case "bgp":
			// One public prefix observation per enabled family, rate-limited by
			// the RouteViews API client.
			total += 4 * time.Second
		case "cpu":
			// sysbench CPU 跑单线程与多线程两轮。
			total += 2*runtime.CPUTime + time.Second
		case "memory":
			// sysbench memory 跑读/写 × 单/多线程共四轮。
			total += 4*runtime.CPUTime + time.Second
		case "disk":
			randomDuration := runtime.CPUTime
			if randomDuration > 3*time.Second {
				randomDuration = 3 * time.Second
			}
			if randomDuration < 500*time.Millisecond {
				randomDuration = 500 * time.Millisecond
			}
			total += 5*time.Second + 2*randomDuration + time.Duration(runtime.DiskMiB/50)*time.Second
		case "dns":
			total += time.Duration(runtime.DNSAttempts) * time.Second
		case "latency":
			total += time.Duration(runtime.LatencyAttempts) * 1500 * time.Millisecond
		case "speed":
			// Typical estimate assumes IPv4 only and no busy-server retry.
			total += time.Duration(len(runtime.IPerfTargets)*2) * runtime.IPerfDuration
		case "ports":
			total += 4 * time.Second
		case "media":
			total += 10 * time.Second
		case "route":
			total += time.Duration(len(runtime.RouteTargets)) * 12 * time.Second
		case "blacklist":
			total += 10 * time.Second
		case "apps":
			total += 8 * time.Second
		case "cnspeed":
			// 三个运营商各选点后串行下载。
			total += 40 * time.Second
		case "ookla":
			total += 90 * time.Second
		case "nat":
			// 多次 UDP 事务，无响应时按超时累计。
			total += 12 * time.Second
		case "backtrace":
			// 参考目标并发追踪，耗时取决于最慢的一个而不是总和。
			total += 30 * time.Second
		}
	}
	if total < 5*time.Second {
		return 5 * time.Second
	}
	return total
}

func durationEstimateText(lower, upper time.Duration) string {
	if lower < 5*time.Second {
		lower = 5 * time.Second
	}
	if upper < lower {
		upper = lower
	}
	if upper < time.Minute {
		lowerSeconds := int((lower.Seconds()+4)/5) * 5
		upperSeconds := int((upper.Seconds()+4)/5) * 5
		return fmt.Sprintf(i18n.T("estimate.seconds"), lowerSeconds, upperSeconds)
	}
	lowerMinutes := int((lower + time.Minute - 1) / time.Minute)
	upperMinutes := int((upper + time.Minute - 1) / time.Minute)
	if lowerMinutes < 1 {
		lowerMinutes = 1
	}
	if upperMinutes <= lowerMinutes {
		upperMinutes = lowerMinutes + 1
	}
	return fmt.Sprintf(i18n.T("estimate.minutes"), lowerMinutes, upperMinutes)
}

func ExampleFile() File {
	reveal := false
	return File{
		Profile:          ProfileStandard,
		Exposure:         DefaultExposure,
		Reveal:           &reveal,
		IPVersion:        IPVersionAuto,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		Output:           "./reports",
		DiskPath:         ".",
		IPerfDuration:    "5s",
		HTTPTimeout:      "10s",
	}
}

// IPVersions returns the protocol families selected for a run.  The result is
// intentionally stable so callers can use it to build deterministic report
// rows and per-family error records.
func IPVersions(mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case IPVersion4:
		return []string{IPVersion4}
	case IPVersion6:
		return []string{IPVersion6}
	default:
		return []string{IPVersion4, IPVersion6}
	}
}

// AllowsIPVersion reports whether a concrete family is enabled by the run.
func AllowsIPVersion(mode, version string) bool {
	for _, candidate := range IPVersions(mode) {
		if candidate == version {
			return true
		}
	}
	return false
}

func KnownModules() []string {
	out := append([]string(nil), ModuleOrder...)
	sort.Strings(out)
	return out
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func normalizeList(items []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

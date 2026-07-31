package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ProfileQuick    = "quick"
	ProfileStandard = "standard"
	ProfileFull     = "full"
)

var ModuleOrder = []string{
	"system",
	"network",
	"cpu",
	"memory",
	"disk",
	"dns",
	"latency",
	"speed",
	"ports",
	"media",
	"route",
	"backtrace",
}

type Endpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind,omitempty"`
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
	Profile          string
	Modules          []string
	Offline          bool
	Reveal           bool
	IPQualitySources []string
	Formats          []string
	Output           string
	NoColor          bool
	CPUTime          time.Duration
	DiskMiB          int
	DiskPath         string
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
	Offline          *bool           `json:"offline,omitempty"`
	Reveal           *bool           `json:"reveal,omitempty"`
	IPQualitySources []string        `json:"ip_quality_sources,omitempty"`
	Formats          []string        `json:"formats,omitempty"`
	Output           string          `json:"output,omitempty"`
	NoColor          *bool           `json:"no_color,omitempty"`
	CPUTime          string          `json:"cpu_time,omitempty"`
	DiskMiB          *int            `json:"disk_mib,omitempty"`
	DiskPath         string          `json:"disk_path,omitempty"`
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
}

func Defaults(profile string) (Runtime, error) {
	if profile == "" {
		profile = ProfileStandard
	}
	base := Runtime{
		Profile:          profile,
		Reveal:           false,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		DiskPath:         ".",
		HTTPTimeout:      10 * time.Second,
		IPerfTargets:     selectIPerfTargets(1),
		DNSResolvers: []Endpoint{
			{Name: "Cloudflare", Address: "1.1.1.1:53"},
			{Name: "Google", Address: "8.8.8.8:53"},
			{Name: "Quad9", Address: "9.9.9.9:53"},
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
		// 三网回程参考目标：北京与广州各取三大运营商一个公开地址，
		// 南北各一组能反映不同的骨干接入。Kind 存放运营商名，供报告分组。
		BacktraceTargets: []Endpoint{
			{Name: "北京电信", Address: "219.141.136.12", Kind: "电信"},
			{Name: "北京联通", Address: "202.106.50.1", Kind: "联通"},
			{Name: "北京移动", Address: "221.179.155.161", Kind: "移动"},
			{Name: "广州电信", Address: "58.60.188.222", Kind: "电信"},
			{Name: "广州联通", Address: "210.21.196.6", Kind: "联通"},
			{Name: "广州移动", Address: "120.196.165.24", Kind: "移动"},
		},
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
		base.Modules = []string{"system", "network", "cpu", "memory", "disk", "dns", "latency", "speed", "ports", "media", "route", "backtrace"}
		base.CPUTime = 10 * time.Second
		base.DiskMiB = 1024
		base.DNSAttempts = 5
		base.LatencyAttempts = 6
		base.SpeedThreads = 8
		base.IPerfDuration = 10 * time.Second
	case ProfileFull:
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
		return Runtime{}, fmt.Errorf("未知配置档 %q，可选 quick、standard、full", profile)
	}
	return base, nil
}

func LoadFile(path string) (File, error) {
	var cfg File
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return cfg, errors.New("配置文件只能包含一个 JSON 对象")
		}
		return cfg, fmt.Errorf("配置文件尾部存在无效内容: %w", err)
	}
	return cfg, nil
}

func ApplyFile(runtime *Runtime, file File) error {
	if file.Offline != nil {
		runtime.Offline = *file.Offline
	}
	if file.Reveal != nil {
		runtime.Reveal = *file.Reveal
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

func Validate(runtime Runtime) error {
	knownModules := make(map[string]bool)
	for _, id := range ModuleOrder {
		knownModules[id] = true
	}
	if len(runtime.Modules) == 0 {
		return errors.New("至少需要选择一个测试模块")
	}
	for _, id := range runtime.Modules {
		if !knownModules[id] {
			return fmt.Errorf("未知测试模块 %q", id)
		}
	}
	if runtime.CPUTime < 100*time.Millisecond || runtime.CPUTime > 30*time.Second {
		return errors.New("CPU 测试时长必须在 100ms 到 30s 之间")
	}
	if runtime.DiskMiB < 16 || runtime.DiskMiB > 16384 {
		return errors.New("磁盘测试大小必须在 16 到 16384 MiB 之间")
	}
	if runtime.HTTPTimeout < time.Second || runtime.HTTPTimeout > time.Minute {
		return errors.New("HTTP 超时必须在 1s 到 1m 之间")
	}
	if runtime.DNSAttempts < 1 || runtime.DNSAttempts > 20 || runtime.LatencyAttempts < 1 || runtime.LatencyAttempts > 20 {
		return errors.New("DNS/延迟采样次数必须在 1 到 20 之间")
	}
	if runtime.SpeedThreads < 1 || runtime.SpeedThreads > 32 {
		return errors.New("测速并发必须在 1 到 32 之间")
	}
	allowedFormats := map[string]bool{"json": true, "md": true, "html": true}
	if len(runtime.Formats) == 0 {
		return errors.New("至少需要一个输出格式")
	}
	for _, format := range runtime.Formats {
		if !allowedFormats[format] {
			return fmt.Errorf("未知输出格式 %q", format)
		}
	}
	allowedIPSources := map[string]bool{
		"all": true, "none": true, "maxmind": true, "ipinfo": true,
		"ipregistry": true, "ipapi": true, "ip2location": true,
		"abuseipdb": true, "scamalytics": true, "ipqs": true,
		"dbip": true, "ipdata": true, "ipwhois": true,
	}
	if len(runtime.IPQualitySources) == 0 {
		return errors.New("IP 质量数据源不能为空；使用 all、none 或逗号分隔的数据源")
	}
	for _, source := range runtime.IPQualitySources {
		if !allowedIPSources[source] {
			return fmt.Errorf("未知 IP 质量数据源 %q", source)
		}
	}
	if len(runtime.IPQualitySources) > 1 && (contains(runtime.IPQualitySources, "all") || contains(runtime.IPQualitySources, "none")) {
		return errors.New("IP 质量数据源 all/none 不能与其他数据源组合")
	}
	for _, group := range [][]Endpoint{runtime.DNSResolvers, runtime.LatencyTargets} {
		for _, endpoint := range group {
			if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
				return errors.New("自定义测试端点必须同时包含 name 和 address")
			}
		}
	}
	for _, endpoint := range runtime.RouteTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return errors.New("自定义路由目标必须同时包含 name 和 address")
		}
		if !validRouteTarget(endpoint.Address) {
			return fmt.Errorf("路由目标 %q 不是安全的 IP 或主机名", endpoint.Address)
		}
	}
	for _, endpoint := range runtime.BacktraceTargets {
		if strings.TrimSpace(endpoint.Name) == "" || strings.TrimSpace(endpoint.Address) == "" {
			return errors.New("三网回程目标必须同时包含 name 和 address")
		}
		if !validRouteTarget(endpoint.Address) {
			return fmt.Errorf("三网回程目标 %q 不是安全的 IP 或主机名", endpoint.Address)
		}
	}
	if runtime.DiskPath == "" {
		return errors.New("磁盘测试路径不能为空")
	}
	if runtime.IPerfDuration < time.Second || runtime.IPerfDuration > 30*time.Second {
		return errors.New("iperf3 单方向时长必须在 1s 到 30s 之间")
	}
	for _, endpoint := range runtime.IPerfTargets {
		if strings.TrimSpace(endpoint.Name) == "" || !validRouteTarget(endpoint.Host) {
			return fmt.Errorf("iperf3 节点名称或主机无效: %q", endpoint.Host)
		}
		if endpoint.PortStart < 1 || endpoint.PortStart > 65535 ||
			endpoint.PortEnd < endpoint.PortStart || endpoint.PortEnd > 65535 {
			return fmt.Errorf("iperf3 节点 %q 端口范围无效", endpoint.Name)
		}
		switch endpoint.Networks {
		case "", "IPv4", "IPv6", "IPv4|IPv6":
		default:
			return fmt.Errorf("iperf3 节点 %q networks 必须是 IPv4、IPv6 或 IPv4|IPv6", endpoint.Name)
		}
	}
	abs, err := filepath.Abs(runtime.DiskPath)
	if err != nil {
		return fmt.Errorf("磁盘测试路径: %w", err)
	}
	if strings.TrimSpace(abs) == "" {
		return errors.New("磁盘测试路径无效")
	}
	return nil
}

var routeHostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?\.?$`)

func validRouteTarget(value string) bool {
	value = strings.TrimSpace(value)
	if net.ParseIP(value) != nil {
		return true
	}
	return len(value) <= 253 &&
		!strings.Contains(value, "..") &&
		routeHostnamePattern.MatchString(value)
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
	if runtime.Offline {
		estimate.NetworkMiB = 0
		estimate.Notes = append(estimate.Notes, "离线模式会跳过全部外网探针")
	}
	if contains(runtime.Modules, "route") {
		estimate.Notes = append(estimate.Notes, "路由探测时长受每跳超时影响")
	}
	return estimate
}

func estimateTypicalDuration(runtime Runtime) time.Duration {
	networkModule := map[string]bool{
		"network": true, "dns": true, "latency": true, "speed": true,
		"ports": true, "media": true, "route": true, "backtrace": true,
	}
	var total time.Duration
	for _, module := range runtime.Modules {
		if runtime.Offline && networkModule[module] {
			total += 100 * time.Millisecond
			continue
		}
		switch module {
		case "system":
			total += time.Second
		case "network":
			total += 5 * time.Second
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
		return fmt.Sprintf("约 %d–%d 秒", lowerSeconds, upperSeconds)
	}
	lowerMinutes := int((lower + time.Minute - 1) / time.Minute)
	upperMinutes := int((upper + time.Minute - 1) / time.Minute)
	if lowerMinutes < 1 {
		lowerMinutes = 1
	}
	if upperMinutes <= lowerMinutes {
		upperMinutes = lowerMinutes + 1
	}
	return fmt.Sprintf("约 %d–%d 分钟", lowerMinutes, upperMinutes)
}

func ExampleFile() File {
	offline := false
	reveal := false
	return File{
		Profile:          ProfileStandard,
		Offline:          &offline,
		Reveal:           &reveal,
		IPQualitySources: []string{"all"},
		Formats:          []string{"json", "md", "html"},
		Output:           "./reports",
		DiskPath:         ".",
		IPerfDuration:    "5s",
		HTTPTimeout:      "10s",
	}
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

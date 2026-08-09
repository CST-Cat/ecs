package probe

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"ecs/internal/config"
)

// 出口 IP 发现。
//
// network、blacklist、bgp 三个模块都要先知道"我的公网地址是什么"才能干活。
// 以前它们各自调用 lookupIP，一次运行可能对 api.ipapi.is 发出多次重复请求，而且
// 从模块列表上看不出 blacklist 和 bgp 也在连商业情报服务——外联性质被藏在模块内部。
//
// 这里把它提成一次共享的显式步骤，并按外联级别分两条路：
//
//   - ipapi.is  第三方情报接口，带 ASN、地理与公司字段。network 的画像需要这些；
//   - STUN      只回一个映射地址，判定逻辑全在协议里。blacklist 只要 IPv4 地址，
//     bgp 只要能定位前缀，两者都不需要商业字段。
//
// 于是 --exposure public 下这两个模块仍然能跑，而待查 IP 不必再交给第三方。

// EgressAddress 是一个协议族的出口地址与来源说明。
type EgressAddress struct {
	// Version 是 config.IPVersion4 或 config.IPVersion6。
	Version string
	// IP 是本机公网出口地址；发现失败时为空。
	IP string
	// Source 记录这个地址由谁给出：ipapi.is 或 stun。
	Source string
	// Intel 是第三方情报接口返回的完整记录，STUN 路径下为零值。
	Intel ipAPIResponse
	// HasIntel 区分"情报字段为空"与"根本没查过情报"。
	HasIntel bool
	// IntelAttempted 表示本次是否尝试过 ipapi.is。它和 HasIntel 分开，
	// 这样报告可以区分"按外联设置没有查询"与"查询失败"。
	IntelAttempted bool
	// IntelErr 保留 ipapi.is 的失败原因；只要 STUN 仍然发现了 IP，Err
	// 不会被设置为发现失败，调用方仍可继续使用地址和 BGP 观测。
	IntelErr error
	// BGPQueried 表示出口发现阶段是否已经为该协议族查询过 RouteViews。
	// BGPObservations/BGPError 让 network 与 bgp 模块共享同一份观测，避免
	// network 为了字段回填再发第二次请求。
	BGPQueried      bool
	BGPObservations []routeViewsPrefix
	BGPError        error
	// Latency 是本次发现耗时。
	Latency time.Duration
	// Err 是发现失败的原因。
	Err error
}

// Egress 是一次运行共享的出口地址快照。
type Egress struct {
	// Attempted 为假时表示本次运行没有任何模块需要出口地址。
	Attempted  bool
	ByVersion  map[string]EgressAddress
	SourceName string
}

// Lookup 返回指定协议族的出口地址记录。
func (e Egress) Lookup(version string) (EgressAddress, bool) {
	if e.ByVersion == nil {
		return EgressAddress{}, false
	}
	address, ok := e.ByVersion[version]
	return address, ok
}

// IntelFor 返回情报记录；没有走情报路径时第二个返回值为假。
//
// 调用方据此区分"查了但对方没给"与"这次压根没查"，前者是数据缺失，
// 后者是用户主动选择的隐私边界，报告里必须说清楚。
func (e Egress) IntelFor(version string) (ipAPIResponse, bool) {
	address, ok := e.Lookup(version)
	if !ok || !address.HasIntel {
		return ipAPIResponse{}, false
	}
	return address.Intel, true
}

// IPFor 返回指定协议族的出口 IP 与失败原因。
func (e Egress) IPFor(version string) (string, error) {
	address, ok := e.Lookup(version)
	if !ok {
		return "", fmt.Errorf("本次运行未发现 IPv%s 出口地址", version)
	}
	if address.Err != nil {
		return "", address.Err
	}
	if address.IP == "" {
		return "", fmt.Errorf("IPv%s 出口地址为空", version)
	}
	return address.IP, nil
}

// DiscoverEgress 按需发现出口地址，每个协议族只查一次。
func DiscoverEgress(ctx context.Context, env Environment) Egress {
	cfg := env.Config
	if cfg.OfflineOnly() || !config.RequiresEgressIP(cfg.Modules) {
		return Egress{}
	}
	useIntel := config.EgressNeedsIPIntel(cfg.Modules, cfg.Exposure)
	versions := config.IPVersions(cfg.IPVersion)

	result := Egress{Attempted: true, ByVersion: make(map[string]EgressAddress, len(versions))}
	result.SourceName = "STUN"
	if useIntel {
		result.SourceName = "ipapi.is"
	}

	var mutex sync.Mutex
	var wg sync.WaitGroup
	for _, version := range versions {
		wg.Add(1)
		go func(version string) {
			defer wg.Done()
			address := discoverEgressForVersion(ctx, env, version, useIntel)
			mutex.Lock()
			result.ByVersion[version] = address
			mutex.Unlock()
		}(version)
	}
	wg.Wait()
	if modulesNeedEgressBGP(cfg.Modules) {
		cacheEgressBGP(ctx, env, &result, versions)
	}
	if useIntel {
		for _, address := range result.ByVersion {
			if address.Source == "stun" {
				result.SourceName = "ipapi.is + STUN fallback"
				break
			}
		}
	}
	return result
}

func discoverEgressForVersion(ctx context.Context, env Environment, version string, useIntel bool) EgressAddress {
	address := EgressAddress{Version: version, IntelAttempted: useIntel}
	if useIntel {
		data, latency, err := lookupIP(ctx, env, version)
		address.Source = "ipapi.is"
		address.Latency = latency
		if data.IP != "" && egressAddressMatchesVersion(data.IP, version) {
			address.IP, address.Intel = data.IP, data
			if err == nil {
				address.HasIntel = true
				return address
			}
			address.IntelErr = err
			// ipapi returned a usable address but incomplete intelligence. Keep
			// that address: BGP and the remaining quality sources can still run.
			return address
		}
		address.IntelErr = err
		// The intelligence endpoint is also the historical IP discovery path.
		// If it is unavailable, fall back to STUN so a service outage does not
		// turn a known egress address into a total module failure.
		start := time.Now()
		ip, stunErr := egressViaSTUN(ctx, env, version)
		address.Latency += time.Since(start)
		if stunErr != nil {
			address.Err = combineEgressErrors(err, stunErr)
			return address
		}
		address.Source = "stun"
		address.IP = ip
		return address
	}

	start := time.Now()
	address.Source = "stun"
	ip, err := egressViaSTUN(ctx, env, version)
	address.Latency = time.Since(start)
	if err != nil {
		address.Err = err
		return address
	}
	address.IP = ip
	return address
}

func cacheEgressBGP(ctx context.Context, env Environment, result *Egress, versions []string) {
	for _, version := range versions {
		address, ok := result.ByVersion[version]
		if !ok {
			continue
		}
		address.BGPQueried = true
		if address.Err != nil || address.IP == "" {
			address.BGPError = fmt.Errorf("IPv%s 出口 IP 不可用", version)
			result.ByVersion[version] = address
			continue
		}
		observations, err := queryRouteViewsPrefix(ctx, env, address.IP)
		address.BGPObservations = observations
		address.BGPError = err
		result.ByVersion[version] = address
	}
}

func moduleSelected(modules []string, target string) bool {
	for _, module := range modules {
		if module == target {
			return true
		}
	}
	return false
}

func modulesNeedEgressBGP(modules []string) bool {
	return moduleSelected(modules, "bgp") || moduleSelected(modules, "network")
}

func combineEgressErrors(intelErr, stunErr error) error {
	switch {
	case intelErr == nil:
		return stunErr
	case stunErr == nil:
		return intelErr
	default:
		return fmt.Errorf("ipapi.is: %v；STUN: %v", intelErr, stunErr)
	}
}

// egressViaSTUN 依次尝试配置里的 STUN 服务器，取第一个返回的映射地址。
//
// 这里不做 NAT 行为判定，只要地址：那是 nat 模块的职责，重复做既慢又可能
// 因为并发的 Binding 请求互相干扰。
func egressViaSTUN(ctx context.Context, env Environment, version string) (string, error) {
	network := udpNetworkForMode(version)
	bindIP := net.IPv4zero
	if version == config.IPVersion6 {
		bindIP = net.IPv6zero
	}
	var lastErr error
	for _, server := range env.Config.STUNServers {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		target, err := net.ResolveUDPAddr(network, server.Address)
		if err != nil {
			lastErr = err
			continue
		}
		connection, err := net.ListenUDP(network, &net.UDPAddr{IP: bindIP, Port: 0})
		if err != nil {
			lastErr = err
			continue
		}
		outcome, err := stunTransaction(connection, target, 0, egressSTUNTimeout)
		connection.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if !outcome.Mapped.valid() {
			lastErr = fmt.Errorf("STUN 服务器 %s 未返回映射地址", server.Name)
			continue
		}
		if !egressAddressMatchesVersion(outcome.Mapped.IP, version) {
			lastErr = fmt.Errorf("STUN 服务器 %s 返回的地址不属于 IPv%s", server.Name, version)
			continue
		}
		return outcome.Mapped.IP, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 STUN 服务器")
	}
	return "", fmt.Errorf("STUN 未能发现 IPv%s 出口地址: %w", version, lastErr)
}

const egressSTUNTimeout = 3 * time.Second

func egressAddressMatchesVersion(value, version string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	if version == config.IPVersion6 {
		return ip.To4() == nil
	}
	return ip.To4() != nil
}

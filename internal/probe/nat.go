package probe

import (
	"context"
	"net"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
)

// NAT 类型与 UDP 可达性检测。
//
// 这是判断"独立公网 IP"还是"NAT 小鸡"的直接证据，也决定 P2P、游戏联机、BT
// 与自建服务能不能用。做法遵循 RFC 5780：把 NAT 行为拆成映射行为与过滤行为
// 分别检测，再折算成社区习惯的 NAT1–NAT4 说法。
//
// 与常见实现的区别在于对"测不出来"的处理。多数公共 STUN 服务器出于 UDP 放大
// 攻击的顾虑禁用了 CHANGE-REQUEST，此时过滤行为测试只会静默超时。把这种沉默
// 当作"被 NAT 过滤"，会把一台公网机器误报成 Port-Restricted NAT。因此这里
// 区分"确实被过滤"与"服务器没有配合"，后者一律报未知。

type natProbe struct{}

func (natProbe) ID() string         { return "nat" }
func (natProbe) Title() string      { return "NAT 类型" }
func (natProbe) NeedsNetwork() bool { return true }

// NAT 映射行为（RFC 5780 §4.3）。
const (
	mappingUnknown              = "未知"
	mappingEndpointIndependent  = "端点无关"
	mappingAddressDependent     = "地址相关"
	mappingAddressPortDependent = "地址与端口相关"
)

// NAT 过滤行为（RFC 5780 §4.4）。
const (
	filteringUnknown              = "未知"
	filteringEndpointIndependent  = "端点无关"
	filteringAddressDependent     = "地址相关"
	filteringAddressPortDependent = "地址与端口相关"
)

// natFinding 是对一台 STUN 服务器完成的检测结果。
type natFinding struct {
	Server config.Endpoint
	// LocalAddr 是本机发包用的地址，用于判断是否存在 NAT。
	LocalAddr netAddr
	// Mapped 是主服务器地址看到的映射。
	Mapped netAddr
	// MappedAlt 是换服务器 IP 后看到的映射，用于判断映射行为。
	MappedAlt netAddr
	// MappedAltPort 是换服务器 IP 与端口后看到的映射。
	MappedAltPort netAddr
	// Other 是服务器公布的备用地址。
	Other netAddr
	// DualStackServer 表示备用地址确实是另一个 IP，而不是同 IP 换端口。
	DualStackServer bool

	Mapping   string
	Filtering string
	// FilteringTested 表示过滤行为的两次 CHANGE-REQUEST 至少有一次得到了来自
	// 正确源地址的应答，也就是服务器确实实现了该属性；否则过滤行为不可判定。
	FilteringTested bool
	// ChangeRequestIgnored 表示服务器收到 CHANGE-REQUEST 后照常从原地址回复，
	// 说明它没有实现该属性——这与"被 NAT 过滤"是两回事，必须分开记。
	ChangeRequestIgnored bool

	UDPBlocked bool
	Err        error
}

// natTimeout 是单次 STUN 事务的等待时长。
//
// UDP 无重传，太短会把慢链路误判成"无响应"，而无响应在过滤行为判定里是有语义的。
const natTimeout = 3 * time.Second

func (natProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("nat", "NAT 类型")
	result.Description = "按 RFC 5780 检测 UDP 映射与过滤行为，判断是否处于 NAT 之后"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议测量",
		Engine:          "STUN (RFC 5389/5780)",
		Profile:         "binding mapping + filtering behaviour discovery",
		ComparisonScope: "当次 UDP 路径上的 NAT 行为；不代表 TCP 或其他协议",
	}

	servers := env.Config.STUNServers
	if len(servers) == 0 {
		result.Skip("未配置 STUN 服务器")
		result.Finish(start)
		return result
	}

	family := config.IPVersion4
	if env.Config.IPVersion == config.IPVersion6 {
		family = config.IPVersion6
	}
	var findings []natFinding
	for _, server := range servers {
		finding := probeNATForVersion(ctx, server, family)
		findings = append(findings, finding)
		// 拿到一次完整判定就够了，不必对每台服务器都做完整探测。
		if finding.Err == nil && finding.Mapping != mappingUnknown {
			break
		}
		if ctx.Err() != nil {
			break
		}
	}

	table := model.Table{
		Title:   "STUN 探测明细",
		Columns: []string{"协议", "服务器", "映射地址", "映射行为", "过滤行为", "备用地址", "状态"},
		// 映射地址是本机公网出口，默认按段遮盖。
		SensitiveColumns: []int{2},
	}
	var best *natFinding
	for index := range findings {
		finding := &findings[index]
		status := "完成"
		switch {
		case finding.UDPBlocked:
			status = "UDP 无响应"
		case finding.Err != nil:
			status = "失败：" + compactError(finding.Err)
		}
		other := finding.Other.String()
		if other == "" {
			other = "—"
		} else if !finding.DualStackServer {
			other += "（同 IP）"
		}
		table.Rows = append(table.Rows, []string{
			"IPv" + family,
			finding.Server.Name,
			fallback(finding.Mapped.String(), "—"),
			finding.Mapping,
			finding.Filtering,
			other,
			status,
		})
		if best == nil && finding.Err == nil && finding.Mapped.valid() {
			best = finding
		}
	}
	result.Tables = []model.Table{table}

	if best == nil {
		result.Status = model.StatusWarning
		result.Summary = "全部 STUN 服务器无响应，无法判定 NAT 类型"
		result.Notes = append(result.Notes,
			"所有 STUN 服务器都没有返回映射地址。常见原因是出站 UDP 被封锁、"+
				"防火墙只放行 TCP，或本次选用的服务器全部不可用；这本身也说明 UDP 类应用会受影响。",
		)
		result.Finish(start)
		return result
	}

	behind := best.Mapped.IP != best.LocalAddr.IP
	category, categoryNote := natCategory(*best, behind)

	result.Fields = []model.Field{
		{Key: "ip_version", Label: "协议族", Value: "IPv" + family},
		{Key: "nat_category", Label: "NAT 类型", Value: category},
		{Key: "mapped_address", Label: "公网映射地址", Value: best.Mapped.String(), Sensitive: true},
		{Key: "local_address", Label: "本地出口地址", Value: best.LocalAddr.String(), Sensitive: true},
		{Key: "behind_nat", Label: "位于 NAT 之后", Value: yesNo(behind)},
		{Key: "mapping_behaviour", Label: "映射行为", Value: best.Mapping},
		{Key: "filtering_behaviour", Label: "过滤行为", Value: best.Filtering},
		{Key: "stun_server", Label: "判定所用服务器", Value: best.Server.Name},
		{Key: "stun_pool", Label: "候选服务器", Value: describeNATServers(servers)},
	}
	result.Measurements = []model.Measurement{
		{
			Key: "udp_stun_reachable", Label: "STUN 可达",
			Value: 1, Unit: "项", Display: "是",
			Method: "stun-binding-rfc5389-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	result.Sources = []model.Source{
		{Name: "RFC 5389", URL: "https://www.rfc-editor.org/rfc/rfc5389", Purpose: "STUN Binding 协议"},
		{Name: "RFC 5780", URL: "https://www.rfc-editor.org/rfc/rfc5780", Purpose: "NAT 映射与过滤行为发现方法"},
	}
	result.Notes = append(result.Notes,
		"映射行为决定同一本地端口对不同目标是否复用同一个公网端口，端点无关（锥形）才利于 P2P 打洞。",
		"这里只测 UDP 路径上的 NAT 行为，不代表 TCP：两者可以由不同设备处理，结论不能互相套用。",
		categoryNote,
	)
	if !best.FilteringTested {
		reason := "本次 STUN 服务器没有响应任何 CHANGE-REQUEST"
		if best.ChangeRequestIgnored {
			reason = "本次 STUN 服务器收到 CHANGE-REQUEST 后仍从原地址回复，说明它没有实现该属性"
		}
		result.Notes = append(result.Notes,
			"过滤行为无法判定："+reason+"。"+
				"多数公共服务器出于 UDP 放大攻击的顾虑禁用或忽略了该属性，因此这既可能是服务器不配合，"+
				"也可能是真的被过滤；ecs 不会把这种沉默当成证据去猜一个 NAT 等级。"+
				"判定过滤行为时会核对响应的源地址，只看“有没有回包”会把忽略该属性的服务器误判成全锥型。",
		)
	}
	if !best.DualStackServer {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes,
			"本次服务器的备用地址与主地址是同一个 IP，只能改端口不能改源 IP，"+
				"因此无法区分“地址相关”与“地址与端口相关”的映射行为；结论按可测到的部分给出。",
		)
	}
	if behind {
		result.Status = model.StatusWarning
	}
	result.Summary = category
	if best.Mapping != mappingUnknown {
		result.Summary += " · 映射" + best.Mapping
	}
	if best.FilteringTested {
		result.Summary += " · 过滤" + best.Filtering
	}
	result.Finish(start)
	return result
}

// probeNAT 对一台 STUN 服务器完成 RFC 5780 的 IPv4 行为发现。
// probeNATForVersion 负责双栈调用与协议族扩展。
func probeNAT(ctx context.Context, server config.Endpoint) natFinding {
	return probeNATForVersion(ctx, server, config.IPVersion4)
}

func probeNATForVersion(ctx context.Context, server config.Endpoint, family string) natFinding {
	finding := natFinding{
		Server:    server,
		Mapping:   mappingUnknown,
		Filtering: filteringUnknown,
	}
	deadline, hasDeadline := ctx.Deadline()
	network := "udp" + family
	primary, err := net.ResolveUDPAddr(network, server.Address)
	if err != nil {
		finding.Err = err
		return finding
	}
	// 整个检测必须复用同一个 socket：换端口就换了 NAT 映射，所有对比都失效。
	bindIP := net.IPv4zero
	if family == config.IPVersion6 {
		bindIP = net.IPv6zero
	}
	connection, err := net.ListenUDP(network, &net.UDPAddr{IP: bindIP, Port: 0})
	if err != nil {
		finding.Err = err
		return finding
	}
	defer connection.Close()
	if hasDeadline {
		_ = connection.SetDeadline(deadline)
	}
	if local, ok := connection.LocalAddr().(*net.UDPAddr); ok {
		finding.LocalAddr = netAddr{IP: outboundLocalIPForNetwork(primary, network).String(), Port: local.Port}
	}

	// Test I：基础 Binding，拿到映射地址与服务器备用地址。
	first, err := stunTransaction(connection, primary, 0, natTimeout)
	if err != nil {
		finding.UDPBlocked = true
		finding.Err = err
		return finding
	}
	finding.Mapped = first.Mapped
	finding.Other = first.Other

	if finding.Mapped.IP == finding.LocalAddr.IP {
		// 映射地址等于本机地址，说明路径上没有 NAT，映射行为按端点无关处理。
		finding.Mapping = mappingEndpointIndependent
	}

	if finding.Other.valid() {
		finding.DualStackServer = finding.Other.IP != primary.IP.String()
	}

	// 过滤行为：先请求换 IP 且换端口，再退一步只换端口。
	//
	// 关键是必须核对响应的**源地址**：不少公共服务器（实测 stun.l.google.com、
	// stun.cloudflare.com）直接忽略 CHANGE-REQUEST 属性，照常从原地址回复。
	// 只看"有没有响应"就会把这种忽略当成"NAT 放行了来自其他地址的包"，
	// 把一台对称型 NAT 后的机器误报成全锥型 NAT1。
	if second, err := stunTransaction(connection, primary, changeIP|changePort, natTimeout); err == nil {
		switch {
		case second.From.valid() && second.From.IP != primary.IP.String():
			finding.Filtering = filteringEndpointIndependent
			finding.FilteringTested = true
		default:
			finding.ChangeRequestIgnored = true
		}
	}
	if !finding.FilteringTested {
		if third, err := stunTransaction(connection, primary, changePort, natTimeout); err == nil {
			switch {
			case third.From.valid() && third.From.Port != primary.Port:
				finding.Filtering = filteringAddressDependent
				finding.FilteringTested = true
			default:
				finding.ChangeRequestIgnored = true
			}
		}
	}

	// 映射行为：换服务器 IP 再看映射是否变化。没有真正的第二个 IP 就测不了。
	if finding.DualStackServer {
		alternate := &net.UDPAddr{IP: net.ParseIP(finding.Other.IP), Port: primary.Port}
		if second, err := stunTransaction(connection, alternate, 0, natTimeout); err == nil {
			finding.MappedAlt = second.Mapped
			if second.Mapped == finding.Mapped {
				finding.Mapping = mappingEndpointIndependent
			} else {
				// 映射随目标地址变化，再换端口区分是地址相关还是地址+端口相关。
				alternateBoth := &net.UDPAddr{IP: net.ParseIP(finding.Other.IP), Port: finding.Other.Port}
				if third, err := stunTransaction(connection, alternateBoth, 0, natTimeout); err == nil {
					finding.MappedAltPort = third.Mapped
					if third.Mapped == second.Mapped {
						finding.Mapping = mappingAddressDependent
					} else {
						finding.Mapping = mappingAddressPortDependent
					}
				} else {
					finding.Mapping = mappingAddressDependent
				}
			}
		}
	}

	// 过滤行为两次都没应答时，只有在服务器确实支持 CHANGE-REQUEST 的前提下
	// 才能推断为"地址与端口相关"，而这一点无法从沉默中确认，因此保持未知。
	return finding
}

// natCategory 把映射与过滤行为折算成社区习惯的 NAT 说法。
//
// 只在证据充分时给出 NAT1–NAT4；缺过滤行为时如实说明只测到了映射行为，
// 不去凑一个看起来完整的等级。
func natCategory(finding natFinding, behind bool) (string, string) {
	if !behind {
		return "公网直连（无 NAT）",
			"映射地址与本机出口地址一致，UDP 路径上没有 NAT；独立公网 IP 的常见结果。"
	}
	switch finding.Mapping {
	case mappingAddressDependent, mappingAddressPortDependent:
		return "对称型 NAT（NAT4）",
			"映射端口随目标地址变化，属于对称型 NAT：P2P 打洞基本不可用，" +
				"BT、游戏联机与自建 P2P 服务会明显受限。"
	case mappingEndpointIndependent:
		switch finding.Filtering {
		case filteringEndpointIndependent:
			return "全锥型 NAT（NAT1）",
				"映射与过滤都与端点无关，是 NAT 环境里最利于 P2P 的一类。"
		case filteringAddressDependent:
			return "受限锥型 NAT（NAT2）",
				"映射端点无关但过滤按地址受限，P2P 打洞通常仍可行。"
		case filteringAddressPortDependent:
			return "端口受限锥型 NAT（NAT3）",
				"映射端点无关但过滤按地址与端口受限，P2P 打洞需要双方配合。"
		default:
			return "锥型 NAT（NAT1–NAT3，过滤行为未测出）",
				"映射行为为端点无关，属于锥型 NAT；但过滤行为未能判定，" +
					"无法进一步区分全锥、受限锥与端口受限锥。"
		}
	default:
		return "位于 NAT 之后（类型未判定）",
			"确认存在 NAT，但本次 STUN 服务器不具备判定映射行为所需的第二个 IP，" +
				"无法给出 NAT 等级。"
	}
}

// outboundLocalIP 给出前往 target 时内核选择的源地址。
//
// ListenUDP 绑定 0.0.0.0 时 LocalAddr 的 IP 是全零，拿它和映射地址比较永远
// 会得出"存在 NAT"。这里用一次不发包的 UDP dial 让内核做路由决策。
func outboundLocalIP(target *net.UDPAddr) net.IP {
	return outboundLocalIPForNetwork(target, "udp4")
}

func outboundLocalIPForNetwork(target *net.UDPAddr, network string) net.IP {
	connection, err := net.DialUDP(network, nil, target)
	if err != nil {
		if network == "udp6" {
			return net.IPv6zero
		}
		return net.IPv4zero
	}
	defer connection.Close()
	if local, ok := connection.LocalAddr().(*net.UDPAddr); ok {
		return local.IP
	}
	if network == "udp6" {
		return net.IPv6zero
	}
	return net.IPv4zero
}

// describeNATServers 给出参与检测的服务器列表，用于报告披露。
func describeNATServers(servers []config.Endpoint) string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Address)
	}
	return strings.Join(names, i18n.T("punct.listSep"))
}

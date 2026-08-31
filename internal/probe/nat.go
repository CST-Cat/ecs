package probe

import (
	"context"
	"net"
	"time"

	"ecs/internal/config"
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

func (natProbe) ID() string { return "nat" }

func newNATResult() model.Result {
	result := model.NewResult("nat", "module.nat.title")
	result.Description = "probe.nat.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "STUN (RFC 5389/5780)",
		Profile:         "probe.nat.profile",
		ComparisonScope: "probe.nat.comparison_scope",
	}
	return result
}

// NAT 映射行为（RFC 5780 §4.3）。这些值是机器枚举，不是展示文案。
const (
	mappingUnknown              = "unknown"
	mappingEndpointIndependent  = "endpoint_independent"
	mappingAddressDependent     = "address_dependent"
	mappingAddressPortDependent = "address_port_dependent"
)

// NAT 过滤行为（RFC 5780 §4.4）。这些值与映射枚举保持同一机器词汇。
const (
	filteringUnknown              = "unknown"
	filteringEndpointIndependent  = "endpoint_independent"
	filteringAddressDependent     = "address_dependent"
	filteringAddressPortDependent = "address_port_dependent"
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
	result := newNATResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameterHash(result.Methodology.Parameters, "servers_sha256", env.Config.STUNServers)

	servers := env.Config.STUNServers
	if len(servers) == 0 {
		result.Skip(model.NewMessage("probe.nat.summary.skipped"))
		result.Evidence = model.NewEvidence(0, 1, "target")
		result.Notes = stableNATNotes(result)
		result.SummaryMessages = []model.Message{model.NewMessage("probe.nat.summary.skipped")}
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
		Key:   "network.nat.stun",
		Title: "probe.nat.table.stun",
		Columns: []model.TableColumn{
			{Key: "protocol", Label: "probe.nat.column.protocol"},
			{Key: "server", Label: "probe.nat.column.server"},
			{Key: "kind", Label: "probe.nat.column.kind"},
			{Key: "mapped_address", Label: "probe.nat.column.mapped_address", Sensitive: true},
			{Key: "mapping_behavior", Label: "probe.nat.column.mapping"},
			{Key: "filtering_behavior", Label: "probe.nat.column.filtering"},
			{Key: "alternate_address", Label: "probe.nat.column.alternate_address"},
			{Key: "status", Label: "probe.nat.column.status"},
		},
		// 映射地址是本机公网出口，默认按段遮盖。
	}
	var best *natFinding
	for index := range findings {
		finding := &findings[index]
		status := "probe.nat.status.complete"
		switch {
		case finding.UDPBlocked:
			status = "probe.nat.status.udp_blocked"
			message := ""
			if finding.Err != nil {
				message = compactError(finding.Err)
			}
			result.AddFailure(model.Failure{Category: model.FailureTimeout, Stage: "stun_binding", Target: finding.Server.Address, Retryable: true, Count: 1, Message: message})
		case finding.Err != nil:
			status = "probe.nat.status.failed"
			addFailure(&result, "stun_binding", finding.Server.Address, finding.Err)
		}
		other := finding.Other.String()
		if other == "" {
			other = "—"
		}
		table.Rows = append(table.Rows, []model.Value{
			model.RawValue("IPv" + family), model.RawValue(finding.Server.Name), stunServerKindValue(finding.Server.Kind),
			model.RawValue(fallback(finding.Mapped.String(), "—")), natBehaviorValue(finding.Mapping, false),
			natBehaviorValue(finding.Filtering, true), model.RawValue(other), model.KeyValue(status),
		})
		if best == nil && finding.Err == nil && finding.Mapped.valid() {
			best = finding
		}
	}
	result.Tables = []model.Table{table, natSTUNPoolTable(servers)}

	if best == nil {
		result.Status = model.StatusWarning
		result.Evidence = model.NewEvidence(0, 1, "target")
		result.Notes = stableNATNotes(result)
		result.SummaryMessages = []model.Message{model.NewMessage("probe.nat.summary.all_failed")}
		result.Finish(start)
		return result
	}

	behind := best.Mapped.IP != best.LocalAddr.IP
	categoryCode := natCategoryCode(*best, behind)
	behindValue := "probe.nat.boolean.no"
	if behind {
		behindValue = "probe.nat.boolean.yes"
	}

	result.Fields = []model.Field{
		{Key: "ip_version", Label: "probe.nat.field.ip_version", Value: model.RawValue("IPv" + family)},
		{Key: "nat_category", Label: "probe.nat.field.nat_category", Value: natCategoryValue(categoryCode)},
		{Key: "mapped_address", Label: "probe.nat.field.mapped_address", Value: model.RawValue(best.Mapped.String()), Sensitive: true},
		{Key: "local_address", Label: "probe.nat.field.local_address", Value: model.RawValue(best.LocalAddr.String()), Sensitive: true},
		{Key: "behind_nat", Label: "probe.nat.field.behind_nat", Value: model.KeyValue(behindValue)},
		{Key: "mapping_behaviour", Label: "probe.nat.field.mapping_behaviour", Value: natBehaviorValue(best.Mapping, false)},
		{Key: "filtering_behaviour", Label: "probe.nat.field.filtering_behaviour", Value: natBehaviorValue(best.Filtering, true)},
		{Key: "stun_server", Label: "probe.nat.field.stun_server", Value: model.RawValue(best.Server.Name)},
	}
	result.Measurements = []model.Measurement{
		{
			Key: "udp_stun_reachable", Label: "probe.nat.metric.udp_stun_reachable",
			Value: 1, Unit: "项", Display: model.KeyValue("probe.nat.boolean.yes"),
			Method: "stun-binding-rfc5389-v1", HigherIsBetter: model.BoolPtr(true),
		},
	}
	result.Evidence = model.NewEvidence(1, 1, "target")
	result.Sources = []model.Source{
		{Name: "RFC 5389", URL: "https://www.rfc-editor.org/rfc/rfc5389", Purpose: "probe.nat.source.rfc"},
		{Name: "RFC 5780", URL: "https://www.rfc-editor.org/rfc/rfc5780", Purpose: "probe.nat.source.rfc"},
	}
	if !best.DualStackServer {
		result.Status = model.StatusWarning
	}
	if behind {
		result.Status = model.StatusWarning
	}
	result.Notes = stableNATNotes(result)
	result.SummaryMessages = []model.Message{model.NewMessage(natSummaryKey(natCategoryKey(categoryCode)))}
	result.Finish(start)
	return result
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
	first, err := stunTransactionWithContext(ctx, connection, primary, 0, natTimeout)
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
	if second, err := stunTransactionWithContext(ctx, connection, primary, changeIP|changePort, natTimeout); err == nil {
		switch {
		case second.From.valid() && second.From.IP != primary.IP.String():
			finding.Filtering = filteringEndpointIndependent
			finding.FilteringTested = true
		default:
			finding.ChangeRequestIgnored = true
		}
	} else if cause := contextCauseError(ctx); cause != nil {
		finding.Err = cause
		return finding
	}
	if !finding.FilteringTested {
		if third, err := stunTransactionWithContext(ctx, connection, primary, changePort, natTimeout); err == nil {
			switch {
			case third.From.valid() && third.From.Port != primary.Port:
				finding.Filtering = filteringAddressDependent
				finding.FilteringTested = true
			default:
				finding.ChangeRequestIgnored = true
			}
		} else if cause := contextCauseError(ctx); cause != nil {
			finding.Err = cause
			return finding
		}
	}

	// 映射行为：换服务器 IP 再看映射是否变化。没有真正的第二个 IP 就测不了。
	if finding.DualStackServer {
		alternate := &net.UDPAddr{IP: net.ParseIP(finding.Other.IP), Port: primary.Port}
		if second, err := stunTransactionWithContext(ctx, connection, alternate, 0, natTimeout); err == nil {
			finding.MappedAlt = second.Mapped
			if second.Mapped == finding.Mapped {
				finding.Mapping = mappingEndpointIndependent
			} else {
				// 映射随目标地址变化，再换端口区分是地址相关还是地址+端口相关。
				alternateBoth := &net.UDPAddr{IP: net.ParseIP(finding.Other.IP), Port: finding.Other.Port}
				if third, err := stunTransactionWithContext(ctx, connection, alternateBoth, 0, natTimeout); err == nil {
					finding.MappedAltPort = third.Mapped
					if third.Mapped == second.Mapped {
						finding.Mapping = mappingAddressDependent
					} else {
						finding.Mapping = mappingAddressPortDependent
					}
				} else {
					if cause := contextCauseError(ctx); cause != nil {
						finding.Err = cause
						return finding
					}
					finding.Mapping = mappingAddressDependent
				}
			}
		} else if cause := contextCauseError(ctx); cause != nil {
			finding.Err = cause
			return finding
		}
	}

	// 过滤行为两次都没应答时，只有在服务器确实支持 CHANGE-REQUEST 的前提下
	// 才能推断为"地址与端口相关"，而这一点无法从沉默中确认，因此保持未知。
	return finding
}

// natCategoryCode is the language-independent classification stored in fields.
func natCategoryCode(finding natFinding, behind bool) string {
	if !behind {
		return "public"
	}
	switch finding.Mapping {
	case mappingAddressDependent, mappingAddressPortDependent:
		return "symmetric"
	case mappingEndpointIndependent:
		switch finding.Filtering {
		case filteringEndpointIndependent:
			return "full_cone"
		case filteringAddressDependent:
			return "restricted_cone"
		case filteringAddressPortDependent:
			return "port_restricted"
		default:
			return "cone_unknown_filtering"
		}
	default:
		return "unknown"
	}
}

// natSTUNPoolTable discloses the configured candidate pool as machine rows.
// It is intentionally built from configuration rather than findings so that
// early stop, failed probes, and partial execution never hide candidates.
func natSTUNPoolTable(servers []config.Endpoint) model.Table {
	table := model.Table{
		Key:   "network.nat.stun_pool",
		Title: "probe.nat.table.stun_pool",
		Columns: []model.TableColumn{
			{Key: "server_name", Label: "probe.nat.column.server_name"},
			{Key: "server_address", Label: "probe.nat.column.server_address"},
			{Key: "kind", Label: "probe.nat.column.kind"},
		},
		Rows: make([][]model.Value, 0, len(servers)),
	}
	for _, server := range servers {
		table.Rows = append(table.Rows, []model.Value{model.RawValue(server.Name), model.RawValue(server.Address), stunServerKindValue(server.Kind)})
	}
	return table
}

func stunServerKindValue(kind string) model.Value {
	switch kind {
	case config.STUNServerKindDualAddress, config.STUNServerKindMappingOnly:
		return model.KeyValue("probe.nat.stun_kind." + kind)
	default:
		// A custom STUN classification is caller-owned metadata; never rewrite
		// it as one of ECS's catalog identities.
		return model.RawValue(kind)
	}
}

func natBehaviorKey(value string, filtering bool) string {
	prefix := "probe.nat.mapping."
	if filtering {
		prefix = "probe.nat.filtering."
	}
	switch value {
	case mappingEndpointIndependent:
		return prefix + "endpoint_independent"
	case mappingAddressDependent:
		return prefix + "address_dependent"
	case mappingAddressPortDependent:
		return prefix + "address_port_dependent"
	case mappingUnknown:
		return prefix + "unknown"
	default:
		return value
	}
}

func natCategoryKey(value string) string {
	switch value {
	case "public":
		return "probe.nat.category.public"
	case "symmetric":
		return "probe.nat.category.symmetric"
	case "full_cone":
		return "probe.nat.category.full_cone"
	case "restricted_cone":
		return "probe.nat.category.restricted_cone"
	case "port_restricted":
		return "probe.nat.category.port_restricted"
	case "cone_unknown_filtering":
		return "probe.nat.category.cone_unknown_filtering"
	case "unknown":
		return "probe.nat.category.unknown"
	default:
		return value
	}
}

func natCategoryValue(value string) model.Value {
	key := natCategoryKey(value)
	if key == value {
		return model.RawValue(value)
	}
	return model.KeyValue(key)
}

func natBehaviorValue(value string, filtering bool) model.Value {
	key := natBehaviorKey(value, filtering)
	if key == value {
		return model.RawValue(value)
	}
	return model.KeyValue(key)
}

func natSummaryKey(category string) string {
	switch category {
	case "probe.nat.category.public":
		return "probe.nat.summary.public"
	case "probe.nat.category.symmetric":
		return "probe.nat.summary.symmetric"
	case "probe.nat.category.full_cone":
		return "probe.nat.summary.full_cone"
	case "probe.nat.category.restricted_cone":
		return "probe.nat.summary.restricted_cone"
	case "probe.nat.category.port_restricted":
		return "probe.nat.summary.port_restricted"
	case "probe.nat.category.cone_unknown_filtering":
		return "probe.nat.summary.cone_unknown_filtering"
	default:
		return "probe.nat.summary.unknown"
	}
}

func stableNATNotes(result model.Result) []string {
	notes := []string{"probe.nat.note.mapping", "probe.nat.note.udp_scope"}
	if result.Status == model.StatusSkipped {
		return append(notes, "probe.nat.note.skipped")
	}
	if result.Evidence != nil && result.Evidence.Valid == 0 {
		return append(notes, "probe.nat.note.no_response")
	}
	if result.Status == model.StatusWarning {
		notes = append(notes, "probe.nat.note.limited_evidence")
	}
	return notes
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

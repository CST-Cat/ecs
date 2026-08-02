package i18n

// 校验错误文案。
//
// 与探针文案不同，这些错误在命令行上立即打印，没有报告渲染层可以挂原文查表；
// 而且它们会把用户输入原样嵌进去（`未知配置档 "快速"`），用户输入含中文时
// 片段占位模板会失配。因此这一批走 key，由 i18n.Errorf 在构造错误时取译文。
//
// key 一律以 err. 开头，i18n_test 据此核对中英两侧的覆盖与格式符一致性。

var errorChinese = map[string]string{
	// ── 配置档与模块 ────────────────────────────────────────
	"err.unknownProfile":     "未知配置档 %q，可选 quick、standard、full",
	"err.unknownModule":      "未知测试模块 %q",
	"err.noModules":          "至少需要选择一个测试模块",
	"err.unknownIPVersion":   "未知 IP 协议族 %q，可选 auto、4、6",
	"err.unknownFormat":      "未知输出格式 %q",
	"err.noFormats":          "至少需要一个输出格式",
	"err.unknownMediaRegion": "未知流媒体地区 %q，可选 global、jp、tw、hk、cn",
	"err.ipv4AndIPv6":        "-4 与 -6 不能同时使用",

	// ── 外联级别与同意 ──────────────────────────────────────
	"err.unknownExposure":     "未知外联级别 %q，可选 %s",
	"err.acceptUnknownModule": "--accept 指定了未知模块 %q",
	"err.acceptNotNeeded":     "模块 %q 不需要显式同意；需要同意的模块：%s",
	"err.moduleNeedsConsent":  "模块 %q 是需显式同意的外部服务，请加 --accept %s",
	"err.moduleConsentShort":  "模块 %q 需要显式同意：--accept %s",
	"err.moduleAboveLimit":    "模块 %q 的外联级别是 %s，超过当前 --exposure %s；请改用 --exposure %s",
	"err.moduleAboveLimitFix": "模块 %q 的外联级别 %s 超过 --exposure %s",

	// ── 资源参数 ────────────────────────────────────────────
	"err.cpuTimeRange":     "CPU 测试时长必须在 100ms 到 30s 之间",
	"err.diskSizeRange":    "磁盘测试大小必须在 16 到 16384 MiB 之间",
	"err.httpTimeoutRange": "HTTP 超时必须在 1s 到 1m 之间",
	"err.attemptsRange":    "DNS/延迟采样次数必须在 1 到 20 之间",
	"err.threadsRange":     "测速并发必须在 1 到 32 之间",
	"err.iperfDuration":    "iperf3 单方向时长必须在 1s 到 30s 之间",
	"err.diskPathEmpty":    "磁盘测试路径不能为空",
	"err.diskPathInvalid":  "磁盘测试路径无效",
	"err.diskPathWrap":     "磁盘测试路径: %w",

	// ── IP 质量数据源 ───────────────────────────────────────
	"err.ipSourceUnknown": "未知 IP 质量数据源 %q",
	"err.ipSourceEmpty":   "IP 质量数据源不能为空；使用 all、none 或逗号分隔的数据源",
	"err.ipSourceCombo":   "IP 质量数据源 all/none 不能与其他数据源组合",

	// ── 端点通用 ────────────────────────────────────────────
	"err.endpointMissingAddress": "端点 %q 缺少地址",
	"err.endpointNeedsHostPort":  "端点 %q 必须是 host:port 形式",
	"err.endpointUnsafeHost":     "端点主机 %q 不是安全的 IP 或主机名",
	"err.endpointUnsafe":         "端点 %q 不是安全的 IP 或主机名",
	"err.endpointDuplicate":      "端点 %q 重复",
	"err.endpointNameAddress":    "自定义测试端点必须同时包含 name 和 address",
	"err.endpointFamily":         "测试端点 %q 的 family 必须是 4、6 或空值",
	"err.portMissing":            "缺少端口",
	"err.portInvalid":            "端口无效",

	// ── 路由与回程目标 ──────────────────────────────────────
	"err.routeNameAddress":     "自定义路由目标必须同时包含 name 和 address",
	"err.routeUnsafe":          "路由目标 %q 不是安全的 IP 或主机名",
	"err.routeFamily":          "路由目标 %q 的 family 必须是 4、6 或空值",
	"err.backtraceNameAddress": "三网回程目标必须同时包含 name 和 address",
	"err.backtraceUnsafe":      "三网回程目标 %q 不是安全的 IP 或主机名",
	"err.backtraceFamily":      "三网回程目标 %q 的 family 必须是 4、6 或空值",
	"err.unknownCity":          "未知回程城市 %q，可选 beijing、guangzhou、shanghai、chengdu、all",
	"err.cityAllCombo":         "回程城市 all 不能与其他城市组合",

	// ── STUN 与 iperf3 ──────────────────────────────────────
	"err.stunNameAddress":  "STUN 服务器必须同时包含 name 和 address",
	"err.stunHostPort":     "STUN 服务器 %q 必须是 host:port 形式",
	"err.iperfNodeFormat":  "iperf3 节点 %q 必须是 host:port 或 host:start-end 形式",
	"err.iperfNodeHost":    "iperf3 节点主机 %q 不是安全的 IP 或主机名",
	"err.iperfNodeStart":   "iperf3 节点 %q 起始端口无效",
	"err.iperfNodeRange":   "iperf3 节点 %q 端口范围无效",
	"err.iperfNodeName":    "iperf3 节点名称或主机无效: %q",
	"err.iperfNodeNetwork": "iperf3 节点 %q networks 必须是 IPv4、IPv6 或 IPv4|IPv6",

	// ── Ookla 服务器 ────────────────────────────────────────
	"err.ooklaFormat":       "Ookla 节点必须是 运营商=服务器ID",
	"err.ooklaCarrier":      "未知 Ookla 运营商 %q，可选 电信、联通、移动",
	"err.ooklaCarrierField": "Ookla 服务器 carrier 必须是电信、联通或移动: %q",
	"err.ooklaIDInvalid":    "Ookla 服务器 ID 无效 %q",
	"err.ooklaIDField":      "Ookla 服务器 %q 的 ID 无效",
	"err.ooklaDuplicate":    "Ookla 运营商重复 %q",
	"err.ooklaDupField":     "Ookla 服务器不能重复配置运营商 %q",

	// ── 配置文件 ────────────────────────────────────────────
	"err.configRead":     "读取配置文件: %w",
	"err.configParse":    "解析配置文件: %w",
	"err.configSingle":   "配置文件只能包含一个 JSON 对象",
	"err.configTrailing": "配置文件尾部存在无效内容: %w",
}

var errorEnglish = map[string]string{
	// ── Profiles and modules ────────────────────────────────
	"err.unknownProfile":     "unknown profile %q; choose quick, standard, or full",
	"err.unknownModule":      "unknown module %q",
	"err.noModules":          "at least one module must be selected",
	"err.unknownIPVersion":   "unknown IP family %q; choose auto, 4, or 6",
	"err.unknownFormat":      "unknown output format %q",
	"err.noFormats":          "at least one output format is required",
	"err.unknownMediaRegion": "unknown streaming region %q; choose global, jp, tw, hk, or cn",
	"err.ipv4AndIPv6":        "-4 and -6 cannot be used together",

	// ── Exposure and consent ────────────────────────────────
	"err.unknownExposure":     "unknown exposure level %q; choose %s",
	"err.acceptUnknownModule": "--accept names an unknown module %q",
	"err.acceptNotNeeded":     "module %q does not require explicit consent; modules that do: %s",
	"err.moduleNeedsConsent":  "module %q is an external service requiring explicit consent; add --accept %s",
	"err.moduleConsentShort":  "module %q requires explicit consent: --accept %s",
	"err.moduleAboveLimit":    "module %q has exposure level %s, above the current --exposure %s; use --exposure %s instead",
	"err.moduleAboveLimitFix": "module %q has exposure level %s, above --exposure %s",

	// ── Resource limits ─────────────────────────────────────
	"err.cpuTimeRange":     "CPU test duration must be between 100ms and 30s",
	"err.diskSizeRange":    "disk test size must be between 16 and 16384 MiB",
	"err.httpTimeoutRange": "HTTP timeout must be between 1s and 1m",
	"err.attemptsRange":    "DNS/latency sample counts must be between 1 and 20",
	"err.threadsRange":     "speed test concurrency must be between 1 and 32",
	"err.iperfDuration":    "iperf3 per-direction duration must be between 1s and 30s",
	"err.diskPathEmpty":    "disk test path must not be empty",
	"err.diskPathInvalid":  "disk test path is invalid",
	"err.diskPathWrap":     "disk test path: %w",

	// ── IP quality sources ──────────────────────────────────
	"err.ipSourceUnknown": "unknown IP quality source %q",
	"err.ipSourceEmpty":   "IP quality sources must not be empty; use all, none, or a comma-separated list",
	"err.ipSourceCombo":   "IP quality sources all/none cannot be combined with other sources",

	// ── Endpoints ───────────────────────────────────────────
	"err.endpointMissingAddress": "endpoint %q has no address",
	"err.endpointNeedsHostPort":  "endpoint %q must be in host:port form",
	"err.endpointUnsafeHost":     "endpoint host %q is not a safe IP or hostname",
	"err.endpointUnsafe":         "endpoint %q is not a safe IP or hostname",
	"err.endpointDuplicate":      "endpoint %q is duplicated",
	"err.endpointNameAddress":    "custom test endpoints must have both name and address",
	"err.endpointFamily":         "test endpoint %q family must be 4, 6, or empty",
	"err.portMissing":            "missing port",
	"err.portInvalid":            "invalid port",

	// ── Route and backtrace targets ─────────────────────────
	"err.routeNameAddress":     "custom route targets must have both name and address",
	"err.routeUnsafe":          "route target %q is not a safe IP or hostname",
	"err.routeFamily":          "route target %q family must be 4, 6, or empty",
	"err.backtraceNameAddress": "backtrace targets must have both name and address",
	"err.backtraceUnsafe":      "backtrace target %q is not a safe IP or hostname",
	"err.backtraceFamily":      "backtrace target %q family must be 4, 6, or empty",
	"err.unknownCity":          "unknown backtrace city %q; choose beijing, guangzhou, shanghai, chengdu, or all",
	"err.cityAllCombo":         "backtrace city all cannot be combined with other cities",

	// ── STUN and iperf3 ─────────────────────────────────────
	"err.stunNameAddress":  "STUN servers must have both name and address",
	"err.stunHostPort":     "STUN server %q must be in host:port form",
	"err.iperfNodeFormat":  "iperf3 node %q must be in host:port or host:start-end form",
	"err.iperfNodeHost":    "iperf3 node host %q is not a safe IP or hostname",
	"err.iperfNodeStart":   "iperf3 node %q has an invalid start port",
	"err.iperfNodeRange":   "iperf3 node %q has an invalid port range",
	"err.iperfNodeName":    "invalid iperf3 node name or host: %q",
	"err.iperfNodeNetwork": "iperf3 node %q networks must be IPv4, IPv6, or IPv4|IPv6",

	// ── Ookla servers ───────────────────────────────────────
	"err.ooklaFormat":       "Ookla nodes must be in carrier=server-id form",
	"err.ooklaCarrier":      "unknown Ookla carrier %q; choose telecom, unicom, or mobile",
	"err.ooklaCarrierField": "Ookla server carrier must be telecom, unicom, or mobile: %q",
	"err.ooklaIDInvalid":    "invalid Ookla server ID %q",
	"err.ooklaIDField":      "Ookla server %q has an invalid ID",
	"err.ooklaDuplicate":    "duplicate Ookla carrier %q",
	"err.ooklaDupField":     "Ookla servers must not configure the same carrier twice: %q",

	// ── Config file ─────────────────────────────────────────
	"err.configRead":     "read config file: %w",
	"err.configParse":    "parse config file: %w",
	"err.configSingle":   "the config file must contain exactly one JSON object",
	"err.configTrailing": "invalid trailing content in the config file: %w",
}

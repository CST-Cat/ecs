package i18n

// 探针文案的英文译文（第 7 批：覆盖率测试抓出的模板）。
//
// 这一批的 key 全部由与 templateOf 相同的正则生成，没有手写：
// 手写会漏掉 "4K" 里的 4、把 "127.255.255.x" 当成四段而不是一整个数值序列，
// 这些错误在第 6 批里都真实发生过，是覆盖率测试逐条抓出来的。

var probeEnglishTemplates = map[string]string{
	"\x1f (关)": "\x1f (off)",
	"\x1f (开)": "\x1f (on)",
	"\x1f 个目标的 TCP 建连延迟不到同目标 ICMP 往返的 \x1f/\x1f：握手几乎不可能真的到达目标，通常是本机或网关上的透明代理、TPROXY 重定向或加速器代答了 TCP。此时 TCP 列反映的是到代理的距离，不能当作本机到目标的链路延迟；ICMP 列不经代理，更接近真实往返。": "\x1f target(s) show TCP connect latency below \x1f/\x1f of the ICMP round trip to the same host: the handshake can hardly be reaching the target. Usually a transparent proxy, TPROXY redirect or accelerator on this host or its gateway is answering TCP on its behalf. The TCP column then measures the distance to that proxy, not the link to the target; the ICMP column bypasses it and is closer to the real round trip.",
	"\x1f 项成功，\x1f 项异常":  "\x1f passed, \x1f failed",
	"\x1f 项成功，\x1f 项需留意": "\x1f passed, \x1f need attention",
	"\x1f 项测试完成":         "\x1f checks completed",
	"\x1f.x 是“查询被拒绝”的保留码（多为使用了公共解析器或超出配额），ecs 不会把它当作命中——否则换个 DNS 就会凭空多出几条黑名单记录。": "\x1f.x is the reserved \"query refused\" code (usually a public resolver or an exceeded quota). ecs never counts it as a listing — otherwise merely switching DNS would conjure up blocklist hits.",
	"\x1f/\x1f 可达 · Telegram 最近 DC\x1f Amsterdam": "\x1f/\x1f reachable · nearest Telegram DC\x1f Amsterdam",
	"\x1f/\x1f 可达 · Telegram 最近 DC\x1f Singapore": "\x1f/\x1f reachable · nearest Telegram DC\x1f Singapore",
	"fio \x1fK 随机写 QD\x1f":                        "fio \x1fK random write QD\x1f",
	"fio \x1fK 随机写延迟 P\x1f":                       "fio \x1fK random write latency P\x1f",
	"fio \x1fK 随机读 QD\x1f":                        "fio \x1fK random read QD\x1f",
	"fio \x1fK 随机读延迟 P\x1f":                       "fio \x1fK random read latency P\x1f",
	"fio Direct I/O 的顺序吞吐、\x1fK 随机 IOPS 与 YABS 兼容的 \x1f/\x1f 混合矩阵":    "fio Direct I/O sequential throughput, \x1fK random IOPS and the YABS-compatible \x1f/\x1f mixed matrix",
	"fio 写 \x1f MiB/s · 读 \x1f MiB/s · \x1fK 读/写 \x1f IOPS/\x1f IOPS": "fio write \x1f MiB/s · read \x1f MiB/s · \x1fK read/write \x1f/\x1f IOPS",
	"疑似被代答的目标":  "Targets likely answered by a proxy",
	"，\x1f 项跳过": ", \x1f skipped",
}

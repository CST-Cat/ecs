package i18n

// probeStage9Chinese/probeStage9English collect stable presentation keys for
// probes migrated during the machine-semantics stage. Machine facts and raw
// third-party evidence do not belong in these catalogs.
var probeStage9Chinese = map[string]string{
	"probe.ports.description":           "常用 Web、SSH、DNS 与邮件端口的 TCP 出站建连",
	"probe.ports.profile":               "固定公共端点",
	"probe.ports.comparison_scope":      "可达性诊断；单目标失败不能等同于端口被封",
	"probe.ports.target_type.web":       "Web",
	"probe.ports.target_type.git":       "Git",
	"probe.ports.target_type.dns":       "DNS",
	"probe.ports.target_type.mail":      "邮件",
	"probe.ports.table.title":           "TCP 出站能力",
	"probe.ports.column.service":        "服务",
	"probe.ports.column.target":         "目标",
	"probe.ports.column.type":           "类型",
	"probe.ports.column.status":         "结果",
	"probe.ports.column.detail":         "延迟/原因",
	"probe.ports.status.reachable":      "可达",
	"probe.ports.status.unreachable":    "阻断/不可达",
	"probe.ports.metric.reachable":      "可达端口",
	"probe.ports.metric.reachable_mail": "可达邮件端口",
	"probe.ports.note.handshake_only":   "只完成 TCP 握手，不发送邮件、认证信息或应用层命令。",
	"probe.ports.note.failure_scope":    "失败可能来自本机防火墙、上游封锁、DNS、目标服务或地区限制；单一目标失败不能证明端口被运营商封锁。",
	"probe.ports.summary":               "%d/%d 可达 · 邮件 %d/4",
}

var probeStage9English = map[string]string{
	"probe.ports.description":           "TCP egress connectivity to common Web, SSH, DNS, and mail ports",
	"probe.ports.profile":               "Fixed public endpoints",
	"probe.ports.comparison_scope":      "Reachability diagnostic; failure to one target does not by itself prove that the port is blocked",
	"probe.ports.target_type.web":       "Web",
	"probe.ports.target_type.git":       "Git",
	"probe.ports.target_type.dns":       "DNS",
	"probe.ports.target_type.mail":      "Mail",
	"probe.ports.table.title":           "TCP egress capability",
	"probe.ports.column.service":        "Service",
	"probe.ports.column.target":         "Target",
	"probe.ports.column.type":           "Type",
	"probe.ports.column.status":         "Result",
	"probe.ports.column.detail":         "Latency/reason",
	"probe.ports.status.reachable":      "Reachable",
	"probe.ports.status.unreachable":    "Blocked/unreachable",
	"probe.ports.metric.reachable":      "Reachable ports",
	"probe.ports.metric.reachable_mail": "Reachable mail ports",
	"probe.ports.note.handshake_only":   "Only the TCP handshake is completed; no mail, credentials, or application-layer commands are sent.",
	"probe.ports.note.failure_scope":    "Failures may come from the local firewall, upstream filtering, DNS, the target service, or regional restrictions; one failed target does not prove carrier-level port blocking.",
	"probe.ports.summary":               "%d/%d reachable · mail %d/4",
}

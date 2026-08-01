package i18n

// 探针文案的英文译文（第 9 批：跨机器差异导致的片段模板）。
//
// 这一批全部由与 tokenTemplateOf 相同的正则生成，解决的是同一句话在不同机器上
// 长得不一样：CI 机器与开发机在单位（KiB/MiB）、挂载路径（/ 与 /tmp）、
// 虚拟化类型（Hyper-V 与 none/unknown）上都不同，这些差异不是数字，
// 数值占位覆盖不到，只能靠片段占位。这些遗漏是 CI 在真实机器上跑出来的，
// 开发机上永远不会触发。

var probeEnglishCross = map[string]string{
	// methodology.engine 与 measurement.method 里混入的中文描述。
	"NextTrace/traceroute + 骨干网段特征表":     "NextTrace/traceroute + backbone prefix signatures",
	"公司滥用概率；官方免密直连":                      "Company abuse probability; official keyless endpoint",
	"ASN 滥用概率；官方免密直连":                    "ASN abuse probability; official keyless endpoint",
	"90 天滥用置信度；IPQuality/check.place 中转": "90-day abuse confidence; IPQuality/check.place relay",
	"IP2Proxy 欺诈分；官方免密接口":                "IP2Proxy fraud score; official keyless endpoint",
	"Web 流量欺诈分；IPQuality/check.place 中转": "Web traffic fraud score; IPQuality/check.place relay",
	"IP 欺诈分；官方公开查询页":                     "IP fraud score; official public lookup page",
	"威胁等级（0/50/100 映射*）；官方 free API（免密保底）；low/medium/high 映射为 0/50/100，仅用于展示": "Threat level (mapped to 0/50/100*); official free API (keyless fallback); low/medium/high mapped to 0/50/100 for display only",
	// 量纲：给人看的单位，跟随语言。
	"线程": "threads",
	"项":  "items",
	"小时": "hours",
	// 平台专有名词：英文用户看不懂中文台名，给出通行的罗马化/英文名。
	"巴哈姆特動畫瘋":      "Bahamut Anime (TW)",
	"网易云音乐":        "NetEase Cloud Music",
	"Bilibili 港澳台": "Bilibili (HK/MO/TW)",
	// 拆分后的固定说明句与新增字段
	"ioping 延迟测试失败，详见下方失败原因。": "ioping latency test failed; see the failure reason below.",
	"ioping 失败原因": "ioping failure reason",
	"SMART 失败原因":  "SMART failure reason",
	"未能读取磁盘 SMART 信息：需要 root 权限，或该设备不提供 SMART。VPS 的虚拟磁盘通常不透传 SMART，这不影响 fio 与 ioping 的成绩。": "Could not read disk SMART data: it needs root privileges, or the device exposes none. VPS virtual disks usually do not pass SMART through; this does not affect the fio or ioping results.",
	"\x1f \x1f · \x1f \x1f 内存 · \x1f \x1f 可用盘 · \x1f": "\x1f \x1f · \x1f \x1f RAM · \x1f \x1f free disk · \x1f",
	"\x1f \x1f 总计 / \x1f \x1f 可用 (/)":                 "\x1f \x1f total / \x1f \x1f available (/)",
	"\x1f \x1f 总计 / \x1f \x1f 可用 (/tmp)":              "\x1f \x1f total / \x1f \x1f available (/tmp)",
	"\x1f 小时 \x1f 分":                                  "\x1fh \x1fm",
	"接收缓冲上限 \x1f \x1f，在 \x1f \x1f 往返的链路上单条 \x1f 连接最高只能跑到约 \x1f \x1f——跨洋链路跑不满带宽时先看这里，而不是怀疑线路。多线程下载或调大 \x1f 可以绕开这个限制。": "With a \x1f \x1f receive buffer, a single \x1f connection over a \x1f \x1f round trip tops out near \x1f \x1f. When a long-haul link will not saturate, look here before blaming the route. Multi-threaded transfers or a larger \x1f work around it.",
	"未能读取磁盘 \x1f 信息（\x1f）。\x1f 的虚拟磁盘通常不透传 \x1f，这不影响 \x1f 与 \x1f 的成绩。":                                                "Could not read disk \x1f data (\x1f). \x1f virtual disks usually do not pass \x1f through; this does not affect the \x1f or \x1f results.",
	"未能读取磁盘 \x1f 信息（需要 \x1f 权限，或该设备不提供 \x1f）。\x1f 的虚拟磁盘通常不透传 \x1f，这不影响 \x1f 与 \x1f 的成绩。":                             "Could not read disk \x1f data (needs \x1f privileges, or the device exposes no \x1f). \x1f virtual disks usually do not pass \x1f through; this does not affect the \x1f or \x1f results.",
}

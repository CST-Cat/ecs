package i18n

// modelMessageChinese/modelMessageEnglish contain only templates consumed from
// model.Message. Their arguments are serialized strings, so templates use %s
// rather than numeric printf verbs.
var modelMessageChinese = map[string]string{
	"message.summary.withErrors":        "%s 项成功，%s 项异常",
	"message.summary.withWarnings":      "%s 项成功，%s 项需留意",
	"message.summary.allOK":             "%s 项测试完成",
	"message.summary.skipped":           "，%s 项跳过",
	"message.result.failed":             "测试失败",
	"message.notice.localOnly":          "ecs 报告只写入本地，不会自动上传；网络探针仍会按模块访问必要的公开目标。",
	"message.notice.compareScope":       "性能结果只应在相同测试方法、版本和资源参数下比较。",
	"message.notice.ooklaPrivacy":       "Ookla 调用外部测速客户端；Ookla 可能独立处理测量元数据，这不属于 ecs 的本地零上传保证。",
	"message.notice.egressShared":       "出口 IP 由 %s 统一发现一次，供需要它的模块共用。",
	"message.runner.skip.offline":       "离线模式",
	"message.runner.skip.noRequestedIP": "未检测到用户请求的可用 IPv4/IPv6 出站能力",
	"message.runner.panic":              "探针异常，已隔离并继续",
}

var modelMessageEnglish = map[string]string{
	"message.summary.withErrors":        "%s succeeded, %s failed",
	"message.summary.withWarnings":      "%s succeeded, %s need attention",
	"message.summary.allOK":             "%s tests completed",
	"message.summary.skipped":           ", %s skipped",
	"message.result.failed":             "Test failed",
	"message.notice.localOnly":          "ecs reports are written locally and are never uploaded automatically; network probes still contact the public targets required by each module.",
	"message.notice.compareScope":       "Performance results should only be compared when the test method, version, and resource parameters are the same.",
	"message.notice.ooklaPrivacy":       "Ookla uses an external speed-test client and may independently process measurement metadata; that processing is outside ecs's local no-upload guarantee.",
	"message.notice.egressShared":       "The egress IP is discovered once through %s and shared by modules that require it.",
	"message.runner.skip.offline":       "Offline mode",
	"message.runner.skip.noRequestedIP": "No usable IPv4/IPv6 outbound capability was detected for the requested address family",
	"message.runner.panic":              "The probe panicked; it was isolated and execution continued",
}

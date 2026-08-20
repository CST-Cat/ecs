package i18n

// modelMessageChinese/modelMessageEnglish contain only templates consumed from
// model.Message. Their arguments are serialized strings, so templates use %s
// rather than numeric printf verbs.
var modelMessageChinese = map[string]string{
	"message.summary.withErrors":   "%s 项成功，%s 项异常",
	"message.summary.withWarnings": "%s 项成功，%s 项需留意",
	"message.summary.allOK":        "%s 项测试完成",
	"message.summary.skipped":      "，%s 项跳过",
	"message.result.failed":        "测试失败",
}

var modelMessageEnglish = map[string]string{
	"message.summary.withErrors":   "%s succeeded, %s failed",
	"message.summary.withWarnings": "%s succeeded, %s need attention",
	"message.summary.allOK":        "%s tests completed",
	"message.summary.skipped":      ", %s skipped",
	"message.result.failed":        "Test failed",
}

package i18n

var probeRetryChinese = map[string]string{
	"probe.retry.selection_rule.interference_score": "先排除无有效证据的轮次，再选择干扰评分较低的一轮；同分保留首次结果，不按性能数字挑选",
}

var probeRetryEnglish = map[string]string{
	"probe.retry.selection_rule.interference_score": "Exclude attempts without valid evidence, then choose the lower interference score; keep the first attempt on a tie instead of selecting by performance values",
}

package i18n

// probeEnglish 汇总各批探针文案译文。
//
// 分文件维护是为了让每一批的领域边界清晰；这里在初始化时合并成一张表供查询。
// 重复 key 会在启动时 panic——同一句话出现两份译文，说明分批时切错了边界，
// 那是必须当场发现的错误，而不是让其中一份静默生效。
var probeEnglish = func() map[string]string {
	merged := make(map[string]string, 512)
	for _, table := range []map[string]string{
		probeEnglishSystem,
		probeEnglishBench,
		probeEnglishNet,
		probeEnglishRep,
		probeEnglishQuality,
		probeEnglishMisc,
		probeEnglishTemplates,
		probeEnglishExtra,
	} {
		for key, value := range table {
			if existing, duplicated := merged[key]; duplicated && existing != value {
				panic("i18n: 探针文案 " + key + " 有两份不同的译文")
			}
			merged[key] = value
		}
	}
	return merged
}()

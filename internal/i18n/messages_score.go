package i18n

// 评分与纯文本报告文案。

var scoreChinese = map[string]string{
	"score.title":      "综合评分",
	"score.total":      "总分",
	"score.coverage":   "基于 %d/%d 个维度",
	"score.ofBaseline": "排行榜参考的 %.0f%%",

	"score.dimension.cpu":       "CPU",
	"score.dimension.memory":    "内存",
	"score.dimension.disk":      "磁盘",
	"score.dimension.bandwidth": "带宽",

	"score.missing.moduleNotRun":       "未测（未计入）",
	"score.missing.noComparableMetric": "无可比指标（未计入）",

	"score.incompleteWarning":   "未跑满全部维度，本次总分不可与完整跑分直接比较。",
	"score.incompleteStatus":    "评分状态：未覆盖全部维度",
	"score.matrixItemCount":     "%d 项",
	"score.matrixMissingCount":  "%d 项",
	"score.missingMetrics":      "%s 缺少 %d 项指标（%s），未补零。",
	"score.weightingNote":       "磁盘按基线、混合、Crystal、ATTO 四个等权子组计算；每组先平均，矩阵单元不会按数量放大。内存按 STREAM Copy、Scale、Add、Triad 四个等权子组计算，每个 kernel 的 1T/NT 取中位数；缺失项不补零。",
	"score.singleSampleWarning": "当前排行榜参考样本不足，分数仅供自查；跨机器比较需要用多机样本重建排行榜统计。",
	"score.baseline.aggregated": "本地聚合",
	"score.baselineLine":        "排行榜参考均值：%s（样本 %d 台）。更换参考后分数不可直接比较。",
	"score.rank.available":      "当前排行榜前 %.1f%%（样本 %d 台）",
	"score.rank.insufficient":   "排行榜样本不足（当前 %d 台，至少需要 %d 台）",
	"score.rank.unavailable":    "排行榜排名不可用（当前参考未保存足够的分数分布；暂不排名）",

	"score.tierLine":         "本机 %d 核，与 %s 档的机器比较。",
	"score.tierFallbackLine": "本机 %d 核，该档样本不足，改用全部机型的排行榜参考均值；跨机型比较对两端都不够公平。",

	"score.metric.cpu_single":           "CPU 单线程",
	"score.metric.cpu_multi":            "CPU 多线程",
	"score.metric.memory_copy":          "STREAM Copy（1T/NT 中位数）",
	"score.metric.memory_scale":         "STREAM Scale（1T/NT 中位数）",
	"score.metric.memory_add":           "STREAM Add（1T/NT 中位数）",
	"score.metric.memory_triad":         "STREAM Triad（1T/NT 中位数）",
	"score.metric.fio_mixed":            "混合读写",
	"score.metric.crystal":              "Crystal",
	"score.metric.atto":                 "ATTO",
	"score.metric.disk_seq_read":        "磁盘顺序读",
	"score.metric.disk_seq_write":       "磁盘顺序写",
	"score.metric.disk_rand_read_iops":  "磁盘 4K 随机读",
	"score.metric.disk_rand_write_iops": "磁盘 4K 随机写",
	"score.metric.bandwidth_download":   "下行带宽（参考样本聚合）",
	"score.metric.bandwidth_upload":     "上行带宽（参考样本聚合）",
}

var scoreEnglish = map[string]string{
	"score.title":      "Composite score",
	"score.total":      "Total",
	"score.coverage":   "based on %d/%d dimensions",
	"score.ofBaseline": "%.0f%% of leaderboard reference",

	"score.dimension.cpu":       "CPU",
	"score.dimension.memory":    "Memory",
	"score.dimension.disk":      "Disk",
	"score.dimension.bandwidth": "Bandwidth",

	"score.missing.moduleNotRun":       "not measured (excluded)",
	"score.missing.noComparableMetric": "no comparable metric (excluded)",

	"score.incompleteWarning":   "Not all dimensions ran; this total is not directly comparable with a full run.",
	"score.incompleteStatus":    "Score status: not all dimensions ran",
	"score.matrixItemCount":     "%d items",
	"score.matrixMissingCount":  "(%d)",
	"score.missingMetrics":      "%s is missing %d metric(s) (%s); missing values were not filled with zero.",
	"score.weightingNote":       "Disk uses four equal-weight subgroups: baseline, mixed, Crystal and ATTO; each subgroup is averaged first, so matrix cells do not gain weight by count. Memory uses equal-weight STREAM Copy, Scale, Add and Triad subgroups, with the 1T/NT median per kernel; missing values are excluded.",
	"score.singleSampleWarning": "The current leaderboard reference has too few samples; scores are for self-comparison. Rebuild the leaderboard statistics from multiple hosts before comparing across machines.",
	"score.baseline.aggregated": "Local aggregation",
	"score.baselineLine":        "Leaderboard reference mean: %s (%d sample host(s)). Scores are not comparable across different references.",
	"score.rank.available":      "Currently in the top %.1f%% of the leaderboard (%d samples)",
	"score.rank.insufficient":   "Not enough leaderboard samples (currently %d; need at least %d)",
	"score.rank.unavailable":    "Leaderboard rank unavailable (the current reference has no sufficient score distribution; no rank shown)",

	"score.tierLine":         "This host has %d vCPU and is compared against the %s tier.",
	"score.tierFallbackLine": "This host has %d vCPU; that tier has too few samples, so the global leaderboard reference mean across all sizes is used instead — which is unfair to both ends of the range.",

	"score.metric.cpu_single":           "CPU single-thread",
	"score.metric.cpu_multi":            "CPU multi-thread",
	"score.metric.memory_copy":          "STREAM Copy (1T/NT median)",
	"score.metric.memory_scale":         "STREAM Scale (1T/NT median)",
	"score.metric.memory_add":           "STREAM Add (1T/NT median)",
	"score.metric.memory_triad":         "STREAM Triad (1T/NT median)",
	"score.metric.fio_mixed":            "Mixed read/write",
	"score.metric.crystal":              "Crystal",
	"score.metric.atto":                 "ATTO",
	"score.metric.disk_seq_read":        "Disk sequential read",
	"score.metric.disk_seq_write":       "Disk sequential write",
	"score.metric.disk_rand_read_iops":  "Disk 4K random read",
	"score.metric.disk_rand_write_iops": "Disk 4K random write",
	"score.metric.bandwidth_download":   "Download (sample aggregate)",
	"score.metric.bandwidth_upload":     "Upload (sample aggregate)",
}

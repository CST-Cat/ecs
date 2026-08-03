package i18n

// 评分与纯文本报告文案。

var scoreChinese = map[string]string{
	"score.title":      "综合评分",
	"score.total":      "总分",
	"score.coverage":   "基于 %d/%d 个维度",
	"score.ofBaseline": "基线的 %.0f%%",

	"score.dimension.cpu":       "CPU",
	"score.dimension.memory":    "内存",
	"score.dimension.disk":      "磁盘",
	"score.dimension.bandwidth": "带宽",

	"score.missing.moduleNotRun":       "未测（未计入）",
	"score.missing.noComparableMetric": "无可比指标（未计入）",

	"score.incompleteWarning":   "未跑满全部维度，本次总分不可与完整跑分直接比较。",
	"score.missingMetrics":      "%s 缺少 %d 项指标（%s），未补零。",
	"score.weightingNote":       "磁盘按 legacy、混合、Crystal、ATTO 四个等权子组计算；每组先平均，矩阵单元不会按数量放大。内存按 memcpy、读、写、时延子组等权；缺失项不补零。",
	"score.singleSampleWarning": "当前基线只有 1 个样本，分数仅供自查；跨机器比较需要用多机样本重建基线。",
	"score.baselineLine":        "评分基线：%s（样本 %d 台）。换基线后分数不可直接比较。",

	"score.tierLine":         "本机 %d 核，与 %s 档的机器比较。",
	"score.tierFallbackLine": "本机 %d 核，该档样本不足，改用全部机型的全局基线；跨机型比较对两端都不够公平。",

	"score.baseline.builtinSingleHost": "内置单机快照，非 VPS 典型值",

	"score.metric.cpu_single":           "CPU 单线程",
	"score.metric.cpu_multi":            "CPU 多线程",
	"score.metric.memory_copy":          "内存带宽 memcpy",
	"score.metric.memory_write":         "内存顺序写",
	"score.metric.memory_write_multi":   "内存顺序写（多线程）",
	"score.metric.memory_read":          "内存顺序读",
	"score.metric.memory_latency":       "内存事件时延",
	"score.metric.fio_mixed":            "混合读写",
	"score.metric.crystal":              "Crystal",
	"score.metric.atto":                 "ATTO",
	"score.metric.disk_seq_read":        "磁盘顺序读",
	"score.metric.disk_seq_write":       "磁盘顺序写",
	"score.metric.disk_rand_read_iops":  "磁盘 4K 随机读",
	"score.metric.disk_rand_write_iops": "磁盘 4K 随机写",
	"score.metric.bandwidth_download":   "下行带宽（中位）",
	"score.metric.bandwidth_upload":     "上行带宽（中位）",
}

var scoreEnglish = map[string]string{
	"score.title":      "Composite score",
	"score.total":      "Total",
	"score.coverage":   "based on %d/%d dimensions",
	"score.ofBaseline": "%.0f%% of baseline",

	"score.dimension.cpu":       "CPU",
	"score.dimension.memory":    "Memory",
	"score.dimension.disk":      "Disk",
	"score.dimension.bandwidth": "Bandwidth",

	"score.missing.moduleNotRun":       "not measured (excluded)",
	"score.missing.noComparableMetric": "no comparable metric (excluded)",

	"score.incompleteWarning":   "Not all dimensions ran; this total is not directly comparable with a full run.",
	"score.missingMetrics":      "%s is missing %d metric(s) (%s); missing values were not filled with zero.",
	"score.weightingNote":       "Disk uses four equal-weight subgroups: legacy, mixed, Crystal and ATTO; each subgroup is averaged first, so matrix cells do not gain weight by count. Memory uses equal-weight memcpy, read, write and latency subgroups; missing values are excluded.",
	"score.singleSampleWarning": "The current baseline has only 1 sample; scores are for self-comparison. Rebuild the baseline from multiple hosts before comparing across machines.",
	"score.baselineLine":        "Scoring baseline: %s (%d sample host(s)). Scores are not comparable across different baselines.",

	"score.tierLine":         "This host has %d vCPU and is compared against the %s tier.",
	"score.tierFallbackLine": "This host has %d vCPU; that tier has too few samples, so the global baseline across all sizes is used instead — which is unfair to both ends of the range.",

	"score.baseline.builtinSingleHost": "built-in single-host snapshot, not a typical VPS",

	"score.metric.cpu_single":           "CPU single-thread",
	"score.metric.cpu_multi":            "CPU multi-thread",
	"score.metric.memory_copy":          "Memory bandwidth (memcpy)",
	"score.metric.memory_write":         "Memory sequential write",
	"score.metric.memory_write_multi":   "Memory sequential write (multi-thread)",
	"score.metric.memory_read":          "Memory sequential read",
	"score.metric.memory_latency":       "Memory event latency",
	"score.metric.fio_mixed":            "Mixed read/write",
	"score.metric.crystal":              "Crystal",
	"score.metric.atto":                 "ATTO",
	"score.metric.disk_seq_read":        "Disk sequential read",
	"score.metric.disk_seq_write":       "Disk sequential write",
	"score.metric.disk_rand_read_iops":  "Disk 4K random read",
	"score.metric.disk_rand_write_iops": "Disk 4K random write",
	"score.metric.bandwidth_download":   "Download (median)",
	"score.metric.bandwidth_upload":     "Upload (median)",
}

package i18n

// probeMemoryInventoryChinese/probeMemoryInventoryEnglish contain the stable
// presentation keys used by the memory-inventory boundary. The values stored
// in result fields remain language-independent machine facts.
var probeMemoryInventoryChinese = map[string]string{
	"probe.memory.stream.profile":                    "Copy/Scale/Add/Triad × 1T/NT",
	"probe.memory.stream.profile.single_core":        "Copy/Scale/Add/Triad × 1T/NT（单次 1T 实测复用）",
	"probe.memory.field.balloon_reclaim":             "Balloon reclaim",
	"probe.memory.field.balloon_reclaim_available":   "Balloon reclaim 可用",
	"probe.memory.field.balloon_reclaim_evidence":    "Balloon reclaim 证据",
	"probe.memory.field.ksm_merging":                 "KSM merging",
	"probe.memory.field.ksm_merging_available":       "KSM merging 可用",
	"probe.memory.field.ksm_merging_evidence":        "KSM merging 证据",
	"probe.memory.note.cgroup_current":               "cgroup memory.current 提供当前 cgroup 使用量；可用余量同时受每个适用限制及其聚合使用量约束，祖先使用量缺失时标记为未知。",
	"probe.memory.note.memavailable_legacy_fallback": "MemAvailable 不可用；使用 MemFree + Buffers + Cached 估算可用内存。",
	"probe.memory.note.balloon_reclaim_unavailable":  "Balloon reclaim 不可用：未找到可验证的 Linux sysfs/proc reclaim 证据。",
}

var probeMemoryInventoryEnglish = map[string]string{
	"probe.memory.stream.profile":                    "Copy/Scale/Add/Triad × 1T/NT",
	"probe.memory.stream.profile.single_core":        "Copy/Scale/Add/Triad × 1T/NT (one measured 1T run reused)",
	"probe.memory.field.balloon_reclaim":             "Balloon reclaim",
	"probe.memory.field.balloon_reclaim_available":   "Balloon reclaim available",
	"probe.memory.field.balloon_reclaim_evidence":    "Balloon reclaim evidence",
	"probe.memory.field.ksm_merging":                 "KSM merging",
	"probe.memory.field.ksm_merging_available":       "KSM merging available",
	"probe.memory.field.ksm_merging_evidence":        "KSM merging evidence",
	"probe.memory.note.cgroup_current":               "cgroup memory.current provides current cgroup usage; available headroom considers every applicable limit and its aggregate usage, and is unknown when ancestor usage is unavailable.",
	"probe.memory.note.memavailable_legacy_fallback": "MemAvailable is unavailable; available memory is estimated as MemFree + Buffers + Cached.",
	"probe.memory.note.balloon_reclaim_unavailable":  "Balloon reclaim is unavailable: no verifiable Linux sysfs/proc reclaim evidence was found.",
}

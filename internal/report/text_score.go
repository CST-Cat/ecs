package report

import (
	"fmt"
	"strings"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/textwidth"
)

// scoreSection 渲染综合评分。
//
// 覆盖度与基线来源和分数并排呈现：一个不说明基于什么、覆盖了多少的分数，
// 读者无法判断它值多少。
func (r *textRenderer) scoreSection() {
	r.sectionTitle(i18n.T("score.title"), "")
	total := r.score.Total
	ratio := total / score.FullScale
	coverage := fmt.Sprintf(i18n.T("score.coverage"), r.score.Covered, r.score.Possible)

	r.linef("  %s  %s   %s",
		textwidth.Pad(i18n.T("score.total"), 12),
		r.palette.WrapRatio(fmt.Sprintf("%7.0f", total), ratio),
		r.palette.Dim(coverage))
	r.blank()

	for _, dimension := range r.score.Dimensions {
		label := textwidth.Pad(i18n.T("score.dimension."+dimension.Key), 12)
		if dimension.Missing {
			reason := i18n.T("score.missing." + dimension.MissingReason)
			r.linef("  %s  %s", label, r.palette.Dim(reason))
			continue
		}
		r.linef("  %s  %s  %s", label,
			r.palette.Bar(dimension.Ratio, adaptiveBarWidth(r.width, barWidth)),
			r.palette.WrapRatio(fmt.Sprintf("%7.0f", dimension.Score), dimension.Ratio))
		r.matrixScoreSummary(dimension)
		// 分项指标缩进一级，读者能看到维度分是怎么来的。
		for _, metric := range dimension.Metrics {
			if matrixKindForMeasurement(metric.Key) != "" {
				continue
			}
			r.linef("      %s  %s  %s",
				textwidth.Pad(textwidth.Truncate(metricLabel(metric), 22), 22),
				r.palette.Dim(textwidth.PadLeft(formatFloat(metric.Value)+" "+metric.Unit, 20)),
				r.palette.Dim(fmt.Sprintf(i18n.T("score.ofBaseline"), metric.Ratio*100)))
		}
		if len(dimension.MissingMetrics) > 0 {
			r.indented(fmt.Sprintf(i18n.T("score.missingMetrics"), i18n.T("score.dimension."+dimension.Key), len(dimension.MissingMetrics), compactMissingMetrics(dimension)))
		}
	}
	r.blank()
	if !r.score.Complete {
		r.indented(i18n.T("score.incompleteStatus"))
	}
	if r.score.BaselineSample > 0 {
		r.indented(fmt.Sprintf(i18n.T("score.baselineLine"), baselineSourceLabel(r.score.BaselineSource), r.score.BaselineSample))
	}
	if r.score.RankStatus != "" || r.score.RankSamples > 0 || r.score.BaselineSample > 0 {
		switch r.score.EffectiveRankStatus() {
		case score.RankStatusAvailable:
			r.indented(fmt.Sprintf(i18n.T("score.rank.available"), r.score.TopPercent, r.score.EffectiveRankSamples()))
		case score.RankStatusInsufficient:
			r.indented(fmt.Sprintf(i18n.T("score.rank.insufficient"), r.score.EffectiveRankSamples(), r.score.EffectiveRankMinSamples()))
		default:
			r.indented(i18n.T("score.rank.unavailable"))
		}
	}
	r.blank()
}

// matrixScoreSummary keeps the score section readable when a disk result has
// dozens of Crystal/ATTO cells.  The score still uses every cell; only the
// textual explanation is grouped by the same subgroup boundaries used by the
// calculation.
func (r *textRenderer) matrixScoreSummary(dimension score.DimensionScore) {
	counts := matrixScoreCounts(dimension)
	if len(counts) == 0 {
		return
	}
	parts := make([]string, 0, len(counts))
	for _, kind := range []diskMatrixKind{matrixCrystal, matrixMixed, matrixATTO} {
		if count := counts[kind]; count > 0 {
			itemCount := fmt.Sprintf(i18n.T("score.matrixItemCount"), count)
			parts = append(parts, matrixKindLabel(kind)+" "+itemCount)
		}
	}
	if len(parts) > 0 {
		separator := " · "
		r.linef("      %s", r.palette.Dim(strings.Join(parts, separator)))
	}
}

func matrixScoreCounts(dimension score.DimensionScore) map[diskMatrixKind]int {
	counts := make(map[diskMatrixKind]int)
	for _, group := range dimension.Groups {
		if kind := matrixKindForGroup(group.Key); kind != "" && group.MetricCount > 0 {
			counts[kind] = group.MetricCount
		}
	}
	return counts
}

func compactMissingMetrics(dimension score.DimensionScore) string {
	nonMatrix := make([]string, 0, len(dimension.MissingMetrics))
	counts := make(map[diskMatrixKind]int)
	for _, key := range dimension.MissingMetrics {
		if kind := matrixKindForMeasurement(key); kind != "" {
			counts[kind]++
			continue
		}
		nonMatrix = append(nonMatrix, key)
	}
	for _, kind := range []diskMatrixKind{matrixCrystal, matrixMixed, matrixATTO} {
		if count := counts[kind]; count > 0 {
			label := matrixKindLabel(kind)
			label += " " + fmt.Sprintf(i18n.T("score.matrixMissingCount"), count)
			nonMatrix = append(nonMatrix, label)
		}
	}
	if len(nonMatrix) == 0 {
		return "—"
	}
	return strings.Join(nonMatrix, ", ")
}

// statusStyle colors a complete status row as one unit.  Unknown values are
// intentionally dim rather than guessed as success or failure.
func (r *textRenderer) statusStyle(status model.Status) func(string) string {
	switch status {
	case model.StatusOK:
		return r.palette.SuccessBold
	case model.StatusWarning:
		return r.palette.WarningBold
	case model.StatusError:
		return r.palette.ErrorBold
	case model.StatusSkipped:
		return r.palette.Dim
	default:
		return r.palette.Dim
	}
}

// explicitValueTone returns a presentation tone only for known stable value
// keys. Raw provider values never enter this switch: their text is data, not
// an ECS-owned status.
func explicitValueTone(value model.Value) (termcolor.Tone, bool) {
	key, ok := value.Key()
	if !ok {
		return termcolor.ToneLabel, false
	}
	switch key {
	case "probe.network.status.ok",
		"probe.network.risk.very_low", "probe.network.risk.low",
		"probe.dns.status.ok",
		"probe.cnspeed.status.complete",
		"probe.nat.status.complete",
		"probe.npb.verification.successful",
		"probe.cpu.validity.valid", "probe.cpu.validity.quota",
		"probe.memory.stream.evidence.best_rate", "probe.memory.stream.evidence.reused",
		"probe.ookla.status.complete",
		"probe.ports.status.reachable",
		"probe.apps.status.reachable",
		"probe.blacklist.outcome.clean",
		"probe.backtrace.status.identified",
		"probe.backtrace.hop.responded",
		"probe.rdns.status.passed",
		"probe.speed.status.complete",
		"probe.route.status.complete",
		"probe.disk.status.complete",
		"probe.media.verdict.unlocked":
		return termcolor.ToneSuccess, true
	case "probe.network.status.partial",
		"probe.network.risk.medium", "probe.network.risk.suspicious",
		"probe.dns.status.partial",
		"probe.ookla.status.partial",
		"probe.speed.status.partial",
		"probe.nat.status.udp_blocked",
		"probe.media.verdict.originals", "probe.media.verdict.login", "probe.media.verdict.restricted",
		"probe.cpu.validity.partial",
		"probe.ookla.status.unparsed":
		return termcolor.ToneWarning, true
	case "probe.network.status.failed",
		"probe.network.value.lookup_failed",
		"probe.network.value.intel_unavailable",
		"probe.network.risk.high", "probe.network.risk.very_high",
		"probe.dns.status.failed",
		"probe.cnspeed.status.failed",
		"probe.npb.verification.failed",
		"probe.ports.status.unreachable",
		"probe.apps.status.unreachable",
		"probe.blacklist.outcome.listed", "probe.blacklist.outcome.refused", "probe.blacklist.outcome.failed",
		"probe.backtrace.status.failed",
		"probe.rdns.status.failed",
		"probe.latency.status.resolve_failed",
		"probe.speed.status.failed",
		"probe.route.status.failed", "probe.route.status.parse_failed",
		"probe.memory.stream.evidence.failed",
		"probe.media.verdict.unreachable":
		return termcolor.ToneError, true
	case "probe.network.status.disabled",
		"probe.network.risk.unknown",
		"probe.network.boolean.unknown",
		"probe.network.value.missing",
		"probe.network.value.intel_not_attempted",
		"probe.backtrace.status.unidentified",
		"probe.backtrace.value.missing",
		"probe.backtrace.hop.no_response",
		"probe.ookla.status.ip_family",
		"probe.latency.status.no_resolution",
		"probe.memory.stream.evidence.missing",
		"probe.disk.status.missing",
		"probe.media.verdict.locked",
		"probe.npb.workload.unknown",
		"probe.nat.mapping.unknown", "probe.nat.filtering.unknown", "probe.nat.category.unknown",
		"probe.media.verdict.unknown":
		return termcolor.ToneLabel, true
	default:
		return termcolor.ToneLabel, false
	}
}

func (r *textRenderer) styledValue(value string, source model.Value) string {
	tone, ok := explicitValueTone(source)
	if !ok {
		return value
	}
	if tone == termcolor.ToneLabel {
		return r.palette.Dim(value)
	}
	return r.palette.Tone(value, tone)
}

func (r *textRenderer) valueStyle(source model.Value) func(string) string {
	tone, ok := explicitValueTone(source)
	if !ok {
		return nil
	}
	if tone == termcolor.ToneLabel {
		return r.palette.Dim
	}
	return func(value string) string { return r.palette.Tone(value, tone) }
}

func explicitValueClass(value model.Value) string {
	tone, ok := explicitValueTone(value)
	if !ok {
		return ""
	}
	switch tone {
	case termcolor.ToneSuccess:
		return "cell-good"
	case termcolor.ToneWarning:
		return "cell-warn"
	case termcolor.ToneError:
		return "cell-bad"
	case termcolor.ToneLabel:
		return "cell-muted"
	default:
		return ""
	}
}

type diskMatrixKind string

const (
	matrixCrystal diskMatrixKind = "crystal"
	matrixMixed   diskMatrixKind = "mixed"
	matrixATTO    diskMatrixKind = "atto"
)

func matrixKindLabel(kind diskMatrixKind) string {
	switch kind {
	case matrixCrystal:
		return i18n.T("score.metric.crystal")
	case matrixMixed:
		return i18n.T("score.metric.fio_mixed")
	case matrixATTO:
		return i18n.T("score.metric.atto")
	default:
		return string(kind)
	}
}

func matrixKindForGroup(key string) diskMatrixKind {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case string(matrixCrystal):
		return matrixCrystal
	case string(matrixMixed), "fio_mixed":
		return matrixMixed
	case string(matrixATTO):
		return matrixATTO
	default:
		return matrixKindForMeasurement(key)
	}
}

// metricLabel 优先用 i18n 文案：measurement 的 label 是探针产出时的语言，
// 换语言重新导出时会与报告其余部分不一致。
func metricLabel(metric score.MetricScore) string {
	if key := "score.metric." + metric.Key; i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	if kind := matrixKindForMeasurement(metric.Key); kind != "" {
		key := strings.ToLower(strings.TrimSpace(metric.Key))
		switch kind {
		case matrixCrystal:
			key = strings.TrimPrefix(key, "crystal_")
		case matrixMixed:
			key = strings.TrimPrefix(key, "fio_mixed_")
		case matrixATTO:
			key = strings.TrimPrefix(key, "atto_")
		}
		return matrixKindLabel(kind) + " · " + key
	}
	return metric.Label
}

func baselineSourceLabel(source string) string {
	if key := "score.baseline." + source; i18n.Has(i18n.Current(), key) {
		return i18n.T(key)
	}
	return source
}

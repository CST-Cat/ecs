package score

// 当前排行榜参考。
//
// 有真实提交时，CI 会把聚合好的 baseline.json 写进这里，随发行包编译进去。
// 没有当前样本时，文件保持为空参考；运行不会回落到历史数据，也不会为了评分额外联网。
//
// 加载优先级：--score-baseline 指定的文件 > 当前内嵌参考 > 无评分。

import (
	_ "embed"
	"encoding/json"
	"math"
)

//go:embed embedded/baseline.json
var embeddedBaselineJSON []byte

// EmbeddedBaseline 返回随二进制编译进来的基线。
//
// 解析失败时返回空参考而不是 panic：一份坏的内嵌文件不该让整个程序跑不起来，
// 更不能让它退回到已删除的历史口径。
func EmbeddedBaseline() Baseline {
	var baseline Baseline
	if err := json.Unmarshal(embeddedBaselineJSON, &baseline); err != nil {
		return emptyBaseline()
	}
	if baseline.Schema != BaselineSchema || baseline.Source == "" || baseline.SampleCount < 0 || baseline.RankMinSamples < 0 {
		return emptyBaseline()
	}
	if baseline.Metrics == nil {
		baseline.Metrics = map[string]float64{}
	}
	for _, value := range baseline.Metrics {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return emptyBaseline()
		}
	}
	return baseline
}

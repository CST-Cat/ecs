package score

// 内嵌基线。
//
// 新人跑 `curl … | sh` 时不该为了拿一份参考基线再联网一次——那会多出一个外联点，
// 而且 --exposure local 下根本拿不到。所以基线随二进制走：CI 在打包前把仓库里
// 聚合好的 baseline.json 写进这里，发版时一并编译进去。
//
// 加载优先级：--score-baseline 指定的文件 > 内嵌基线 > 代码里的兜底常量。
// 三层都保证有值，因此评分永远不会因为缺基线而消失。

import (
	_ "embed"
	"encoding/json"
)

//go:embed embedded/baseline.json
var embeddedBaselineJSON []byte

// EmbeddedBaseline 返回随二进制编译进来的基线。
//
// 解析失败时回落到代码常量而不是 panic：一份坏的内嵌文件不该让整个程序跑不起来，
// 它只该让分数退回到内置口径。
func EmbeddedBaseline() Baseline {
	var baseline Baseline
	if err := json.Unmarshal(embeddedBaselineJSON, &baseline); err != nil {
		return DefaultBaseline()
	}
	if baseline.Schema != BaselineSchema || len(baseline.Metrics) == 0 {
		return DefaultBaseline()
	}
	for _, value := range baseline.Metrics {
		if value <= 0 {
			return DefaultBaseline()
		}
	}
	return baseline
}

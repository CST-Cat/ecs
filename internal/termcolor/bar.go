package termcolor

// 柱状图。
//
// 参考的样例报告里柱状图大多是坏的：TCP 延迟表里 183ms 与 113ms 都是七个 █，
// ATTO 表里 2.91MB/s 与 2110MB/s 都是十九个 █——柱长完全不随数值变化，等于把
// 装饰当成了信息。这里的柱子长度一定按比例，读者扫一眼就能比出大小。
//
// 层次由两条腿共同支撑：颜色随比例连续变化，字符密度随比例分四档。任何一条腿
// 断了（单色终端、纯文本文件、被 grep 的输出）另一条仍然站得住。

import (
	"math"
	"strings"
)

// Bar 渲染一条比例柱。
//
// ratio 允许超过 1：跑赢基线是常态，截断会让"两倍于基线"和"刚好达标"看起来
// 一样。超出部分用满格加尾标表示，而不是把柱子画出边界。
func (p Palette) Bar(ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(ratio) {
		return p.Dim(strings.Repeat("·", width))
	}
	if ratio < 0 {
		ratio = 0
	}
	clamped := ratio
	overflow := false
	if clamped > 1 {
		clamped, overflow = 1, true
	}

	filled := int(clamped*float64(width) + 0.5)
	// 非零值至少给一格：0.4% 与 0 在视觉上必须能区分开。
	if filled == 0 && ratio > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}

	solid := string(density(ratio))
	body := p.WrapRatio(strings.Repeat(solid, filled), ratio)
	if rest := width - filled; rest > 0 {
		body += p.Dim(strings.Repeat("·", rest))
	}
	if overflow {
		body += p.WrapRatio("▸", ratio)
	}
	return body
}

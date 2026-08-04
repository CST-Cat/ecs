package termcolor

// 终端颜色能力自适应。
//
// 同一份报告要在从单色终端到真彩色终端的整条链路上都读得懂，而终端之间的差距
// 不只是"有没有颜色"，是**能不能寻址到某个颜色**：
//
//	8/16 色    只能说"给我红色"，具体是哪种红由主题决定
//	256 色     可以说"第 196 号"，落在约定的 6×6×6 立方体与 24 级灰阶里
//	真彩色     可以直接说 RGB(255,80,120)
//
// 能力还会在链路上掉档：本地终端支持真彩色，SSH 到没有对应 terminfo 的机器、
// 或穿过配置不全的 tmux，就可能退回 256 色甚至 16 色。因此这里按环境变量与
// TERM 逐级判定，并且**任何一档都要能表达层次**——
//
// 最关键的一条：无色时不能只剩长度。用 █▓▒░ 四级密度字符，单色终端、纯文本
// 文件、被 grep 过的输出里层次依然在。颜色是增强，不是层次的唯一载体。

import (
	"os"
	"strconv"
	"strings"
)

// Level 是终端的颜色寻址能力。
type Level int

const (
	// LevelNone 不输出任何转义序列，层次全靠密度字符表达。
	LevelNone Level = iota
	// LevelBasic 是 ANSI 8 色。
	LevelBasic
	// LevelANSI256 是 256 色索引调色板。
	LevelANSI256
	// LevelTrueColor 是 24 位直接色。
	LevelTrueColor
)

func (l Level) String() string {
	switch l {
	case LevelBasic:
		return "8"
	case LevelANSI256:
		return "256"
	case LevelTrueColor:
		return "truecolor"
	default:
		return "none"
	}
}

// Detect 判定当前环境的颜色能力。
//
// 判定顺序刻意保守：宁可少用一档也不要吐出终端读不懂的转义序列——多余的
// 转义在不支持的终端上会变成可见乱码，那比少几种颜色糟得多。
func Detect(isTTY bool) Level {
	// NO_COLOR 是跨工具的既定约定，只要被设置（哪怕为空）就一律关闭。
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return LevelNone
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "dumb" {
		return LevelNone
	}
	// CI 环境通常不是 TTY 但能正确记录并回放转义序列。
	if !isTTY && strings.TrimSpace(os.Getenv("CI")) == "" {
		return LevelNone
	}

	colorTerm := strings.ToLower(strings.TrimSpace(os.Getenv("COLORTERM")))
	if colorTerm == "truecolor" || colorTerm == "24bit" {
		return LevelTrueColor
	}
	// 部分终端只在 TERM 里声明真彩色能力。
	if strings.Contains(term, "truecolor") || strings.Contains(term, "24bit") ||
		strings.Contains(term, "direct") {
		return LevelTrueColor
	}
	if strings.Contains(term, "256color") || strings.Contains(term, "256") {
		return LevelANSI256
	}
	// Ghostty、kitty、WezTerm 等现代终端即使 TERM 未声明也支持真彩色，
	// 但只在它们自报家门时才认，不靠猜。
	switch {
	case strings.Contains(term, "ghostty"), strings.Contains(term, "kitty"),
		strings.Contains(term, "wezterm"), strings.Contains(term, "alacritty"):
		return LevelTrueColor
	}
	if strings.Contains(term, "color") || strings.Contains(term, "xterm") ||
		strings.Contains(term, "screen") || strings.Contains(term, "tmux") ||
		strings.Contains(term, "vt100") || strings.Contains(term, "linux") {
		return LevelBasic
	}
	if term == "" {
		return LevelNone
	}
	return LevelBasic
}

// ParseLevel 解析显式指定的能力档位。
func ParseLevel(raw string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none", "no", "off", "never", "0":
		return LevelNone, true
	case "basic", "8", "16":
		return LevelBasic, true
	case "256", "ansi256":
		return LevelANSI256, true
	case "truecolor", "24bit", "rgb":
		return LevelTrueColor, true
	}
	return LevelNone, false
}

// LevelNames 列出可显式指定的档位。
func LevelNames() []string {
	return []string{"none", "basic", "256", "truecolor"}
}

// RGB 是一个 24 位颜色。
type RGB struct{ R, G, B uint8 }

// Tone is a semantic terminal color used by the text report.  The names are
// deliberately about meaning rather than a particular hue so that the same
// report remains legible when the palette is quantized to 8 or 256 colors.
type Tone int

const (
	ToneAccent Tone = iota
	ToneInfo
	ToneSuccess
	ToneWarning
	ToneError
	ToneLabel
)

var toneColors = [...]RGB{
	ToneAccent:  {R: 0x5F, G: 0xC8, B: 0xFF}, // cyan-blue headings
	ToneInfo:    {R: 0x75, G: 0xA9, B: 0xD8}, // calm blue metadata
	ToneSuccess: {R: 0x3A, G: 0xC6, B: 0x9E}, // teal-green success
	ToneWarning: {R: 0xF0, G: 0xC0, B: 0x4A}, // orange-yellow warning
	ToneError:   {R: 0xE0, G: 0x5A, B: 0x5A}, // red error
	ToneLabel:   {R: 0x9A, G: 0xD9, B: 0xD9}, // restrained cyan labels
}

func toneColor(tone Tone) RGB {
	if tone < 0 || int(tone) >= len(toneColors) {
		return toneColors[ToneLabel]
	}
	return toneColors[tone]
}

// Palette 把一个 0..1 的比例映射到颜色与密度字符。
//
// 比例的含义由调用方决定（相对基线的倍率、组内相对最大值的占比），这里只负责
// 把"越高越好"的一维强度渲染成视觉层次。
type Palette struct {
	Level Level
}

// stops 是色标：从低到高的锚点颜色，中间做线性插值。
//
// 选色避开纯红绿对立：红绿色盲人群约占男性的 8%，因此低分端用偏橙的红、高分端
// 用偏青的绿，两端在明度上也拉开——即使色相分辨不出，深浅仍然有效。
var stops = []struct {
	at    float64
	color RGB
}{
	{0.00, RGB{0xC0, 0x39, 0x2B}}, // 深红
	{0.35, RGB{0xE0, 0x7B, 0x39}}, // 橙
	{0.60, RGB{0xD6, 0xC0, 0x3A}}, // 黄
	{0.85, RGB{0x6F, 0xC1, 0x5A}}, // 绿
	{1.00, RGB{0x3A, 0xC6, 0x9E}}, // 青绿
}

// Color 返回该比例对应的 RGB。比例超过 1 时保持最高档：跑赢基线越多，
// 颜色不再继续变化，避免让"超出"看起来像另一种状态。
func Color(ratio float64) RGB {
	if ratio <= stops[0].at {
		return stops[0].color
	}
	if ratio >= 1 {
		return stops[len(stops)-1].color
	}
	for index := 1; index < len(stops); index++ {
		if ratio > stops[index].at {
			continue
		}
		low, high := stops[index-1], stops[index]
		span := high.at - low.at
		if span <= 0 {
			return high.color
		}
		position := (ratio - low.at) / span
		return RGB{
			R: lerp(low.color.R, high.color.R, position),
			G: lerp(low.color.G, high.color.G, position),
			B: lerp(low.color.B, high.color.B, position),
		}
	}
	return stops[len(stops)-1].color
}

func lerp(from, to uint8, position float64) uint8 {
	return uint8(float64(from) + (float64(to)-float64(from))*position)
}

// densityRunes 是无色时表达层次的密度字符，从低到高。
//
// 这四个字符在等宽字体里同宽，且在纯文本、日志、被 grep 过的输出里都保留层次。
var densityRunes = []rune{'░', '▒', '▓', '█'}

// Density 返回该比例对应的密度字符。
func Density(ratio float64) rune {
	switch {
	case ratio < 0.35:
		return densityRunes[0]
	case ratio < 0.60:
		return densityRunes[1]
	case ratio < 0.85:
		return densityRunes[2]
	default:
		return densityRunes[3]
	}
}

// ansi256 把 RGB 映射到 256 色调色板。
//
// 灰度单独走 232-255 的 24 级灰阶：用 6×6×6 立方体表达灰色会因为量化误差
// 泛出偏色。
func ansi256(color RGB) int {
	if color.R == color.G && color.G == color.B {
		if color.R < 8 {
			return 16
		}
		if color.R > 248 {
			return 231
		}
		return 232 + int((float64(color.R)-8)/247*23)
	}
	index := func(value uint8) int { return int(float64(value) / 255 * 5) }
	return 16 + 36*index(color.R) + 6*index(color.G) + index(color.B)
}

// basicANSI 把 RGB 折到 8 色加亮度位。
//
// 只有几个色相可选时，量化门槛比色相本身更要紧：用"分量是否超过最大分量的
// 四分之三"来判定，绿 (111,193,90) 才不会因为红分量偏高被误判成黄。亮度位
// 则按感知亮度而非最大分量决定——深红与青绿的最大分量接近，但前者该显得暗。
func basicANSI(color RGB) (code int, bright bool) {
	maximum := color.R
	if color.G > maximum {
		maximum = color.G
	}
	if color.B > maximum {
		maximum = color.B
	}
	// ITU-R BT.601 的感知亮度权重：人眼对绿最敏感，对蓝最不敏感。
	luma := 0.299*float64(color.R) + 0.587*float64(color.G) + 0.114*float64(color.B)
	bright = luma > 140
	threshold := uint8(float64(maximum) * 0.75)
	bits := 0
	if color.R > threshold {
		bits |= 1
	}
	if color.G > threshold {
		bits |= 2
	}
	if color.B > threshold {
		bits |= 4
	}
	switch bits {
	case 1:
		return 31, bright // 红
	case 2:
		return 32, bright // 绿
	case 3:
		return 33, bright // 黄
	case 4:
		return 34, bright // 蓝
	case 5:
		return 35, bright // 品红
	case 6:
		return 36, bright // 青
	case 7:
		return 37, bright // 白
	default:
		return 37, false
	}
}

// Wrap 按当前能力给文本着色。
func (p Palette) Wrap(text string, color RGB) string {
	if p.Level == LevelNone || text == "" {
		return text
	}
	return p.escape(color) + text + "\x1b[0m"
}

// Tone applies a semantic color.  It is safe to call at the report's leaf
// values: an already styled value is returned unchanged rather than wrapped
// in another reset sequence.
func (p Palette) Tone(text string, tone Tone) string {
	if p.Level == LevelNone || text == "" || strings.Contains(text, "\x1b") {
		return text
	}
	return p.Wrap(text, toneColor(tone))
}

// ToneBold applies a semantic color and bold emphasis in one CSI sequence.
// Keeping this as one renderer operation avoids the uncontrolled nesting that
// would result from calling Bold(Wrap(...)) throughout the report.
func (p Palette) ToneBold(text string, tone Tone) string {
	if p.Level == LevelNone || text == "" || strings.Contains(text, "\x1b") {
		return text
	}
	return p.escapeBold(toneColor(tone)) + text + "\x1b[0m"
}

func (p Palette) Accent(text string) string  { return p.Tone(text, ToneAccent) }
func (p Palette) Info(text string) string    { return p.Tone(text, ToneInfo) }
func (p Palette) Success(text string) string { return p.Tone(text, ToneSuccess) }
func (p Palette) Warning(text string) string { return p.Tone(text, ToneWarning) }
func (p Palette) Error(text string) string   { return p.Tone(text, ToneError) }
func (p Palette) Label(text string) string   { return p.Tone(text, ToneLabel) }

func (p Palette) AccentBold(text string) string  { return p.ToneBold(text, ToneAccent) }
func (p Palette) InfoBold(text string) string    { return p.ToneBold(text, ToneInfo) }
func (p Palette) SuccessBold(text string) string { return p.ToneBold(text, ToneSuccess) }
func (p Palette) WarningBold(text string) string { return p.ToneBold(text, ToneWarning) }
func (p Palette) ErrorBold(text string) string   { return p.ToneBold(text, ToneError) }
func (p Palette) LabelBold(text string) string   { return p.ToneBold(text, ToneLabel) }

// WrapRatio 按比例着色，是柱状图与分数的主要入口。
func (p Palette) WrapRatio(text string, ratio float64) string {
	return p.Wrap(text, Color(ratio))
}

func (p Palette) escape(color RGB) string {
	switch p.Level {
	case LevelTrueColor:
		return "\x1b[38;2;" + strconv.Itoa(int(color.R)) + ";" +
			strconv.Itoa(int(color.G)) + ";" + strconv.Itoa(int(color.B)) + "m"
	case LevelANSI256:
		return "\x1b[38;5;" + strconv.Itoa(ansi256(color)) + "m"
	case LevelBasic:
		code, bright := basicANSI(color)
		if bright {
			return "\x1b[1;" + strconv.Itoa(code) + "m"
		}
		return "\x1b[" + strconv.Itoa(code) + "m"
	default:
		return ""
	}
}

func (p Palette) escapeBold(color RGB) string {
	switch p.Level {
	case LevelTrueColor:
		return "\x1b[1;38;2;" + strconv.Itoa(int(color.R)) + ";" +
			strconv.Itoa(int(color.G)) + ";" + strconv.Itoa(int(color.B)) + "m"
	case LevelANSI256:
		return "\x1b[1;38;5;" + strconv.Itoa(ansi256(color)) + "m"
	case LevelBasic:
		code, _ := basicANSI(color)
		return "\x1b[1;" + strconv.Itoa(code) + "m"
	default:
		return ""
	}
}

// Dim 把文本渲染为弱化色，用于柱状图未填充的部分与次要说明。
func (p Palette) Dim(text string) string {
	if p.Level == LevelNone || text == "" {
		return text
	}
	switch p.Level {
	case LevelTrueColor:
		return "\x1b[38;2;90;90;90m" + text + "\x1b[0m"
	case LevelANSI256:
		return "\x1b[38;5;240m" + text + "\x1b[0m"
	default:
		return "\x1b[2m" + text + "\x1b[0m"
	}
}

// Bold 加粗，用于标题。单色终端上加粗通常仍然可见，因此不受色阶影响。
func (p Palette) Bold(text string) string {
	if p.Level == LevelNone || text == "" {
		return text
	}
	return "\x1b[1m" + text + "\x1b[0m"
}

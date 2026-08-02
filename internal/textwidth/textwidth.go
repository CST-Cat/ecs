package textwidth

// 显示宽度。
//
// 报告里中英混排是常态，而对齐必须按终端实际占用的列数算，不是按字符数：
// "系统与资源" 是 5 个 rune 但占 10 列，用 len() 或 RuneCountInString 对齐
// 会让整张表歪掉。
//
// 这里不引第三方宽字符库：需要的判定只有"东亚宽字符占两列"，标准库的
// unicode 表已经够用，而多一个依赖就多一份供应链面。

import (
	"strings"
	"unicode"
)

// Width 返回字符串在等宽终端里占用的列数。
func Width(value string) int {
	width := 0
	for _, character := range value {
		width += RuneWidth(character)
	}
	return width
}

// RuneWidth 返回单个字符占用的列数。
func RuneWidth(character rune) int {
	switch {
	// 零宽与组合字符不占位：把它们算成一列会让带声调的文本对齐提前。
	case character == '\u200b' || character == '\ufeff':
		return 0
	case unicode.Is(unicode.Mn, character):
		return 0
	case unicode.Is(unicode.Han, character),
		unicode.Is(unicode.Hiragana, character),
		unicode.Is(unicode.Katakana, character),
		unicode.Is(unicode.Hangul, character):
		return 2
	// 全角标点、全角字母数字与全角货币符号。
	case character >= 0xFF01 && character <= 0xFF60:
		return 2
	case character >= 0xFFE0 && character <= 0xFFE6:
		return 2
	// CJK 符号与标点、括号、方块元素中的全角部分。
	case character >= 0x3000 && character <= 0x303F:
		return 2
	case character >= 0x3200 && character <= 0x33FF:
		return 2
	case character >= 0xFE30 && character <= 0xFE4F:
		return 2
	default:
		return 1
	}
}

// Pad 在右侧补空格到指定显示宽度。已经超宽时原样返回，不截断——
// 截断会悄悄丢信息，让列歪掉反而看得见。
func Pad(value string, width int) string {
	gap := width - Width(value)
	if gap <= 0 {
		return value
	}
	return value + strings.Repeat(" ", gap)
}

// PadLeft 在左侧补空格，用于数值右对齐。
func PadLeft(value string, width int) string {
	gap := width - Width(value)
	if gap <= 0 {
		return value
	}
	return strings.Repeat(" ", gap) + value
}

// Center 居中，用于标题块。
func Center(value string, width int) string {
	gap := width - Width(value)
	if gap <= 0 {
		return value
	}
	left := gap / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", gap-left)
}

// Truncate 按显示宽度截断，超出时以省略号结尾。
//
// 只在明确需要限宽的场合使用（如表格单元格），并且用省略号让截断可见。
func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(value) <= width {
		return value
	}
	var builder strings.Builder
	used := 0
	for _, character := range value {
		size := RuneWidth(character)
		if used+size > width-1 {
			break
		}
		builder.WriteRune(character)
		used += size
	}
	return builder.String() + "…"
}

// VisibleWidth 返回忽略 ANSI 转义序列后的显示宽度。
//
// 着色后的字符串仍然要参与对齐计算，而转义序列本身不占列。
func VisibleWidth(value string) int {
	width := 0
	inEscape := false
	for _, character := range value {
		if inEscape {
			// CSI 序列以字母结尾。
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
				inEscape = false
			}
			continue
		}
		if character == '\x1b' {
			inEscape = true
			continue
		}
		width += RuneWidth(character)
	}
	return width
}

// PadVisible 按忽略转义序列后的宽度右补空格。
func PadVisible(value string, width int) string {
	gap := width - VisibleWidth(value)
	if gap <= 0 {
		return value
	}
	return value + strings.Repeat(" ", gap)
}

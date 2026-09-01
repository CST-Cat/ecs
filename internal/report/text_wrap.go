package report

import (
	"fmt"
	"strings"

	"ecs/internal/textwidth"
)

// chineseNumeral 把章节号转成中文数字。
func chineseNumeral(value int) string {
	digits := []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	switch {
	case value <= 0:
		return "零"
	case value < 10:
		return digits[value]
	case value < 20:
		if value == 10 {
			return "十"
		}
		return "十" + digits[value-10]
	case value < 100:
		tens := value / 10
		rest := value % 10
		out := digits[tens] + "十"
		if rest > 0 {
			out += digits[rest]
		}
		return out
	default:
		return fmt.Sprintf("%d", value)
	}
}

// wrapText 按显示宽度折行，尽量在空格处断开。
func wrapText(text string, width int) []string {
	if width <= 0 || textwidth.Width(text) <= width {
		return []string{text}
	}
	var lines []string
	var current strings.Builder
	used := 0
	lastSpace := -1
	flush := func() {
		if current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
			used, lastSpace = 0, -1
		}
	}
	for _, character := range text {
		size := textwidth.RuneWidth(character)
		if used+size > width {
			// 有空格可断就从空格断，否则硬断——中文没有词间空格，硬断是正常的。
			if lastSpace > 0 {
				text := current.String()
				lines = append(lines, strings.TrimRight(text[:lastSpace], " "))
				rest := strings.TrimLeft(text[lastSpace:], " ")
				current.Reset()
				current.WriteString(rest)
				used = textwidth.Width(rest)
				lastSpace = -1
			} else {
				flush()
			}
		}
		if character == ' ' {
			lastSpace = current.Len()
		}
		current.WriteRune(character)
		used += size
	}
	flush()
	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

func formatFloat(value float64) string {
	switch {
	case value >= 1000:
		return fmt.Sprintf("%.0f", value)
	case value >= 10:
		return fmt.Sprintf("%.1f", value)
	default:
		return fmt.Sprintf("%.2f", value)
	}
}

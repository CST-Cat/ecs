package i18n

import (
	"regexp"
	"strings"
)

// 探针文本的翻译。
//
// 与结构性文本不同，探针产出的是成句的技术说明，数量大（实际运行会产生三百余条
// 唯一文本）且大量带有运行时数值。给每一句编一个 key 再改所有调用点，改动面会
// 铺满整个 probe 包；因此这里换一种做法：**在渲染层按原文查表**，探针代码一行不动。
//
// 代价是原文改字会静默丢译文。为此有测试遍历真实运行产出的文本，检查是否都能
// 命中译文表——改了句子而忘了改译文，测试会指出来。
//
// 带数值的句子用占位模板匹配：先把数字序列抠成占位符去查表，命中后把原来的数字
// 按顺序填回译文。这样 "16 线程事件率" 与 "8 线程事件率" 共用一条译文。

// numberPattern 匹配句子里的数值（含小数与千分位），用于模板化。
var numberPattern = regexp.MustCompile(`\d+(?:[.,]\d+)*`)

// numberPlaceholder 是模板里代替数值的记号。
//
// 选用 ASCII 单元分隔符：它不会出现在任何正常文案里，因此不必担心把用户可见的
// 字符误当成占位符。
const numberPlaceholder = "\x1f"

// templateOf 把句子里的数值换成占位符，并按出现顺序返回被抠掉的数值。
func templateOf(text string) (string, []string) {
	numbers := numberPattern.FindAllString(text, -1)
	if len(numbers) == 0 {
		return text, nil
	}
	return numberPattern.ReplaceAllString(text, numberPlaceholder), numbers
}

// fillTemplate 把数值按顺序填回译文。
//
// 译文里的占位符数量与原文不一致时返回 false：那说明译文写错了，
// 与其输出一句缺数字或多出占位符的话，不如退回原文。
func fillTemplate(template string, numbers []string) (string, bool) {
	if strings.Count(template, numberPlaceholder) != len(numbers) {
		return "", false
	}
	var builder strings.Builder
	index := 0
	for _, part := range strings.Split(template, numberPlaceholder) {
		builder.WriteString(part)
		if index < len(numbers) {
			builder.WriteString(numbers[index])
			index++
		}
	}
	return builder.String(), true
}

// Text 翻译一句探针文案。
//
// 查找顺序：整句精确匹配 → 数值占位模板匹配 → 原样返回。
// 任何一步都不会返回空串：缺译文只应表现为那一句没翻译。
func Text(text string) string {
	if text == "" || Current() == LangZH {
		return text
	}
	if translated, ok := probeEnglish[text]; ok && translated != "" {
		return translated
	}
	template, numbers := templateOf(text)
	if len(numbers) == 0 {
		return text
	}
	translated, ok := probeEnglish[template]
	if !ok || translated == "" {
		return text
	}
	if filled, ok := fillTemplate(translated, numbers); ok {
		return filled
	}
	return text
}

// TextSlice 翻译一组文案。
func TextSlice(items []string) []string {
	if len(items) == 0 || Current() == LangZH {
		return items
	}
	out := make([]string, len(items))
	for index, item := range items {
		out[index] = Text(item)
	}
	return out
}

// HasProbeText 报告某句文案是否有译文，供测试核对覆盖率。
func HasProbeText(text string) bool {
	if text == "" {
		return true
	}
	if _, ok := probeEnglish[text]; ok {
		return true
	}
	template, numbers := templateOf(text)
	if len(numbers) == 0 {
		return false
	}
	_, ok := probeEnglish[template]
	return ok
}

// ProbeTextKeys 返回全部已登记的探针文案，供测试统计。
func ProbeTextKeys() []string {
	keys := make([]string, 0, len(probeEnglish))
	for key := range probeEnglish {
		keys = append(keys, key)
	}
	return keys
}

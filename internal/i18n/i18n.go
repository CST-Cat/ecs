package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// 轻量国际化。
//
// 静态界面文本只走稳定 key → 当前语言文案这一条路径。中文和英文是并列的
// presentation catalog：选择英文时只查英文表，不会先生成中文，也不会在缺译文时
// 偷偷回退中文。缺 key 直接返回 key 本身，让遗漏在测试和界面上都显式可见。
//
// probe 历史上仍存在一套 source-text 翻译兼容层；它不属于静态 key lookup，后续会在
// 动态 Message 迁移完成后删除。这里刻意不让那条兼容路径参与 T/TL。

// Lang 是支持的界面语言。
type Lang string

const (
	// LangZH 是默认语言。
	LangZH Lang = "zh"
	// LangEN 是英文。
	LangEN Lang = "en"
)

var (
	mutex   sync.RWMutex
	current = LangZH
)

// Supported 列出支持的语言。
func Supported() []Lang { return []Lang{LangZH, LangEN} }

// Parse 解析语言标识，无法识别时返回 false。
func Parse(value string) (Lang, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "zh", "zh-cn", "zh_cn", "cn", "chinese":
		return LangZH, true
	case "en", "en-us", "en_us", "english":
		return LangEN, true
	default:
		return LangZH, false
	}
}

// DetectFromEnv 依据 LANG/LC_ALL 猜测语言。
//
// 只在用户没有显式指定时使用：容器里常常 LANG=C，此时保持中文默认，
// 不去猜一个用户没要求的语言。
func DetectFromEnv() Lang {
	for _, key := range []string{"ECS_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "zh") {
			return LangZH
		}
		if strings.HasPrefix(lower, "en") {
			return LangEN
		}
	}
	return LangZH
}

// Set 切换当前语言。
func Set(lang Lang) {
	mutex.Lock()
	defer mutex.Unlock()
	current = lang
}

// Current 返回当前语言。
func Current() Lang {
	mutex.RLock()
	defer mutex.RUnlock()
	return current
}

// T 按当前语言取译文。缺失时返回稳定 key，不跨语言 fallback。
func T(key string) string {
	mutex.RLock()
	lang := current
	mutex.RUnlock()
	return translate(lang, key)
}

// TL 按指定语言取译文，用于同时输出多语言的场景。缺失时返回稳定 key。
func TL(lang Lang, key string) string { return translate(lang, key) }

// Errorf 构造一个按当前语言渲染的错误。
//
// 校验错误在命令行上立即打印，因此在 CLI presentation 边界直接用稳定 key 选取
// 当前语言格式串并填充参数。动态报告消息不会复用这条错误路径。
func Errorf(key string, args ...any) error {
	return fmt.Errorf(T(key), args...)
}

// ErrorKeyPrefix 是校验错误 key 的统一前缀，测试据此核对中英覆盖。
const ErrorKeyPrefix = "err."

// JoinList 按当前语言的习惯连接一组枚举值。
func JoinList(items []string) string {
	separator := "、"
	if Current() == LangEN {
		separator = ", "
	}
	return strings.Join(items, separator)
}

func catalogsFor(lang Lang) []map[string]string {
	switch lang {
	case LangEN:
		return []map[string]string{modelMessageEnglish, probeMessageEnglish, probeMemoryInventoryEnglish, compareFlagEnglish, errorEnglish, scoreEnglish, cliEnglish, english}
	default:
		return []map[string]string{modelMessageChinese, probeMessageChinese, probeMemoryInventoryChinese, compareFlagChinese, errorChinese, scoreChinese, cliChinese, chinese}
	}
}

func lookup(lang Lang, key string) (string, bool) {
	for _, catalog := range catalogsFor(lang) {
		if value, ok := catalog[key]; ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func translate(lang Lang, key string) string {
	if value, ok := lookup(lang, key); ok {
		return value
	}
	return key
}

// Has 报告某个 key 是否在指定语言中有非空译文。它只检查该语言，不跨语言 fallback。
func Has(lang Lang, key string) bool {
	_, ok := lookup(lang, key)
	return ok
}

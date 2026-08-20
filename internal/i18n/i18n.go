package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// 轻量国际化。
//
// 不引入第三方依赖：一个 key → 各语言译文的映射加一个当前语言变量就够了。
// 之所以用 key 而不是"中文原文当 key"，是因为原文一旦改字就会静默丢译文，
// 而 key 改名编译期就能发现。
//
// 覆盖范围是分层的：命令行、模块名称、状态、表头、报告框架这些**结构性文本**
// 必须全部有译文——它们决定英文用户能不能看懂这份报告的骨架。探针内部那些
// 长篇技术说明（notes）尚未全部翻译，未登记的 key 会原样回退到中文，
// 不会变成空白或 key 名。这是有意的取舍：宁可中英混排，也不能丢信息。

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

// T 按当前语言取译文。
//
// 找不到译文时原样返回中文，绝不返回空串或 key 名——缺译文只该表现为
// 那一句没翻译，不该表现为信息消失。
func T(key string) string {
	mutex.RLock()
	lang := current
	mutex.RUnlock()
	return translate(lang, key)
}

// TL 按指定语言取译文，用于同时输出多语言的场景。
func TL(lang Lang, key string) string { return translate(lang, key) }

// Errorf 构造一个按当前语言渲染的错误。
//
// 校验错误在命令行上立即打印，没有报告渲染层可以挂翻译（探针文案走的是
// probe_text.go 的原文查表）。因此这里让错误在构造时就取译文：key 决定语义，
// 译文本身是格式串，参数按位置填入。
//
// 用独立函数而不是 fmt.Errorf(T(key), ...) 直接写在调用点，是为了避免 vet 的
// printf 检查对非常量格式串报警，同时给测试一个统一的入口去核对 key 覆盖。
func Errorf(key string, args ...any) error {
	return fmt.Errorf(T(key), args...)
}

// ErrorKeyPrefix 是校验错误 key 的统一前缀，测试据此核对中英覆盖。
const ErrorKeyPrefix = "err."

// JoinList 按当前语言的习惯连接一组枚举值。
//
// 中文用顿号，英文用逗号加空格。硬编码任一种都会让另一种语言的句子读起来别扭：
// `choose local、public、any` 和 `可选 local, public, any` 都不对。
func JoinList(items []string) string {
	separator := "、"
	if Current() == LangEN {
		separator = ", "
	}
	return strings.Join(items, separator)
}

func translate(lang Lang, key string) string {
	if lang == LangEN {
		if value, ok := compareFlagEnglish[key]; ok && value != "" {
			return value
		}
		if value, ok := errorEnglish[key]; ok && value != "" {
			return value
		}
		if value, ok := scoreEnglish[key]; ok && value != "" {
			return value
		}
		if value, ok := cliEnglish[key]; ok && value != "" {
			return value
		}
	}
	if value, ok := compareFlagChinese[key]; ok && value != "" {
		return value
	}
	if value, ok := errorChinese[key]; ok && value != "" {
		return value
	}
	if value, ok := scoreChinese[key]; ok && value != "" {
		return value
	}
	if value, ok := cliChinese[key]; ok && value != "" {
		return value
	}
	if lang == LangEN {
		if value, ok := english[key]; ok && value != "" {
			return value
		}
	}
	if value, ok := chinese[key]; ok && value != "" {
		return value
	}
	// key 未登记：返回 key 本身，让缺失在界面上可见而不是静默变空。
	return key
}

// Has 报告某个 key 是否有当前语言的译文，供测试核对覆盖率。
func Has(lang Lang, key string) bool {
	if lang == LangEN {
		if value, ok := compareFlagEnglish[key]; ok && value != "" {
			return true
		}
		if value, ok := errorEnglish[key]; ok && value != "" {
			return true
		}
		if value, ok := scoreEnglish[key]; ok && value != "" {
			return true
		}
		if value, ok := cliEnglish[key]; ok && value != "" {
			return true
		}
	} else {
		if value, ok := compareFlagChinese[key]; ok && value != "" {
			return true
		}
		if value, ok := errorChinese[key]; ok && value != "" {
			return true
		}
		if value, ok := scoreChinese[key]; ok && value != "" {
			return true
		}
		if value, ok := cliChinese[key]; ok && value != "" {
			return true
		}
	}
	if lang == LangEN {
		value, ok := english[key]
		return ok && value != ""
	}
	value, ok := chinese[key]
	return ok && value != ""
}

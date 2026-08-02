package i18n

import (
	"strings"
	"testing"
)

// 两张表必须一一对应：缺 key 会让界面上冒出 key 名，多 key 说明有译文永远用不到。
func TestTranslationTablesAreInSync(t *testing.T) {
	for key, value := range chinese {
		if value == "" {
			t.Errorf("中文 %q 为空", key)
		}
		if translated, ok := english[key]; !ok || translated == "" {
			t.Errorf("缺少英文译文：%q", key)
		}
	}
	for key := range english {
		if _, ok := chinese[key]; !ok {
			t.Errorf("英文多出无用 key：%q", key)
		}
	}
}

// 带格式动词的译文，两种语言的动词数量必须一致，否则 Sprintf 会输出 %!d(MISSING)。
func TestFormatVerbsMatchAcrossLanguages(t *testing.T) {
	for key, zh := range chinese {
		en := english[key]
		if zhCount, enCount := strings.Count(zh, "%"), strings.Count(en, "%"); zhCount != enCount {
			t.Errorf("%q 的格式动词数量不一致：中文 %d 个，英文 %d 个", key, zhCount, enCount)
		}
	}
}

func TestParseAndFallback(t *testing.T) {
	for _, value := range []string{"en", "EN", "en-US", "english"} {
		if lang, ok := Parse(value); !ok || lang != LangEN {
			t.Errorf("Parse(%q) = %v, %v", value, lang, ok)
		}
	}
	for _, value := range []string{"", "zh", "zh-CN", "chinese"} {
		if lang, ok := Parse(value); !ok || lang != LangZH {
			t.Errorf("Parse(%q) = %v, %v", value, lang, ok)
		}
	}
	// 无法识别时回落中文并明确报告失败，不静默切成英文。
	if lang, ok := Parse("klingon"); ok || lang != LangZH {
		t.Fatalf("未知语言 = %v, %v", lang, ok)
	}
}

// 未登记的 key 必须原样可见，绝不能变成空串——空串会让信息凭空消失。
func TestMissingKeyStaysVisible(t *testing.T) {
	Set(LangEN)
	defer Set(LangZH)
	if got := T("some.key.that.does.not.exist"); got != "some.key.that.does.not.exist" {
		t.Fatalf("未登记 key = %q，应原样返回", got)
	}
}

// 英文缺译文时回退中文，而不是显示 key 名——宁可中英混排也不能丢信息。
func TestEnglishFallsBackToChinese(t *testing.T) {
	const key = "test.only.chinese"
	chinese[key] = "只有中文"
	defer delete(chinese, key)
	if got := TL(LangEN, key); got != "只有中文" {
		t.Fatalf("英文缺译文时 = %q，应回退中文", got)
	}
}

func TestModuleTitlesCoverAllModules(t *testing.T) {
	// 每个模块都必须有标题译文，否则英文界面会露出中文模块名。
	for _, id := range []string{
		"system", "network", "bgp", "cpu", "memory", "disk", "dns", "latency", "speed",
		"ports", "nat", "blacklist", "apps", "cnspeed", "ookla", "media", "route", "backtrace",
	} {
		key := "module." + id + ".title"
		if !Has(LangZH, key) || !Has(LangEN, key) {
			t.Errorf("模块 %q 缺少标题译文", id)
		}
	}
}

func TestMethodologyKindsAreTranslated(t *testing.T) {
	for _, kind := range []string{
		"standard-benchmark", "protocol-measurement", "provider-assessment", "heuristic", "inventory",
	} {
		if !Has(LangEN, "methodology."+kind) {
			t.Errorf("方法学 %q 缺少英文译文", kind)
		}
	}
}

// 校验错误表与其余译文表同样必须一一对应。
func TestErrorTablesAreInSync(t *testing.T) {
	for key, value := range errorChinese {
		if value == "" {
			t.Errorf("中文错误 %q 为空", key)
		}
		if !strings.HasPrefix(key, ErrorKeyPrefix) {
			t.Errorf("错误 key %q 应以 %q 开头", key, ErrorKeyPrefix)
		}
		if translated, ok := errorEnglish[key]; !ok || translated == "" {
			t.Errorf("缺少英文错误译文：%q", key)
		}
	}
	for key := range errorEnglish {
		if _, ok := errorChinese[key]; !ok {
			t.Errorf("英文错误多出无用 key：%q", key)
		}
	}
}

// 错误译文是 Errorf 的格式串，动词数量与种类不一致会输出 %!q(MISSING) 之类的残句。
func TestErrorFormatVerbsMatchAcrossLanguages(t *testing.T) {
	for key, zh := range errorChinese {
		en := errorEnglish[key]
		if zhCount, enCount := strings.Count(zh, "%"), strings.Count(en, "%"); zhCount != enCount {
			t.Errorf("%q 的格式动词数量不一致：中文 %d 个，英文 %d 个", key, zhCount, enCount)
		}
		// %w 决定错误能否被 errors.Is/As 解包，两种语言必须同时有或同时无。
		if strings.Count(zh, "%w") != strings.Count(en, "%w") {
			t.Errorf("%q 的 %%w 包装动词在两种语言间不一致", key)
		}
	}
}

// 错误 key 不能与其他表撞名：translate 会先命中错误表，撞名会让原本的译文失效。
func TestErrorKeysDoNotCollide(t *testing.T) {
	for key := range errorChinese {
		if _, ok := chinese[key]; ok {
			t.Errorf("错误 key %q 与结构性文案表撞名", key)
		}
		if _, ok := cliChinese[key]; ok {
			t.Errorf("错误 key %q 与命令行文案表撞名", key)
		}
	}
}

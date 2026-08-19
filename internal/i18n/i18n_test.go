package i18n

import (
	"strings"
	"testing"
)

func TestTranslationTablesStaySynchronizedAndFormatSafe(t *testing.T) {
	tables := []struct {
		name string
		zh   map[string]string
		en   map[string]string
	}{
		{name: "core", zh: chinese, en: english},
		{name: "errors", zh: errorChinese, en: errorEnglish},
		{name: "cli", zh: cliChinese, en: cliEnglish},
		{name: "score", zh: scoreChinese, en: scoreEnglish},
	}
	for _, table := range tables {
		for key, zh := range table.zh {
			if strings.TrimSpace(zh) == "" {
				t.Errorf("%s translation %q is empty", table.name, key)
			}
			en, ok := table.en[key]
			if !ok || strings.TrimSpace(en) == "" {
				t.Errorf("%s translation missing English key %q", table.name, key)
				continue
			}
			if strings.Count(zh, "%") != strings.Count(en, "%") {
				t.Errorf("%s format verbs differ for %q", table.name, key)
			}
		}
		for key := range table.en {
			if _, ok := table.zh[key]; !ok {
				t.Errorf("%s has an English-only key %q", table.name, key)
			}
		}
	}
}

func TestParseAndSetLanguage(t *testing.T) {
	original := Current()
	defer Set(original)
	if lang, ok := Parse("en-US"); !ok || lang != LangEN {
		t.Fatalf("Parse(en-US) = %v, %v", lang, ok)
	}
	if lang, ok := Parse("klingon"); ok || lang != LangZH {
		t.Fatalf("unknown language = %v, %v", lang, ok)
	}
	Set(LangEN)
	if Current() != LangEN {
		t.Fatalf("Current() = %v, want en", Current())
	}
}

func TestMissingTranslationFallsBackWithoutLosingText(t *testing.T) {
	original := Current()
	defer Set(original)
	const key = "test.only.chinese"
	chinese[key] = "只有中文"
	defer delete(chinese, key)
	if got := TL(LangEN, key); got != "只有中文" {
		t.Fatalf("English fallback = %q", got)
	}
	if got := TL(LangEN, "missing.test.key"); got != "missing.test.key" {
		t.Fatalf("missing key fallback = %q", got)
	}
}

func TestStableProbeMessageTranslates(t *testing.T) {
	const key = "probe.memory.stream_missing"
	if got := TL(LangZH, key); got == key || got == "" {
		t.Fatalf("Chinese probe message = %q", got)
	}
	if got := TL(LangEN, key); got == key || got == "" || got == TL(LangZH, key) {
		t.Fatalf("English probe message = %q", got)
	}
}

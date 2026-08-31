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
		{name: "compare flags", zh: compareFlagChinese, en: compareFlagEnglish},
		{name: "model messages", zh: modelMessageChinese, en: modelMessageEnglish},
		{name: "cpu", zh: probeCPUChinese, en: probeCPUEnglish},
		{name: "memory", zh: probeMemoryChinese, en: probeMemoryEnglish},
		{name: "pressure", zh: probePressureChinese, en: probePressureEnglish},
		{name: "probe retry", zh: probeRetryChinese, en: probeRetryEnglish},
		{name: "report retry", zh: reportRetryChinese, en: reportRetryEnglish},
		{name: "memory inventory", zh: probeMemoryInventoryChinese, en: probeMemoryInventoryEnglish},
		{name: "ports", zh: probePortsChinese, en: probePortsEnglish},
		{name: "rdns", zh: probeRDNSChinese, en: probeRDNSEnglish},
		{name: "kernel", zh: probeKernelChinese, en: probeKernelEnglish},
		{name: "system", zh: probeSystemChinese, en: probeSystemEnglish},
		{name: "npb", zh: probeNPBChinese, en: probeNPBEnglish},
		{name: "zstd", zh: probeZstdChinese, en: probeZstdEnglish},
		{name: "crypto", zh: probeCryptoChinese, en: probeCryptoEnglish},
		{name: "disk", zh: probeDiskChinese, en: probeDiskEnglish},
		{name: "dns", zh: probeDNSChinese, en: probeDNSEnglish},
		{name: "latency", zh: probeLatencyChinese, en: probeLatencyEnglish},
		{name: "nat", zh: probeNATChinese, en: probeNATEnglish},
		{name: "apps", zh: probeAppsChinese, en: probeAppsEnglish},
		{name: "blacklist", zh: probeBlacklistChinese, en: probeBlacklistEnglish},
		{name: "bgp", zh: probeBGPChinese, en: probeBGPEnglish},
		{name: "speed", zh: probeSpeedChinese, en: probeSpeedEnglish},
		{name: "cnspeed", zh: probeCNSpeedChinese, en: probeCNSpeedEnglish},
		{name: "ookla", zh: probeOoklaChinese, en: probeOoklaEnglish},
		{name: "network", zh: probeNetworkChinese, en: probeNetworkEnglish},
		{name: "media", zh: probeMediaChinese, en: probeMediaEnglish},
		{name: "route", zh: probeRouteChinese, en: probeRouteEnglish},
		{name: "backtrace", zh: probeBacktraceChinese, en: probeBacktraceEnglish},
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
			if formatVerbCount(zh) != formatVerbCount(en) {
				t.Errorf("%s format verbs differ for %q: %q vs %q", table.name, key, zh, en)
			}
		}
		for key := range table.en {
			if _, ok := table.zh[key]; !ok {
				t.Errorf("%s has an English-only key %q", table.name, key)
			}
		}
	}
}

func TestPrivacyCopyDistinguishesReportUploadFromMeasurementTraffic(t *testing.T) {
	cases := []struct {
		lang          Lang
		reportToken   string
		trafficToken  string
		forbiddenText []string
	}{
		{
			lang:          LangZH,
			reportToken:   "报告",
			trafficToken:  "测量流量",
			forbiddenText: []string{"全程零上传", "零上传"},
		},
		{
			lang:          LangEN,
			reportToken:   "report",
			trafficToken:  "measurement traffic",
			forbiddenText: []string{"nothing was uploaded", "nothing is ever uploaded", "never uploads"},
		},
	}
	for _, test := range cases {
		t.Run(string(test.lang), func(t *testing.T) {
			texts := []string{
				TL(test.lang, "cli.tagline"),
				TL(test.lang, "report.local"),
				TL(test.lang, "term.subtitle"),
				TL(test.lang, "term.noUpload"),
				TL(test.lang, "wizard.subtitle"),
				TL(test.lang, "module.ookla.desc"),
			}
			for _, text := range texts {
				for _, forbidden := range test.forbiddenText {
					if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
						t.Fatalf("privacy copy %q contains forbidden wording %q", text, forbidden)
					}
				}
			}
			if !strings.Contains(TL(test.lang, "report.local"), test.reportToken) {
				t.Fatalf("report.local does not identify the report file: %q", TL(test.lang, "report.local"))
			}
			for _, key := range []string{"term.noUpload", "wizard.subtitle", "module.ookla.desc"} {
				if !strings.Contains(TL(test.lang, key), test.trafficToken) {
					t.Fatalf("%s does not identify measurement traffic: %q", key, TL(test.lang, key))
				}
			}
		})
	}
}

func formatVerbCount(format string) int {
	count := 0
	for index := 0; index < len(format); index++ {
		if format[index] != '%' || index+1 >= len(format) {
			continue
		}
		if format[index+1] == '%' {
			index++
			continue
		}
		count++
	}
	return count
}

func TestLanguageParsingAndEnvironmentSelection(t *testing.T) {
	for _, test := range []struct {
		raw  string
		lang Lang
		ok   bool
	}{
		{raw: "", lang: LangZH, ok: true},
		{raw: "zh-cn", lang: LangZH, ok: true},
		{raw: "en_US", lang: LangEN, ok: true},
		{raw: "english", lang: LangEN, ok: true},
		{raw: "klingon", lang: LangZH},
	} {
		lang, ok := Parse(test.raw)
		if lang != test.lang || ok != test.ok {
			t.Errorf("Parse(%q) = %v, %v", test.raw, lang, ok)
		}
	}

	for _, key := range []string{"ECS_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	t.Setenv("ECS_LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	if got := DetectFromEnv(); got != LangEN {
		t.Fatalf("ECS_LANG should take priority: %v", got)
	}
	t.Setenv("ECS_LANG", "")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := DetectFromEnv(); got != LangEN {
		t.Fatalf("LC_ALL should take priority over LC_MESSAGES and LANG: %v", got)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "zh_CN.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := DetectFromEnv(); got != LangZH {
		t.Fatalf("LC_MESSAGES should take priority over LANG: %v", got)
	}
	t.Setenv("LC_MESSAGES", "")
	if got := DetectFromEnv(); got != LangEN {
		t.Fatalf("LANG locale should be detected: %v", got)
	}
	for _, key := range []string{"ECS_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "C")
	}
	if got := DetectFromEnv(); got != LangZH {
		t.Fatalf("unknown locale should retain Chinese default: %v", got)
	}
}

func TestTranslationHelpersExposeMissingKeysWithoutCrossLanguageFallback(t *testing.T) {
	original := Current()
	t.Cleanup(func() { Set(original) })
	Set(LangEN)
	if Current() != LangEN || T("cli.usage") == "cli.usage" || TL(LangZH, "cli.usage") == "cli.usage" {
		t.Fatal("language selection did not resolve known translations")
	}
	if got := Errorf("err.unknownProfile", "demo").Error(); !strings.Contains(got, "unknown profile") || !strings.Contains(got, "demo") {
		t.Fatalf("English Errorf = %q", got)
	}
	if JoinList([]string{"a", "b"}) != "a, b" {
		t.Fatalf("English JoinList = %q", JoinList([]string{"a", "b"}))
	}
	Set(LangZH)
	if JoinList([]string{"a", "b"}) != "a、b" {
		t.Fatalf("Chinese JoinList = %q", JoinList([]string{"a", "b"}))
	}
	const key = "test.only.chinese"
	chinese[key] = "只有中文"
	t.Cleanup(func() { delete(chinese, key) })
	if got := TL(LangEN, key); got != key {
		t.Fatalf("missing English translation must expose key, got %q", got)
	}
	if Has(LangEN, key) {
		t.Fatal("English catalog must not claim a Chinese-only key")
	}
	if got := TL(LangZH, key); got != "只有中文" || !Has(LangZH, key) {
		t.Fatalf("Chinese catalog lookup = %q, Has=%v", got, Has(LangZH, key))
	}
	if got := TL(LangEN, "missing.test.key"); got != "missing.test.key" {
		t.Fatalf("missing key = %q", got)
	}
}

func TestProbeKeysUseOneWayLookup(t *testing.T) {
	original := Current()
	t.Cleanup(func() { Set(original) })
	Set(LangEN)
	cases := []struct {
		name, input, want string
	}{
		{name: "stable key", input: "probe.memory.stream_missing", want: "The official STREAM executable was not found; the memory benchmark did not run."},
		{name: "legacy text", input: "未找到官方 STREAM 可执行文件", want: "未找到官方 STREAM 可执行文件"},
		{name: "unknown text", input: "未登记的中文说明", want: "未登记的中文说明"},
		{name: "external output", input: "external output 123", want: "external output 123"},
	}
	for _, test := range cases {
		if got := T(test.input); got != test.want {
			t.Errorf("T(%s) = %q, want %q", test.name, got, test.want)
		}
	}
	if Has(LangEN, "未找到官方 STREAM 可执行文件") {
		t.Fatal("source text must not be registered as a translation key")
	}
	Set(LangZH)
	if got := T("probe.memory.stream_missing"); got != "未找到官方 STREAM 可执行文件；内存基准未运行。" {
		t.Fatalf("Chinese stable key translation = %q", got)
	}
	if got := T("未找到官方 STREAM 可执行文件"); got != "未找到官方 STREAM 可执行文件" {
		t.Fatalf("Chinese source text changed: %q", got)
	}
}

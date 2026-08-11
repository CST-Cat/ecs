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

func TestFullProfileTranslationNamesFullOnlyModules(t *testing.T) {
	for _, testCase := range []struct {
		lang      Lang
		required  string
		forbidden string
	}{
		{LangZH, "额外包含多源 IP 质量与 Ookla", "额外包含 cnspeed"},
		{LangEN, "adds multi-source IP quality and Ookla", "adds cnspeed"},
	} {
		text := TL(testCase.lang, "profile.full")
		if !strings.Contains(text, testCase.required) {
			t.Errorf("profile.full %s text %q lacks %q", testCase.lang, text, testCase.required)
		}
		if strings.Contains(text, testCase.forbidden) {
			t.Errorf("profile.full %s text retains stale wording %q: %q", testCase.lang, testCase.forbidden, text)
		}
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
	// 模块标题表的中英文 key 必须一一对应；descriptor 与两种语言的
	// 完整覆盖由外部 module_descriptor_test.go 统一检查。
	for key := range chinese {
		if !strings.HasPrefix(key, "module.") || !strings.HasSuffix(key, ".title") {
			continue
		}
		if !Has(LangZH, key) || !Has(LangEN, key) {
			t.Errorf("模块文案 %q 缺少标题译文", key)
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

func TestATTOExpansionNoteTranslation(t *testing.T) {
	const note = "为安全容纳 ATTO 64 MiB 作业，fio 文件从配置的 32.00 MiB 对齐/扩展为 128.00 MiB（至少两个 64 MiB 窗口）。"
	original := Current()
	defer Set(original)
	Set(LangEN)

	if !HasProbeText(note) {
		t.Fatalf("ATTO expansion note is missing an English template: %q", note)
	}
	want := "To safely fit ATTO 64 MiB jobs, the fio file was aligned/expanded from 32.00 MiB to 128.00 MiB (at least two 64 MiB windows)."
	if got := Text(note); got != want {
		t.Fatalf("ATTO expansion note translation = %q, want %q", got, want)
	}
}

func TestFIONewMissingValueNotesTranslate(t *testing.T) {
	original := Current()
	defer Set(original)
	Set(LangEN)
	cases := map[string]string{
		"fio 基础作业有 2 项未返回；缺失项不补零。":        "fio has 2 baseline job(s) not returned; missing items are not filled with zero.",
		"fio 混合矩阵有 3 项作业未返回完整统计；缺失项不补零。":  "fio has 3 mixed job(s) without complete statistics; missing items are not filled with zero.",
		"多盘 fio 的一个或多个挂载点只返回部分统计；缺失项不补零。": "One or more extra-disk fio mount points returned partial statistics; missing items are not filled with zero.",
	}
	for note, want := range cases {
		if !HasProbeText(note) {
			t.Errorf("missing probe translation template: %q", note)
			continue
		}
		if got := Text(note); got != want {
			t.Errorf("translation for %q = %q, want %q", note, got, want)
		}
	}
}

func TestStructuredDiagnosticLabelsTranslate(t *testing.T) {
	original := Current()
	defer Set(original)
	Set(LangEN)
	cases := map[string]string{
		"8 线程事件延迟 P95":                             "8-thread event latency P95",
		"STREAM Copy 多线程扩展倍率":                      "STREAM Copy multi-thread scaling ratio",
		"iperf3 TCP 分秒稳定性":                         "iperf3 TCP interval stability",
		"loopback IPv4 上传分秒 P50":                   "loopback IPv4 upload interval P50",
		"Cloudflare DNS 成功率":                       "Cloudflare DNS success rate",
		"Cloudflare DNS 抖动":                        "Cloudflare DNS jitter",
		"Aliyun IPv4 TCP 标准差":                      "Aliyun IPv4 TCP standard deviation",
		"Google 可见跳点":                              "Google visible hops",
		"cgroup 与 PSI 压力诊断":                        "cgroup and PSI pressure diagnostics",
		"cgroup CPU throttle 时间占比":                 "cgroup CPU throttled time ratio",
		"测试窗口资源干扰":                                 "Test-window resource interference",
		"自动复测判定":                                   "Automatic retry decision",
		"测试前 load 4.00 高于 2 CPU allowance 的 1.5 倍": "Pre-test load 4.00 was above the 2-CPU allowance's 1.5× threshold",
		"检测到测试干扰：测试窗口 CPU steal 2.50%。":            "Interference detected: CPU steal during the test window was 2.50%.",
	}
	for source, want := range cases {
		if !HasProbeText(source) {
			t.Errorf("missing diagnostic translation: %q", source)
			continue
		}
		if got := Text(source); got != want {
			t.Errorf("Text(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestLocalBenchmarkReportCopyTranslates(t *testing.T) {
	original := Current()
	defer Set(original)
	Set(LangEN)
	cases := map[string]string{
		"zstd 8 worker 压缩吞吐":                                 "zstd 8-worker compression throughput",
		"zstd 全 worker（8）原始输出":                               "zstd all-workers (8) raw output",
		"zstd 压缩 1T 100.00 MB/s · 8T 700.00 MB/s · 扩展 7.00×": "zstd compression 1T 100.00 MB/s · 8T 700.00 MB/s · 7.00x scaling",
		"NPB EP 浮点计算吞吐 8T":                                   "NPB EP floating-point throughput 8T",
		"NPB FT Class A 全线程（8T）原始输出":                         "NPB FT Class A all threads (8T) raw output",
		"未找到固定 NPB EP Class A binary，EP 未运行":                 "The pinned NPB EP Class A binary was not found; EP did not run",
		"AES-256-GCM 8 worker 吞吐":                            "AES-256-GCM 8-worker throughput",
		"完整参数（ChaCha20-Poly1305 全 worker（8））":                "Full arguments (ChaCha20-Poly1305, all 8 workers)",
		"OpenSSL speed SHA-256 全 worker（8）原始输出":              "OpenSSL speed SHA-256 all-workers (8) raw output",
	}
	for source, want := range cases {
		if !HasProbeText(source) {
			t.Errorf("missing local benchmark translation: %q", source)
			continue
		}
		if got := Text(source); got != want {
			t.Errorf("Text(%q) = %q, want %q", source, got, want)
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

// 命令行文案表同样必须一一对应。
//
// 这个检查是补上的：此前只有 chinese/english 两张表被核对，cliChinese/cliEnglish
// 没人管，结果英文侧整块缺了 19 个 key 而测试全绿——英文用户看到的是中文回退。
func TestCLITablesAreInSync(t *testing.T) {
	for key, value := range cliChinese {
		if value == "" {
			t.Errorf("中文命令行文案 %q 为空", key)
		}
		if translated, ok := cliEnglish[key]; !ok || translated == "" {
			t.Errorf("缺少英文命令行文案：%q", key)
		}
	}
	for key := range cliEnglish {
		if _, ok := cliChinese[key]; !ok {
			t.Errorf("英文命令行文案多出无用 key：%q", key)
		}
	}
}

func TestCLIFormatVerbsMatchAcrossLanguages(t *testing.T) {
	for key, zh := range cliChinese {
		en := cliEnglish[key]
		if zhCount, enCount := strings.Count(zh, "%"), strings.Count(en, "%"); zhCount != enCount {
			t.Errorf("%q 的格式动词数量不一致：中文 %d 个，英文 %d 个", key, zhCount, enCount)
		}
	}
}

// 评分文案表同理。
func TestScoreTablesAreInSync(t *testing.T) {
	for key, value := range scoreChinese {
		if value == "" {
			t.Errorf("中文评分文案 %q 为空", key)
		}
		if translated, ok := scoreEnglish[key]; !ok || translated == "" {
			t.Errorf("缺少英文评分文案：%q", key)
		}
	}
	for key := range scoreEnglish {
		if _, ok := scoreChinese[key]; !ok {
			t.Errorf("英文评分文案多出无用 key：%q", key)
		}
	}
	for key, zh := range scoreChinese {
		if zhCount, enCount := strings.Count(zh, "%"), strings.Count(scoreEnglish[key], "%"); zhCount != enCount {
			t.Errorf("%q 的格式动词数量不一致：中文 %d 个，英文 %d 个", key, zhCount, enCount)
		}
	}
}

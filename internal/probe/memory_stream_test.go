package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
)

var streamHanPattern = regexp.MustCompile(`\p{Han}`)

var streamEnglishSentencePattern = regexp.MustCompile(`(?i)\b(?:run failed|see raw output|uses|under the detected|remain separate runs|kernel / threads)\b`)

func streamFixture(unit string) string {
	return fmt.Sprintf(`-------------------------------------------------------------
STREAM version $Revision: 5.10 $
-------------------------------------------------------------
Number of Threads requested = 4
Function      Best Rate %s     Avg time     Min time     Max time
Copy:           1000.0         0.0100       0.0090       0.0110
Scale:          2000.0         0.0200       0.0190       0.0210
Add:            3000.0         0.0300       0.0290       0.0310
Triad:          4000.0         0.0400       0.0390       0.0410
Solution Validates
`, unit)
}

func TestParseStreamOutputCoversAllKernelsAndUnits(t *testing.T) {
	for _, testCase := range []struct {
		unit        string
		copyRateMiB float64
	}{
		{unit: "MB/s", copyRateMiB: 1000.0 * 1000 * 1000 / (1024 * 1024)},
		{unit: "MiB/s", copyRateMiB: 1000},
		{unit: "GB/s", copyRateMiB: 1000.0 * 1000 * 1000 * 1000 / (1024 * 1024)},
		{unit: "GiB/s", copyRateMiB: 1000 * 1024},
	} {
		t.Run(testCase.unit, func(t *testing.T) {
			parsed, err := parseStreamOutput(streamFixture(testCase.unit))
			if err != nil {
				t.Fatalf("parseStreamOutput(%s): %v", testCase.unit, err)
			}
			if parsed.Unit != testCase.unit {
				t.Fatalf("source unit = %q, want %q", parsed.Unit, testCase.unit)
			}
			if parsed.RequestedThreads != 4 {
				t.Fatalf("requested threads = %d, want 4", parsed.RequestedThreads)
			}
			if len(parsed.Samples) != 4 {
				t.Fatalf("kernel samples = %d, want 4: %+v", len(parsed.Samples), parsed.Samples)
			}
			for index, kernel := range streamKernels {
				sample, ok := parsed.Samples[kernel]
				if !ok {
					t.Fatalf("missing %s sample", kernel)
				}
				want := float64((index + 1) * 1000)
				if kernel == "Copy" && sample.RateMiBS != testCase.copyRateMiB {
					t.Fatalf("Copy rate = %f, want %f", sample.RateMiBS, testCase.copyRateMiB)
				}
				if kernel != "Copy" && sample.RawRate != want {
					t.Fatalf("%s raw rate = %f, want %f", kernel, sample.RawRate, want)
				}
				if sample.RateMiBS <= 0 || sample.AvgTime <= 0 || sample.MinTime <= 0 || sample.MaxTime <= 0 {
					t.Fatalf("invalid %s sample: %+v", kernel, sample)
				}
			}
		})
	}
}

func TestParseStreamOutputRejectsMissingLinesAndBadValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{
			name: "missing kernel",
			mutate: func(output string) string {
				return strings.Replace(output, "Add:            3000.0         0.0300       0.0290       0.0310\n", "", 1)
			},
			wantErr: "缺少 Add",
		},
		{
			name: "bad rate",
			mutate: func(output string) string {
				return strings.Replace(output, "Copy:           1000.0", "Copy:           not-a-rate", 1)
			},
			wantErr: "Copy",
		},
		{
			name: "nan timing",
			mutate: func(output string) string {
				return strings.Replace(output, "Scale:          2000.0         0.0200", "Scale:          2000.0         NaN", 1)
			},
			wantErr: "Scale",
		},
		{
			name: "negative rate",
			mutate: func(output string) string {
				return strings.Replace(output, "Triad:          4000.0", "Triad:          -1.0", 1)
			},
			wantErr: "Triad",
		},
		{
			name: "missing header",
			mutate: func(output string) string {
				return strings.Replace(output, "Function      Best Rate MB/s     Avg time     Min time     Max time\n", "", 1)
			},
			wantErr: "表头",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := parseStreamOutput(testCase.mutate(streamFixture("MB/s")))
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("parse error = %v, want substring %q", err, testCase.wantErr)
			}
		})
	}
}

func TestStreamEnvironmentControlsOpenMPThreads(t *testing.T) {
	for _, threads := range []int{1, 7} {
		env := streamEnvironment(threads)
		values := make(map[string][]string)
		for _, item := range env {
			key, value, ok := strings.Cut(item, "=")
			if ok && (key == "OMP_NUM_THREADS" || key == "OMP_DYNAMIC") {
				values[key] = append(values[key], value)
			}
		}
		if got := values["OMP_NUM_THREADS"]; len(got) != 1 || got[0] != strconv.Itoa(threads) {
			t.Fatalf("OMP_NUM_THREADS for %d = %v", threads, got)
		}
		if got := values["OMP_DYNAMIC"]; len(got) != 1 || got[0] != "FALSE" {
			t.Fatalf("OMP_DYNAMIC for %d = %v", threads, got)
		}
	}
}

func TestOfficialStreamBinaryMarkerCheckRejectsImageMagick(t *testing.T) {
	directory := t.TempDir()
	imageMagick := filepath.Join(directory, "stream")
	if err := os.WriteFile(imageMagick, []byte("ImageMagick stream utility\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isOfficialStreamBinary(imageMagick) {
		t.Fatal("ImageMagick stream utility was accepted as official STREAM")
	}
	official := filepath.Join(directory, "official-stream")
	if err := os.WriteFile(official, []byte("STREAM version Number of Threads requested Function Best Rate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isOfficialStreamBinary(official) {
		t.Fatal("official STREAM markers were rejected")
	}
}

func TestMemoryProbeDoesNotRunImageMagickStreamOrSysbench(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "stream")
	if err := os.WriteFile(path, []byte("ImageMagick stream utility\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	result := (memoryProbe{}).Run(context.Background(), Environment{Config: cfg})
	if result.Status != model.StatusWarning {
		t.Fatalf("collision result status = %s, want warning", result.Status)
	}
	if len(result.Measurements) != 0 {
		t.Fatalf("collision produced benchmark measurements: %+v", result.Measurements)
	}
	if strings.Contains(strings.ToLower(result.Summary), "sysbench") {
		t.Fatalf("collision summary unexpectedly mentions sysbench fallback: %q", result.Summary)
	}
}

func TestStreamReportTextHasEnglishTranslations(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	texts := []string{
		"官方 STREAM 内存带宽的 Copy、Scale、Add、Triad kernel，分别运行 1T 与当前 CPU allowance 的 NT",
		"相同 STREAM 实现、数组规模、编译选项、线程上下文与运行环境",
		"官方 STREAM 内存带宽标准基准",
		"STREAM 四 kernel 带宽（Copy/Triad 主结果）",
		"STREAM Copy NT（4 线程）",
		"Copy / NT（4 线程）",
		"STREAM NT（4 线程） 原始输出",
		"STREAM 1T 原始输出",
		"STREAM 使用官方 Copy、Scale、Add、Triad kernel；1T 与 NT 是两次独立运行，分别通过 OMP_NUM_THREADS=1 与 OMP_NUM_THREADS=4 控制。",
		"OMP_NUM_THREADS=1；OMP_NUM_THREADS=4（独立运行）",
		"MiB/s（结构化值）",
		"STREAM 输出的 Best Rate 原始单位保留在字段中；结构化指标统一换算为 MiB/s。",
		"两次 STREAM 输出的原始速率单位不同；报告仍将结构化数值统一为 MiB/s。",
		"未找到官方 STREAM，标准内存基准未运行",
		"STREAM 1T 运行失败；请查看原始输出。",
		"STREAM NT 运行失败；请查看原始输出。",
		"STREAM NT 使用 4 个线程；检测到的 CPU allowance 为 2.00 个核（cgroup v2 cpu.max）；1T 与 NT 仍是独立运行。",
		"内核 / 线程",
		"最佳速率",
		"STREAM 最佳速率",
	}
	for _, lang := range []i18n.Lang{i18n.LangEN, i18n.LangZH} {
		i18n.Set(lang)
		for _, text := range texts {
			translated := i18n.Text(text)
			if lang == i18n.LangEN && streamHanPattern.MatchString(translated) {
				t.Errorf("English STREAM text still contains Chinese: %q -> %q", text, translated)
			}
			if lang == i18n.LangZH && streamEnglishSentencePattern.MatchString(translated) {
				t.Errorf("Chinese STREAM text still contains an English sentence: %q -> %q", text, translated)
			}
		}
	}
}

func TestStreamMemoryTableUsesLocalizedSourceText(t *testing.T) {
	runs := []streamMemoryRun{
		{
			Context: "1t",
			Threads: 1,
			Sample: streamParsedOutput{Samples: map[string]streamSample{
				"Copy": {RateMiBS: 1000, Unit: "MB/s"},
			}},
		},
		{Context: "nt", Threads: 4, Sample: streamParsedOutput{Samples: make(map[string]streamSample)}},
	}
	table := streamMemoryTable(runs)
	wantColumns := []string{"内核 / 线程", "最佳速率", "原始单位", "方法", "证据"}
	if strings.Join(table.Columns, "\x00") != strings.Join(wantColumns, "\x00") {
		t.Fatalf("STREAM table columns = %v, want %v", table.Columns, wantColumns)
	}
	if len(table.Rows) != 8 {
		t.Fatalf("STREAM table rows = %d, want 8", len(table.Rows))
	}
	if got := table.Rows[0][4]; got != "STREAM 最佳速率" {
		t.Fatalf("STREAM table evidence = %q, want Chinese source text", got)
	}
	if got := table.Rows[0][3]; got != "stream-official-copy-1t-v1" {
		t.Fatalf("STREAM table method = %q, want stable machine method", got)
	}
}

// TestRunStreamWithRealBinary intentionally executes only a real executable
// discovered on PATH.  It never creates a command substitute: a missing or
// non-STREAM `stream` command is a skip outside CI and a failure in CI.
func TestRunStreamWithRealBinary(t *testing.T) {
	path, err := exec.LookPath("stream")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("official STREAM is required in CI: %v", err)
		}
		t.Skip("非 CI 环境未安装官方 STREAM，跳过真实 smoke test")
	}
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.CPUTime = time.Second
	result := runStreamMemory(context.Background(), Environment{Config: cfg}, path)
	if len(result.Measurements) == 0 {
		if os.Getenv("CI") != "" {
			t.Fatalf("stream command did not produce official STREAM output: %+v", result)
		}
		t.Skipf("非 CI 环境没有可解析的官方 STREAM 输出（可能是同名非 STREAM 命令），明确跳过：%s", path)
	}
	if result.Status != model.StatusOK {
		t.Fatalf("real STREAM result = %+v", result)
	}
	if len(result.Measurements) != 8 {
		t.Fatalf("STREAM measurements = %d, want 8: %+v", len(result.Measurements), result.Measurements)
	}
	for _, contextName := range []string{"1t", "nt"} {
		for _, kernel := range []string{"copy", "scale", "add", "triad"} {
			key := "stream_" + kernel + "_" + contextName + "_mib_s"
			found := false
			for _, measurement := range result.Measurements {
				if measurement.Key != key {
					continue
				}
				found = true
				if measurement.Value <= 0 || measurement.Unit != "MiB/s" || measurement.Method != "stream-official-"+kernel+"-"+contextName+"-v1" {
					t.Fatalf("STREAM measurement contract = %+v", measurement)
				}
			}
			if !found {
				t.Fatalf("STREAM missing measurement %q", key)
			}
		}
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 8 {
		t.Fatalf("STREAM table rows = %+v", result.Tables)
	}
	if len(result.TextBlocks) != 2 {
		t.Fatalf("STREAM raw output blocks = %d, want 2", len(result.TextBlocks))
	}
}

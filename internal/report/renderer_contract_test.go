package report

import (
	"os"
	"strings"
	"testing"
	"time"

	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/termcolor"
)

func rendererContractReport() model.Report {
	start := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	higher := true
	lower := false

	kernels := []string{"Copy", "Scale", "Add", "Triad"}
	contexts := []string{"1T", "NT（8 线程）"}
	stream := model.Result{
		ID: "memory", Title: "内存性能", Status: model.StatusOK,
		Methodology: model.Methodology{
			Kind: "standard-benchmark", Label: "标准基准", Engine: "STREAM",
			Profile: "Copy/Scale/Add/Triad × 1T/NT",
		},
		Measurements: make([]model.Measurement, 0, 8),
		Tables: []model.Table{{
			Title:   "STREAM 四 kernel 带宽",
			Columns: []string{"内核 / 线程", "最佳速率", "方法"},
		}},
	}
	for index, kernel := range kernels {
		for contextIndex, context := range contexts {
			contextKey := []string{"1t", "nt"}[contextIndex]
			method := "stream-official-" + strings.ToLower(kernel) + "-" + contextKey + "-v1"
			stream.Measurements = append(stream.Measurements, model.Measurement{
				Key:   "stream_" + strings.ToLower(kernel) + "_" + contextKey + "_mib_s",
				Label: "STREAM " + kernel + " " + context, Value: float64(1000 + index*100 + contextIndex*50),
				Unit: "MiB/s", Display: "1,000.00 MiB/s", Method: method, HigherIsBetter: &higher,
			})
			stream.Tables[0].Rows = append(stream.Tables[0].Rows, []string{kernel + " / " + context, "1,000.00 MiB/s", method})
		}
	}

	latencyMethod := "fio-direct-4KiB-randread-qd1-latency-v1"
	disk := model.Result{
		ID: "disk", Title: "磁盘性能", Status: model.StatusOK,
		Methodology: model.Methodology{Kind: "standard-benchmark", Label: "标准基准", Engine: "fio", Profile: "Direct I/O QD1 latency"},
		Measurements: []model.Measurement{
			{Key: "fio_random_read_4k_qd1_latency_avg_ms", Label: "fio QD1 4K 随机读延迟均值", Value: 1.234, Unit: "ms", Display: "1.234 ms", Method: latencyMethod, HigherIsBetter: &lower},
			{Key: "fio_random_read_4k_qd1_latency_p95_ms", Label: "fio QD1 4K 随机读延迟 P95", Value: 2.345, Unit: "ms", Display: "2.345 ms", Method: latencyMethod, HigherIsBetter: &lower},
			{Key: "fio_random_read_4k_qd1_latency_p99_ms", Label: "fio QD1 4K 随机读延迟 P99", Value: 3.456, Unit: "ms", Display: "3.456 ms", Method: latencyMethod, HigherIsBetter: &lower},
			{Key: "fio_random_read_4k_qd1_latency_max_ms", Label: "fio QD1 4K 随机读延迟最大", Value: 4.567, Unit: "ms", Display: "4.567 ms", Method: latencyMethod, HigherIsBetter: &lower},
		},
	}

	route := func(id, engine, version string) model.Result {
		return model.Result{
			ID: id, Title: "路由追踪", Status: model.StatusOK,
			Methodology: model.Methodology{Kind: "protocol-measurement", Label: "协议诊断", Engine: "NextTrace Tiny", Profile: "max 12 hops, one query"},
			Fields: []model.Field{
				{Key: "engine", Label: "引擎", Value: engine},
				{Key: "version", Label: "NextTrace Tiny 版本", Value: version},
			},
		}
	}

	ookla := model.Result{
		ID: "ookla", Title: "Ookla Speedtest（外部服务）", Status: model.StatusWarning,
		Methodology: model.Methodology{Kind: "provider-assessment", Label: "第三方评估", Engine: "official Ookla Speedtest CLI", Profile: "JSON, one server selected by Ookla"},
		Measurements: []model.Measurement{
			{Key: "ookla_ping_ms", Label: "Ookla 延迟", Value: 8.5, Unit: "ms", Display: "8.50 ms", Method: "ookla-cli-json-v1", HigherIsBetter: &lower},
			{Key: "ookla_upload_mbps", Label: "Ookla 上传", Value: 200, Unit: "Mbps", Display: "200.00 Mbps", Method: "ookla-cli-json-v1-bandwidth-bytes-per-second", HigherIsBetter: &higher},
			{Key: "ookla_packet_loss_percent", Label: "Ookla 丢包", Value: 0, Unit: "%", Display: "0.00 %", Method: "ookla-cli-json-v1", HigherIsBetter: &lower},
		},
		Tables: []model.Table{
			{Title: "Ookla 测速结果", Columns: []string{"运营商", "延迟", "下载", "上传", "丢包", "状态"}, Rows: [][]string{
				{"自动", "8.50 ms", "—", "200.00 Mbps", "0.00 %", "部分完成"},
			}},
		},
		Notes: []string{"Ookla JSON 测速字段不完整；缺失值按未返回处理，结果按部分完成处理。"},
	}

	return model.Report{
		SchemaVersion: "ecs.report/v1",
		Tool:          model.ToolInfo{Name: "ecs", Version: "test", Commit: "abc"},
		Run:           model.RunInfo{ID: "renderer-contract", Profile: "full", StartedAt: start, DurationMS: 1000, Redacted: true},
		Results:       []model.Result{stream, disk, route("route", "nexttrace-tiny", "NextTrace Tiny 1.4.0"), ookla},
		Summary:       model.Summary{Status: model.StatusWarning, OK: 3, Warnings: 1, Headline: "3 项成功，1 项需留意"},
	}
}

func TestFourRenderersPreserveBenchmarkContract(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangZH)

	directory := t.TempDir()
	written, err := WriteFilesWithOptions(rendererContractReport(), directory, "contract", []string{"json", "txt", "md", "html"}, Options{TextColor: termcolor.LevelNone})
	if err != nil {
		t.Fatal(err)
	}

	outputs := make(map[string]string, 4)
	for _, format := range []string{"json", "txt", "md", "html"} {
		content, err := os.ReadFile(written[format])
		if err != nil {
			t.Fatalf("read %s: %v", format, err)
		}
		outputs[format] = string(content)
	}

	for format, output := range outputs {
		for _, marker := range []string{
			"STREAM Copy 1T", "STREAM Copy NT（8 线程）", "STREAM Scale 1T", "STREAM Scale NT（8 线程）",
			"STREAM Add 1T", "STREAM Add NT（8 线程）", "STREAM Triad 1T", "STREAM Triad NT（8 线程）",
			"fio QD1 4K 随机读延迟", "fio-direct-4KiB-randread-qd1-latency-v1",
			"nexttrace-tiny", "NextTrace Tiny 1.4.0",
			"—", "0.00 %", "部分完成",
		} {
			if !strings.Contains(output, marker) {
				t.Fatalf("%s renderer missing %q:\n%s", format, marker, output)
			}
		}
	}

	loaded, err := LoadJSON(written["json"])
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != "ecs.report/v1" {
		t.Fatalf("schema = %q", loaded.SchemaVersion)
	}
	for _, result := range loaded.Results {
		if result.ID == "disk" && (result.Methodology.Engine != "fio" || result.Methodology.Profile != "Direct I/O QD1 latency") {
			t.Fatalf("fio methodology changed: %+v", result.Methodology)
		}
	}
	ookla := loaded.Results[len(loaded.Results)-1]
	for _, measurement := range ookla.Measurements {
		if measurement.Key == "ookla_download_mbps" {
			t.Fatal("missing Ookla download must not become a zero measurement")
		}
	}
	if got := ookla.Tables[0].Rows[0][2]; got != "—" {
		t.Fatalf("missing Ookla download cell = %q", got)
	}
}

func TestEnglishHTMLLocalizesFrameworkWithoutChangingMethodIDs(t *testing.T) {
	original := i18n.Current()
	defer i18n.Set(original)
	i18n.Set(i18n.LangEN)

	html, err := HTML(rendererContractReport(), nil)
	if err != nil {
		t.Fatal(err)
	}
	output := string(html)
	for _, marker := range []string{"<html lang=\"en\">", "ecs VPS Benchmark Report", "Generated locally; nothing was uploaded.", "<div class=\"metrics\">", "Details"} {
		if !strings.Contains(output, marker) {
			t.Fatalf("English HTML missing %q:\n%s", marker, output)
		}
	}
	if !strings.Contains(output, "fio-direct-4KiB-randread-qd1-latency-v1") {
		t.Fatal("English HTML lost the stable fio method ID")
	}
}

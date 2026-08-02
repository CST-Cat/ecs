package score

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func sampleSubmissionReport() model.Report {
	return model.Report{
		Tool: model.ToolInfo{Version: "v1.2.3"},
		Run:  model.RunInfo{Profile: "full", StartedAt: time.Unix(1700000000, 0).UTC()},
		Results: []model.Result{
			{
				ID: "system", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "logical_cpus", Value: 4},
					{Key: "memory_total_bytes", Value: 8 * (1 << 30)},
				},
				Fields: []model.Field{
					{Key: "hostname", Value: "secret-host-01"},
					{Key: "virtualization", Value: "kvm"},
					{Key: "cpu_model", Value: "AMD EPYC 7003"},
					{Key: "arch", Value: "amd64"},
				},
			},
			{
				ID: "cpu", Status: model.StatusOK,
				Fields: []model.Field{{Key: "version", Value: "sysbench 1.0.20"}},
				Measurements: []model.Measurement{
					{Key: "sysbench_cpu_single_events_s", Value: 900},
					{Key: "sysbench_cpu_multi_events_s", Value: 3400},
				},
			},
			{
				ID: "speed", Status: model.StatusOK,
				Measurements: []model.Measurement{
					{Key: "iperf3_target_01_ipv4_download_mbps", Value: 800},
					{Key: "iperf3_target_02_ipv4_download_mbps", Value: 900},
					{Key: "iperf3_target_01_ipv4_upload_mbps", Value: 700},
				},
			},
		},
	}
}

// 提交格式的核心约束：字段是白名单。报告里的可定位信息一律不得带出去。
func TestSubmissionExcludesLocatingFields(t *testing.T) {
	data := sampleSubmissionReport()
	// 往报告里塞满敏感字段，提交里一个都不该出现。
	data.Results = append(data.Results, model.Result{
		ID: "network", Status: model.StatusOK,
		Fields: []model.Field{
			{Key: "ipv4_ip", Value: "203.0.113.44", Sensitive: true},
			{Key: "ipv4_owner", Value: "Example Telecom"},
			{Key: "ipv4_route", Value: "203.0.113.0/24"},
		},
		TextBlocks: []model.TextBlock{{Content: "1  203.0.113.1  AS64500"}},
	})
	submission, err := BuildSubmission(data, SubmissionOptions{Region: "jp", Provider: "vultr"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := submission.Encode()
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"203.0.113", "secret-host-01", "AS64500", "Example Telecom", "/24",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("提交里泄露了 %q：\n%s", forbidden, text)
		}
	}
	// 该带的必须带上。
	if submission.Host.VCPU != 4 || submission.Host.CPUModel != "AMD EPYC 7003" {
		t.Errorf("机器规格未正确提取：%+v", submission.Host)
	}
	if submission.Tool.Sysbench != "sysbench 1.0.20" {
		t.Errorf("工具版本未提取：%+v", submission.Tool)
	}
}

// 指纹由内容派生：手改数值而不重算指纹，校验必须发现。
func TestSubmissionFingerprintDetectsTampering(t *testing.T) {
	submission, err := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := submission.Validate(); err != nil {
		t.Fatalf("刚生成的提交应当合法：%v", err)
	}
	tampered := submission
	tampered.Metrics = map[string]float64{}
	for key, value := range submission.Metrics {
		tampered.Metrics[key] = value
	}
	tampered.Metrics["cpu_single"] *= 10
	if err := tampered.Validate(); err == nil {
		t.Fatal("改了数值而未重算指纹，校验应当失败")
	}
}

// 同一台机器同一次结果重复提交必须得到同一个 ID，CI 才查得出重复。
func TestSubmissionFingerprintIsStable(t *testing.T) {
	first, _ := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{Region: "jp"})
	second, _ := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{Region: "jp"})
	if first.ID != second.ID {
		t.Fatalf("同一份内容应得到同一 ID：%q vs %q", first.ID, second.ID)
	}
}

func TestSubmissionValidateRejectsBadInput(t *testing.T) {
	base, _ := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{})
	cases := map[string]func(*Submission){
		"未知指标":      func(s *Submission) { s.Metrics["not_a_metric"] = 1 },
		"负值":        func(s *Submission) { s.Metrics["cpu_single"] = -1 },
		"缺 vCPU":    func(s *Submission) { s.Host.VCPU = 0 },
		"缺版本":       func(s *Submission) { s.Tool.ECS = "" },
		"错误 schema": func(s *Submission) { s.Schema = "nope" },
	}
	for name, mutate := range cases {
		copied := base
		copied.Metrics = map[string]float64{}
		for key, value := range base.Metrics {
			copied.Metrics[key] = value
		}
		mutate(&copied)
		if err := copied.Validate(); err == nil {
			t.Errorf("%s 应当被拒绝", name)
		}
	}
}

// 自报字段要被清洗：控制字符与超长内容不能进库。
func TestSubmissionSanitizesSelfReportedFields(t *testing.T) {
	submission, err := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{
		Region:   "jp\x00\x1b[31m",
		Provider: strings.Repeat("x", 200),
		Note:     strings.Repeat("说明", 300),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(submission.Host.Region, "\x00\x1b") {
		t.Errorf("地区未清洗控制字符：%q", submission.Host.Region)
	}
	if len([]rune(submission.Host.Provider)) > 48 {
		t.Errorf("商家未限长：%d", len([]rune(submission.Host.Provider)))
	}
	if len([]rune(submission.Note)) > maxNoteLength {
		t.Errorf("备注未限长：%d", len([]rune(submission.Note)))
	}
	if err := submission.Validate(); err != nil {
		t.Errorf("清洗后应当合法：%v", err)
	}
}

// 提交转成最小报告后，聚合基线必须得到与原始值一致的结果——
// 这保证了基线聚合对两种输入只有一条代码路径。
func TestSubmissionRoundTripsThroughBaseline(t *testing.T) {
	submission, err := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := BuildBaseline([]model.Report{submission.AsReport()}, "test")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range submission.Metrics {
		if got := baseline.Metrics[key]; math.Abs(got-want) > 0.001 {
			t.Errorf("指标 %q 往返后 = %v，期望 %v", key, got, want)
		}
	}
	// 带宽是聚合项：原报告两个下载节点取中位 850，往返后应保持。
	if got := baseline.Metrics["bandwidth_download"]; math.Abs(got-850) > 0.001 {
		t.Errorf("带宽中位数往返后 = %v，期望 850", got)
	}
}

func TestSubmissionFileNameIsSafe(t *testing.T) {
	submission, _ := BuildSubmission(sampleSubmissionReport(), SubmissionOptions{
		Region: "US West/2", Provider: "Acme Cloud Inc.",
	})
	name := submission.FileName()
	if strings.ContainsAny(name, "/\\ ") {
		t.Errorf("文件名含不安全字符：%q", name)
	}
	if !strings.HasSuffix(name, ".json") {
		t.Errorf("文件名应以 .json 结尾：%q", name)
	}
}

// CI 用这个测试校验整个提交库：格式、指纹、重复。
//
// 走测试而不是单独写个校验命令，是因为它同时被本地 `go test ./...` 覆盖——
// 校验逻辑与它要保护的格式定义放在一起，不会各自漂移。
func TestSubmissionCorpus(t *testing.T) {
	directory := os.Getenv("ECS_SUBMISSION_DIR")
	if directory == "" {
		directory = filepath.Join("..", "..", "submissions")
	}
	if _, err := os.Stat(directory); err != nil {
		t.Skipf("提交库目录不存在：%s", directory)
	}
	entries, err := filepath.Glob(filepath.Join(directory, "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Skip("提交库为空")
	}

	seen := make(map[string]string, len(entries))
	for _, path := range entries {
		submission, err := LoadSubmission(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if previous, duplicated := seen[submission.ID]; duplicated {
			t.Errorf("%s 与 %s 重复（ID %s）", path, previous, submission.ID)
			continue
		}
		seen[submission.ID] = path
		// 文件名应当与内容自洽，否则目录列表会误导人。
		if want := submission.FileName(); filepath.Base(path) != want {
			t.Errorf("%s 的文件名应为 %s", path, want)
		}
	}
	t.Logf("校验了 %d 份提交", len(seen))
}

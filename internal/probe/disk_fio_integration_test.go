//go:build integration

// 真实 fio 的端到端契约。

package probe

import (
	"context"
	"ecs/internal/config"
	"ecs/internal/model"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 探测结果必须真的出自 fio --enghelp 的可用列表，而不是猜的。
func TestDetectFIOEngineAgreesWithEnghelp(t *testing.T) {
	fioPath := requireTool(t, "fio")
	engine := detectFIOEngine(context.Background(), fioPath)
	if !engine.Detected {
		t.Fatalf("engine detection failed on a working fio: %+v", engine)
	}

	output, err := exec.Command(fioPath, "--enghelp").CombinedOutput()
	if err != nil {
		t.Fatalf("fio --enghelp: %v", err)
	}
	available := make(map[string]bool)
	for _, line := range strings.Split(sanitizeCommandOutput(output), "\n") {
		available[strings.TrimSpace(line)] = true
	}
	if !available[engine.Name] {
		t.Fatalf("detected engine %q is not in fio --enghelp: %v", engine.Name, output)
	}
	// AsyncQueue 决定报告里标注的队列深度，标错会让 QD1 成绩冒充 QD64。
	if want := engine.Name == "io_uring" || engine.Name == "libaio"; engine.AsyncQueue != want {
		t.Fatalf("engine %q AsyncQueue = %v, want %v", engine.Name, engine.AsyncQueue, want)
	}
	// 优先级必须是 io_uring > libaio > psync。
	if available["io_uring"] && engine.Name != "io_uring" {
		t.Fatalf("io_uring is available but %q was chosen", engine.Name)
	}
	if !available["io_uring"] && available["libaio"] && engine.Name != "libaio" {
		t.Fatalf("libaio is available but %q was chosen", engine.Name)
	}
	t.Logf("fio %s 探测到引擎 %s（异步=%v）", fioPath, engine.Name, engine.AsyncQueue)
}

// 真实 fio 端到端：单独跑固定 QD1 作业，快速验证 fio JSON 的 clat 统计和
// 参数口径；完整矩阵由 TestRunFIODiskWithRealFIO 覆盖。
func TestRunFIOD1LatencyWithRealFIO(t *testing.T) {
	fioPath := requireTool(t, "fio")
	file, err := os.CreateTemp(t.TempDir(), ".ecs-fio-latency-*")
	if err != nil {
		t.Fatal(err)
	}
	filename := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	engine := detectFIOEngine(context.Background(), fioPath)
	plan := []fioJobSpec{{Name: fioQD1LatencyJobName, RW: "randread", BlockSize: "4k", IODepth: 1, NumJobs: 1}}
	args := fioArguments(filename, 16*1024*1024, engine, plan)
	command := exec.Command(fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("real fio QD1 run: %v", err)
	}
	jobs, err := parseFIOJobs(output)
	if err != nil {
		t.Fatal(err)
	}
	job, ok := jobs[fioQD1LatencyJobName]
	if !ok {
		t.Fatalf("real fio output missing %q: %v", fioQD1LatencyJobName, jobs)
	}
	stats, ok := fioLatencyStatsFor(job.Read)
	if !ok || !stats.AvgOK || !stats.P95OK || !stats.P99OK || !stats.MaxOK {
		t.Fatalf("real fio QD1 clat stats = %+v, ok=%v", stats, ok)
	}
	if stats.AvgMS <= 0 || stats.P95MS <= 0 || stats.P99MS <= 0 || stats.MaxMS <= 0 {
		t.Fatalf("real fio QD1 clat stats are non-positive: %+v", stats)
	}
}

// 真实 fio 功能 smoke 只选取生产计划中的代表项，覆盖顺序/随机、QD1/QD32、
// 混合读写、Crystal，以及 ATTO 的最小和最大块。完整 53-job 矩阵由纯单元测试
// 校验结构，并可通过 TestRunFIODiskFullMatrixOptIn 显式端到端复核。
func TestRunRepresentativeFIOJobsWithRealFIO(t *testing.T) {
	fioPath := requireTool(t, "fio")
	file, err := os.CreateTemp(t.TempDir(), ".ecs-fio-smoke-*")
	if err != nil {
		t.Fatal(err)
	}
	filename := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	fullPlan := fioJobPlan()
	wanted := []string{
		"seqread",
		"randwrite",
		fioQD1LatencyJobName,
		"mix4k",
		"crystal_read_rnd4k_q32",
		"crystal_write_seq1m_q8",
		"atto_read_512b",
		"atto_write_64m",
	}
	plan := make([]fioJobSpec, 0, len(wanted))
	for _, name := range wanted {
		spec, ok := findFIOJobSpec(fullPlan, name)
		if !ok {
			t.Fatalf("representative fio job %q is missing from production plan", name)
		}
		plan = append(plan, spec)
	}

	engine := detectFIOEngine(context.Background(), fioPath)
	args := fioArguments(filename, 128*1024*1024, engine, plan)
	command := exec.Command(fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("representative real fio run: %v", err)
	}
	jobs, err := parseFIOJobs(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != len(plan) {
		t.Fatalf("representative fio jobs = %d, want %d: %v", len(jobs), len(plan), jobs)
	}
	for _, spec := range plan {
		job, ok := jobs[spec.Name]
		if !ok {
			t.Fatalf("representative fio output missing %q", spec.Name)
		}
		if job.Error != 0 {
			t.Fatalf("representative fio job %q error = %d", spec.Name, job.Error)
		}
	}

	assertDirection := func(name, direction string, value fioDirection) {
		t.Helper()
		if !isPositiveFinite(value.IOPS) || !isPositiveFinite(fioBandwidthMiB(value)) {
			t.Fatalf("representative fio %s %s statistics = %+v", name, direction, value)
		}
	}
	assertDirection("seqread", "read", jobs["seqread"].Read)
	assertDirection("randwrite", "write", jobs["randwrite"].Write)
	assertDirection("mix4k", "read", jobs["mix4k"].Read)
	assertDirection("mix4k", "write", jobs["mix4k"].Write)
	assertDirection("crystal_read_rnd4k_q32", "read", jobs["crystal_read_rnd4k_q32"].Read)
	assertDirection("crystal_write_seq1m_q8", "write", jobs["crystal_write_seq1m_q8"].Write)
	assertDirection("atto_read_512b", "read", jobs["atto_read_512b"].Read)
	assertDirection("atto_write_64m", "write", jobs["atto_write_64m"].Write)
	stats, ok := fioLatencyStatsFor(jobs[fioQD1LatencyJobName].Read)
	if !ok || !stats.AvgOK || !stats.P95OK || !stats.P99OK || !stats.MaxOK {
		t.Fatalf("representative fio QD1 latency statistics = %+v, ok=%v", stats, ok)
	}
}

// 完整真实 fio 端到端保留为显式诊断测试，不进入日常本地测试。它验证报告
// 标注的队列深度与实际引擎能力一致，并检查完整 53-job/106-measurement 契约。
func TestRunFIODiskFullMatrixOptIn(t *testing.T) {
	if os.Getenv("ECS_FULL_BENCH_TESTS") != "1" {
		t.Skip("set ECS_FULL_BENCH_TESTS=1 to run the complete 53-job fio matrix")
	}
	fioPath := requireTool(t, "fio")
	directory := t.TempDir()
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DiskPath = directory
	cfg.DiskMiB = 16
	engine := detectFIOEngine(context.Background(), fioPath)
	result := runFIODisk(context.Background(), Environment{Config: cfg}, fioPath)
	if result.Status != model.StatusOK {
		t.Fatalf("fio result = %+v", result)
	}

	if got := resultField(result, "ioengine"); !strings.Contains(got, engine.Name) {
		t.Fatalf("ioengine field = %q, want it to name %q", got, engine.Name)
	}
	if got := resultField(result, "version"); !strings.HasPrefix(got, "fio-") {
		t.Fatalf("fio version field = %q", got)
	}
	if got := resultField(result, "binary_sha256"); len(got) != 64 {
		t.Fatalf("fio SHA-256 = %q", got)
	}
	if got := resultField(result, "arguments"); got != "" {
		t.Fatalf("fio report must omit command arguments, got %q", got)
	}

	methods := make(map[string]string, len(result.Measurements))
	for _, measurement := range result.Measurements {
		methods[measurement.Key] = measurement.Method
		if measurement.Value <= 0 {
			t.Fatalf("real fio produced a non-positive value: %+v", measurement)
		}
	}
	randomMethod := methods["fio_random_read_4k_iops"]
	mixedMethod := methods["fio_mixed_4k_read_mib_s"]
	for _, key := range []string{fioQD1LatencyAvgKey, fioQD1LatencyP95Key, fioQD1LatencyP99Key, fioQD1LatencyMaxKey} {
		measurement, ok := findMeasurement(result.Measurements, key)
		if !ok || measurement.Value <= 0 || measurement.Unit != "ms" || measurement.Method != fioQD1LatencyMethod || measurement.HigherIsBetter == nil || *measurement.HigherIsBetter {
			t.Fatalf("real fio QD1 latency contract for %q = %+v", key, measurement)
		}
	}
	if engine.AsyncQueue {
		if !strings.Contains(randomMethod, "qd32") || !strings.Contains(mixedMethod, "qd64") {
			t.Fatalf("async methods = %q / %q, want qd32 / qd64", randomMethod, mixedMethod)
		}
		if !strings.Contains(resultField(result, "ioengine"), "异步") {
			t.Fatalf("async engine must be labelled as such: %q", resultField(result, "ioengine"))
		}
		for _, note := range result.Notes {
			if strings.Contains(note, "队列深度对它无效") {
				t.Fatalf("async engine must not be labelled synchronous: %q", note)
			}
		}
	} else {
		if !strings.Contains(randomMethod, "qd1") || !strings.Contains(mixedMethod, "qd1") {
			t.Fatalf("sync methods = %q / %q, want qd1", randomMethod, mixedMethod)
		}
		downgradeNoted := false
		for _, note := range result.Notes {
			if strings.Contains(note, "队列深度对它无效") {
				downgradeNoted = true
			}
		}
		if !downgradeNoted {
			t.Fatalf("sync engine must disclose the queue-depth downgrade: %+v", result.Notes)
		}
	}

	// 四项基础指标 + 两个 qd32 P95 字段 + 四项固定 QD1 延迟 + 四档混合各读写两项 +
	// Crystal 8 个读写单元 × 吞吐/IOPS + ATTO 36 个读写单元 × 吞吐/IOPS。
	if len(result.Measurements) != 106 {
		t.Fatalf("fio measurements = %d, want 106: %+v", len(result.Measurements), result.Measurements)
	}
	measurementKeys := make(map[string]bool, len(result.Measurements))
	for _, measurement := range result.Measurements {
		measurementKeys[measurement.Key] = true
	}
	for _, block := range attoBlockSizes {
		for _, suffix := range []string{"read_mib_s", "read_iops", "write_mib_s", "write_iops"} {
			key := "atto_" + block.FIO + "_" + suffix
			if !measurementKeys[key] {
				t.Fatalf("disk result missing ATTO measurement %q", key)
			}
		}
	}
	crystalKeys := 0
	for key := range measurementKeys {
		if strings.HasPrefix(key, "crystal_") {
			crystalKeys++
		}
	}
	if crystalKeys != 16 {
		t.Fatalf("disk result Crystal measurement cells = %d, want 16", crystalKeys)
	}
	mixedFound := false
	crystalFound, attoFound := false, false
	for _, table := range result.Tables {
		if strings.Contains(table.Title, "混合") && len(table.Rows) == 4 {
			mixedFound = true
		}
		if table.Title == "Crystal" {
			if len(table.Rows) != 4 || len(table.Rows)*2 != 8 {
				t.Fatalf("disk Crystal rows/cells = %d/%d, want 4/8", len(table.Rows), len(table.Rows)*2)
			}
			crystalFound = true
		}
		if table.Title == "ATTO" {
			if len(table.Rows) != 18 || len(table.Rows)*2 != 36 {
				t.Fatalf("disk ATTO rows/cells = %d/%d, want 18/36", len(table.Rows), len(table.Rows)*2)
			}
			for _, row := range table.Rows {
				if len(row) < 5 || row[1] == "—" || row[2] == "—" || row[3] == "—" || row[4] == "—" {
					t.Fatalf("disk ATTO row has missing read/write cell: %v", row)
				}
			}
			attoFound = true
		}
	}
	if !mixedFound {
		t.Fatalf("mixed matrix table missing: %+v", result.Tables)
	}
	if !crystalFound || !attoFound {
		t.Fatalf("disk complete matrix tables missing: crystal=%v atto=%v", crystalFound, attoFound)
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(jsonResult)
	for _, key := range []string{"atto_512b_read_mib_s", "atto_512b_write_iops", "atto_64m_read_mib_s", "atto_64m_write_iops"} {
		if !strings.Contains(jsonText, key) {
			t.Fatalf("disk JSON missing ATTO measurement key %q", key)
		}
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".ecs-fio-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("fio temporary files remain: %v", matches)
	}
}

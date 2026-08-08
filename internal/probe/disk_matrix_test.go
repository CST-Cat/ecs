package probe

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestFIOJobPlanIncludesCompleteCrystalAndATTO(t *testing.T) {
	if got := fioJobDuration(); got != 10*time.Second {
		t.Fatalf("fio duration = %s, want 10s", got)
	}
	plan := fioJobPlan()
	mixed := 0
	crystal, atto := 0, 0
	for _, job := range plan {
		switch job.Matrix {
		case "":
			if job.Mixed() {
				mixed++
			}
		case "crystal":
			crystal++
		case "atto":
			atto++
			if strings.Contains(strings.ToLower(job.BlockSize), "5m") {
				t.Fatalf("ATTO plan must not silently include 5M: %+v", job)
			}
		}
	}
	if mixed != 4 || crystal != 8 || atto != 36 {
		t.Fatalf("matrix job counts = mixed %d jobs, Crystal %d, ATTO %d; plan=%+v", mixed, crystal, atto, plan)
	}
	jobs := make(map[string]fioJobSpec)
	for _, job := range plan {
		if job.Mixed() {
			jobs[job.Name] = job
		}
	}
	for _, block := range []string{"4k", "64k", "512k", "1m"} {
		name := "mix" + block
		job, ok := jobs[name]
		if !ok || job.BlockSize != block || job.MixRead != 50 || job.NumJobs != 2 || job.IODepth != 64 {
			t.Fatalf("missing complete mixed %s job: %+v", block, job)
		}
	}
	latencyJob, ok := findFIOJobSpec(plan, fioQD1LatencyJobName)
	if !ok || latencyJob.RW != "randread" || latencyJob.BlockSize != "4k" || latencyJob.IODepth != 1 || latencyJob.NumJobs != 1 {
		t.Fatalf("fixed QD1 latency job = %+v", latencyJob)
	}
	for _, workload := range []string{"RND4K/Q1", "RND4K/Q32", "SEQ1M/Q1", "SEQ1M/Q8"} {
		read, write := false, false
		for _, job := range crystalJobSpecs() {
			if job.Workload != workload {
				continue
			}
			read = read || job.Direction == "read"
			write = write || job.Direction == "write"
		}
		if !read || !write {
			t.Fatalf("Crystal workload %s lacks both directions", workload)
		}
	}
	wantBlocks := []string{"512b", "1k", "2k", "4k", "8k", "16k", "32k", "64k", "128k", "256k", "512k", "1m", "2m", "4m", "8m", "16m", "32m", "64m"}
	if len(attoBlockSizes) != len(wantBlocks) {
		t.Fatalf("ATTO block count = %d, want %d", len(attoBlockSizes), len(wantBlocks))
	}
	for index, want := range wantBlocks {
		if attoBlockSizes[index].FIO != want {
			t.Fatalf("ATTO block %d = %q, want %q", index, attoBlockSizes[index].FIO, want)
		}
	}
}

func TestFIOArgumentsSafelySupportLargestATTOJob(t *testing.T) {
	engine := fioEngine{Name: "io_uring", AsyncQueue: true, Detected: true}
	plan := fioJobPlan()
	latencyJob, ok := findFIOJobSpec(plan, fioQD1LatencyJobName)
	if !ok {
		t.Fatalf("fixed QD1 latency job missing from plan: %v", plan)
	}
	args := strings.Join(fioArguments("<tempfile>", 128*1024*1024, 10_000_000_000, engine, plan), " ")
	for _, want := range []string{"--name=atto_read_64m", "--name=atto_write_64m", "--name=" + fioQD1LatencyJobName, "--bs=64m", "--size=134217728", "--direct=1", "--iodepth=1", "--output-format=json", "--clat_percentiles=1"} {
		if !strings.Contains(args, want) {
			t.Fatalf("fio args missing %q", want)
		}
	}
	latencyArgs := strings.Join(fioArguments("<tempfile>", 128*1024*1024, 10_000_000_000, engine, []fioJobSpec{latencyJob}), " ")
	for _, want := range []string{"--output-format=json", "--direct=1", "--rw=randread", "--bs=4k", "--iodepth=1", "--numjobs=1", "--clat_percentiles=1"} {
		if !strings.Contains(latencyArgs, want) {
			t.Fatalf("fixed QD1 fio args missing %q: %s", want, latencyArgs)
		}
	}

	actual, err := fioDiskSize(256*1024*1024, 2*1024*1024*1024, true)
	if err != nil {
		t.Fatal(err)
	}
	if actual < 128*1024*1024 || actual%(64*1024*1024) != 0 {
		t.Fatalf("matrix disk size = %d, must be aligned and support two 64MiB windows", actual)
	}
	if _, err := fioDiskSize(256*1024*1024, 512*1024*1024, true); err == nil {
		t.Fatal("a disk with less than the matrix safety reserve must be refused")
	}
	expanded, err := fioDiskSize(64*1024*1024, 2*1024*1024*1024, true)
	if err != nil || expanded != 128*1024*1024 {
		t.Fatalf("small configured matrix file should expand to the safe minimum: %d/%v", expanded, err)
	}
}

func TestFIOCrystalAndATTOMetricKeysAndTables(t *testing.T) {
	jobs := make(map[string]fioJob)
	for index, spec := range append(crystalJobSpecs(), attoJobSpecs()...) {
		throughput := float64(index+1) * 1024 * 1024
		direction := fioDirection{BWBytes: throughput, IOPS: float64(index+1) * 10}
		job := fioJob{Name: spec.Name}
		if spec.Direction == "write" {
			job.Write = direction
		} else {
			job.Read = direction
		}
		jobs[spec.Name] = job
	}
	result := model.Result{ID: "disk", Status: model.StatusOK}
	engine := fioEngine{Name: "io_uring", AsyncQueue: true, Detected: true}
	appendCrystalMatrix(&result, jobs, engine)
	appendATTOMatrix(&result, jobs, engine)

	if len(result.Measurements) != 16+72 {
		t.Fatalf("matrix measurements = %d, want 88", len(result.Measurements))
	}
	keys := make(map[string]model.Measurement, len(result.Measurements))
	for _, measurement := range result.Measurements {
		if measurement.Value <= 0 || measurement.Method == "" || measurement.HigherIsBetter == nil || !*measurement.HigherIsBetter {
			t.Fatalf("invalid matrix measurement semantics: %+v", measurement)
		}
		if _, exists := keys[measurement.Key]; exists {
			t.Fatalf("duplicate matrix key %q", measurement.Key)
		}
		keys[measurement.Key] = measurement
	}
	for _, key := range []string{
		"crystal_rnd4k_q1_read_mib_s", "crystal_rnd4k_q1_write_iops",
		"crystal_seq1m_q8_read_mib_s", "crystal_seq1m_q8_write_iops",
		"atto_512b_read_mib_s", "atto_64m_write_iops",
	} {
		measurement, ok := keys[key]
		if !ok {
			t.Fatalf("missing stable matrix key %q", key)
		}
		if !strings.HasPrefix(measurement.Method, "fio-direct-") {
			t.Fatalf("unstable matrix method %q", measurement.Method)
		}
	}
	if strings.Contains(keys["crystal_rnd4k_q1_read_mib_s"].Method, "/") {
		t.Fatalf("Crystal method should not contain an unstable slash: %q", keys["crystal_rnd4k_q1_read_mib_s"].Method)
	}

	if len(result.Tables) != 2 || result.Tables[0].Title != "Crystal" || result.Tables[1].Title != "ATTO" {
		t.Fatalf("matrix tables = %+v", result.Tables)
	}
	if len(result.Tables[0].Rows) != 4 || len(result.Tables[1].Rows) != 18 {
		t.Fatalf("matrix row counts = %d/%d", len(result.Tables[0].Rows), len(result.Tables[1].Rows))
	}
	for _, row := range result.Tables[1].Rows {
		if row[0] == "5M" {
			t.Fatal("ATTO table must not contain an unrequested 5M row")
		}
		if row[len(row)-1] != "完成" {
			t.Fatalf("complete ATTO row has status %q: %v", row[len(row)-1], row)
		}
	}
}

func TestFIOMatrixMissingCellsRemainExplicit(t *testing.T) {
	result := model.Result{ID: "disk", Status: model.StatusOK}
	engine := fioEngine{Name: "psync", Detected: true}
	appendCrystalMatrix(&result, nil, engine)
	appendATTOMatrix(&result, nil, engine)
	if result.Status != model.StatusWarning || len(result.Measurements) != 0 {
		t.Fatalf("missing matrix result = %+v", result)
	}
	for _, table := range result.Tables {
		for _, row := range table.Rows {
			if row[len(row)-1] != "未返回" || row[1] != "—" || row[4] != "—" {
				t.Fatalf("missing matrix cell was not explicit: %s %v", table.Title, row)
			}
		}
	}
	if !containsNote(result.Notes, "缺") {
		t.Fatalf("missing matrix warning note absent: %v", result.Notes)
	}
}

func TestFIOMatrixPartialCellsRemainExplicit(t *testing.T) {
	result := model.Result{ID: "disk", Status: model.StatusOK}
	jobs := map[string]fioJob{
		"crystal_read_rnd4k_q1": {
			Name: "crystal_read_rnd4k_q1",
			Read: fioDirection{BWBytes: 8 * 1024 * 1024},
		},
		"crystal_write_rnd4k_q1": {
			Name:  "crystal_write_rnd4k_q1",
			Write: fioDirection{BWBytes: 4 * 1024 * 1024, IOPS: 50},
		},
	}
	appendCrystalMatrix(&result, jobs, fioEngine{Name: "psync", Detected: true})

	if result.Status != model.StatusWarning {
		t.Fatalf("partial matrix status = %s, want warning", result.Status)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 4 {
		t.Fatalf("partial matrix rows = %+v, want four retained rows", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if row[1] == "—" || row[2] != "—" || row[3] == "—" || row[4] == "—" || row[5] != "未返回" {
		t.Fatalf("partial matrix cell was not shown explicitly: %v", row)
	}
	if containsNote(result.Notes, "缺失项") == false && !containsNote(result.Notes, "未返回") {
		t.Fatalf("partial matrix warning note absent: %v", result.Notes)
	}
}

func containsNote(notes []string, needle string) bool {
	for _, note := range notes {
		if strings.Contains(note, needle) {
			return true
		}
	}
	return false
}

func TestATTOJobNamesRemainStable(t *testing.T) {
	for _, block := range attoBlockSizes {
		for _, direction := range []string{"read", "write"} {
			want := fmt.Sprintf("atto_%s_%s", direction, block.FIO)
			found := false
			for _, job := range attoJobSpecs() {
				if job.Name == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("missing stable ATTO job name %q", want)
			}
		}
	}
}

func findFIOJobSpec(plan []fioJobSpec, name string) (fioJobSpec, bool) {
	for _, job := range plan {
		if job.Name == name {
			return job, true
		}
	}
	return fioJobSpec{}, false
}

func findMeasurement(measurements []model.Measurement, key string) (model.Measurement, bool) {
	for _, measurement := range measurements {
		if measurement.Key == key {
			return measurement, true
		}
	}
	return model.Measurement{}, false
}

func TestFIOLatencyStatsConvertAllClatUnits(t *testing.T) {
	cases := []struct {
		name   string
		factor float64
		clat   fioClat
	}{
		{name: "ns", factor: 1_000_000, clat: fioClat{Mean: 1_500_000, Max: 4_000_000, Percentile: map[string]float64{"95.000000": 2_500_000, "99.000000": 3_000_000}}},
		{name: "us", factor: 1_000, clat: fioClat{Mean: 1_500, Max: 4_000, Percentile: map[string]float64{"95.000000": 2_500, "99.000000": 3_000}}},
		{name: "ms", factor: 1, clat: fioClat{Mean: 1.5, Max: 4, Percentile: map[string]float64{"95.000000": 2.5, "99.000000": 3}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			direction := fioDirection{}
			switch testCase.name {
			case "ns":
				direction.ClatNS = testCase.clat
			case "us":
				direction.ClatUS = testCase.clat
			case "ms":
				direction.ClatMS = testCase.clat
			}
			stats, ok := fioLatencyStatsFor(direction)
			if !ok || !stats.AvgOK || !stats.P95OK || !stats.P99OK || !stats.MaxOK {
				t.Fatalf("latency stats = %+v, ok=%v", stats, ok)
			}
			for name, got := range map[string]float64{
				"avg": stats.AvgMS, "p95": stats.P95MS, "p99": stats.P99MS, "max": stats.MaxMS,
			} {
				want := map[string]float64{"avg": 1.5, "p95": 2.5, "p99": 3, "max": 4}[name]
				if diff := got - want; diff > 1e-12 || diff < -1e-12 {
					t.Errorf("%s = %g ms, want %g ms", name, got, want)
				}
			}
		})
	}
}

func TestFIOLatencyMissingValuesRemainMissing(t *testing.T) {
	result := model.Result{ID: "disk", Status: model.StatusOK}
	appendFIOQD1LatencyMeasurements(&result, map[string]fioJob{
		fioQD1LatencyJobName: {Name: fioQD1LatencyJobName, Read: fioDirection{ClatNS: fioClat{
			Mean: 1_000_000, Max: 2_000_000, Percentile: map[string]float64{"95.000000": 1_500_000},
		}}},
	})
	if result.Status != model.StatusWarning {
		t.Fatalf("partial latency result status = %s, want warning", result.Status)
	}
	if len(result.Measurements) != 3 {
		t.Fatalf("partial latency measurements = %d, want 3: %+v", len(result.Measurements), result.Measurements)
	}
	for _, measurement := range result.Measurements {
		if measurement.Value <= 0 || measurement.Unit != "ms" || measurement.Method != fioQD1LatencyMethod || measurement.HigherIsBetter == nil || *measurement.HigherIsBetter {
			t.Fatalf("invalid partial latency measurement: %+v", measurement)
		}
	}
	if !containsNote(result.Notes, "未返回") || !containsNote(result.Notes, "不补零") {
		t.Fatalf("missing latency warning = %v", result.Notes)
	}

	missingJob := model.Result{ID: "disk", Status: model.StatusOK}
	appendFIOQD1LatencyMeasurements(&missingJob, nil)
	if missingJob.Status != model.StatusWarning || len(missingJob.Measurements) != 0 {
		t.Fatalf("missing latency job = %+v", missingJob)
	}
	if !containsNote(missingJob.Notes, "4 项") || !containsNote(missingJob.Notes, "未返回") {
		t.Fatalf("missing latency job warning = %v", missingJob.Notes)
	}
}

func TestFIOBaseMissingValuesRemainMissing(t *testing.T) {
	result := model.Result{ID: "disk", Status: model.StatusOK}
	if !appendFIOBaseMeasurement(&result, "present", "present", 12.5, "MiB/s", "fio-test") {
		t.Fatal("positive fio measurement was rejected")
	}
	if appendFIOBaseMeasurement(&result, "missing", "missing", 0, "MiB/s", "fio-test") {
		t.Fatal("zero fio measurement was accepted")
	}
	if len(result.Measurements) != 1 || result.Measurements[0].Key != "present" || result.Measurements[0].Value <= 0 {
		t.Fatalf("missing fio measurement was emitted: %+v", result.Measurements)
	}
}

func TestFIOMixedMissingValuesRemainVisible(t *testing.T) {
	result := model.Result{ID: "disk", Status: model.StatusOK}
	plan := fioJobPlan()
	jbs := map[string]fioJob{
		"mix4k": {
			Name:  "mix4k",
			Read:  fioDirection{BWBytes: 8 * 1024 * 1024, IOPS: 100},
			Write: fioDirection{BWBytes: 4 * 1024 * 1024, IOPS: 50},
		},
	}
	appendFIOMixedResults(&result, plan, jbs, 64)
	if result.Status != model.StatusWarning {
		t.Fatalf("partial mixed result status = %s, want warning", result.Status)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 4 {
		t.Fatalf("mixed rows = %+v, want all four planned jobs", result.Tables)
	}
	if result.Tables[0].Rows[0][1] == "—" || result.Tables[0].Rows[0][3] == "—" || result.Tables[0].Rows[0][5] == "—" {
		t.Fatalf("complete mixed row lost a value: %v", result.Tables[0].Rows[0])
	}
	for _, row := range result.Tables[0].Rows[1:] {
		if row[1] != "—" || row[2] != "—" || row[3] != "—" || row[4] != "—" || row[5] != "—" {
			t.Fatalf("missing mixed job fabricated a value: %v", row)
		}
	}
	if !containsNote(result.Notes, "3 项") || !containsNote(result.Notes, "不补零") {
		t.Fatalf("mixed missing warning = %v", result.Notes)
	}
}

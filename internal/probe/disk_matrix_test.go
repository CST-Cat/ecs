package probe

import (
	"fmt"
	"strings"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func TestFIOJobPlanIncludesCompleteCrystalAndATTO(t *testing.T) {
	plan := fioJobPlan(config.ProfileFull)
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
	for _, profile := range []string{config.ProfileStandard, config.ProfileFull} {
		jobs := make(map[string]fioJobSpec)
		for _, job := range fioJobPlan(profile) {
			if job.Mixed() {
				jobs[job.Name] = job
			}
		}
		for _, block := range []string{"4k", "64k", "512k", "1m"} {
			for _, direction := range []string{"read", "write"} {
				name := "mix" + block
				job, ok := jobs[name]
				if !ok || job.BlockSize != block || job.MixRead != 50 || job.NumJobs != 2 || job.IODepth != 64 {
					t.Fatalf("%s missing complete mixed %s/%s job: %+v", profile, block, direction, job)
				}
			}
		}
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
	if quick := fioJobPlan(config.ProfileQuick); anyMatrixJob(quick) {
		t.Fatal("quick profile should preserve its bounded legacy matrix")
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
	args := strings.Join(fioArguments("<tempfile>", 128*1024*1024, 10_000_000_000, engine, fioJobPlan(config.ProfileFull)), " ")
	for _, want := range []string{"--name=atto_read_64m", "--name=atto_write_64m", "--bs=64m", "--size=134217728", "--direct=1", "--iodepth=1"} {
		if !strings.Contains(args, want) {
			t.Fatalf("fio args missing %q", want)
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

func anyMatrixJob(plan []fioJobSpec) bool {
	for _, job := range plan {
		if job.Matrix != "" {
			return true
		}
	}
	return false
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

package probe

import (
	"strings"
	"testing"
	"time"

	"ecs/internal/model"
)

func TestFIOParserAndLatency(t *testing.T) {
	output := []byte(`{"fio version":"fio-3.39","jobs":[
{"jobname":"seqwrite","write":{"io_bytes":4096,"bw_bytes":2097152,"iops":20}},
{"jobname":"latency_qd1","read":{"io_bytes":4096,"clat_ns":{"mean":1000000,"max":2000000,"percentile":{"95.00":1500000,"99.00":1800000}}}}
]}`)
	jobs, err := parseFIOJobs(output)
	if err != nil || len(jobs) != 2 || fioBandwidthMiB(jobs["seqwrite"].Write) != 2 {
		t.Fatalf("fio jobs = %v/%v", jobs, err)
	}
	if fioBandwidthMiB(fioDirection{BW: 1024}) != 1 || fioBandwidthMiB(fioDirection{}) != 0 {
		t.Fatal("fio bandwidth fallback/invalid boundary failed")
	}
	stats, ok := fioLatencyStatsFor(jobs[fioQD1LatencyJobName].Read)
	if !ok || stats.AvgMS != 1 || stats.P95MS != 1.5 || stats.P99MS < 1.79 || stats.P99MS > 1.81 || stats.MaxMS != 2 {
		t.Fatalf("fio latency = %+v/%v", stats, ok)
	}
	if converted, ok := fioLatencyStatsFor(fioDirection{ClatUS: fioClat{Mean: 1000, Max: 2000}}); !ok || converted.AvgMS != 1 || converted.MaxMS != 2 {
		t.Fatalf("fio microsecond latency = %+v/%v", converted, ok)
	}
	if _, err := parseFIOJobs([]byte(`{"jobs":`)); err == nil || !strings.Contains(err.Error(), "解析 fio JSON") {
		t.Fatal("malformed fio JSON did not retain diagnostic")
	}

	result := model.NewResult("disk", "disk")
	if !appendFIOBaseMeasurement(&result, "seq_write", 2, "MiB/s", "fixture") || appendFIOBaseMeasurement(&result, "bad", 0, "MiB/s", "fixture") {
		t.Fatal("base measurement validity boundary failed")
	}
	if result.Measurements[0].Label != "probe.disk.metric.seq_write" {
		t.Fatalf("base measurement label = %q", result.Measurements[0].Label)
	}
	appendFIOQD1LatencyMeasurements(&result, jobs)
	for _, key := range []string{fioQD1LatencyAvgKey, fioQD1LatencyP95Key, fioQD1LatencyP99Key, fioQD1LatencyMaxKey} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing QD1 latency measurement %q", key)
		}
	}
	if fioP95Milliseconds(jobs[fioQD1LatencyJobName].Read) != 1.5 || fioP95Milliseconds(fioDirection{}) != 0 {
		t.Fatal("fio P95 success/fallback failed")
	}
	partial := model.NewResult("disk", "disk")
	appendFIOQD1LatencyMeasurements(&partial, map[string]fioJob{fioQD1LatencyJobName: {Read: fioDirection{ClatNS: fioClat{Mean: 1000000}}}})
	if len(partial.Measurements) != 1 || partial.Status != model.StatusWarning || len(partial.Notes) != 1 || !strings.Contains(partial.Notes[0], "3 项") {
		t.Fatalf("partial QD1 latency = %+v", partial)
	}
	missing := model.NewResult("disk", "disk")
	appendFIOQD1LatencyMeasurements(&missing, map[string]fioJob{})
	if missing.Status != model.StatusWarning || len(missing.Notes) != 1 {
		t.Fatalf("missing QD1 latency = %+v", missing)
	}
}

func TestFIOMatricesAndMixedResults(t *testing.T) {
	validRead := fioDirection{IOBytes: 1, BWBytes: 2 * 1024 * 1024, IOPS: 10}
	validWrite := fioDirection{IOBytes: 1, BWBytes: 4 * 1024 * 1024, IOPS: 20}
	for _, test := range []struct {
		name string
		spec fioJobSpec
		job  fioJob
		want bool
	}{
		{name: "read", spec: fioJobSpec{RW: "read"}, job: fioJob{Read: validRead}, want: true},
		{name: "write", spec: fioJobSpec{RW: "write"}, job: fioJob{Write: validWrite}, want: true},
		{name: "mixed", spec: fioJobSpec{RW: "randrw"}, job: fioJob{Read: validRead, Write: validWrite}, want: true},
		{name: "mixed partial", spec: fioJobSpec{RW: "randrw"}, job: fioJob{Read: validRead}},
		{name: "job error", spec: fioJobSpec{RW: "read"}, job: fioJob{Error: 1, Read: validRead}},
		{name: "unknown direction", spec: fioJobSpec{RW: "unknown"}, job: fioJob{Read: validRead}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := fioJobHasEvidence(test.spec, test.job); got != test.want {
				t.Fatalf("fio job evidence = %v, want %v", got, test.want)
			}
		})
	}
	fullPlan := []fioJobSpec{{Name: "mix4k", RW: "randrw", BlockSize: "4k"}}
	fullJobs := map[string]fioJob{"mix4k": {Name: "mix4k", Read: validRead, Write: validWrite}}
	full := model.NewResult("disk", "disk")
	appendFIOMixedResults(&full, fullPlan, fullJobs, 64)
	if len(full.Tables) != 1 || full.Tables[0].Key != "disk.fio.mixed" || full.Tables[0].RowIdentity != "block_size" || len(full.Tables[0].Columns) != 6 || full.Tables[0].Rows[0][5].Text() == "—" {
		t.Fatalf("full mixed matrix = %+v", full.Tables)
	}
	partial := model.NewResult("disk", "disk")
	appendFIOMixedResults(&partial, fullPlan, map[string]fioJob{"mix4k": {Name: "mix4k", Read: fullJobs["mix4k"].Read}}, 64)
	if partial.Status != model.StatusWarning || partial.Tables[0].Rows[0][5].Text() != "—" || len(partial.Notes) == 0 {
		t.Fatalf("partial mixed matrix = %+v", partial)
	}

	crystalJobs := map[string]fioJob{}
	for _, spec := range crystalJobSpecs() {
		if spec.Workload != "RND4K/Q1" {
			continue
		}
		job := fioJob{Name: spec.Name}
		if spec.Direction == "read" {
			job.Read = fioDirection{BWBytes: 2 * 1024 * 1024, IOPS: 10}
		} else {
			job.Write = fioDirection{BWBytes: 4 * 1024 * 1024, IOPS: 20}
		}
		crystalJobs[spec.Name] = job
	}
	crystal := model.NewResult("disk", "disk")
	appendCrystalMatrix(&crystal, crystalJobs, fioEngine{Name: "psync"}, map[string]float64{crystalJobSpecs()[0].Name: 1})
	if len(crystal.Tables) != 1 || crystal.Tables[0].Key != "disk.fio.crystal" || len(crystal.Tables[0].Columns) != 7 || len(crystal.Tables[0].Rows) != 4 || crystal.Tables[0].Rows[0][6].Text() != "probe.disk.status.complete" || crystal.Status != model.StatusWarning {
		t.Fatalf("crystal matrix = %+v status=%s", crystal.Tables, crystal.Status)
	}
	if crystal.Tables[0].Title != "probe.disk.table.crystal" || crystal.Tables[0].Columns[0].Label != "probe.disk.column.workload" {
		t.Fatalf("crystal presentation metadata = %+v", crystal.Tables[0])
	}
	if _, ok := crystal.Tables[0].Rows[0][6].Key(); !ok {
		t.Fatalf("crystal status is not a tagged key: %#v", crystal.Tables[0].Rows[0][6])
	}
	crystalFullJobs := make(map[string]fioJob, len(crystalJobSpecs()))
	for _, spec := range crystalJobSpecs() {
		job := fioJob{Name: spec.Name}
		if spec.Direction == "read" {
			job.Read = fioDirection{BWBytes: 2 * 1024 * 1024, IOPS: 10}
		} else {
			job.Write = fioDirection{BWBytes: 4 * 1024 * 1024, IOPS: 20}
		}
		crystalFullJobs[spec.Name] = job
	}
	crystalFull := model.NewResult("disk", "disk")
	appendCrystalMatrix(&crystalFull, crystalFullJobs, fioEngine{Name: "io_uring", AsyncQueue: true}, nil)
	if crystalFull.Status != model.StatusOK || crystalFull.Tables[0].Rows[1][6].Text() != "probe.disk.status.complete" {
		t.Fatalf("complete crystal matrix = %+v status=%s", crystalFull.Tables, crystalFull.Status)
	}

	block := attoBlockSizes[0]
	nameRead, nameWrite := "atto_read_"+block.FIO, "atto_write_"+block.FIO
	jobs := map[string]fioJob{
		nameRead:  {Name: nameRead, Read: fioDirection{BW: 1024, IOPS: 8}},
		nameWrite: {Name: nameWrite, Write: fioDirection{BW: 2048, IOPS: 16}},
	}
	attach := model.NewResult("disk", "disk")
	appendATTOMatrix(&attach, jobs, fioEngine{Name: "io_uring", AsyncQueue: true}, nil)
	if len(attach.Tables) != 1 || attach.Tables[0].Key != "disk.fio.atto" || len(attach.Tables[0].Columns) != 8 || len(attach.Tables[0].Rows) != len(attoBlockSizes) || !hasMeasurement(attach, "atto_512b_read_mib_s") {
		t.Fatalf("ATTO matrix = %+v measurements=%+v", attach.Tables, attach.Measurements)
	}
	if attach.Tables[0].Rows[0][7].Text() != "probe.disk.status.complete" || attach.Tables[0].Rows[1][7].Text() != "probe.disk.status.missing" {
		t.Fatalf("ATTO partial rows = %+v/%+v", attach.Tables[0].Rows[0], attach.Tables[0].Rows[1])
	}
	if _, ok := attach.Tables[0].Rows[1][7].Key(); !ok {
		t.Fatalf("ATTO status is not a tagged key: %#v", attach.Tables[0].Rows[1][7])
	}
	fullATTOJobs := make(map[string]fioJob, len(attoJobSpecs()))
	for _, spec := range attoJobSpecs() {
		job := fioJob{Name: spec.Name}
		if spec.Direction == "read" {
			job.Read = fioDirection{BW: 1024, IOPS: 8}
		} else {
			job.Write = fioDirection{BW: 2048, IOPS: 16}
		}
		fullATTOJobs[spec.Name] = job
	}
	fullATTO := model.NewResult("disk", "disk")
	appendATTOMatrix(&fullATTO, fullATTOJobs, fioEngine{Name: "io_uring", AsyncQueue: true}, nil)
	if fullATTO.Status != model.StatusOK || fullATTO.Tables[0].Rows[len(fullATTO.Tables[0].Rows)-1][7].Text() != "probe.disk.status.complete" {
		t.Fatalf("complete ATTO matrix = %+v status=%s", fullATTO.Tables, fullATTO.Status)
	}
}

func TestFIOPlanAndDiskSafety(t *testing.T) {
	if got, err := fioDiskSize(256*1024*1024, 2*1024*1024*1024); err != nil || got != 256*1024*1024 {
		t.Fatalf("normal fio size = %d/%v", got, err)
	}
	if got, err := fioDiskSize(1024*1024*1024, 1024*1024*1024); err != nil || got <= 0 || got >= 1024*1024*1024 || got%(64*1024*1024) != 0 {
		t.Fatalf("free-space capped fio size = %d/%v", got, err)
	}
	if got, err := fioDiskSize(64*1024*1024, 0); err != nil || got != 128*1024*1024 {
		t.Fatalf("minimum fio size = %d/%v", got, err)
	}
	if _, err := fioDiskSize(256*1024*1024, 100*1024*1024); err == nil || !strings.Contains(err.Error(), "安全余量不足") {
		t.Fatal("insufficient fio free-space boundary accepted")
	}
	if (fioEngine{Name: "psync"}).EffectiveDepth(32) != 1 || (fioEngine{Name: "io_uring", AsyncQueue: true}).EffectiveDepth(32) != 32 || !strings.Contains(describeFIOEngine(fioEngine{Name: "psync"}), "队列深度恒为 1") || !strings.Contains(describeFIOEngine(fioEngine{Name: "io_uring", AsyncQueue: true}), "异步") {
		t.Fatal("fio engine depth policy failed")
	}
	plan := []fioJobSpec{
		{Name: "base", RW: "write", Runtime: 2 * time.Second, EndFsync: true},
		{Name: "atto", RW: "read", Matrix: "atto", Runtime: 3 * time.Second},
		{Name: "mix", RW: "randrw", MixRead: 50, Runtime: 4 * time.Second},
	}
	timeArgs := fioArgumentsForMode("/tmp/file", 128, fioEngine{Name: "psync"}, plan, "time")
	fixedArgs := fioArgumentsForMode("/tmp/file", 128, fioEngine{Name: "psync"}, plan, "fixed")
	if !containsString(timeArgs, "--runtime=2") || !containsString(timeArgs, "--time_based=1") || !containsString(timeArgs, "--rwmixread=50") || !containsString(timeArgs, "--end_fsync=1") || !containsString(timeArgs, "--stonewall=1") || !containsString(fixedArgs, "--io_size=256m") || !containsString(fixedArgs, "--runtime=2") || !containsString(timeArgs, "--iodepth=1") || strings.Count(strings.Join(fixedArgs, "\x00"), "--io_size=256m") != 1 || strings.Count(strings.Join(fixedArgs, "\x00"), "--time_based=1") != 2 {
		t.Fatalf("fio arguments = time:%v fixed:%v", timeArgs, fixedArgs)
	}
	if len(fioJobPlan()) < 10 || FIOPlanDuration() <= 0 {
		t.Fatal("fio plan did not contain the expected workload classes")
	}
	offsets := fioModuleOffsets(map[string]fioJob{"a": {JobStart: 1000}, "b": {JobStart: 2500}})
	if formatModuleOffset(offsets, "b") != "2 s" || formatModuleOffset(offsets, "missing") != "—" {
		t.Fatalf("fio module offsets = %v", offsets)
	}
}

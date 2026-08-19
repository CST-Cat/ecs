package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestParseFIOJSONAddsCrystalMeasurementAndTable(t *testing.T) {
	output := []byte(`{"fio version":"fio-3.39","jobs":[
{"jobname":"crystal_read_rnd4k_q1","read":{"io_bytes":4096,"bw_bytes":1048576,"iops":100}},
{"jobname":"crystal_write_rnd4k_q1","write":{"io_bytes":4096,"bw_bytes":2097152,"iops":200}}
]}`)
	jops, err := parseFIOJobs(output)
	if err != nil {
		t.Fatal(err)
	}
	result := model.Result{ID: "disk", Status: model.StatusOK}
	appendCrystalMatrix(&result, jops, fioEngine{Name: "psync"}, nil)
	if !hasMeasurement(result, "crystal_rnd4k_q1_read_mib_s") {
		t.Fatalf("FIO measurements = %+v", result.Measurements)
	}
	if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 4 {
		t.Fatalf("FIO Crystal table = %+v", result.Tables)
	}
	row := result.Tables[0].Rows[0]
	if row[0] != "RND4K/Q1" || row[len(row)-1] != "完成" {
		t.Fatalf("FIO Crystal row = %v", row)
	}
}

func TestParseFIOJSONRejectsMalformedResult(t *testing.T) {
	if _, err := parseFIOJobs([]byte(`{"jobs":`)); err == nil {
		t.Fatal("malformed FIO JSON unexpectedly parsed")
	}
}

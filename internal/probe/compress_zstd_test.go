package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestParseZstdOutputAddsThroughputMeasurement(t *testing.T) {
	contract := zstdBenchmarkContract{Version: "1.5.7", Level: 3, Seconds: 1, CorpusBytes: 12}
	output := "bench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks\n" +
		"-3 6 (2.000) 40.0 MB/s 20.0 MB/s tiny.corpus"
	sample, err := parseZstdBenchmarkOutput(output, contract)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Version != contract.Version || sample.CompressMBPS != 40 || sample.DecompressMBPS != 20 {
		t.Fatalf("parsed zstd sample = %+v", sample)
	}

	result := model.NewResult("zstd", "zstd")
	appendZstdMeasurements(&result, []zstdBenchmarkSample{sample, {}}, 1)
	if !hasMeasurement(result, "zstd_compress_1t_mb_s") {
		t.Fatalf("zstd measurements = %+v", result.Measurements)
	}
}

func TestParseZstdOutputRejectsVersionMismatch(t *testing.T) {
	contract := zstdBenchmarkContract{Version: "1.5.7", Level: 3, Seconds: 1, CorpusBytes: 12}
	output := "bench 1.5.6 : input 12 bytes, 1 seconds, 0 KB blocks\n" +
		"-3 6 (2.000) 40.0 MB/s 20.0 MB/s tiny.corpus"
	if _, err := parseZstdBenchmarkOutput(output, contract); err == nil {
		t.Fatal("version-mismatched zstd output unexpectedly parsed")
	}
}

// hasMeasurement is shared by the other probe benchmark tests.
func hasMeasurement(result model.Result, key string) bool {
	for _, measurement := range result.Measurements {
		if measurement.Key == key {
			return true
		}
	}
	return false
}

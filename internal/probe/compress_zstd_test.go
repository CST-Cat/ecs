package probe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestZstdParserAndCorpusContracts(t *testing.T) {
	contract := zstdBenchmarkContract{Version: "1.5.7", Level: 3, Seconds: 1, CorpusName: "fixture.corpus", CorpusBytes: 12}
	output := "bench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks\n" +
		"-3 6 (2.000) 40.0 MB/s 20.0 MB/s tiny.corpus"
	sample, err := parseZstdBenchmarkOutput(output, contract)
	if err != nil || sample.Version != contract.Version || sample.InputBytes != 12 || sample.CompressedSize != 6 || sample.CompressMBPS != 40 || sample.DecompressMBPS != 20 {
		t.Fatalf("parsed zstd output = %+v/%v", sample, err)
	}
	if version, ok := parseZstdVersion("zstd v1.5.7"); !ok || version != "1.5.7" {
		t.Fatalf("zstd version parse = %q/%v", version, ok)
	}
	if _, ok := parseZstdVersion("zstd development build"); ok {
		t.Fatal("unversioned zstd output unexpectedly parsed")
	}
	for _, test := range []struct {
		name, output, marker string
	}{
		{name: "unique header", output: strings.Replace(output, "bench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks\n", "", 1), marker: "唯一 benchmark header"},
		{name: "version", output: strings.Replace(output, "bench 1.5.7", "bench 1.5.6", 1), marker: "版本"},
		{name: "input bytes", output: strings.Replace(output, "input 12 bytes", "input 13 bytes", 1), marker: "input bytes"},
		{name: "duration", output: strings.Replace(output, "1 seconds", "2 seconds", 1), marker: "duration"},
		{name: "unique result", output: output + "\n" + strings.Split(output, "\n")[1], marker: "唯一完整吞吐结果"},
		{name: "level", output: strings.Replace(output, "-3 6", "-4 6", 1), marker: "level"},
		{name: "compressed size", output: strings.Replace(output, "-3 6", "-3 12", 1), marker: "compressed size"},
		{name: "ratio", output: strings.Replace(output, "(2.000)", "(0.000)", 1), marker: "ratio 数值无效"},
		{name: "compression", output: strings.Replace(output, "40.0 MB/s", "0.0 MB/s", 1), marker: "compression 数值无效"},
		{name: "decompression", output: strings.Replace(output, "20.0 MB/s", "0.0 MB/s", 1), marker: "decompression 数值无效"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseZstdBenchmarkOutput(test.output, contract); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("zstd error = %v, want %q", err, test.marker)
			}
		})
	}
	if _, err := executeZstdBenchmark(context.Background(), "unused", "unused", 0, contract); err == nil || !strings.Contains(err.Error(), "worker 数必须为正数") {
		t.Fatalf("zstd invalid execute input = %v", err)
	}

	directory := t.TempDir()
	data := []byte("fixture corpus")
	path := filepath.Join(directory, contract.CorpusName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	contract.CorpusBytes = int64(len(data))
	contract.CorpusSHA256 = hash
	if err := verifyZstdCorpus(path, contract); err != nil {
		t.Fatalf("valid corpus rejected: %v", err)
	}
	t.Setenv("ECS_ZSTD_CORPUS", path)
	if found, err := findZstdCorpus("unused", contract); err != nil || found != path {
		t.Fatalf("configured corpus = %q/%v", found, err)
	}
	if err := verifyZstdCorpus(directory, contract); err == nil || !strings.Contains(err.Error(), "不是普通文件") {
		t.Fatal("directory accepted as corpus")
	}
	wrongSize := contract
	wrongSize.CorpusBytes++
	if err := verifyZstdCorpus(path, wrongSize); err == nil || !strings.Contains(err.Error(), "大小") {
		t.Fatal("wrong-size corpus accepted")
	}
	wrongHash := contract
	wrongHash.CorpusSHA256 = "deadbeef"
	if err := verifyZstdCorpus(path, wrongHash); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatal("wrong-hash corpus accepted")
	}
	t.Setenv("ECS_ZSTD_CORPUS", filepath.Join(directory, "missing.corpus"))
	missing := contract
	missing.CorpusName = "ecs-test-missing-corpus"
	if _, err := findZstdCorpus("unused", missing); err == nil || !strings.Contains(err.Error(), "不在 ECS_ZSTD_CORPUS") {
		t.Fatalf("missing corpus diagnostic = %v", err)
	}
}

func TestZstdEnvironmentAndMeasurements(t *testing.T) {
	if got := normalizeCarriageReturnOutput([]byte("a\r\nb\rc")); got != "a\nb\nc" {
		t.Fatalf("normalized zstd output = %q", got)
	}
	t.Setenv("OMP_DYNAMIC", "polluted")
	t.Setenv("OPENSSL_MODULES", "polluted")
	env := benchmarkEnvironment(map[string]string{"OMP_NUM_THREADS": "4", "OPENSSL_CONF": "/dev/null", "NPB_TIMER_FLAG": "0"})
	values := make(map[string]string)
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; exists && (key == "LC_ALL" || key == "OMP_NUM_THREADS" || key == "OPENSSL_CONF" || key == "NPB_TIMER_FLAG") {
			t.Fatalf("duplicate benchmark environment key %q", key)
		}
		values[key] = value
	}
	if values["LC_ALL"] != "C" || values["LANG"] != "C" || values["OMP_NUM_THREADS"] != "4" || values["OPENSSL_CONF"] != "/dev/null" || values["NPB_TIMER_FLAG"] != "0" {
		t.Fatalf("benchmark environment overrides = %v", values)
	}
	for _, key := range []string{"OMP_DYNAMIC", "OPENSSL_MODULES"} {
		if _, ok := values[key]; ok {
			t.Fatalf("blocked %s leaked into benchmark environment", key)
		}
	}
	sample1 := zstdBenchmarkSample{Threads: 1, CompressMBPS: 40, DecompressMBPS: 20, Output: "one"}
	sampleN := zstdBenchmarkSample{Threads: 4, CompressMBPS: 80, DecompressMBPS: 40, Output: "many"}
	result := model.NewResult("zstd", "zstd")
	appendZstdMeasurements(&result, []zstdBenchmarkSample{sample1, sampleN}, 4)
	for _, key := range []string{"zstd_compress_1t_mb_s", "zstd_decompress_1t_mb_s", "zstd_compress_nt_mb_s", "zstd_decompress_nt_mb_s", "zstd_compress_scaling_ratio", "zstd_decompress_per_worker_efficiency_percent"} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing zstd measurement %q", key)
		}
	}
	failure := model.NewResult("zstd", "zstd")
	appendZstdMeasurements(&failure, []zstdBenchmarkSample{sample1, {}}, 4)
	if hasMeasurement(failure, "zstd_compress_nt_mb_s") || hasMeasurement(failure, "zstd_decompress_nt_mb_s") || hasMeasurement(failure, "zstd_compress_scaling_ratio") {
		t.Fatal("incomplete zstd sample emitted multi-worker measurements")
	}
	table := zstdThroughputTable([]zstdBenchmarkSample{sample1, sampleN}, 4)
	if table.Key != "benchmark.zstd.throughput" || len(table.ColumnKeys) != len(table.Columns) || len(table.Rows) != 2 {
		t.Fatalf("zstd table schema = %+v", table)
	}
	partial := zstdThroughputTable([]zstdBenchmarkSample{sample1, {}}, 4)
	if partial.Rows[1][1] != "—" || partial.Rows[1][2] != "—" {
		t.Fatalf("zstd partial row = %+v", partial.Rows[1])
	}
	single := model.NewResult("zstd", "zstd")
	appendZstdMeasurements(&single, []zstdBenchmarkSample{sample1, sample1}, 1)
	if hasMeasurement(single, "zstd_compress_scaling_ratio") {
		t.Fatal("single-core zstd scaling unexpectedly emitted")
	}
	singleTable := zstdThroughputTable([]zstdBenchmarkSample{sample1, sample1}, 1)
	if singleTable.Rows[0][3] != "不适用" || singleTable.Rows[1][3] != "不适用" {
		t.Fatalf("single-core zstd table = %+v", singleTable.Rows)
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

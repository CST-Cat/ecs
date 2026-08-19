package probe

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestParseOpenSSLOutputAndErrors(t *testing.T) {
	spec := openSSLAlgorithmSpecs[0]
	fixture := func(workers int) string {
		lines := make([]string, 0, workers*2+1)
		for index := 0; index < workers; index++ {
			lines = append(lines,
				fmt.Sprintf("+DT:%s:1:16384", spec.OutputName),
				fmt.Sprintf("+R:1000:%s:1.000000", spec.OutputName))
		}
		lines = append(lines, fmt.Sprintf("+F:25:%s:4000000000.00", spec.OutputName))
		return strings.Join(lines, "\n")
	}
	output := fixture(2)
	sample, err := parseOpenSSLSpeedOutput(output, spec, 2, 1, 16384)
	if err != nil || sample.Algorithm != spec.Key || sample.Workers != 2 || sample.ThroughputBPS != 4_000_000_000 || sample.ThroughputMBPS != 4000 {
		t.Fatalf("parsed OpenSSL sample = %+v/%v", sample, err)
	}
	for _, test := range []struct {
		name, output string
		workers      int
		marker       string
	}{
		{name: "DT count", output: fixture(1), workers: 2, marker: "+DT worker"},
		{name: "DT parameters", output: strings.Replace(output, ":1:16384", ":2:16384", 1), workers: 2, marker: "+DT 参数"},
		{name: "R invalid count or duration", output: strings.Replace(output, "+R:1000:", "+R:0:", 1), workers: 2, marker: "+R 计数或时长无效"},
		{name: "R worker count", output: strings.Replace(output, fmt.Sprintf("+R:1000:%s:1.000000\n", spec.OutputName), "", 1), workers: 2, marker: "+R worker"},
		{name: "unique F", output: strings.TrimSuffix(output, fmt.Sprintf("\n+F:25:%s:4000000000.00", spec.OutputName)), workers: 2, marker: "唯一聚合"},
		{name: "F algorithm", output: strings.Replace(output, "+F:25:AES-256-GCM", "+F:25:other", 1), workers: 2, marker: "+F algorithm"},
		{name: "F throughput", output: strings.Replace(output, "4000000000.00", "0.00", 1), workers: 2, marker: "throughput 无效"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseOpenSSLSpeedOutput(test.output, spec, test.workers, 1, 16384); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("OpenSSL error = %v, want %q", err, test.marker)
			}
		})
	}
	if _, err := executeOpenSSLSpeed(context.Background(), "unused", spec, 0, 1, 16384); err == nil || !strings.Contains(err.Error(), "必须为正数") {
		t.Fatalf("OpenSSL invalid execute input = %v", err)
	}
}

func TestOpenSSLMeasurementsTablesAndSummary(t *testing.T) {
	spec := openSSLAlgorithmSpecs[0]
	first := openSSLSpeedSample{Algorithm: spec.Key, Workers: 1, Duration: 5, BlockBytes: 16384, ThroughputBPS: 4_000_000_000, ThroughputMBPS: 4000}
	second := openSSLSpeedSample{Algorithm: spec.Key, Workers: 2, Duration: 5, BlockBytes: 16384, ThroughputBPS: 8_000_000_000, ThroughputMBPS: 8000}
	runs := map[string][]openSSLSpeedSample{spec.Key: {first, second}}
	result := model.NewResult("crypto", "crypto")
	appendOpenSSLMeasurements(&result, []openSSLAlgorithmSpec{spec}, runs, 2)
	for _, key := range []string{"openssl_aes_256_gcm_1w_mb_s", "openssl_aes_256_gcm_nw_mb_s", "openssl_aes_256_gcm_scaling_ratio"} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing OpenSSL measurement %q", key)
		}
	}
	table := openSSLResultsTable([]openSSLAlgorithmSpec{spec}, runs, 2)
	if table.Key != "benchmark.openssl.results" || len(table.ColumnKeys) != len(table.Columns) || len(table.Rows) != 2 || table.Rows[1][5] == "—" {
		t.Fatalf("OpenSSL result table = %+v", table)
	}
	if summary := openSSLSummary([]openSSLAlgorithmSpec{spec}, runs, 2); !strings.Contains(summary, "AES-256-GCM") || !strings.Contains(summary, "×") {
		t.Fatalf("OpenSSL summary = %q", summary)
	}
	singleRuns := map[string][]openSSLSpeedSample{spec.Key: {first, first}}
	single := openSSLResultsTable([]openSSLAlgorithmSpec{spec}, singleRuns, 1)
	if single.Rows[0][5] != "不适用" || !strings.Contains(openSSLSummary([]openSSLAlgorithmSpec{spec}, singleRuns, 1), "1W/NW") {
		t.Fatalf("single-core OpenSSL output = table:%v summary:%q", single.Rows[0], openSSLSummary([]openSSLAlgorithmSpec{spec}, singleRuns, 1))
	}
	partialRuns := map[string][]openSSLSpeedSample{spec.Key: {first, {}}}
	partialResult := model.NewResult("crypto", "crypto")
	appendOpenSSLMeasurements(&partialResult, []openSSLAlgorithmSpec{spec}, partialRuns, 2)
	if hasMeasurement(partialResult, "openssl_aes_256_gcm_nw_mb_s") || hasMeasurement(partialResult, "openssl_aes_256_gcm_scaling_ratio") {
		t.Fatal("incomplete OpenSSL sample emitted multi-worker measurements")
	}
	partial := openSSLResultsTable([]openSSLAlgorithmSpec{spec}, partialRuns, 2)
	if partial.Rows[1][4] != "—" {
		t.Fatalf("partial OpenSSL output = %+v", partial.Rows[1])
	}
	if got := openSSLSummary([]openSSLAlgorithmSpec{spec}, nil, 2); got != "OpenSSL speed 未产出有效密码学吞吐" {
		t.Fatalf("empty OpenSSL summary = %q", got)
	}
}

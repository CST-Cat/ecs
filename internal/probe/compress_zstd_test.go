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

func testZstdContract(corpus []byte) zstdBenchmarkContract {
	digest := sha256.Sum256(corpus)
	return zstdBenchmarkContract{
		Version:          "1.5.7",
		Level:            3,
		Seconds:          1,
		CorpusName:       "tiny.corpus",
		CorpusSHA256:     fmt.Sprintf("%x", digest[:]),
		CorpusSourceHash: "source-sha",
		CorpusBytes:      int64(len(corpus)),
	}
}

func TestParseZstdBenchmarkOutputStrictContract(t *testing.T) {
	contract := zstdBenchmarkContract{Version: "1.5.7", Level: 3, Seconds: 5, CorpusBytes: 211938580}
	valid := "bench 1.5.7 : input 211938580 bytes, 5 seconds, 0 KB blocks\n" +
		"-3     66524496 (3.186) 476.24 MB/s  666.3 MB/s  ecs-silesia-v1.corpus"
	sample, err := parseZstdBenchmarkOutput(valid, contract)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Version != "1.5.7" || sample.InputBytes != 211938580 || sample.CompressedSize != 66524496 ||
		sample.Ratio != 3.186 || sample.CompressMBPS != 476.24 || sample.DecompressMBPS != 666.3 {
		t.Fatalf("parsed zstd sample = %+v", sample)
	}

	for _, testCase := range []struct {
		name   string
		output string
	}{
		{name: "wrong version", output: strings.Replace(valid, "bench 1.5.7", "bench 1.5.6", 1)},
		{name: "wrong input", output: strings.Replace(valid, "211938580 bytes", "211938579 bytes", 1)},
		{name: "wrong duration", output: strings.Replace(valid, "5 seconds", "4 seconds", 1)},
		{name: "wrong level", output: strings.Replace(valid, "-3 ", "-4 ", 1)},
		{name: "duplicate result", output: valid + "\n" + strings.Split(valid, "\n")[1]},
		{name: "missing decompression", output: strings.Replace(valid, "  666.3 MB/s", "", 1)},
		{name: "invalid compression", output: strings.Replace(valid, "476.24", "NaN", 1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseZstdBenchmarkOutput(testCase.output, contract); err == nil {
				t.Fatalf("malformed output unexpectedly parsed: %q", testCase.output)
			}
		})
	}
}

func TestNormalizeCarriageReturnZstdProgressOutput(t *testing.T) {
	raw := []byte("Loading corpus...\r |-corpus : 12 -> \rbench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks\r-3 6 (2.000) 10.0 MB/s 20.0 MB/s corpus\r")
	normalized := normalizeCarriageReturnOutput(raw)
	if strings.Contains(normalized, "\r") || !strings.Contains(normalized, "bench 1.5.7") || !strings.Contains(normalized, "20.0 MB/s") {
		t.Fatalf("normalized zstd output = %q", normalized)
	}
}

func TestVerifyAndFindZstdCorpusByExactDigest(t *testing.T) {
	corpus := []byte("fixed-corpus")
	contract := testZstdContract(corpus)
	path := filepath.Join(t.TempDir(), contract.CorpusName)
	if err := os.WriteFile(path, corpus, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyZstdCorpus(path, contract); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ECS_ZSTD_CORPUS", path)
	if got, err := findZstdCorpus("/tmp/bin/zstd", contract); err != nil || got != path {
		t.Fatalf("findZstdCorpus = %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("changed-corpus"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyZstdCorpus(path, contract); err == nil {
		t.Fatal("changed corpus unexpectedly passed exact size/hash verification")
	}
}

func TestExecuteZstdBenchmarkUsesFixedArguments(t *testing.T) {
	corpus := []byte("fixed-corpus")
	contract := testZstdContract(corpus)
	directory := t.TempDir()
	corpusPath := filepath.Join(directory, contract.CorpusName)
	if err := os.WriteFile(corpusPath, corpus, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(directory, "zstd")
	script := `#!/bin/sh
case " $* " in
  *" -q -b3 -i1 -T4 "*) ;;
  *) echo "unexpected args: $*" >&2; exit 9 ;;
esac
printf '%s\n' 'bench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks'
printf '%s\n' '-3 6 (2.000) 40.0 MB/s 20.0 MB/s tiny.corpus'
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sample, err := executeZstdBenchmark(context.Background(), tool, corpusPath, 4, contract)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"-q", "-b3", "-i1", "-T4", corpusPath}
	if strings.Join(sample.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("zstd args = %v, want %v", sample.Args, wantArgs)
	}
	if sample.Threads != 4 || sample.CompressMBPS != 40 || sample.DecompressMBPS != 20 {
		t.Fatalf("zstd sample = %+v", sample)
	}
}

func TestRunZstdBenchmarkRecordsEvidenceParametersAndNoCompositeScore(t *testing.T) {
	corpus := []byte("fixed-corpus")
	contract := testZstdContract(corpus)
	directory := t.TempDir()
	corpusPath := filepath.Join(directory, contract.CorpusName)
	if err := os.WriteFile(corpusPath, corpus, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(directory, "zstd")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' '*** Zstandard CLI (64-bit) v1.5.7, by Yann Collet ***'
  exit 0
fi
threads=1
for arg in "$@"; do case "$arg" in -T*) threads=${arg#-T} ;; esac; done
compress=10.0
decompress=20.0
if [ "$threads" -gt 1 ]; then compress=30.0; decompress=21.0; fi
printf '%s\n' 'bench 1.5.7 : input 12 bytes, 1 seconds, 0 KB blocks'
printf '%s\n' "-3 6 (2.000) $compress MB/s $decompress MB/s tiny.corpus"
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result := runZstdBenchmark(context.Background(), Environment{}, tool, corpusPath, contract)
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 2 {
		t.Fatalf("zstd result status/evidence = %s %+v", result.Status, result.Evidence)
	}
	for _, key := range []string{
		"zstd_compress_1t_mb_s", "zstd_decompress_1t_mb_s", "zstd_compress_nt_mb_s", "zstd_decompress_nt_mb_s",
		"zstd_compress_scaling_ratio", "zstd_decompress_scaling_ratio",
		"zstd_compress_per_worker_efficiency_percent", "zstd_decompress_per_worker_efficiency_percent",
	} {
		if !hasMeasurement(result, key) {
			t.Errorf("zstd result missing measurement %q: %+v", key, result.Measurements)
		}
	}
	for _, key := range []string{"version", "binary_sha256", "method_version", "compression_level", "threads", "duration", "corpus_sha256", "arguments_1t", "arguments_nt"} {
		if resultField(result, key) == "" {
			t.Errorf("zstd result missing field %q: %+v", key, result.Fields)
		}
	}
	if len(result.TextBlocks) != 2 || len(result.Tables) != 1 || len(result.Tables[0].Rows) != 2 {
		t.Fatalf("zstd raw/table evidence = blocks:%d tables:%+v", len(result.TextBlocks), result.Tables)
	}
	for _, measurement := range result.Measurements {
		if strings.Contains(strings.ToLower(measurement.Key), "score") {
			t.Fatalf("zstd emitted a composite score: %+v", measurement)
		}
	}
}

func TestRunZstdBenchmarkRejectsDifferentVersionBeforeWorkload(t *testing.T) {
	corpus := []byte("fixed-corpus")
	contract := testZstdContract(corpus)
	directory := t.TempDir()
	corpusPath := filepath.Join(directory, contract.CorpusName)
	if err := os.WriteFile(corpusPath, corpus, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(directory, "zstd")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo '*** Zstandard CLI v1.5.6 ***'; exit 0; fi
echo 'benchmark must not run' >&2
exit 99
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result := runZstdBenchmark(context.Background(), Environment{}, tool, corpusPath, contract)
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 ||
		!strings.Contains(result.Summary, "1.5.6") || len(result.TextBlocks) != 0 {
		t.Fatalf("version mismatch result = %+v", result)
	}
}

func hasMeasurement(result model.Result, key string) bool {
	for _, measurement := range result.Measurements {
		if measurement.Key == key {
			return true
		}
	}
	return false
}

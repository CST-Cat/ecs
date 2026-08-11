package probe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecs/internal/model"
)

func openSSLSpeedOutput(spec openSSLAlgorithmSpec, workers, seconds, block int, throughput float64) string {
	var output strings.Builder
	for worker := 0; worker < workers; worker++ {
		fmt.Fprintf(&output, "+DT:%s:%d:%d\n", spec.OutputName, seconds, block)
		fmt.Fprintf(&output, "+R:%d:%s:%.6f\n", 1000+worker, spec.OutputName, float64(seconds))
	}
	fmt.Fprintf(&output, "+F:25:%s:%.2f\n", spec.OutputName, throughput)
	return output.String()
}

func TestParseOpenSSLSpeedOutputStrictFixedContract(t *testing.T) {
	for _, spec := range openSSLAlgorithmSpecs {
		t.Run(spec.Key, func(t *testing.T) {
			output := openSSLSpeedOutput(spec, 4, 5, 16384, 4_000_000_000)
			sample, err := parseOpenSSLSpeedOutput(output, spec, 4, 5, 16384)
			if err != nil {
				t.Fatal(err)
			}
			if sample.Algorithm != spec.Key || sample.Workers != 4 || sample.Duration != 5 ||
				sample.BlockBytes != 16384 || sample.ThroughputBPS != 4_000_000_000 || sample.ThroughputMBPS != 4000 {
				t.Fatalf("parsed OpenSSL sample = %+v", sample)
			}
		})
	}
}

func TestParseOpenSSLSpeedOutputRejectsIncomparableOrAmbiguousRun(t *testing.T) {
	spec := openSSLAlgorithmSpecs[0]
	valid := openSSLSpeedOutput(spec, 2, 5, 16384, 2_000_000_000)
	for _, testCase := range []struct {
		name   string
		output string
	}{
		{name: "wrong algorithm", output: strings.Replace(valid, spec.OutputName, "AES-128-GCM", 1)},
		{name: "wrong duration", output: strings.Replace(valid, ":5:16384", ":4:16384", 1)},
		{name: "wrong block", output: strings.Replace(valid, ":5:16384", ":5:8192", 1)},
		{name: "missing worker", output: strings.Replace(valid, "+DT:AES-256-GCM:5:16384\n", "", 1)},
		{name: "zero count", output: strings.Replace(valid, "+R:1000", "+R:0", 1)},
		{name: "short run", output: strings.Replace(valid, ":5.000000", ":1.000000", 1)},
		{name: "duplicate aggregate", output: valid + "+F:25:AES-256-GCM:1.00\n"},
		{name: "invalid aggregate", output: strings.Replace(valid, "2000000000.00", "NaN", 1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseOpenSSLSpeedOutput(testCase.output, spec, 2, 5, 16384); err == nil {
				t.Fatalf("malformed OpenSSL speed output unexpectedly parsed: %s", testCase.output)
			}
		})
	}
}

func TestExecuteOpenSSLSpeedUsesFixedArgumentsAndIsolatedEnvironment(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "openssl")
	script := `#!/bin/sh
expected='speed -elapsed -seconds 5 -bytes 16384 -mr -multi 4 -evp aes-256-gcm -aead'
test "$*" = "$expected" || { echo "unexpected args: $*" >&2; exit 8; }
test "$OPENSSL_CONF" = /dev/null || exit 9
test -d "$OPENSSL_MODULES" || exit 10
test -d "$OPENSSL_ENGINES" || exit 11
test -z "${OPENSSL_ia32cap:-}" || exit 12
i=0
while [ "$i" -lt 4 ]; do
  printf '%s\n' '+DT:AES-256-GCM:5:16384'
  printf '+R:%d:AES-256-GCM:5.000000\n' "$((1000+i))"
  i=$((i+1))
done
printf '%s\n' '+F:25:AES-256-GCM:4000000000.00'
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sample, err := executeOpenSSLSpeed(context.Background(), tool, openSSLAlgorithmSpecs[0], 4, 5, 16384)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"speed", "-elapsed", "-seconds", "5", "-bytes", "16384", "-mr", "-multi", "4", "-evp", "aes-256-gcm", "-aead"}
	if strings.Join(sample.Args, "\x00") != strings.Join(wantArgs, "\x00") || sample.ThroughputMBPS != 4000 {
		t.Fatalf("OpenSSL execution sample = %+v", sample)
	}
}

func TestQueryOpenSSLVersionRequiresUniqueOpenSSLVersionLine(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "openssl")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf '%s\\n' 'OpenSSL 3.5.7 9 Jun 2026'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, version, err := queryOpenSSLVersion(context.Background(), tool)
	if err != nil || version != "3.5.7" || !strings.Contains(output, "OpenSSL 3.5.7") {
		t.Fatalf("queryOpenSSLVersion = %q, %q, %v", output, version, err)
	}
}

func TestRunOpenSSLSpeedRecordsSixRawRunsScalingAndNoScore(t *testing.T) {
	directory := t.TempDir()
	originalPath := os.Getenv("PATH")
	tool := filepath.Join(directory, "openssl")
	script := `#!/bin/sh
if [ "$1" = version ]; then
  printf '%s\n' 'OpenSSL 3.5.7 9 Jun 2026'
  exit 0
fi
test "$1" = speed || exit 20
workers=0
seconds=0
block=0
algorithm=
previous=
for argument in "$@"; do
  case "$previous" in
    -multi) workers=$argument ;;
    -seconds) seconds=$argument ;;
    -bytes) block=$argument ;;
    -evp) algorithm=$argument ;;
  esac
  previous=$argument
done
test "$seconds" = 5 || exit 21
test "$block" = 16384 || exit 22
case "$algorithm" in
  aes-256-gcm) output_name=AES-256-GCM; base=1000000000 ;;
  chacha20-poly1305) output_name=ChaCha20-Poly1305; base=600000000 ;;
  sha256) output_name=sha256; base=800000000 ;;
  *) exit 23 ;;
esac
i=0
while [ "$i" -lt "$workers" ]; do
  printf '+DT:%s:5:16384\n' "$output_name"
  printf '+R:%d:%s:5.000000\n' "$((1000+i))" "$output_name"
  i=$((i+1))
done
printf '+F:25:%s:%d.00\n' "$output_name" "$((base*workers))"
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	result := runOpenSSLSpeed(context.Background(), Environment{}, tool, openSSLAlgorithmSpecs)
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 6 || result.Evidence.Expected != 6 {
		t.Fatalf("crypto status/evidence = %s %+v failures=%+v notes=%v", result.Status, result.Evidence, result.Failures, result.Notes)
	}
	for _, key := range []string{
		"openssl_aes_256_gcm_1w_mb_s", "openssl_aes_256_gcm_nw_mb_s", "openssl_aes_256_gcm_scaling_ratio",
		"openssl_chacha20_poly1305_1w_mb_s", "openssl_chacha20_poly1305_nw_mb_s", "openssl_chacha20_poly1305_scaling_ratio",
		"openssl_sha_256_1w_mb_s", "openssl_sha_256_nw_mb_s", "openssl_sha_256_scaling_ratio",
	} {
		if !hasMeasurement(result, key) {
			t.Errorf("crypto result missing measurement %q: %+v", key, result.Measurements)
		}
	}
	for _, key := range []string{
		"version", "binary_sha256", "method_version", "algorithms", "block_size", "duration", "workers",
		"arguments_aes_256_gcm_1w", "arguments_aes_256_gcm_nw",
		"arguments_chacha20_poly1305_1w", "arguments_chacha20_poly1305_nw",
		"arguments_sha_256_1w", "arguments_sha_256_nw",
	} {
		if resultField(result, key) == "" {
			t.Errorf("crypto result missing field %q: %+v", key, result.Fields)
		}
	}
	if len(result.TextBlocks) != 6 || len(result.Tables) != 1 || len(result.Tables[0].Rows) != 6 {
		t.Fatalf("crypto raw/table evidence = blocks:%d tables:%+v", len(result.TextBlocks), result.Tables)
	}
	for _, measurement := range result.Measurements {
		if strings.Contains(strings.ToLower(measurement.Key), "score") {
			t.Fatalf("crypto emitted composite score: %+v", measurement)
		}
	}
}

func TestRunOpenSSLSpeedRejectsDifferentVersionBeforeWorkload(t *testing.T) {
	directory := t.TempDir()
	tool := filepath.Join(directory, "openssl")
	script := `#!/bin/sh
if [ "$1" = version ]; then printf '%s\n' 'OpenSSL 3.5.6'; exit 0; fi
echo 'speed must not run' >&2
exit 99
`
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result := runOpenSSLSpeed(context.Background(), Environment{}, tool, openSSLAlgorithmSpecs)
	if result.Status != model.StatusWarning || result.Evidence == nil || result.Evidence.Valid != 0 ||
		!strings.Contains(result.Summary, "3.5.6") || len(result.TextBlocks) != 0 {
		t.Fatalf("OpenSSL version mismatch result = %+v", result)
	}
}

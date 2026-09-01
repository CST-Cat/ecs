package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestCryptoProducerBuildsStableResult(t *testing.T) {
	path := fakeOpenSSLBinary(t)
	spec := openSSLAlgorithmSpecs[0]
	result := runOpenSSLSpeedWithAllowance(context.Background(), Environment{}, path, []openSSLAlgorithmSpec{spec}, cpuAllowance{Visible: 2, Threads: 2})

	if result.Title != "module.crypto.title" || result.Description != "probe.crypto.description" || result.Status != model.StatusOK {
		t.Fatalf("crypto direct result metadata/status = %+v", result)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.crypto.profile" || result.Methodology.ComparisonScope != "probe.crypto.comparison_scope" {
		t.Fatalf("crypto methodology = %+v", result.Methodology)
	}
	if result.Evidence == nil || result.Evidence.Valid != 2 || result.Evidence.Expected != 2 {
		t.Fatalf("crypto evidence = %+v", result.Evidence)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.crypto.summary.values" {
		t.Fatalf("crypto summary = %+v", result.SummaryMessages)
	}

	for _, field := range result.Fields {
		if field.Label == "" || !strings.HasPrefix(field.Label, "probe.crypto.field.") {
			t.Fatalf("crypto field label = %+v", field)
		}
		if field.Key == "arguments_aes_256_gcm_1w" {
			if _, ok := field.Value.Raw(); !ok {
				t.Fatalf("crypto argument field lost raw variant: %+v", field.Value)
			}
		}
	}
	if !hasCryptoMeasurement(result, "openssl_aes_256_gcm_1w_mb_s", "probe.crypto.metric.aes_256_gcm.1w") ||
		!hasCryptoMeasurement(result, "openssl_aes_256_gcm_nw_mb_s", "probe.crypto.metric.aes_256_gcm.nw") ||
		!hasCryptoMeasurement(result, "openssl_aes_256_gcm_scaling_ratio", "probe.crypto.metric.aes_256_gcm.scaling") {
		t.Fatalf("crypto measurements = %+v", result.Measurements)
	}
	if len(result.TextBlocks) != 2 || result.TextBlocks[0].Title != "probe.crypto.raw_output" || result.TextBlocks[1].Title != "probe.crypto.raw_output" {
		t.Fatalf("crypto raw output blocks = %+v", result.TextBlocks)
	}
	if len(result.Sources) != 2 || result.Sources[0].Purpose != "probe.crypto.source.openssl" || result.Sources[1].Purpose != "probe.crypto.source.version" {
		t.Fatalf("crypto sources = %+v", result.Sources)
	}
	for _, note := range result.Notes {
		if !strings.HasPrefix(note, "probe.crypto.note.") {
			t.Fatalf("crypto note is not a stable key: %q", note)
		}
	}
	if len(result.Tables) != 1 || result.Tables[0].Title != "probe.crypto.table.title" || len(result.Tables[0].Rows) != 2 || result.Tables[0].Rows[0][1].Text() != "1W" || result.Tables[0].Rows[1][1].Text() != "NW(2W)" {
		t.Fatalf("crypto table = %+v", result.Tables)
	}
	for _, column := range result.Tables[0].Columns {
		if !strings.HasPrefix(column.Label, "probe.crypto.column.") {
			t.Fatalf("crypto column = %+v", column)
		}
	}
	if _, ok := result.Tables[0].Rows[0][1].Raw(); !ok {
		t.Fatalf("crypto worker context should be raw: %+v", result.Tables[0].Rows[0][1])
	}
}

func TestCryptoProducerOwnsAllComparisonParameters(t *testing.T) {
	path := fakeOpenSSLBinary(t)
	result := runOpenSSLSpeedWithAllowance(context.Background(), Environment{}, path, openSSLAlgorithmSpecs, cpuAllowance{Visible: 2, Threads: 2})
	assertProducerParameterScope(t, result,
		"tool_version", "method_version", "algorithms", "block_size", "duration", "workers", "timing", "machine_output",
		"arguments_aes_256_gcm_1w", "arguments_aes_256_gcm_nw",
		"arguments_chacha20_poly1305_1w", "arguments_chacha20_poly1305_nw",
		"arguments_sha_256_1w", "arguments_sha_256_nw",
	)
	parameters := result.Methodology.Parameters
	if parameters["tool_version"] != "OpenSSL 3.5.7" || parameters["method_version"] != openSSLMethodVersion || parameters["algorithms"] != "AES-256-GCM / ChaCha20-Poly1305 / SHA-256" || parameters["block_size"] != "16384 bytes" || parameters["duration"] != "5s" || parameters["workers"] != "1 / 2" || parameters["timing"] != "-elapsed (wall clock)" || parameters["machine_output"] != "-mr" {
		t.Fatalf("crypto comparison parameters = %v", parameters)
	}
	for _, spec := range openSSLAlgorithmSpecs {
		for _, contextKey := range []string{"1w", "nw"} {
			fieldKey := "arguments_" + spec.Key + "_" + contextKey
			value, ok := cryptoFieldValue(result, fieldKey)
			if !ok || value == "" {
				t.Fatalf("crypto argument field %q missing from producer result", fieldKey)
			}
			if parameters[fieldKey] != value {
				t.Fatalf("crypto argument parameter %q = %q, want %q", fieldKey, parameters[fieldKey], value)
			}
		}
	}
}

func TestCryptoProducerSingleCoreAndFailureContracts(t *testing.T) {
	path := fakeOpenSSLBinary(t)
	result := runOpenSSLSpeedWithAllowance(context.Background(), Environment{}, path, openSSLAlgorithmSpecs[:1], cpuAllowance{Visible: 1, Threads: 1})
	if result.Status != model.StatusOK || result.Evidence == nil || result.Evidence.Valid != 1 || result.Evidence.Expected != 1 {
		t.Fatalf("single-core crypto result = status:%s evidence:%+v failures:%+v", result.Status, result.Evidence, result.Failures)
	}
	if !slices.Contains(result.Notes, "probe.crypto.note.single_core") || len(result.Tables) != 1 || result.Tables[0].Rows[1][1].Text() != "NW(1W-reused)" || result.Tables[0].Rows[0][5].Text() != "na" {
		t.Fatalf("single-core crypto contract = notes:%v table:%+v", result.Notes, result.Tables)
	}
	if len(result.TextBlocks) != 1 {
		t.Fatalf("single-core crypto should retain one physical output block: %+v", result.TextBlocks)
	}

	missing := missingOpenSSLResult(errors.New("openssl missing"))
	if missing.Title != "module.crypto.title" || missing.Status != model.StatusWarning || len(missing.Failures) != 1 || missing.Failures[0].Category != model.FailureToolMissing || len(missing.SummaryMessages) != 1 || missing.SummaryMessages[0].Key != "probe.crypto.summary.none" {
		t.Fatalf("missing crypto result = %+v", missing)
	}
	if !slices.Contains(missing.Notes, "probe.crypto.note.tool_missing") {
		t.Fatalf("missing crypto notes = %v", missing.Notes)
	}
}

func TestCryptoProducerVersionMismatchKeepsStableContract(t *testing.T) {
	path := fakeOpenSSLVersionMismatchBinary(t)
	result := runOpenSSLSpeedWithAllowance(context.Background(), Environment{}, path, openSSLAlgorithmSpecs[:1], cpuAllowance{Visible: 2, Threads: 2})

	if result.Title != "module.crypto.title" || result.Description != "probe.crypto.description" || result.Status != model.StatusWarning {
		t.Fatalf("crypto version mismatch metadata/status = %+v", result)
	}
	assertProducerParameterScope(t, result, "tool_version")
	if result.Methodology.Parameters["tool_version"] != "OpenSSL 3.4.0" {
		t.Fatalf("crypto version mismatch comparison parameters = %v", result.Methodology.Parameters)
	}
	if result.Methodology.Label != "methodology.standard-benchmark" || result.Methodology.Profile != "probe.crypto.profile" || result.Methodology.ComparisonScope != "probe.crypto.comparison_scope" {
		t.Fatalf("crypto version mismatch methodology = %+v", result.Methodology)
	}
	if len(result.Failures) != 1 || result.Failures[0].Stage != "version_check" || result.Evidence == nil || result.Evidence.Valid != 0 || result.Evidence.Expected != 2 {
		t.Fatalf("crypto version mismatch failure/evidence = %+v/%+v", result.Failures, result.Evidence)
	}
	if len(result.SummaryMessages) != 1 || result.SummaryMessages[0].Key != "probe.crypto.summary.version_mismatch" {
		t.Fatalf("crypto version mismatch summary = %+v", result.SummaryMessages)
	}
	if !slices.Contains(result.Notes, "probe.crypto.note.version_mismatch") {
		t.Fatalf("crypto version mismatch notes = %v", result.Notes)
	}
	if len(result.Fields) != 3 {
		t.Fatalf("crypto version mismatch fields = %+v", result.Fields)
	}
	for _, field := range result.Fields {
		if !strings.HasPrefix(field.Label, "probe.crypto.field.") {
			t.Fatalf("crypto version mismatch field label = %+v", field)
		}
		if _, ok := field.Value.Raw(); !ok {
			t.Fatalf("crypto version mismatch field should remain raw = %+v", field)
		}
	}
	if len(result.Tables) != 0 || len(result.TextBlocks) != 0 {
		t.Fatalf("crypto version mismatch should not expose benchmark output = tables:%+v blocks:%+v", result.Tables, result.TextBlocks)
	}
}

func hasCryptoMeasurement(result model.Result, key, label string) bool {
	for _, measurement := range result.Measurements {
		if measurement.Key == key && measurement.Label == label {
			return true
		}
	}
	return false
}

func cryptoFieldValue(result model.Result, key string) (string, bool) {
	for _, field := range result.Fields {
		if field.Key == key {
			return field.Value.Text(), true
		}
	}
	return "", false
}

func fakeOpenSSLBinary(t *testing.T) string {
	return fakeOpenSSLBinaryWithVersion(t, "3.5.7")
}

func fakeOpenSSLVersionMismatchBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openssl")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  printf 'OpenSSL 3.4.0\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeOpenSSLBinaryWithVersion(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openssl")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  printf 'OpenSSL " + version + "\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" != \"speed\" ]; then\n" +
		"  exit 2\n" +
		"fi\n" +
		"workers=\"$9\"\n" +
		"shift 10\n" +
		"algorithm=AES-256-GCM\n" +
		"case \"$1\" in\n" +
		"  aes-256-gcm) algorithm=AES-256-GCM ;;\n" +
		"  chacha20-poly1305) algorithm=ChaCha20-Poly1305 ;;\n" +
		"  sha256) algorithm=sha256 ;;\n" +
		"esac\n" +
		"i=0\n" +
		"while [ \"$i\" -lt \"$workers\" ]; do\n" +
		"  printf '+DT:%s:5:16384\\n' \"$algorithm\"\n" +
		"  printf '+R:1000:%s:5.000000\\n' \"$algorithm\"\n" +
		"  i=$((i + 1))\n" +
		"done\n" +
		"printf '+F:25:%s:4000000000.00\\n' \"$algorithm\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

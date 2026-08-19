package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestParseOpenSSLOutputAddsThroughputMeasurement(t *testing.T) {
	spec := openSSLAlgorithmSpecs[0]
	output := "+DT:AES-256-GCM:1:16384\n" +
		"+R:1000:AES-256-GCM:1.000000\n" +
		"+F:25:AES-256-GCM:4000000000.00\n"
	sample, err := parseOpenSSLSpeedOutput(output, spec, 1, 1, 16384)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Algorithm != spec.Key || sample.ThroughputMBPS != 4000 {
		t.Fatalf("parsed OpenSSL sample = %+v", sample)
	}

	result := model.NewResult("crypto", "crypto")
	appendOpenSSLMeasurements(&result, []openSSLAlgorithmSpec{spec}, map[string][]openSSLSpeedSample{
		spec.Key: {sample},
	}, 1)
	if !hasMeasurement(result, "openssl_aes_256_gcm_1w_mb_s") {
		t.Fatalf("OpenSSL measurements = %+v", result.Measurements)
	}
}

func TestParseOpenSSLOutputRejectsMissingAggregate(t *testing.T) {
	spec := openSSLAlgorithmSpecs[0]
	output := "+DT:AES-256-GCM:1:16384\n" +
		"+R:1000:AES-256-GCM:1.000000\n"
	if _, err := parseOpenSSLSpeedOutput(output, spec, 1, 1, 16384); err == nil {
		t.Fatal("OpenSSL output without aggregate unexpectedly parsed")
	}
}

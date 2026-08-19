package probe

import "testing"

func TestParseIPerfTCPJSONProducesBasicResult(t *testing.T) {
	raw := []byte(`{
		"start":{"test_start":{"protocol":"TCP","reverse":0}},
		"end":{"sum_sent":{"bytes":100,"bits_per_second":1000000000,"retransmits":3,"seconds":1}}
	}`)

	result := parseIPerfTCPJSON(raw, 5200, false)
	if result.Error != "" || result.Mbps != 1000 || result.Bytes != 100 || result.Retransmits != 3 {
		t.Fatalf("iperf3 parsed result = %+v", result)
	}
}

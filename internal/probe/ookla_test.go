package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestOoklaJSONFixturesAndMeasurements(t *testing.T) {
	full, err := parseOoklaJSON([]byte(`{
		"ping":{"jitter":1.5,"latency":8.5},
		"download":{"bandwidth":125000000},
		"upload":{"bandwidth":25000000},
		"packetLoss":0,
		"isp":"Fixture ISP",
		"interface":{"externalIp":"203.0.113.9"},
		"server":{"id":42,"name":"Example","location":"London","country":"GB"}
	}`))
	if err != nil || full.ISP != "Fixture ISP" || full.Interface.ExternalIP != "203.0.113.9" || formatOoklaServer(full) != "Example · London · GB" {
		t.Fatalf("Ookla parsed metadata = %+v, err=%v", full, err)
	}
	result := model.NewResult("ookla", "Ookla")
	appendOoklaMeasurementsFor(&result, full, "", "")
	values := make(map[string]model.Measurement, len(result.Measurements))
	for _, measurement := range result.Measurements {
		values[measurement.Key] = measurement
	}
	if len(values) != 5 || values["ookla_ping_ms"].Value != 8.5 || values["ookla_jitter_ms"].Value != 1.5 || values["ookla_download_mbps"].Value != 1000 || values["ookla_upload_mbps"].Value != 200 || values["ookla_packet_loss_percent"].Value != 0 {
		t.Fatalf("Ookla measurements = %+v", values)
	}
	if values["ookla_ping_ms"].Unit != "ms" || values["ookla_download_mbps"].Unit != "Mbps" || values["ookla_packet_loss_percent"].Unit != "%" {
		t.Fatalf("Ookla measurement units = %+v", values)
	}
	if values["ookla_ping_ms"].HigherIsBetter == nil || *values["ookla_ping_ms"].HigherIsBetter ||
		values["ookla_jitter_ms"].HigherIsBetter == nil || *values["ookla_jitter_ms"].HigherIsBetter ||
		values["ookla_packet_loss_percent"].HigherIsBetter == nil || *values["ookla_packet_loss_percent"].HigherIsBetter ||
		values["ookla_download_mbps"].HigherIsBetter == nil || !*values["ookla_download_mbps"].HigherIsBetter ||
		values["ookla_upload_mbps"].HigherIsBetter == nil || !*values["ookla_upload_mbps"].HigherIsBetter {
		t.Fatalf("Ookla measurement directions = %+v", values)
	}
	if ooklaCarrierKey("联通") != "unicom" {
		t.Fatalf("Ookla carrier key = %q", ooklaCarrierKey("联通"))
	}

	partial, err := parseOoklaJSON([]byte(`{"ping":{"latency":8.5}}`))
	if err != nil || !ooklaHasValidMetric(partial) || ooklaMeasurementsComplete(partial) {
		t.Fatalf("partial Ookla result = %+v, err=%v", partial, err)
	}
	partialResult := model.NewResult("ookla", "Ookla")
	appendOoklaMeasurementsFor(&partialResult, partial, "", "")
	if len(partialResult.Measurements) != 1 {
		t.Fatalf("partial Ookla measurements = %+v", partialResult.Measurements)
	}
	empty, err := parseOoklaJSON([]byte(`{}`))
	if err != nil || ooklaHasValidMetric(empty) {
		t.Fatalf("empty Ookla result = %+v, err=%v", empty, err)
	}
	for _, raw := range []string{"{bad}", "[]"} {
		if _, err := parseOoklaJSON([]byte(raw)); err == nil {
			t.Fatalf("invalid Ookla JSON %q was accepted", raw)
		}
	}
}

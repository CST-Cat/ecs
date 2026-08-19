package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestParseOoklaJSONAppendsBasicMeasurements(t *testing.T) {
	parsed, err := parseOoklaJSON([]byte(`{
		"ping":{"jitter":0,"latency":8.5},
		"download":{"bandwidth":125000000},
		"upload":{"bandwidth":25000000},
		"packetLoss":0,
		"server":{"id":42,"name":"Example"}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	result := model.NewResult("ookla", "Ookla")
	appendOoklaMeasurementsFor(&result, parsed, "", "")
	if len(result.Measurements) != 5 {
		t.Fatalf("Ookla measurements = %+v", result.Measurements)
	}
	values := make(map[string]float64, len(result.Measurements))
	for _, measurement := range result.Measurements {
		values[measurement.Key] = measurement.Value
	}
	if values["ookla_download_mbps"] != 1000 || values["ookla_packet_loss_percent"] != 0 {
		t.Fatalf("Ookla normalized values = %v", values)
	}
}

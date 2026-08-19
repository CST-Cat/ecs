package probe

import (
	"testing"

	"ecs/internal/model"
)

func TestSysbenchOutputAddsBasicMeasurements(t *testing.T) {
	output := "events per second: 1234.50\n" +
		"total number of events: 2469\n" +
		"95th percentile: 1.25\n"
	rate, rateOK := parseFirstFloat(sysbenchEventsRatePattern, output)
	events, eventsOK := parseFirstUint(sysbenchEventsPattern, output)
	p95, p95OK := parseFirstFloat(sysbenchP95Pattern, output)
	if !rateOK || !eventsOK || !p95OK || rate != 1234.5 || events != 2469 || p95 != 1.25 {
		t.Fatalf("parsed sysbench output = rate:%v/%v events:%v/%v p95:%v/%v", rate, rateOK, events, eventsOK, p95, p95OK)
	}

	result := model.NewResult("cpu", "cpu")
	sample := sysbenchCPUResult{Rate: rate, Events: events, P95MS: p95}
	appendSysbenchCPUMeasurements(&result, sample, sysbenchCPUResult{}, 1)
	if !hasMeasurement(result, "sysbench_cpu_single_events_s") {
		t.Fatalf("sysbench measurements = %+v", result.Measurements)
	}
}

func TestSysbenchOutputRejectsMissingP95(t *testing.T) {
	output := "events per second: 1234.50\ntotal number of events: 2469\n"
	if _, ok := parseFirstFloat(sysbenchP95Pattern, output); ok {
		t.Fatal("sysbench output without P95 unexpectedly parsed")
	}
}

package probe

import (
	"strings"
	"testing"

	"ecs/internal/model"
)

func TestSysbenchParserAndMeasurements(t *testing.T) {
	output := "events per second: 1234.50\n" +
		"total number of events: 2469\n" +
		"95th percentile: 1.25\n"
	rate, rateOK := parseFirstFloat(sysbenchEventsRatePattern, output)
	events, eventsOK := parseFirstUint(sysbenchEventsPattern, output)
	p95, p95OK := parseFirstFloat(sysbenchP95Pattern, output)
	if !rateOK || !eventsOK || !p95OK || rate != 1234.5 || events != 2469 || p95 != 1.25 {
		t.Fatalf("parsed sysbench output = rate:%v/%v events:%v/%v p95:%v/%v", rate, rateOK, events, eventsOK, p95, p95OK)
	}
	if _, ok := parseFirstFloat(sysbenchP95Pattern, strings.Replace(output, "95th percentile: 1.25", "95th percentile: bad", 1)); ok {
		t.Fatal("invalid p95 unexpectedly parsed")
	}
	result := model.NewResult("cpu", "cpu")
	appendSysbenchCPUMeasurements(&result,
		sysbenchCPUResult{Rate: rate, Events: events, P95MS: p95},
		sysbenchCPUResult{Rate: 2469, Events: 4938, P95MS: 2.5}, 4)
	for _, key := range []string{"sysbench_cpu_single_events_s", "sysbench_cpu_single_p95_ms", "sysbench_cpu_multi_events_s", "sysbench_cpu_scaling_ratio", "sysbench_cpu_per_thread_efficiency_percent", "sysbench_cpu_multi_p95_ms"} {
		if !hasMeasurement(result, key) {
			t.Fatalf("missing sysbench measurement %q: %+v", key, result.Measurements)
		}
	}
	if result.Measurements[1].HigherIsBetter == nil || *result.Measurements[1].HigherIsBetter {
		t.Fatal("p95 direction was not lower-is-better")
	}
	if got := formatSysbenchEvents(sysbenchCPUResult{}); got != "unavailable" || formatSysbenchEvents(sysbenchCPUResult{Rate: 1, Events: 2}) != "2" {
		t.Fatalf("sysbench event formatting = %q", got)
	}
	single := model.NewResult("cpu", "cpu")
	appendSysbenchCPUMeasurements(&single, sysbenchCPUResult{Rate: 1, Events: 2, P95MS: 3}, sysbenchCPUResult{}, 1)
	if hasMeasurement(single, "sysbench_cpu_multi_events_s") || hasMeasurement(single, "sysbench_cpu_scaling_ratio") || benchmarkThreadField(1) != "1 / 1" || !strings.Contains(benchmarkThreadField(4), "1 / 4") {
		t.Fatalf("single/multi thread assembly = %+v", single.Measurements)
	}
}

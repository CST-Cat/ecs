package compare

import (
	"testing"

	"ecs/internal/model"
)

// platformReport builds a report for one platform target. Only the
// platform-identifying comparison parameter differs between the Linux and
// FreeBSD variants, which is exactly the situation the signature must detect.
func platformReport(id, moduleID, parameter, value string) model.Report {
	report := comparisonTestReport(id, 100, "m-v1", "same", "rate", true)
	report.Results[0].ID = moduleID
	report.Results[0].Methodology.Parameters[parameter] = value
	return report
}

// The FreeBSD port keeps a module descriptor and a measurement.method that are
// platform-independent, so the platform identity has to travel in the
// comparison parameters the probes actually emit. Linux fio prefers
// io_uring/libaio while FreeBSD fio only has posixaio; Linux routing runs
// NextTrace Tiny while FreeBSD routing runs the base-system traceroute. If a
// future change dropped either parameter, the compare layer would happily
// compute a "performance change" across two different engines and the
// regression would only be visible as a wrong number, never as an error.
func TestBuildDoesNotFlattenCrossPlatformProvenance(t *testing.T) {
	cases := []struct {
		name      string
		moduleID  string
		parameter string
		linux     string
		freebsd   string
	}{
		{
			name:      "disk ioengine",
			moduleID:  "disk",
			parameter: "ioengine",
			linux:     "io_uring",
			freebsd:   "posixaio",
		},
		{
			name:      "route adapter",
			moduleID:  "route",
			parameter: "adapter",
			linux:     "nexttrace-tiny-json-v1",
			freebsd:   "freebsd-traceroute-text-v1",
		},
		{
			name:      "backtrace adapter",
			moduleID:  "backtrace",
			parameter: "adapter",
			linux:     "nexttrace-tiny-json-v1",
			freebsd:   "freebsd-traceroute-text-v1",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			linux := platformReport("linux", test.moduleID, test.parameter, test.linux)
			freebsd := platformReport("freebsd", test.moduleID, test.parameter, test.freebsd)

			data, err := Build([]model.Report{linux, freebsd}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(data.Modules) != 1 {
				t.Fatalf("modules = %+v", data.Modules)
			}
			module := data.Modules[0]
			if module.ID != test.moduleID {
				t.Fatalf("module id = %q, want %q", module.ID, test.moduleID)
			}
			if module.Comparability != NotComparable || len(module.Metrics) != 0 {
				t.Fatalf("cross-platform reports were flattened into a comparison: comparability=%q metrics=%+v", module.Comparability, module.Metrics)
			}
			if len(module.MetricIssues) != 1 {
				t.Fatalf("metric issues = %+v", module.MetricIssues)
			}
			issue := module.MetricIssues[0]
			if issue.Reason != "method_or_parameters_mismatch" {
				t.Fatalf("issue reason = %q, want method_or_parameters_mismatch", issue.Reason)
			}
			wantField := "parameter:" + test.parameter
			if len(issue.Differences) != 1 || issue.Differences[0].Field != wantField {
				t.Fatalf("signature differences = %+v, want only %q", issue.Differences, wantField)
			}
			values := issue.Differences[0].Values
			if len(values) != 2 || values[0].Report != 0 || values[0].Value != test.linux ||
				values[1].Report != 1 || values[1].Value != test.freebsd {
				t.Fatalf("difference values = %+v, want %q on report 0 and %q on report 1", values, test.linux, test.freebsd)
			}
		})
	}
}

// The negative control: once both platforms report the same engine, the same
// two reports must compare normally. Without it, an over-broad signature change
// that rejected every pair would still pass the test above.
func TestBuildComparesSameEngineAcrossPlatforms(t *testing.T) {
	linux := platformReport("linux", "disk", "ioengine", "posixaio")
	freebsd := platformReport("freebsd", "disk", "ioengine", "posixaio")

	data, err := Build([]model.Report{linux, freebsd}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Modules) != 1 {
		t.Fatalf("modules = %+v", data.Modules)
	}
	module := data.Modules[0]
	if module.Comparability != Comparable || len(module.Metrics) != 1 || len(module.MetricIssues) != 0 {
		t.Fatalf("same-engine reports were not compared: comparability=%q metrics=%+v issues=%+v", module.Comparability, module.Metrics, module.MetricIssues)
	}
	if got := module.Metrics[0].Parameters["ioengine"]; got != "posixaio" {
		t.Fatalf("metric engine parameter = %q, want posixaio", got)
	}
}

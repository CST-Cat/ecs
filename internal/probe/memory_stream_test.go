package probe

import (
	"strings"
	"testing"
)

const streamFixture = `-------------------------------------------------------------
STREAM version $Revision: 5.10 $
-------------------------------------------------------------
Number of Threads requested = 4
Function      Best Rate MiB/s     Avg time     Min time     Max time
Copy:           1000.0         0.0100       0.0090       0.0110
Scale:          2000.0         0.0200       0.0190       0.0210
Add:            3000.0         0.0300       0.0290       0.0310
Triad:          4000.0         0.0400       0.0390       0.0410
Solution Validates
`

func TestParseStreamOutputProducesNormalizedSample(t *testing.T) {
	parsed, err := parseStreamOutput(streamFixture)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Unit != "MiB/s" || parsed.RequestedThreads != 4 || len(parsed.Samples) != 4 {
		t.Fatalf("parsed STREAM output = %+v", parsed)
	}
	sample, ok := parsed.Samples["Copy"]
	if !ok || sample.RateMiBS != 1000 || sample.RawRate != 1000 || sample.AvgTime <= 0 {
		t.Fatalf("normalized Copy sample = %+v", sample)
	}

	// The parsed numeric value is the machine value used by the report table;
	// no executable or host benchmark is needed to exercise this boundary.
	table := streamMemoryTable([]streamMemoryRun{{Context: "1t", Threads: 1, Sample: parsed}})
	if len(table.Rows) != 4 || table.Rows[0][1] == "—" || table.Rows[0][2] != "MiB/s" {
		t.Fatalf("STREAM report table = %+v", table)
	}
}

func TestParseStreamOutputRejectsMalformedResult(t *testing.T) {
	malformed := strings.Replace(streamFixture, "Copy:", "Broken:", 1)
	if _, err := parseStreamOutput(malformed); err == nil {
		t.Fatal("malformed STREAM output unexpectedly parsed")
	}
}

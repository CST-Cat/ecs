package probe

import (
	"context"
	"errors"
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

func TestStreamParserAndTable(t *testing.T) {
	parsed, err := parseStreamOutput(streamFixture)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Unit != "MiB/s" || parsed.RequestedThreads != 4 || len(parsed.Samples) != 4 || parsed.Samples["Copy"].RateMiBS != 1000 {
		t.Fatalf("parsed STREAM output = %+v", parsed)
	}
	mb, err := parseStreamOutput(strings.Replace(streamFixture, "MiB/s", "MB/s", 1))
	if err != nil || mb.Samples["Copy"].RateMiBS < 953 || mb.Samples["Copy"].RateMiBS > 954 {
		t.Fatalf("STREAM unit conversion = %+v/%v", mb, err)
	}
	for _, test := range []struct {
		name, output, marker string
	}{
		{name: "unique header", output: strings.Replace(streamFixture, "Function      Best Rate MiB/s     Avg time     Min time     Max time\n", "", 1), marker: "Best Rate"},
		{name: "row format", output: strings.Replace(streamFixture, "Copy:           1000.0         0.0100       0.0090       0.0110", "Copy: broken", 1), marker: "行格式无效"},
		{name: "duplicate kernel", output: streamFixture + "Copy: 1000 0.0100 0.0090 0.0110\n", marker: "重复的 Copy"},
		{name: "invalid number", output: strings.Replace(streamFixture, "1000.0", "bad", 1), marker: "无效数值"},
		{name: "zero rate", output: strings.Replace(streamFixture, "1000.0", "0", 1), marker: "Best Rate 必须为正数"},
		{name: "zero time", output: strings.Replace(streamFixture, "0.0090", "0", 1), marker: "时间统计必须为正数"},
		{name: "time order", output: strings.Replace(streamFixture, "0.0100       0.0090       0.0110", "0.0100       0.0120       0.0110", 1), marker: "时间统计顺序无效"},
		{name: "missing kernel", output: strings.Replace(streamFixture, "Copy:", "Broken:", 1), marker: "缺少 Copy 行"},
		{name: "multiple threads", output: streamFixture + "Number of Threads requested = 4\n", marker: "多个线程数声明"},
		{name: "invalid threads", output: strings.Replace(streamFixture, "Number of Threads requested = 4", "Number of Threads requested = 0", 1), marker: "线程数声明无效"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseStreamOutput(test.output); err == nil || !strings.Contains(err.Error(), test.marker) {
				t.Fatalf("STREAM error = %v, want %q", err, test.marker)
			}
		})
	}
	withoutThreads := strings.Replace(streamFixture, "Number of Threads requested = 4\n", "", 1)
	without, err := parseStreamOutput(withoutThreads)
	if err != nil || without.RequestedThreads != 0 {
		t.Fatalf("STREAM missing thread declaration = %+v/%v", without, err)
	}
	if _, err := executeStreamMemory(context.Background(), "unused", 0); err == nil || !strings.Contains(err.Error(), "线程数必须为正数") {
		t.Fatalf("STREAM invalid execute input = %v", err)
	}
	runs := []streamMemoryRun{
		{Context: "1t", Threads: 1, Sample: parsed, Output: streamFixture},
		{Context: "nt", Threads: 4, Sample: streamParsedOutput{Samples: map[string]streamSample{}}},
	}
	stability := streamStabilityTable(runs)
	if stability.Key != "memory.stream.stability" || len(stability.ColumnKeys) != len(stability.Columns) || len(stability.Rows) != 8 || stability.Rows[0][1] == "—" || stability.Rows[1][1] != "—" {
		t.Fatalf("STREAM stability table = %+v", stability)
	}
	if streamVersion(runs[:1]) == "unknown" || streamVersion(runs[1:]) != "unknown" {
		t.Fatalf("STREAM version extraction = %q/%q", streamVersion(runs[:1]), streamVersion(runs[1:]))
	}
	runs = runs[:1]
	reusedTable := streamMemoryTable([]streamMemoryRun{{Context: "nt", Threads: 1, Reused: true, Sample: parsed}})
	if reusedTable.Rows[0][4] != "复用同一次实测" {
		t.Fatalf("STREAM reused table = %+v", reusedTable.Rows[0])
	}
	table := streamMemoryTable(runs)
	if table.Key != "memory.stream.bandwidth" || len(table.Rows) != 4 || len(table.ColumnKeys) != len(table.Columns) || table.Rows[0][1] == "—" {
		t.Fatalf("STREAM report table = %+v", table)
	}
	failureTable := streamMemoryTable([]streamMemoryRun{{Context: "nt", Threads: 4, Err: errors.New("fixture failure")}})
	if failureTable.Rows[0][1] != "—" || failureTable.Rows[0][4] != "失败" {
		t.Fatalf("STREAM failure table = %+v", failureTable)
	}
	if got := streamThreadControlField(4); !strings.Contains(got, "OMP_NUM_THREADS=4") || !strings.Contains(streamThreadControlField(1), "同一次实测") || !strings.Contains(streamContextLabel(runs[0]), "1T") {
		t.Fatalf("STREAM thread controls = %q", got)
	}
}

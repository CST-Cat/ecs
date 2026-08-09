package probe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func writeNextTraceFixture(t *testing.T, directory, name, version, output string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  printf '%s\\n' '" + version + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s' '" + output + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectRouteEngineRequiresOfficialTiny(t *testing.T) {
	t.Run("tiny detected", func(t *testing.T) {
		directory := t.TempDir()
		writeNextTraceFixture(t, directory, routeEngineTiny, "NextTrace Tiny fixture 1.0", `{"Hops":[]}`)
		t.Setenv("PATH", directory)

		engine := detectRouteEngine(context.Background())
		if engine.Name != routeEngineTiny || engine.Version != "NextTrace Tiny fixture 1.0" {
			t.Fatalf("detected engine = %+v, want Tiny", engine)
		}
		if engine.SHA256 == "" {
			t.Fatalf("Tiny engine has no SHA-256: %+v", engine)
		}
	})

	t.Run("full binary is ignored", func(t *testing.T) {
		directory := t.TempDir()
		writeNextTraceFixture(t, directory, "nexttrace", "NextTrace Full fixture 2.0", `{"Hops":[]}`)
		t.Setenv("PATH", directory)

		if engine := detectRouteEngine(context.Background()); engine.Path != "" {
			t.Fatalf("non-Tiny binary was detected: %+v", engine)
		}
	})
}

func TestRouteProbeRequiresValidJSONAndResponsiveHop(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		wantStatus   model.Status
		wantRow      string
		wantSlots    string
		wantVisible  string
		wantEvidence int
		wantMetrics  int
	}{
		{name: "valid nested address", output: `{"Hops":[[{"Address":{"IP":"127.0.0.1"}}]]}`, wantStatus: model.StatusOK, wantRow: "完成", wantSlots: "1", wantVisible: "1", wantEvidence: 1, wantMetrics: 4},
		{name: "malformed", output: `{"Hops":`, wantStatus: model.StatusWarning, wantRow: "NextTrace 解析失败", wantSlots: "0", wantVisible: "0"},
		{name: "empty hops", output: `{"Hops":[]}`, wantStatus: model.StatusWarning, wantRow: "NextTrace 解析失败", wantSlots: "0", wantVisible: "0"},
		{name: "all unresponsive", output: `{"Hops":[[{"Address":null}],[{"Address":""}]]}`, wantStatus: model.StatusWarning, wantRow: "无响应", wantSlots: "2", wantVisible: "0", wantEvidence: 1, wantMetrics: 4},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			writeNextTraceFixture(t, directory, routeEngineTiny, "NextTrace Tiny route fixture", testCase.output)
			t.Setenv("PATH", directory)
			cfg := config.Runtime{
				RouteTargets: []config.Endpoint{{Name: "fixture", Kind: "test", Address: "192.0.2.1"}},
			}

			result := (routeProbe{}).Run(context.Background(), Environment{Config: cfg})
			if result.Status != testCase.wantStatus {
				t.Fatalf("status = %q, want %q; result = %+v", result.Status, testCase.wantStatus, result)
			}
			if len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 {
				t.Fatalf("route tables = %+v", result.Tables)
			}
			row := result.Tables[0].Rows[0]
			if row[2] != testCase.wantRow || row[3] != testCase.wantSlots || row[4] != testCase.wantVisible {
				t.Fatalf("route row = %v, want status=%q slots=%q visible=%q", row, testCase.wantRow, testCase.wantSlots, testCase.wantVisible)
			}
			if result.Evidence == nil || result.Evidence.Valid != testCase.wantEvidence || result.Evidence.Expected != 1 {
				t.Fatalf("route evidence = %+v, want %d/1", result.Evidence, testCase.wantEvidence)
			}
			if len(result.Measurements) != testCase.wantMetrics {
				t.Fatalf("route metrics = %+v, want %d", result.Measurements, testCase.wantMetrics)
			}
			if len(result.TextBlocks) != 1 || result.TextBlocks[0].Content != testCase.output {
				t.Fatalf("raw route output was not retained: %+v", result.TextBlocks)
			}
		})
	}
}

func TestBacktraceTargetRejectsMalformedAndUnresponsiveOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
	}{
		{name: "malformed", output: `{"Hops":`},
		{name: "empty hops", output: `{"Hops":[]}`},
		{name: "all unresponsive", output: `{"Hops":[[{"Address":null}],[{"Address":""}]]}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			path := writeNextTraceFixture(t, directory, routeEngineTiny, "NextTrace Tiny backtrace fixture", testCase.output)
			row := runBacktraceTarget(
				context.Background(),
				routeEngine{Name: routeEngineTiny, Path: path},
				config.Endpoint{Name: "fixture", Kind: "test", Address: "192.0.2.1"},
				config.IPVersion4,
			)
			if row.Err == nil {
				t.Fatalf("invalid backtrace output was accepted: %+v", row)
			}
			if row.Raw != testCase.output {
				t.Fatalf("raw output = %q, want %q", row.Raw, testCase.output)
			}
			if len(row.Hits) != 0 {
				t.Fatalf("invalid output produced route hits: %+v", row.Hits)
			}
		})
	}
}

package probe

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

// assertProducerParameterScope checks the contract at the producer boundary.
// The producer is the source of the result under test; this helper only checks
// that it did not add an out-of-scope key or omit a fact required by this
// particular execution path.
func assertProducerParameterScope(t *testing.T, result model.Result, required ...string) {
	t.Helper()
	parameters := result.Methodology.Parameters
	if parameters == nil {
		t.Fatal("producer returned nil comparison parameters")
	}
	if parameters["scope_revision"] != comparisonParameterRevision {
		t.Fatalf("scope_revision = %q, want %q", parameters["scope_revision"], comparisonParameterRevision)
	}
	allowed := make(map[string]bool, len(required)+1)
	allowed["scope_revision"] = true
	for _, key := range required {
		allowed[key] = true
		if parameters[key] == "" {
			t.Errorf("missing producer parameter %q in %v", key, parameters)
		}
	}
	for key, value := range parameters {
		if !allowed[key] {
			t.Errorf("unrelated producer parameter %q leaked into %s result", key, result.ID)
		}
		if key != "scope_revision" && value == "" {
			t.Errorf("empty producer parameter %q in %v", key, parameters)
		}
	}
}

func noToolPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func selectedValueRows(rows [][]model.Value, columns ...int) [][]model.Value {
	selected := make([][]model.Value, 0, len(rows))
	for _, row := range rows {
		values := make([]model.Value, len(columns))
		for index, column := range columns {
			if column >= 0 && column < len(row) {
				values[index] = row[column]
			}
		}
		selected = append(selected, values)
	}
	return selected
}

func selectedValueHash(table model.Table, columns ...int) string {
	return comparisonParameterHash(selectedValueRows(table.Rows, columns...))
}

func TestDirectProducersOwnOnlyTheirComparisonParameters(t *testing.T) {
	t.Run("system", func(t *testing.T) {
		result := (systemProbe{}).Run(context.Background(), Environment{Config: config.Runtime{DiskPath: t.TempDir()}})
		assertProducerParameterScope(t, result, "disk_path")
	})

	t.Run("network", func(t *testing.T) {
		result := (networkProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, IPQualitySources: []string{"source-a", "source-b"}, HTTPTimeout: time.Second,
		}})
		assertProducerParameterScope(t, result, "ip_version", "ip_quality_sources_sha256", "http_timeout")
	})

	t.Run("bgp", func(t *testing.T) {
		result := (bgpProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		assertProducerParameterScope(t, result, "ip_version", "provider")
	})

	t.Run("cpu tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (cpuProbe{}).Run(context.Background(), Environment{Config: config.Runtime{CPUTime: time.Second}})
		assertProducerParameterScope(t, result, "configured_duration")
	})

	t.Run("zstd tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (zstdProbe{}).Run(context.Background(), Environment{})
		assertProducerParameterScope(t, result)
	})

	t.Run("npb missing binaries", func(t *testing.T) {
		noToolPath(t)
		result := (npbProbe{}).Run(context.Background(), Environment{})
		assertProducerParameterScope(t, result,
			"tool_version", "method_version", "problem_class", "threads", "implementation",
			"compiler_flags", "random_generator", "ep_sha256", "ft_sha256",
			"environment_1t_sha256", "environment_nt_sha256",
		)
	})

	t.Run("memory stream missing", func(t *testing.T) {
		noToolPath(t)
		result := (memoryProbe{}).Run(context.Background(), Environment{})
		assertProducerParameterScope(t, result)
	})

	t.Run("crypto tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (cryptoProbe{}).Run(context.Background(), Environment{})
		assertProducerParameterScope(t, result)
	})

	t.Run("disk fio missing", func(t *testing.T) {
		noToolPath(t)
		result := (diskProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			DiskMiB: 128, DiskMulti: true,
		}})
		assertProducerParameterScope(t, result, "configured_file_mib", "multi_mount")
	})

	t.Run("dns skipped without resolvers", func(t *testing.T) {
		result := (dnsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, DNSAttempts: 2,
		}})
		assertProducerParameterScope(t, result, "ip_version", "query_name", "attempts", "resolvers_sha256")
	})

	t.Run("latency skipped without targets", func(t *testing.T) {
		result := (latencyProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, LatencyAttempts: 2,
		}})
		assertProducerParameterScope(t, result, "ip_version", "attempts", "targets_sha256")
	})

	t.Run("speed tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (speedProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, IPerfDuration: time.Second, SpeedThreads: 2,
		}})
		assertProducerParameterScope(t, result, "ip_version", "configured_duration", "configured_threads", "targets_sha256")
	})

	t.Run("ports canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := (portsProbe{}).Run(ctx, Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		assertProducerParameterScope(t, result, "ip_version", "target_set")
	})

	t.Run("nat skipped without servers", func(t *testing.T) {
		result := (natProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		assertProducerParameterScope(t, result, "ip_version", "servers_sha256")
	})

	t.Run("blacklist skipped for IPv6", func(t *testing.T) {
		result := (blacklistProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion6}})
		assertProducerParameterScope(t, result, "ip_version", "zone_set")
	})

	t.Run("apps completed by producer seam", func(t *testing.T) {
		oldTargetFunc := appProbeTargetFunc
		t.Cleanup(func() { appProbeTargetFunc = oldTargetFunc })
		appProbeTargetFunc = func(_ context.Context, target appTarget, _ string) appResult {
			return appResult{Target: target}
		}
		result := (appsProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		assertProducerParameterScope(t, result, "ip_version", "target_set")
	})

	t.Run("cnspeed node list unavailable", func(t *testing.T) {
		oldURL := cnNodeListURLForTest
		oldFactory := cnspeedHTTPClientFactory
		t.Cleanup(func() {
			cnNodeListURLForTest = oldURL
			cnspeedHTTPClientFactory = oldFactory
		})
		cnNodeListURLForTest = "https://fixture.invalid/CN.csv"
		cnspeedHTTPClientFactory = func(time.Duration, string, cnIPResolver, cnDialContextFunc) *http.Client {
			return &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, context.Canceled
			})}
		}
		result := (cnSpeedProbe{}).Run(context.Background(), Environment{Config: config.Runtime{IPVersion: config.IPVersion4}})
		assertProducerParameterScope(t, result, "ip_version", "download_budget_sha256")
	})

	t.Run("ookla exposure skipped", func(t *testing.T) {
		result := (ooklaProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, Exposure: config.ExposureLocal,
			OoklaServers: []config.OoklaServer{{Carrier: "电信", ID: 42}},
		}})
		assertProducerParameterScope(t, result, "ip_version", "server_configuration_sha256")
	})

	t.Run("media requests fail through fixture transport", func(t *testing.T) {
		client := &http.Client{Transport: fixtureRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})}
		result := (mediaProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersionAuto, HTTPTimeout: time.Second, MediaRegions: []string{"jp"},
		}, HTTPClient: client})
		assertProducerParameterScope(t, result, "ip_version", "regions_sha256", "http_timeout")
	})

	t.Run("route tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (routeProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, RouteTargets: []config.Endpoint{{Name: "fixture", Address: "203.0.113.1"}},
		}})
		assertProducerParameterScope(t, result, "ip_version", "targets_sha256", "max_hops")
	})

	t.Run("backtrace tool missing", func(t *testing.T) {
		noToolPath(t)
		result := (backtraceProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
			IPVersion: config.IPVersion4, BacktraceTargets: []config.Endpoint{{Name: "fixture", Address: "203.0.113.1"}},
		}})
		assertProducerParameterScope(t, result, "ip_version", "targets_sha256", "max_hops", "signature_set")
	})
}

func TestProducerComparisonHashesAreStableOrderedAndTagged(t *testing.T) {
	ordered := []string{"source-a", "source-b"}
	if got, want := comparisonParameterHash(ordered), comparisonParameterHash([]string{"source-a", "source-b"}); got != want {
		t.Fatalf("same ordered input hash changed: %q != %q", got, want)
	}
	if comparisonParameterHash(ordered) == comparisonParameterHash([]string{"source-b", "source-a"}) {
		t.Fatal("reordered input retained the same comparison hash")
	}

	raw := [][]model.Value{{model.RawValue("x")}}
	key := [][]model.Value{{model.KeyValue("x")}}
	if comparisonParameterHash(raw) == comparisonParameterHash(key) {
		t.Fatal("raw and key Value variants share a comparison hash")
	}
	if comparisonParameterHash(raw) != comparisonParameterHash([][]model.Value{{model.RawValue("x")}}) {
		t.Fatal("same raw Value variant/text did not retain a stable hash")
	}
}

func TestNetworkProducerComparisonScopeTracksOrderedSources(t *testing.T) {
	base := config.Runtime{
		IPVersion: config.IPVersion4, IPQualitySources: []string{"source-a", "source-b"}, HTTPTimeout: time.Second,
	}
	first := (networkProbe{}).Run(context.Background(), Environment{Config: base})
	same := (networkProbe{}).Run(context.Background(), Environment{Config: base})
	if first.Methodology.Parameters["ip_quality_sources_sha256"] != same.Methodology.Parameters["ip_quality_sources_sha256"] {
		t.Fatal("same network source order changed producer comparison scope")
	}
	reordered := base
	reordered.IPQualitySources = []string{"source-b", "source-a"}
	third := (networkProbe{}).Run(context.Background(), Environment{Config: reordered})
	if first.Methodology.Parameters["ip_quality_sources_sha256"] == third.Methodology.Parameters["ip_quality_sources_sha256"] {
		t.Fatal("network source order did not change producer comparison scope")
	}
}

func TestZstdComparisonArgumentsIgnoreOnlyTemporaryCorpusPath(t *testing.T) {
	argsA := []string{"-q", "-b3", "-i1", "-T1", "/tmp/ecs-run-a/corpus"}
	argsB := []string{"-q", "-b3", "-i1", "-T1", "/tmp/ecs-run-b/corpus"}
	if got, want := comparisonParameterHash(zstdComparisonArguments(argsA)), comparisonParameterHash(zstdComparisonArguments(argsB)); got != want {
		t.Fatalf("temporary corpus path changed zstd scope: %q != %q", got, want)
	}
	changed := []string{"-q", "-b4", "-i1", "-T1", "/tmp/ecs-run-b/corpus"}
	if comparisonParameterHash(zstdComparisonArguments(argsA)) == comparisonParameterHash(zstdComparisonArguments(changed)) {
		t.Fatal("non-path zstd argument change did not change scope")
	}
}

func TestDiskProducerComparisonParameterUsesExplanationFreeEngineName(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fio")
	script := `#!/bin/sh
if [ "$1" = "--enghelp" ]; then
  printf '%s\n' 'psync'
  exit 0
fi
printf '%s\n' '{"fio version":"fio-fixture","jobs":[{"jobname":"seqwrite","write":{"bw_bytes":2097152}},{"jobname":"seqread","read":{"bw_bytes":2097152}},{"jobname":"randread","read":{"iops":10}},{"jobname":"randwrite","write":{"iops":10}}]}'
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	result := (diskProbe{}).Run(context.Background(), Environment{Config: config.Runtime{
		DiskPath: t.TempDir(), DiskMiB: 128,
	}})
	assertProducerParameterScope(t, result,
		"configured_file_mib", "multi_mount", "tool_version", "tool_sha256", "actual_file_size",
		"direct_io", "ioengine", "jobs", "job_duration",
	)
	parameters := result.Methodology.Parameters
	if parameters["configured_file_mib"] != "128" || parameters["multi_mount"] != "false" || parameters["tool_version"] != "fio-fixture" || parameters["tool_sha256"] != binarySHA256(path) || parameters["direct_io"] != "1" || parameters["ioengine"] != "psync" || parameters["jobs"] != strconv.Itoa(len(fioJobPlan())) || parameters["job_duration"] != fioPlanDuration(fioJobPlan()).String() {
		t.Fatalf("disk comparison parameters = %v", parameters)
	}
	fileSize := ""
	for _, field := range result.Fields {
		if field.Key == "file_size" {
			fileSize = field.Value.Text()
			break
		}
	}
	if fileSize == "" || parameters["actual_file_size"] != fileSize {
		t.Fatalf("disk actual file size parameter = %q, field file_size = %q", parameters["actual_file_size"], fileSize)
	}
}

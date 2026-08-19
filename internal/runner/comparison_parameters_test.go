package runner

import (
	"reflect"
	"testing"

	"ecs/internal/config"
	"ecs/internal/model"
)

func comparisonParameterFields() []model.Field {
	return []model.Field{
		{Key: "version", Value: "tool-1"},
		{Key: "binary_sha256", Value: "binary-1"},
		{Key: "method_version", Value: "method-1"},
		{Key: "compression_level", Value: "3"},
		{Key: "threads", Value: "4"},
		{Key: "duration", Value: "5s"},
		{Key: "prime", Value: "default"},
		{Key: "corpus_bytes", Value: "1024"},
		{Key: "corpus_sha256", Value: "corpus-1"},
		{Key: "corpus_source_sha256", Value: "source-1"},
		{Key: "arguments_1t", Value: "zstd --threads 1 --output /tmp/run-a corpus"},
		{Key: "arguments_nt", Value: "zstd --threads 4 --output /tmp/run-a corpus"},
		{Key: "problem_class", Value: "A"},
		{Key: "implementation", Value: "omp"},
		{Key: "compiler_flags", Value: "-O2"},
		{Key: "random_generator", Value: "default"},
		{Key: "binary_ep_sha256", Value: "ep-1"},
		{Key: "binary_ft_sha256", Value: "ft-1"},
		{Key: "environment_1t", Value: "env-1"},
		{Key: "environment_nt", Value: "env-n"},
		{Key: "kernel_order", Value: "copy,scale,add,triad"},
		{Key: "algorithms", Value: "aes,chacha,sha"},
		{Key: "block_size", Value: "16K"},
		{Key: "workers", Value: "4"},
		{Key: "timing", Value: "wall"},
		{Key: "machine_output", Value: "openssl-output"},
		{Key: "arguments_aes_256_gcm_1w", Value: "aes-1w"},
		{Key: "arguments_aes_256_gcm_nw", Value: "aes-nw"},
		{Key: "arguments_chacha20_poly1305_1w", Value: "chacha-1w"},
		{Key: "arguments_chacha20_poly1305_nw", Value: "chacha-nw"},
		{Key: "arguments_sha_256_1w", Value: "sha-1w"},
		{Key: "arguments_sha_256_nw", Value: "sha-nw"},
		{Key: "file_size", Value: "2048 MiB"},
		{Key: "direct_io", Value: "true"},
		{Key: "ioengine", Value: "libaio（fixture explanation）"},
		{Key: "jobs", Value: "1"},
		{Key: "job_duration", Value: "5s"},
		{Key: "download_budget", Value: "1 MiB"},
		{Key: "speedtest_version", Value: "speedtest-1"},
		{Key: "arguments", Value: "--json --server 1"},
		{Key: "nexttrace_version", Value: "nexttrace-1"},
		{Key: "nexttrace_binary_sha256", Value: "nexttrace-bin-1"},
	}
}

func comparisonParameterResult() model.Result {
	return model.Result{
		Fields: comparisonParameterFields(),
		Tables: []model.Table{{
			Rows: [][]string{{"node-a", "https://node-a", "region-a", "ok"}},
		}},
	}
}

func setComparisonField(result *model.Result, key, value string) {
	for index := range result.Fields {
		if result.Fields[index].Key == key {
			result.Fields[index].Value = value
		}
	}
}

func TestComparisonParametersCoverModuleScopes(t *testing.T) {
	cfg, err := config.Defaults(config.ProfileStandard)
	if err != nil {
		t.Fatal(err)
	}
	cfg.IPQualitySources = []string{"source-a", "source-b"}
	cases := []struct {
		id   string
		keys []string
	}{
		{id: "system", keys: []string{"disk_path"}},
		{id: "network", keys: []string{"ip_version", "ip_quality_sources_sha256", "http_timeout"}},
		{id: "bgp", keys: []string{"ip_version", "provider"}},
		{id: "cpu", keys: []string{"configured_duration", "tool_version", "tool_sha256", "threads", "duration", "prime"}},
		{id: "zstd", keys: []string{"tool_version", "tool_sha256", "method_version", "compression_level", "threads", "duration", "corpus_bytes", "corpus_sha256", "corpus_source_sha256", "arguments_1t_sha256", "arguments_nt_sha256"}},
		{id: "npb", keys: []string{"tool_version", "method_version", "problem_class", "threads", "implementation", "compiler_flags", "random_generator", "ep_sha256", "ft_sha256", "environment_1t_sha256", "environment_nt_sha256"}},
		{id: "memory", keys: []string{"tool_version", "tool_sha256", "threads", "kernel_order"}},
		{id: "crypto", keys: []string{"tool_version", "tool_sha256", "method_version", "algorithms", "block_size", "duration", "workers", "timing", "machine_output", "arguments_aes_256_gcm_1w_sha256", "arguments_aes_256_gcm_nw_sha256", "arguments_chacha20_poly1305_1w_sha256", "arguments_chacha20_poly1305_nw_sha256", "arguments_sha_256_1w_sha256", "arguments_sha_256_nw_sha256"}},
		{id: "disk", keys: []string{"configured_file_mib", "multi_mount", "tool_version", "tool_sha256", "actual_file_size", "direct_io", "ioengine", "jobs", "job_duration"}},
		{id: "dns", keys: []string{"ip_version", "query_name", "attempts", "resolvers_sha256"}},
		{id: "latency", keys: []string{"ip_version", "attempts", "targets_sha256"}},
		{id: "speed", keys: []string{"ip_version", "configured_duration", "configured_threads", "targets_sha256", "tool_version", "tool_sha256", "threads", "duration"}},
		{id: "ports", keys: []string{"ip_version", "target_set"}},
		{id: "nat", keys: []string{"ip_version", "servers_sha256"}},
		{id: "blacklist", keys: []string{"ip_version", "zone_set"}},
		{id: "apps", keys: []string{"ip_version", "target_set"}},
		{id: "cnspeed", keys: []string{"ip_version", "download_budget_sha256", "selected_nodes_sha256"}},
		{id: "ookla", keys: []string{"ip_version", "tool_version", "tool_sha256", "arguments_sha256", "server_configuration_sha256", "selected_servers_sha256"}},
		{id: "media", keys: []string{"ip_version", "regions_sha256", "http_timeout"}},
		{id: "route", keys: []string{"ip_version", "targets_sha256", "max_hops", "tool_version", "tool_sha256", "arguments_sha256"}},
		{id: "backtrace", keys: []string{"ip_version", "targets_sha256", "max_hops", "signature_set", "tool_version", "tool_sha256"}},
	}
	for _, test := range cases {
		t.Run(test.id, func(t *testing.T) {
			got := comparisonParameters(test.id, cfg, comparisonParameterResult())
			allowed := map[string]bool{"scope_revision": true}
			for _, key := range test.keys {
				allowed[key] = true
				if got[key] == "" {
					t.Errorf("missing non-empty parameter %q in %v", key, got)
				}
			}
			for key := range got {
				if !allowed[key] {
					t.Errorf("unrelated parameter %q leaked into %s scope", key, test.id)
				}
			}
			if got["scope_revision"] != comparisonParameterRevision {
				t.Errorf("scope revision = %q", got["scope_revision"])
			}
		})
	}

	result := comparisonParameterResult()
	first := comparisonParameters("network", cfg, result)
	second := comparisonParameters("network", cfg, result)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same comparison inputs produced different parameters")
	}
	reordered := cfg
	reordered.IPQualitySources = []string{"source-b", "source-a"}
	if reflect.DeepEqual(first, comparisonParameters("network", reordered, result)) {
		t.Fatal("ordered comparison inputs lost their scope distinction")
	}

	duplicate := comparisonParameterResult()
	duplicate.Fields = append(duplicate.Fields, model.Field{Key: "version", Value: "second-version"})
	if params := comparisonParameters("cpu", cfg, duplicate); params["tool_version"] != "" {
		t.Fatal("duplicate machine field was used as a comparison parameter")
	}

	zstdA := comparisonParameterResult()
	zstdB := comparisonParameterResult()
	setComparisonField(&zstdB, "arguments_1t", "zstd --threads 1 --output /tmp/run-b corpus")
	setComparisonField(&zstdB, "arguments_nt", "zstd --threads 4 --output /tmp/run-b corpus")
	paramsA := comparisonParameters("zstd", cfg, zstdA)
	paramsB := comparisonParameters("zstd", cfg, zstdB)
	if paramsA["arguments_1t_sha256"] != paramsB["arguments_1t_sha256"] || paramsA["arguments_nt_sha256"] != paramsB["arguments_nt_sha256"] {
		t.Fatal("zstd temporary argument path changed the comparison scope")
	}

	disk := comparisonParameters("disk", cfg, result)
	if disk["ioengine"] != "libaio" {
		t.Fatalf("disk ioengine scope = %q, want explanation-free value", disk["ioengine"])
	}

	for _, id := range []string{"cnspeed", "ookla"} {
		base := comparisonParameterResult()
		changedOutcome := comparisonParameterResult()
		changedOutcome.Tables[0].Rows[0][3] = "different-result"
		if !reflect.DeepEqual(comparisonParameters(id, cfg, base), comparisonParameters(id, cfg, changedOutcome)) {
			t.Fatalf("%s outcome column changed selected-target scope", id)
		}
		changedTarget := comparisonParameterResult()
		changedTarget.Tables[0].Rows[0][0] = "node-b"
		if reflect.DeepEqual(comparisonParameters(id, cfg, base), comparisonParameters(id, cfg, changedTarget)) {
			t.Fatalf("%s selected target change was ignored", id)
		}
	}

	unknown := comparisonParameters("future-module", cfg, result)
	if len(unknown) != 1 || unknown["scope_revision"] != comparisonParameterRevision {
		t.Fatalf("unknown module scope = %v, want revision only", unknown)
	}
}

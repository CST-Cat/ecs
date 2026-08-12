package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"ecs/internal/config"
	"ecs/internal/model"
	"ecs/internal/probe"
)

// comparisonParameterRevision changes only when the rules used to build a
// module's machine comparison scope change. Measurement.Method remains the
// version for the workload itself.
const comparisonParameterRevision = "1"

// comparisonParameters records the runtime inputs that are not already
// encoded by measurement.key/method/unit/direction. Values are deliberately
// language-neutral so a Chinese report and an English report have the same
// signature. Large target sets are represented by a deterministic SHA-256.
func comparisonParameters(id string, cfg config.Runtime, result model.Result) map[string]string {
	parameters := map[string]string{
		"scope_revision": comparisonParameterRevision,
	}
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			parameters[key] = value
		}
	}
	addField := func(parameterKey, fieldKey string) {
		if value, ok := uniqueFieldValue(result.Fields, fieldKey); ok {
			add(parameterKey, value)
		}
	}
	addFieldHash := func(parameterKey, fieldKey string) {
		if value, ok := uniqueFieldValue(result.Fields, fieldKey); ok {
			add(parameterKey, scopeHash(value))
		}
	}
	addIPVersion := func() { add("ip_version", cfg.IPVersion) }

	switch id {
	case "system":
		add("disk_path", cfg.DiskPath)
	case "network":
		addIPVersion()
		add("ip_quality_sources_sha256", scopeHash(cfg.IPQualitySources))
		add("http_timeout", cfg.HTTPTimeout.String())
	case "bgp":
		addIPVersion()
		add("provider", "routeviews-current-rib")
	case "cpu":
		add("configured_duration", cfg.CPUTime.String())
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("threads", "threads")
		addField("duration", "duration")
		addField("prime", "prime")
	case "zstd":
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("method_version", "method_version")
		addField("compression_level", "compression_level")
		addField("threads", "threads")
		addField("duration", "duration")
		addField("corpus_bytes", "corpus_bytes")
		addField("corpus_sha256", "corpus_sha256")
		addField("corpus_source_sha256", "corpus_source_sha256")
		addZstdArgumentHash := func(parameterKey, fieldKey string) {
			if value, ok := uniqueFieldValue(result.Fields, fieldKey); ok {
				// The fifth argument is the verified corpus's per-run temporary
				// path. Its identity is already fixed by corpus_bytes and
				// corpus_sha256, so including that path would make two otherwise
				// identical run.sh invocations incomparable.
				parts := strings.Fields(value)
				if len(parts) >= 4 {
					parts = parts[:4]
				}
				add(parameterKey, scopeHash(parts))
			}
		}
		addZstdArgumentHash("arguments_1t_sha256", "arguments_1t")
		addZstdArgumentHash("arguments_nt_sha256", "arguments_nt")
	case "npb":
		addField("tool_version", "version")
		addField("method_version", "method_version")
		addField("problem_class", "problem_class")
		addField("threads", "threads")
		addField("implementation", "implementation")
		addField("compiler_flags", "compiler_flags")
		addField("random_generator", "random_generator")
		addField("ep_sha256", "binary_ep_sha256")
		addField("ft_sha256", "binary_ft_sha256")
		addFieldHash("environment_1t_sha256", "environment_1t")
		addFieldHash("environment_nt_sha256", "environment_nt")
	case "memory":
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("threads", "threads")
		addField("kernel_order", "kernel_order")
	case "crypto":
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("method_version", "method_version")
		addField("algorithms", "algorithms")
		addField("block_size", "block_size")
		addField("duration", "duration")
		addField("workers", "workers")
		addField("timing", "timing")
		addField("machine_output", "machine_output")
		for _, key := range []string{
			"arguments_aes_256_gcm_1w", "arguments_aes_256_gcm_nw",
			"arguments_chacha20_poly1305_1w", "arguments_chacha20_poly1305_nw",
			"arguments_sha_256_1w", "arguments_sha_256_nw",
		} {
			addFieldHash(key+"_sha256", key)
		}
	case "disk":
		add("configured_file_mib", strconv.Itoa(cfg.DiskMiB))
		add("multi_mount", strconv.FormatBool(cfg.DiskMulti))
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("actual_file_size", "file_size")
		addField("direct_io", "direct_io")
		if value, ok := uniqueFieldValue(result.Fields, "ioengine"); ok {
			value, _, _ = strings.Cut(value, "（")
			add("ioengine", value)
		}
		addField("jobs", "jobs")
		addField("job_duration", "job_duration")
	case "dns":
		addIPVersion()
		add("query_name", probe.DNSQueryName)
		add("attempts", strconv.Itoa(cfg.DNSAttempts))
		add("resolvers_sha256", scopeHash(cfg.DNSResolvers))
	case "latency":
		addIPVersion()
		add("attempts", strconv.Itoa(cfg.LatencyAttempts))
		add("targets_sha256", scopeHash(cfg.LatencyTargets))
	case "speed":
		addIPVersion()
		add("configured_duration", cfg.IPerfDuration.String())
		add("configured_threads", strconv.Itoa(cfg.SpeedThreads))
		add("targets_sha256", scopeHash(cfg.IPerfTargets))
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addField("threads", "threads")
		addField("duration", "duration")
	case "ports":
		addIPVersion()
		add("target_set", "ports-v1")
	case "nat":
		addIPVersion()
		add("servers_sha256", scopeHash(cfg.STUNServers))
	case "blacklist":
		addIPVersion()
		add("zone_set", "dnsbl-zones-v1")
	case "apps":
		addIPVersion()
		add("target_set", "apps-v1")
	case "cnspeed":
		addIPVersion()
		addFieldHash("download_budget_sha256", "download_budget")
		add("selected_nodes_sha256", selectedTableColumnsHash(result.Tables, 0, 1, 2))
	case "ookla":
		addIPVersion()
		addField("tool_version", "speedtest_version")
		addField("tool_sha256", "binary_sha256")
		addFieldHash("arguments_sha256", "arguments")
		add("server_configuration_sha256", scopeHash(cfg.OoklaServers))
		add("selected_servers_sha256", selectedTableColumnsHash(result.Tables, 0, 1))
	case "media":
		addIPVersion()
		add("regions_sha256", scopeHash(cfg.MediaRegions))
		add("http_timeout", cfg.HTTPTimeout.String())
	case "route":
		addIPVersion()
		add("targets_sha256", scopeHash(cfg.RouteTargets))
		add("max_hops", strconv.Itoa(probe.RouteSnapshotHops))
		addField("tool_version", "version")
		addField("tool_sha256", "binary_sha256")
		addFieldHash("arguments_sha256", "arguments")
	case "backtrace":
		addIPVersion()
		add("targets_sha256", scopeHash(cfg.BacktraceTargets))
		add("max_hops", strconv.Itoa(probe.BacktraceMaxHops))
		add("signature_set", "china-backbone-v2")
		addField("tool_version", "nexttrace_version")
		addField("tool_sha256", "nexttrace_binary_sha256")
	}
	return parameters
}

func uniqueFieldValue(fields []model.Field, key string) (string, bool) {
	value := ""
	found := false
	for _, field := range fields {
		if field.Key != key {
			continue
		}
		if found {
			return "", false
		}
		value, found = field.Value, true
	}
	return value, found && strings.TrimSpace(value) != ""
}

func scopeHash(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// selectedTableColumnsHash fingerprints dynamic targets selected during a
// probe (for example cnspeed and Ookla). Outcome/status columns are excluded.
func selectedTableColumnsHash(tables []model.Table, columns ...int) string {
	if len(tables) == 0 || len(columns) == 0 {
		return ""
	}
	rows := make([][]string, 0, len(tables[0].Rows))
	for _, row := range tables[0].Rows {
		selected := make([]string, len(columns))
		for index, column := range columns {
			if column >= 0 && column < len(row) {
				selected[index] = row[column]
			}
		}
		rows = append(rows, selected)
	}
	return scopeHash(rows)
}

package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

// ooklaProbe runs the official Ookla CLI directly and records its structured
// result. Direct ecs runs
// expect the client to be installed already; run.sh may prepare it from the
// signed official source when the selected profile/module needs it. Keeping
// it as an explicit module matters: the client has its own licence, privacy
// terms and measurement service, unlike ecs's local-only report.
type ooklaProbe struct{}

func (ooklaProbe) ID() string { return "ookla" }

func newOoklaProbeResult() model.Result {
	result := model.NewResult("ookla", "module.ookla.title")
	result.Description = "probe.ookla.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "ookla-speedtest-cli",
		Profile:         "probe.ookla.profile",
		ComparisonScope: "probe.ookla.comparison_scope",
	}
	result.Methodology.Parameters = newComparisonParameters()
	result.Notes = ooklaStableNotes()
	return result
}

func ooklaStableNotes() []string {
	return []string{
		"probe.ookla.note.external_service",
		"probe.ookla.note.no_raw_json",
		"probe.ookla.note.traffic",
	}
}

type ooklaResult struct {
	Ping struct {
		Jitter  float64 `json:"jitter"`
		Latency float64 `json:"latency"`
	} `json:"ping"`
	Download struct {
		Bandwidth float64 `json:"bandwidth"`
	} `json:"download"`
	Upload struct {
		Bandwidth float64 `json:"bandwidth"`
	} `json:"upload"`
	PacketLoss *float64 `json:"packetLoss"`
	ISP        string   `json:"isp"`
	Interface  struct {
		ExternalIP string `json:"externalIp"`
	} `json:"interface"`
	Server struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Country  string `json:"country"`
	} `json:"server"`
	Result struct {
		Persisted bool `json:"persisted"`
	} `json:"result"`
	presence ooklaFieldPresence `json:"-"`
}

type ooklaFieldPresence struct {
	PingJitter        bool
	PingLatency       bool
	DownloadBandwidth bool
	UploadBandwidth   bool
}

func (ooklaProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := newOoklaProbeResult()
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "ip_version", env.Config.IPVersion)
	addComparisonParameterHash(result.Methodology.Parameters, "server_configuration_sha256", env.Config.OoklaServers)

	if !config.AllowsModule(env.Config.Exposure, "ookla") {
		result.Skip(model.NewMessage("probe.ookla.summary.skipped"))
		appendOoklaSkipDetails(&result, "exposure_denied", "rerun_with_more_exposure")
		result.Notes = ooklaStableNotes()
		result.Evidence = model.NewEvidence(0, 1, "run")
		result.Finish(start)
		return result
	}
	path, err := exec.LookPath("speedtest")
	if err != nil {
		result.Skip(model.NewMessage("probe.ookla.summary.skipped"))
		addFailure(&result, "tool_lookup", "speedtest", err)
		appendOoklaSkipDetails(&result, "tool_unavailable", "install_official_client")
		result.Notes = ooklaStableNotes()
		result.Evidence = model.NewEvidence(0, 1, "run")
		result.Finish(start)
		return result
	}

	args := []string{"--accept-license", "--accept-gdpr", "--format=json", "--progress=no"}
	if env.Config.IPVersion == config.IPVersion4 || env.Config.IPVersion == config.IPVersion6 {
		args = append(args, "--ip-version", env.Config.IPVersion)
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.ookla.field.engine", Value: model.RawValue("official speedtest")},
		{Key: "speedtest_version", Label: "probe.ookla.field.speedtest_version", Value: model.RawValue(commandVersion(ctx, path))},
		{Key: "binary", Label: "probe.ookla.field.binary", Value: model.RawValue(path)},
		{Key: "binary_sha256", Label: "probe.ookla.field.binary_sha256", Value: model.RawValue(fallback(binarySHA256(path), "unavailable"))},
		{Key: "arguments", Label: "probe.ookla.field.arguments", Value: model.RawValue(strings.Join(args, " "))},
		{Key: "external_service", Label: "probe.ookla.field.external_service", Value: model.RawValue("Ookla")},
	}
	addComparisonParameter(result.Methodology.Parameters, "tool_version", commandVersion(ctx, path))
	addComparisonParameter(result.Methodology.Parameters, "tool_sha256", fallback(binarySHA256(path), "unavailable"))
	addComparisonParameterHash(result.Methodology.Parameters, "arguments_sha256", strings.Join(args, " "))
	servers := append([]config.OoklaServer(nil), env.Config.OoklaServers...)
	if len(servers) == 0 {
		servers = []config.OoklaServer{{Carrier: config.OoklaCarrierAuto, ID: 0}}
		result.Fields = append(result.Fields, model.Field{Key: "server_selection", Label: "probe.ookla.field.server_selection", Value: ooklaServerSelectionValue("automatic")})
	} else {
		result.Fields = append(result.Fields, model.Field{Key: "server_selection", Label: "probe.ookla.field.server_selection", Value: ooklaServerSelectionValue("configured")})
	}

	table := model.Table{
		Key:   "network.ookla.results",
		Title: "probe.ookla.table.results",
		Columns: []model.TableColumn{
			{Key: "carrier", Label: "probe.ookla.column.carrier"},
			{Key: "server", Label: "probe.ookla.column.server"},
			{Key: "latency_ms", Label: "probe.ookla.column.latency", Numeric: true, HigherIsBetter: false},
			{Key: "download_mbps", Label: "probe.ookla.column.download", Numeric: true, HigherIsBetter: true},
			{Key: "upload_mbps", Label: "probe.ookla.column.upload", Numeric: true, HigherIsBetter: true},
			{Key: "packet_loss_percent", Label: "probe.ookla.column.loss", Numeric: true, HigherIsBetter: false},
			{Key: "status", Label: "probe.ookla.column.status"},
		},
	}
	selectedServers := make([][]model.Value, 0, len(servers))
	warningMessages := make([]model.Message, 0)
	successes := 0
	validResults := 0
	for _, target := range servers {
		targetArgs := append([]string(nil), args...)
		if target.ID > 0 {
			targetArgs = append(targetArgs, "--server-id", strconv.Itoa(target.ID))
		}
		parsed, runErr, parseErr, _ := runOfficialOokla(ctx, path, targetArgs)
		label := target.Carrier
		if parseErr != nil {
			addFailure(&result, "parse", label, parseErr)
			if runErr != nil {
				addFailure(&result, "execute", label, runErr)
			}
			markOoklaWarning(&result, &warningMessages, model.NewMessage("probe.ookla.summary.warn.unparsed", label))
			row := []model.Value{
				ooklaCarrierValue(label), model.RawValue(formatOoklaServer(parsed)), model.RawValue("—"),
				model.RawValue("—"), model.RawValue("—"), model.RawValue("—"), model.RawValue("—"),
			}
			row[len(row)-1] = model.KeyValue(ooklaStatusKey(parsed, label, result.Failures))
			table.Rows = append(table.Rows, row)
			selectedServers = append(selectedServers, append([]model.Value(nil), row[:2]...))
			result.Fields = append(result.Fields, model.Field{Key: "error_" + ooklaCarrierKey(label), Label: "probe.ookla.field.error", Value: model.RawValue(compactError(parseErr))})
			continue
		}

		prefix := ""
		if len(servers) > 1 {
			prefix = ooklaCarrierKey(label) + "_"
		}
		appendOoklaMeasurementsFor(&result, parsed, prefix)
		serverName := formatOoklaServer(parsed)
		hasMetric := ooklaHasValidMetric(parsed)
		complete := ooklaMeasurementsComplete(parsed)
		if !complete {
			incompleteFields := ooklaIncompleteRequiredFields(parsed)
			addOoklaIncompleteFieldsFailure(&result, label, incompleteFields)
			result.Fields = append(result.Fields, model.Field{
				Key: "incomplete_fields_" + ooklaCarrierKey(label), Label: "probe.ookla.field.incomplete_fields",
				Value: model.RawValue(strings.Join(incompleteFields, ",")),
			})
			markOoklaWarning(&result, &warningMessages, model.NewMessage(
				"probe.ookla.summary.warn.incomplete", label, strings.Join(incompleteFields, ","),
			))
		}
		if runErr != nil {
			addFailure(&result, "execute", label, runErr)
			markOoklaWarning(&result, &warningMessages, model.NewMessage("probe.ookla.summary.warn.execution", label))
		}
		if parsed.ISP != "" {
			result.Fields = append(result.Fields, model.Field{Key: "isp_" + ooklaCarrierKey(label), Label: "probe.ookla.field.isp", Value: model.RawValue(parsed.ISP)})
		}
		ipVersionMismatch := ""
		if parsed.Interface.ExternalIP != "" {
			result.Fields = append(result.Fields, model.Field{Key: "external_ip_" + ooklaCarrierKey(label), Label: "probe.ookla.field.external_ip", Value: model.RawValue(parsed.Interface.ExternalIP), Sensitive: true})
			if expected := env.Config.IPVersion; expected == config.IPVersion4 || expected == config.IPVersion6 {
				if ip := net.ParseIP(parsed.Interface.ExternalIP); ip != nil {
					actual := config.IPVersion4
					if ip.To4() == nil {
						actual = config.IPVersion6
					}
					if actual != expected {
						ipVersionMismatch = actual
						result.Fields = append(result.Fields, model.Field{
							Key: "ip_version_mismatch_" + ooklaCarrierKey(label), Label: "probe.ookla.field.ip_family",
							Value: model.RawValue("requested=" + expected + ";returned=" + actual),
						})
						markOoklaWarning(&result, &warningMessages, model.NewMessage(
							"probe.ookla.summary.warn.ip_family", label, expected, actual,
						))
					}
				}
			}
		}
		row := []model.Value{
			ooklaCarrierValue(label), model.RawValue(serverName), model.RawValue(ooklaLatencyDisplay(parsed.Ping.Latency)),
			model.RawValue(ooklaBandwidthDisplay(parsed.Download.Bandwidth)), model.RawValue(ooklaBandwidthDisplay(parsed.Upload.Bandwidth)),
			model.RawValue(ooklaPacketLossDisplay(parsed)), model.RawValue("—"),
		}
		statusKey := ooklaStatusKey(parsed, label, result.Failures)
		if ipVersionMismatch != "" && statusKey == "probe.ookla.status.complete" {
			statusKey = "probe.ookla.status.ip_family"
		}
		row[len(row)-1] = model.KeyValue(statusKey)
		table.Rows = append(table.Rows, row)
		selectedServers = append(selectedServers, append([]model.Value(nil), row[:2]...))
		if hasMetric {
			validResults++
			if complete && runErr == nil {
				successes++
			}
		}
	}
	result.Tables = []model.Table{table}
	addComparisonParameterHash(result.Methodology.Parameters, "selected_servers_sha256", selectedServers)
	result.Evidence = model.NewEvidence(validResults, len(servers), "run")
	if successes == 0 && validResults == 0 {
		markOoklaWarning(&result, &warningMessages, model.NewMessage("probe.ookla.summary.warn.no_result"))
	} else if len(result.Measurements) == 0 {
		markOoklaWarning(&result, &warningMessages, model.NewMessage("probe.ookla.summary.warn.no_metrics"))
	}
	result.Sources = []model.Source{{
		Name:    "Ookla Speedtest",
		URL:     "https://www.speedtest.net/",
		Purpose: "probe.ookla.source.engine",
	}}
	result.Notes = ooklaStableNotes()
	switch {
	case result.Status == model.StatusSkipped:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.skipped")}
	case len(result.Measurements) == 0 && len(result.Tables) > 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.no_metric")}
	case len(result.Measurements) == 0:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.no_result")}
	default:
		result.SummaryMessages = []model.Message{model.NewMessage("probe.ookla.summary.values", ooklaMachineSummary(result))}
	}
	result.SummaryMessages = append(result.SummaryMessages, warningMessages...)
	result.Finish(start)
	return result
}

func markOoklaWarning(result *model.Result, warningMessages *[]model.Message, message model.Message) {
	result.Status = model.StatusWarning
	*warningMessages = append(*warningMessages, message)
}

func addOoklaIncompleteFieldsFailure(result *model.Result, target string, fields []string) {
	if len(fields) == 0 {
		return
	}
	result.AddFailure(model.Failure{
		Category: model.FailureParse,
		Stage:    "validate",
		Target:   target,
		Count:    1,
		Message:  "fields_incomplete=" + strings.Join(fields, ","),
	})
}

func appendOoklaSkipDetails(result *model.Result, reason, nextStep string) {
	result.Fields = append(result.Fields,
		model.Field{Key: "skip_reason", Label: "probe.ookla.field.skip_reason", Value: ooklaSkipReasonValue(reason)},
		model.Field{Key: "next_step", Label: "probe.ookla.field.next_step", Value: ooklaNextStepValue(nextStep)},
	)
}

func ooklaServerSelectionValue(selection string) model.Value {
	switch selection {
	case "automatic", "configured":
		return model.KeyValue("probe.ookla.server_selection." + selection)
	default:
		return model.RawValue(selection)
	}
}

func ooklaSkipReasonValue(reason string) model.Value {
	switch reason {
	case "exposure_denied", "tool_unavailable":
		return model.KeyValue("probe.ookla.skip_reason." + reason)
	default:
		return model.RawValue(reason)
	}
}

func ooklaNextStepValue(nextStep string) model.Value {
	switch nextStep {
	case "rerun_with_more_exposure", "install_official_client":
		return model.KeyValue("probe.ookla.next_step." + nextStep)
	default:
		return model.RawValue(nextStep)
	}
}

func runOfficialOokla(ctx context.Context, path string, args []string) (ooklaResult, error, error, bool) {
	var parsed ooklaResult
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(runCtx, path, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, runErr := command.CombinedOutput()
	if len(output) > 512*1024 {
		output = output[:512*1024]
	}
	parsed, parseErr := parseOoklaJSON(output)
	return parsed, runErr, parseErr, runCtx.Err() != nil
}

func formatOoklaServer(parsed ooklaResult) string {
	server := parsed.Server.Name
	if server == "" && parsed.Server.ID > 0 {
		server = "ID " + strconv.Itoa(parsed.Server.ID)
	}
	if parsed.Server.Location != "" {
		server += " · " + parsed.Server.Location
	}
	if parsed.Server.Country != "" {
		server += " · " + parsed.Server.Country
	}
	if server == "" {
		return "unknown"
	}
	return server
}

func ooklaCarrierKey(carrier string) string {
	switch strings.ToLower(strings.TrimSpace(carrier)) {
	case "电信", config.OoklaCarrierTelecom, "ct", "chinatelecom":
		return "telecom"
	case "联通", config.OoklaCarrierUnicom, "cu", "chinaunicom":
		return "unicom"
	case "移动", config.OoklaCarrierMobile, "cm", "chinamobile":
		return "mobile"
	default:
		return "auto"
	}
}

func ooklaCarrierValue(carrier string) model.Value {
	switch ooklaCarrierKey(carrier) {
	case config.OoklaCarrierTelecom, config.OoklaCarrierUnicom, config.OoklaCarrierMobile:
		return carrierMachineValue(ooklaCarrierKey(carrier))
	case config.OoklaCarrierAuto:
		if strings.EqualFold(strings.TrimSpace(carrier), config.OoklaCarrierAuto) || strings.TrimSpace(carrier) == "自动" {
			return model.KeyValue("probe.ookla.carrier.auto")
		}
	}
	return model.RawValue(carrier)
}

func parseOoklaJSON(output []byte) (ooklaResult, error) {
	var parsed ooklaResult
	text := strings.TrimSpace(string(output))
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return parsed, fmt.Errorf("未找到 JSON 对象")
	}
	jsonObject := []byte(text[start : end+1])
	if err := json.Unmarshal(jsonObject, &parsed); err != nil {
		return parsed, fmt.Errorf("解析 JSON 失败")
	}
	markOoklaFieldPresence(jsonObject, &parsed)
	return parsed, nil
}

func markOoklaFieldPresence(data []byte, parsed *ooklaResult) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return
	}
	nested := func(name string) map[string]json.RawMessage {
		raw, ok := object[name]
		if !ok || strings.TrimSpace(string(raw)) == "null" {
			return nil
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil
		}
		return fields
	}
	present := func(fields map[string]json.RawMessage, name string) bool {
		raw, ok := fields[name]
		return ok && strings.TrimSpace(string(raw)) != "null"
	}
	ping := nested("ping")
	download := nested("download")
	upload := nested("upload")
	parsed.presence = ooklaFieldPresence{
		PingJitter:        present(ping, "jitter"),
		PingLatency:       present(ping, "latency"),
		DownloadBandwidth: present(download, "bandwidth"),
		UploadBandwidth:   present(upload, "bandwidth"),
	}
}

func ooklaIncompleteRequiredFields(parsed ooklaResult) []string {
	fields := make([]string, 0, 3)
	if !parsed.presence.PingLatency || !isPositiveFinite(parsed.Ping.Latency) {
		fields = append(fields, "ping.latency")
	}
	if !parsed.presence.DownloadBandwidth || !isPositiveFinite(ooklaBandwidthMbps(parsed.Download.Bandwidth)) {
		fields = append(fields, "download.bandwidth")
	}
	if !parsed.presence.UploadBandwidth || !isPositiveFinite(ooklaBandwidthMbps(parsed.Upload.Bandwidth)) {
		fields = append(fields, "upload.bandwidth")
	}
	return fields
}

// ooklaPacketLoss returns packet loss only when the client supplied a value in
// its documented percentage range.  A missing, null, or out-of-range value is
// deliberately not turned into zero: zero is a valid measured loss value.
func ooklaPacketLoss(parsed ooklaResult) (float64, bool) {
	if parsed.PacketLoss == nil || !nonNegativeFinite(*parsed.PacketLoss) || *parsed.PacketLoss > 100 {
		return 0, false
	}
	return *parsed.PacketLoss, true
}

// ooklaHasValidMetric distinguishes a parsed JSON object from a usable result.
// In particular, an empty object must not count as a successful speedtest.
func ooklaHasValidMetric(parsed ooklaResult) bool {
	return isPositiveFinite(parsed.Ping.Latency) ||
		isPositiveFinite(ooklaBandwidthMbps(parsed.Download.Bandwidth)) ||
		isPositiveFinite(ooklaBandwidthMbps(parsed.Upload.Bandwidth))
}

// ooklaMeasurementsComplete requires all three core measurements.  Jitter and
// packet loss are valid even when measured as zero, and are not required for a
// row to be complete; when absent they remain visibly unavailable.
func ooklaMeasurementsComplete(parsed ooklaResult) bool {
	return isPositiveFinite(parsed.Ping.Latency) &&
		isPositiveFinite(ooklaBandwidthMbps(parsed.Download.Bandwidth)) &&
		isPositiveFinite(ooklaBandwidthMbps(parsed.Upload.Bandwidth))
}

func ooklaLatencyDisplay(value float64) string {
	if !isPositiveFinite(value) {
		return "—"
	}
	return fmt.Sprintf("%.2f ms", value)
}

func ooklaBandwidthDisplay(bytesPerSecond float64) string {
	if speed := ooklaBandwidthMbps(bytesPerSecond); isPositiveFinite(speed) {
		return fmt.Sprintf("%.2f Mbps", speed)
	}
	return "—"
}

func ooklaPacketLossDisplay(parsed ooklaResult) string {
	loss, ok := ooklaPacketLoss(parsed)
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.2f %%", loss)
}

func appendOoklaMeasurementsFor(result *model.Result, parsed ooklaResult, prefix string) {
	if isPositiveFinite(parsed.Ping.Latency) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_ping_ms", Label: "probe.ookla.metric.latency", Value: parsed.Ping.Latency, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", parsed.Ping.Latency)),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if (parsed.presence.PingJitter || parsed.Ping.Jitter > 0) && nonNegativeFinite(parsed.Ping.Jitter) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_jitter_ms", Label: "probe.ookla.metric.value", Value: parsed.Ping.Jitter, Unit: "ms", Display: model.RawValue(fmt.Sprintf("%.2f ms", parsed.Ping.Jitter)),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if speed := ooklaBandwidthMbps(parsed.Download.Bandwidth); isPositiveFinite(speed) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_download_mbps", Label: "probe.ookla.metric.download", Value: speed, Unit: "Mbps", Display: model.RawValue(fmt.Sprintf("%.2f Mbps", speed)),
			Method: "ookla-cli-json-v1-bandwidth-bytes-per-second", HigherIsBetter: model.BoolPtr(true),
		})
	}
	if speed := ooklaBandwidthMbps(parsed.Upload.Bandwidth); isPositiveFinite(speed) {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_upload_mbps", Label: "probe.ookla.metric.upload", Value: speed, Unit: "Mbps", Display: model.RawValue(fmt.Sprintf("%.2f Mbps", speed)),
			Method: "ookla-cli-json-v1-bandwidth-bytes-per-second", HigherIsBetter: model.BoolPtr(true),
		})
	}
	if loss, ok := ooklaPacketLoss(parsed); ok {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: prefix + "ookla_packet_loss_percent", Label: "probe.ookla.metric.loss", Value: loss, Unit: "%", Display: model.RawValue(fmt.Sprintf("%.2f %%", loss)),
			Method: "ookla-cli-json-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
}

func ooklaBandwidthMbps(bytesPerSecond float64) float64 {
	if !isPositiveFinite(bytesPerSecond) {
		return 0
	}
	value := bytesPerSecond * 8 / 1e6
	if !isPositiveFinite(value) {
		return 0
	}
	return value
}

func ooklaStatusKey(parsed ooklaResult, target string, failures []model.Failure) string {
	for _, failure := range failures {
		if failure.Target != target {
			continue
		}
		if failure.Stage == "parse" {
			return "probe.ookla.status.unparsed"
		}
		if failure.Stage == "execute" {
			return "probe.ookla.status.partial"
		}
	}
	if ooklaMeasurementsComplete(parsed) {
		return "probe.ookla.status.complete"
	}
	return "probe.ookla.status.partial"
}

func ooklaMachineSummary(result model.Result) string {
	parts := make([]string, 0, 4)
	for _, measurement := range result.Measurements {
		if measurement.Value > 0 {
			parts = append(parts, measurement.Key+"="+measurement.Display.Text())
		}
		if len(parts) >= 4 {
			break
		}
	}
	return strings.Join(parts, ";")
}

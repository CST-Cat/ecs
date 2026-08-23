package probe

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type routeProbe struct{}

func (routeProbe) ID() string         { return "route" }
func (routeProbe) Title() string      { return "module.route.title" }
func (routeProbe) NeedsNetwork() bool { return true }

type routeEngine struct {
	Name    string
	Path    string
	Version string
	SHA256  string
}

const (
	routeEngineTiny = "nexttrace-tiny"

	routeStatusComplete    = "probe.route.status.complete"
	routeStatusFailed      = "probe.route.status.failed"
	routeStatusParseFailed = "probe.route.status.parse_failed"
	routeStatusNoResponse  = "probe.route.status.no_response"
)

func (routeProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("route", "module.route.title")
	result.Description = "probe.route.description"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "methodology.protocol-measurement",
		Engine:          "probe.route.methodology.engine",
		Profile:         "probe.route.profile",
		ComparisonScope: "probe.route.comparison_scope",
	}

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.route.summary.tool_missing")}
		result.AddFailure(model.Failure{Category: model.FailureToolMissing, Stage: "tool_lookup", Target: routeEngineTiny, Count: 1})
		result.Evidence = model.NewEvidence(0, len(env.Config.RouteTargets), "target")
		result.Notes = []string{"probe.route.note.tool_missing"}
		result.Finish(start)
		return result
	}
	targets := endpointsForIPVersion(env.Config.RouteTargets, env.Config.IPVersion)
	if len(targets) == 0 {
		result.Status = model.StatusSkipped
		result.SummaryMessages = []model.Message{model.NewMessage("probe.route.summary.no_targets")}
		result.Evidence = model.NewEvidence(0, 0, "target")
		result.Notes = []string{"probe.route.note.no_targets"}
		result.Finish(start)
		return result
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "probe.route.field.engine", Value: engine.Name},
		{Key: "version", Label: "probe.route.field.version", Value: fallback(engine.Version, "unknown")},
		{Key: "binary_sha256", Label: "probe.route.field.binary_sha256", Value: fallback(engine.SHA256, "unavailable")},
		{Key: "arguments", Label: "probe.route.field.arguments", Value: strings.Join(routeCommandArgsForFamily(engine, "<target>", routeSnapshotHops, endpointFamily(targets[0], env.Config.IPVersion)), " ")},
	}
	result.Sources = append(result.Sources, model.Source{
		Name: "probe.route.source.nexttrace.name", URL: "https://github.com/nxtrace/NTrace-core", Purpose: "probe.route.source.nexttrace",
	})
	table := model.Table{
		Key:                   "network.route.summary",
		Title:                 "probe.route.table.summary",
		Columns:               []string{"probe.route.column.target", "probe.route.column.target_type", "probe.route.column.status", "probe.route.column.probed_hops", "probe.route.column.visible_hops", "probe.route.column.timeout_hops", "probe.route.column.duration"},
		ColumnKeys:            []string{"target", "target_type", "status", "probed_hops", "visible_hops", "timeout_hops", "elapsed_ms"},
		NumericColumns:        []int{3, 4, 5, 6},
		NumericHigherIsBetter: []bool{false, true, false, false},
	}
	successes := 0
	// validTraces is both usable evidence and the historical summary count:
	// parsed no-response traces count here, while only visible successes affect
	// the module warning status.
	validTraces := 0
	parseFailed := false
	for targetIndex, target := range targets {
		traceCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		traceStart := time.Now()
		output, err := runRouteCommandForFamily(traceCtx, engine, target.Address, routeSnapshotHops, endpointFamily(target, env.Config.IPVersion))
		elapsed := time.Since(traceStart)
		cancel()
		clean := sanitizeCommandOutput(output)
		slots, visible, timeouts, parsed := routeHopSummary(engine.Name, clean)
		status := routeStatusComplete
		switch {
		case err != nil:
			status = routeStatusFailed
			addFailure(&result, "trace", target.Address, err)
		case !parsed:
			status = routeStatusParseFailed
			parseFailed = true
			result.AddFailure(model.Failure{Category: model.FailureParse, Stage: "parse", Target: target.Address, Count: 1})
		case visible == 0:
			status = routeStatusNoResponse
			validTraces++
		default:
			successes++
			validTraces++
		}
		table.Rows = append(table.Rows, []string{
			target.Name, routeTargetKindKey(target.Kind), status, strconv.Itoa(slots), strconv.Itoa(visible), strconv.Itoa(timeouts), elapsed.Round(time.Millisecond).String(),
		})
		if parsed {
			prefix := fmt.Sprintf("route_target_%02d", targetIndex+1)
			result.Measurements = append(result.Measurements,
				model.Measurement{
					Key: prefix + "_hop_slots", Label: "probe.route.metric.hop_slots",
					Value: float64(slots), Unit: "hops", Display: strconv.Itoa(slots),
					Method: "nexttrace-tiny-json-v1", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_visible_hops", Label: "probe.route.metric.visible_hops",
					Value: float64(visible), Unit: "hops", Display: strconv.Itoa(visible),
					Method: "nexttrace-tiny-json-v1", HigherIsBetter: model.BoolPtr(true),
				},
				model.Measurement{
					Key: prefix + "_timeout_hops", Label: "probe.route.metric.timeout_hops",
					Value: float64(timeouts), Unit: "hops", Display: strconv.Itoa(timeouts),
					Method: "nexttrace-tiny-json-v1", HigherIsBetter: model.BoolPtr(false),
				},
				model.Measurement{
					Key: prefix + "_duration_ms", Label: "probe.route.metric.duration",
					Value: float64(elapsed) / float64(time.Millisecond), Unit: "ms", Display: elapsed.Round(time.Millisecond).String(),
					Method: "nexttrace-tiny-json-v1", HigherIsBetter: model.BoolPtr(false),
				},
			)
		}
		if clean != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:    "probe.route.raw_output",
				Language: "json",
				Content:  clean,
			})
		}
	}
	result.Tables = []model.Table{table}
	result.Evidence = model.NewEvidence(validTraces, len(targets), "target")
	if successes < len(targets) {
		result.Status = model.StatusWarning
	}
	result.Notes = append(result.Notes,
		"probe.route.note.forward_path",
		"probe.route.note.execution",
	)
	result.Notes = append(result.Notes,
		"probe.route.note.json",
	)
	if parseFailed {
		result.Notes = append(result.Notes, "probe.route.note.parse_failed")
	}
	result.SummaryMessages = []model.Message{model.NewMessage("probe.route.summary.values", validTraces, len(targets))}
	result.Finish(start)
	return result
}

func routeTargetKindKey(kind string) string {
	switch kind {
	case config.RouteTargetKindGlobal:
		return "probe.route.target_type.global"
	case config.RouteTargetKindMainlandChina:
		return "probe.route.target_type.mainland_china"
	default:
		return kind
	}
}

func detectRouteEngine(ctx context.Context) routeEngine {
	path, err := exec.LookPath(routeEngineTiny)
	if err != nil {
		return routeEngine{}
	}
	return routeEngine{
		Name:    routeEngineTiny,
		Path:    path,
		Version: commandVersion(ctx, path),
		SHA256:  binarySHA256(path),
	}
}

// routeSnapshotHops 是路径快照的跳数上限。
// RouteSnapshotHops 供 runner 组装比较签名，避免把 12 抄成第二份。
const routeSnapshotHops = 12

// RouteSnapshotHops 是 route 模块实际使用的跳数上限。
const RouteSnapshotHops = routeSnapshotHops

func runRouteCommandForFamily(ctx context.Context, engine routeEngine, target string, maxHops int, family string) ([]byte, error) {
	if !isNextTraceEngine(engine.Name) || engine.Path == "" {
		return nil, fmt.Errorf("unsupported route engine: %s", engine.Name)
	}
	args := routeCommandArgsForFamily(engine, target, maxHops, family)
	if len(args) == 0 {
		return nil, fmt.Errorf("unsupported route engine: %s", engine.Name)
	}
	command := exec.CommandContext(ctx, engine.Path, args...)
	command.Env = append(os.Environ(), "NO_COLOR=1", "LC_ALL=C", "LANG=C")
	var buffer bytes.Buffer
	command.Stdout = &buffer
	command.Stderr = &buffer
	err := command.Run()
	output := buffer.Bytes()
	if len(output) > 256*1024 {
		output = output[:256*1024]
	}
	return output, err
}

func routeCommandArgsForFamily(engine routeEngine, target string, maxHops int, family string) []string {
	hops := strconv.Itoa(maxHops)
	familyArg := ""
	if family == config.IPVersion4 || family == config.IPVersion6 {
		familyArg = "-" + family
	}
	switch engine.Name {
	case routeEngineTiny:
		args := []string{"--no-color", "--json", "-M", "--max-hops", hops, "--queries", "1", "--parallel-requests", "1", "--timeout", "1000"}
		if familyArg != "" {
			args = append([]string{familyArg}, args...)
		}
		return append(args, target)
	default:
		return nil
	}
}

func routeHopSummary(engineName, output string) (slots, visible, timeouts int, ok bool) {
	if !isNextTraceEngine(engineName) {
		return 0, 0, 0, false
	}
	details, ok := extractNextTraceDetails(output)
	if !ok {
		return 0, 0, 0, false
	}
	for _, hop := range details {
		if hop.IP != "" && hop.IP != "—" {
			visible++
		}
	}
	return len(details), visible, len(details) - visible, true
}

func isNextTraceEngine(name string) bool {
	return name == routeEngineTiny
}

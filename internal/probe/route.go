package probe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/model"
)

type routeProbe struct{}

func (routeProbe) ID() string         { return "route" }
func (routeProbe) Title() string      { return "路由追踪" }
func (routeProbe) NeedsNetwork() bool { return true }

type routeEngine struct {
	Name    string
	Path    string
	Version string
	SHA256  string
}

var (
	ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

func (routeProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("route", "路由追踪")
	result.Description = "使用 NextTrace 追踪多个参考目标"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议诊断",
		Engine:          "NextTrace",
		Profile:         "max 12 hops, one query",
		ComparisonScope: "当次正向路径快照；不是性能基准，也不代表回程",
	}

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Skip("未发现 NextTrace")
		result.Notes = append(result.Notes, "当前运行环境没有 NextTrace；run.sh 只会在 ECS_AUTO_DEPS 未关闭时从 NextTrace 官方 GitHub Release 临时准备已校验的 full 二进制，失败时跳过路由探测。")
		result.Finish(start)
		return result
	}
	targets := endpointsForIPVersion(env.Config.RouteTargets, env.Config.IPVersion)
	if len(targets) == 0 {
		result.Skip("没有匹配当前协议族的路由目标")
		result.Notes = append(result.Notes, "当前协议族没有可用的字面量路由目标；请用 --route-targets 提供对应协议族的目标地址。")
		result.Finish(start)
		return result
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "引擎", Value: engine.Name},
		{Key: "version", Label: "版本", Value: fallback(engine.Version, "unknown")},
		{Key: "binary_sha256", Label: "外部程序 SHA-256", Value: fallback(engine.SHA256, "unavailable")},
		{Key: "arguments", Label: "命令参数", Value: strings.Join(routeCommandArgsForFamily(engine, "<target>", routeSnapshotHops, endpointFamily(targets[0], env.Config.IPVersion)), " ") + "（按目标协议族）"},
	}
	result.Sources = append(result.Sources, model.Source{
		Name: "NextTrace", URL: "https://github.com/nxtrace/NTrace-core", Purpose: "以纯 JSON、无启动横幅模式执行路由追踪",
	})
	table := model.Table{
		Title:   "追踪摘要",
		Columns: []string{"目标", "类型", "状态", "已见跳数", "耗时"},
	}
	successes := 0
	for _, target := range targets {
		traceCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		traceStart := time.Now()
		output, err := runRouteCommandForFamily(traceCtx, engine, target.Address, routeSnapshotHops, endpointFamily(target, env.Config.IPVersion))
		elapsed := time.Since(traceStart)
		cancel()
		clean := sanitizeCommandOutput(output)
		hops := routeHopCount(engine.Name, clean)
		status := "完成"
		if err != nil {
			status = "部分/失败"
		} else {
			successes++
		}
		table.Rows = append(table.Rows, []string{target.Name, target.Kind, status, fmt.Sprintf("%d", hops), elapsed.Round(time.Millisecond).String()})
		if clean != "" {
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:     target.Name + " (" + target.Address + ")",
				Language:  "json",
				Content:   clean,
				Sensitive: true,
			})
		}
	}
	result.Tables = []model.Table{table}
	if successes == 0 {
		result.Status = model.StatusWarning
	}
	result.Notes = append(result.Notes,
		"这是从 VPS 到目标的正向路径，不等同于用户所在地到 VPS 的去程或三网回程。",
		"外部程序通过参数数组启动，不经过 shell；ecs 记录可安全读取的版本和程序 SHA-256。一次性依赖由 run.sh 在探针外准备并在退出时清理。",
	)
	result.Notes = append(result.Notes, "NextTrace 使用 --json 并关闭地图 URL；不会调用会输出推广内容的版本界面。")
	result.Summary = fmt.Sprintf("%d/%d 个目标完成 · NextTrace", successes, len(targets))
	result.Finish(start)
	return result
}

func detectRouteEngine(ctx context.Context) routeEngine {
	path, err := exec.LookPath("nexttrace")
	if err != nil {
		return routeEngine{}
	}
	version := ""
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	command := exec.CommandContext(commandCtx, path, "--version")
	output, _ := command.CombinedOutput()
	cancel()
	if text := strings.TrimSpace(string(output)); text != "" {
		version = strings.Split(sanitizeCommandOutput(output), "\n")[0]
	}
	return routeEngine{Name: "nexttrace", Path: path, Version: version, SHA256: binarySHA256(path)}
}

// routeSnapshotHops 是路径快照的跳数上限。
const routeSnapshotHops = 12

func runRouteCommand(ctx context.Context, engine routeEngine, target string, maxHops int) ([]byte, error) {
	return runRouteCommandForFamily(ctx, engine, target, maxHops, config.IPVersionAuto)
}

func runRouteCommandForFamily(ctx context.Context, engine routeEngine, target string, maxHops int, family string) ([]byte, error) {
	if engine.Name != "nexttrace" || engine.Path == "" {
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

// routeCommandArgs 组装路由追踪参数。
//
// maxHops 由调用方决定：路径快照 12 跳足够看清出口方向，但三网回程识别必须给到
// 更大的跳数——从海外到中国骨干通常要走 15 跳以上，截断会让骨干特征根本不出现。
func routeCommandArgs(engine routeEngine, target string, maxHops int) []string {
	return routeCommandArgsForFamily(engine, target, maxHops, config.IPVersionAuto)
}

func routeCommandArgsForFamily(engine routeEngine, target string, maxHops int, family string) []string {
	hops := strconv.Itoa(maxHops)
	familyArg := ""
	if family == config.IPVersion4 || family == config.IPVersion6 {
		familyArg = "-" + family
	}
	switch engine.Name {
	case "nexttrace":
		args := []string{"--no-color", "--json", "-M", "--max-hops", hops, "--queries", "1", "--parallel-requests", "1", "--timeout", "1000"}
		if familyArg != "" {
			args = append([]string{familyArg}, args...)
		}
		return append(args, target)
	default:
		return nil
	}
}

func routeHopCount(engineName, output string) int {
	if engineName != "nexttrace" {
		return 0
	}
	var payload struct {
		Hops [][]json.RawMessage `json:"Hops"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return 0
	}
	count := 0
	for _, hop := range payload.Hops {
		if len(hop) > 0 {
			count++
		}
	}
	return count
}

func binarySHA256(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func sanitizeCommandOutput(output []byte) string {
	text := strings.ReplaceAll(string(output), "\x00", "")
	text = ansiPattern.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)
	return text
}

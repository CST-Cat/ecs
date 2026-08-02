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
	hopPattern  = regexp.MustCompile(`(?m)^\s*\d+[.)]?\s+`)
)

func (routeProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("route", "路由追踪")
	result.Description = "使用系统现有路由工具追踪多个参考目标"
	result.Methodology = model.Methodology{
		Kind:            "protocol-measurement",
		Label:           "协议诊断",
		Engine:          "NextTrace/traceroute/tracepath",
		Profile:         "max 12 hops, one query",
		ComparisonScope: "当次正向路径快照；不是性能基准，也不代表回程",
	}

	engine := detectRouteEngine(ctx)
	if engine.Path == "" {
		result.Skip("未发现 nexttrace、traceroute 或 tracepath")
		result.Notes = append(result.Notes, "ecs 不会在探针内部静默下载外部二进制；run.sh 会优先临时准备系统 traceroute，安装 NextTrace 后重跑可获得更完整的路由标注。")
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
	if engine.Name == "nexttrace" {
		result.Sources = append(result.Sources, model.Source{
			Name: "NextTrace", URL: "https://github.com/nxtrace/NTrace-core", Purpose: "以纯 JSON、无启动横幅模式执行路由追踪",
		})
	}
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
			language := "text"
			if engine.Name == "nexttrace" {
				language = "json"
			}
			result.TextBlocks = append(result.TextBlocks, model.TextBlock{
				Title:     target.Name + " (" + target.Address + ")",
				Language:  language,
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
	if engine.Name == "nexttrace" {
		result.Notes = append(result.Notes, "NextTrace 使用 --json 并关闭地图 URL；不会调用会输出推广内容的版本界面。")
	}
	result.Summary = fmt.Sprintf("%d/%d 个目标完成 · %s", successes, len(targets), engine.Name)
	result.Finish(start)
	return result
}

func detectRouteEngine(ctx context.Context) routeEngine {
	for _, name := range []string{"nexttrace", "traceroute", "tracepath"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		version := ""
		if name != "nexttrace" {
			for _, args := range [][]string{{"--version"}, {"-V"}} {
				commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				command := exec.CommandContext(commandCtx, path, args...)
				output, _ := command.CombinedOutput()
				cancel()
				if text := strings.TrimSpace(string(output)); text != "" {
					version = strings.Split(sanitizeCommandOutput(output), "\n")[0]
					break
				}
			}
		}
		return routeEngine{Name: name, Path: path, Version: version, SHA256: binarySHA256(path)}
	}
	return routeEngine{}
}

// routeSnapshotHops 是路径快照的跳数上限。
const routeSnapshotHops = 12

func runRouteCommand(ctx context.Context, engine routeEngine, target string, maxHops int) ([]byte, error) {
	return runRouteCommandForFamily(ctx, engine, target, maxHops, config.IPVersionAuto)
}

func runRouteCommandForFamily(ctx context.Context, engine routeEngine, target string, maxHops int, family string) ([]byte, error) {
	args := routeCommandArgsForFamily(engine, target, maxHops, family)
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
	case "tracepath":
		args := []string{"-n", "-m", hops}
		if familyArg != "" {
			args = append([]string{familyArg}, args...)
		}
		return append(args, target)
	default:
		args := []string{"-n", "-m", hops, "-q", "1", "-w", "1"}
		if familyArg != "" {
			args = append([]string{familyArg}, args...)
		}
		return append(args, target)
	}
}

func routeHopCount(engineName, output string) int {
	if engineName == "nexttrace" {
		var payload struct {
			Hops [][]json.RawMessage `json:"Hops"`
		}
		if err := json.Unmarshal([]byte(output), &payload); err == nil {
			count := 0
			for _, hop := range payload.Hops {
				if len(hop) > 0 {
					count++
				}
			}
			return count
		}
	}
	return len(hopPattern.FindAllStringIndex(output, -1))
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

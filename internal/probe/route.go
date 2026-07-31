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
	"runtime"
	"strings"
	"time"

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
		result.Skip("未发现 nexttrace、traceroute、tracepath 或 tracert")
		result.Notes = append(result.Notes, "ecs 不会静默下载外部二进制；安装 NextTrace 后重跑即可获得更完整的路由标注。")
		result.Finish(start)
		return result
	}
	result.Fields = []model.Field{
		{Key: "engine", Label: "引擎", Value: engine.Name},
		{Key: "version", Label: "版本", Value: fallback(engine.Version, "unknown")},
		{Key: "binary_sha256", Label: "外部程序 SHA-256", Value: fallback(engine.SHA256, "unavailable")},
		{Key: "arguments", Label: "命令参数", Value: strings.Join(routeCommandArgs(engine, "<target>"), " ")},
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
	for _, target := range env.Config.RouteTargets {
		traceCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		traceStart := time.Now()
		output, err := runRouteCommand(traceCtx, engine, target.Address)
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
				Title:    target.Name + " (" + target.Address + ")",
				Language: language,
				Content:  clean,
			})
		}
	}
	result.Tables = []model.Table{table}
	if successes == 0 {
		result.Status = model.StatusWarning
	}
	result.Notes = append(result.Notes,
		"这是从 VPS 到目标的正向路径，不等同于用户所在地到 VPS 的去程或三网回程。",
		"外部程序通过参数数组启动，不经过 shell；ecs 记录可安全读取的版本和程序 SHA-256，且不自动安装依赖。",
	)
	if engine.Name == "nexttrace" {
		result.Notes = append(result.Notes, "NextTrace 使用 --json 并关闭地图 URL；不会调用会输出推广内容的版本界面。")
	}
	result.Summary = fmt.Sprintf("%d/%d 个目标完成 · %s", successes, len(env.Config.RouteTargets), engine.Name)
	result.Finish(start)
	return result
}

func detectRouteEngine(ctx context.Context) routeEngine {
	candidates := []string{"nexttrace", "traceroute", "tracepath"}
	if runtime.GOOS == "windows" {
		candidates = []string{"nexttrace.exe", "tracert.exe"}
	}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		version := ""
		engineName := strings.TrimSuffix(name, ".exe")
		if engineName != "nexttrace" {
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
		return routeEngine{Name: engineName, Path: path, Version: version, SHA256: binarySHA256(path)}
	}
	return routeEngine{}
}

func runRouteCommand(ctx context.Context, engine routeEngine, target string) ([]byte, error) {
	args := routeCommandArgs(engine, target)
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

func routeCommandArgs(engine routeEngine, target string) []string {
	switch engine.Name {
	case "nexttrace":
		return []string{"--no-color", "--json", "-M", "--max-hops", "12", "--queries", "1", "--parallel-requests", "1", "--timeout", "1000", target}
	case "tracepath":
		return []string{"-n", "-m", "12", target}
	case "tracert":
		return []string{"-d", "-h", "12", "-w", "1000", target}
	default:
		return []string{"-n", "-m", "12", "-q", "1", "-w", "1", target}
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

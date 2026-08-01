package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/runner"
	"ecs/internal/ui"
)

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	command := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "run":
		return runCommand(ctx, args, stdout, stderr)
	case "render":
		return renderCommand(args, stdout, stderr)
	case "list":
		return listCommand(stdout)
	case "config":
		return configCommand(args, stdout, stderr)
	case "doctor":
		return doctorCommand(ctx, stdout)
	case "version":
		fmt.Fprintf(stdout, "%s %s commit=%s built=%s go=%s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.Version())
		return 0
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "未知命令 %q\n\n", command)
		printHelp(stderr)
		return 1
	}
}

func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	configPath, profileFromCLI := preparse(args)
	var fileConfig config.File
	var err error
	if configPath != "" {
		fileConfig, err = config.LoadFile(configPath)
		if err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
	}
	profile := profileFromCLI
	if profile == "" {
		profile = fileConfig.Profile
	}
	cfg, err := config.Defaults(profile)
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	if err := config.ApplyFile(&cfg, fileConfig); err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	if cfg.Output == "" {
		cfg.Output = "./reports"
	}

	flags := flag.NewFlagSet("ecs run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profileFlag := flags.String("profile", cfg.Profile, "配置档：quick、standard、full")
	configFlag := flags.String("config", configPath, "JSON 配置文件")
	onlyFlag := flags.String("only", "", "只运行这些模块，逗号分隔")
	skipFlag := flags.String("skip", "", "跳过这些模块，逗号分隔")
	offlineFlag := flags.Bool("offline", cfg.Offline, "跳过全部外网探针")
	revealFlag := flags.Bool("reveal", cfg.Reveal, "在本地报告中保留完整 IP/主机名")
	ipSourcesFlag := flags.String("ip-quality-sources", strings.Join(cfg.IPQualitySources, ","), "IP 质量数据源：all、none 或逗号分隔名称")
	formatsFlag := flags.String("format", strings.Join(cfg.Formats, ","), "输出格式：json,md,html")
	outputFlag := flags.String("output", cfg.Output, "报告输出目录")
	nameFlag := flags.String("name", "", "报告文件名前缀")
	noColorFlag := flags.Bool("no-color", cfg.NoColor, "禁用 ANSI 颜色")
	cpuTimeFlag := flags.Duration("cpu-time", cfg.CPUTime, "每轮 CPU/内存测试时长")
	diskFlag := flags.Int("disk-mib", cfg.DiskMiB, "磁盘临时文件 MiB")
	diskPathFlag := flags.String("disk-path", cfg.DiskPath, "磁盘测试目录")
	diskMultiFlag := flags.Bool("disk-multi", cfg.DiskMulti, "额外测试系统盘之外的挂载盘")
	iperfDurationFlag := flags.Duration("iperf-duration", cfg.IPerfDuration, "iperf3 每个节点、每个方向的测试时长")
	threadsFlag := flags.Int("speed-threads", cfg.SpeedThreads, "测速并发流")
	timeoutFlag := flags.Duration("timeout", cfg.HTTPTimeout, "单次 HTTP 请求超时")
	mediaRegionFlag := flags.String("media-region", strings.Join(cfg.MediaRegions, ","), "流媒体检测地区：global、jp、tw、hk、cn（逗号分隔，默认全部）")
	backtraceCityFlag := flags.String("backtrace-city", "", "三网回程测试城市：beijing、guangzhou、shanghai、chengdu 或 all（默认 beijing,guangzhou）")
	strictFlag := flags.Bool("strict", false, "探针警告或错误时返回非零退出码")
	versionFlag := flags.Bool("version", false, "显示版本")
	flags.Usage = func() { printRunHelp(stderr, flags) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *versionFlag {
		fmt.Fprintf(stdout, "%s %s\n", buildinfo.Name, buildinfo.Version)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "错误: 多余参数 %s\n", strings.Join(flags.Args(), " "))
		return 1
	}
	_ = configFlag
	cfg.Profile = *profileFlag
	cfg.Offline = *offlineFlag
	cfg.Reveal = *revealFlag
	cfg.IPQualitySources = config.ParseList(*ipSourcesFlag)
	cfg.Formats = config.ParseList(*formatsFlag)
	cfg.Output = *outputFlag
	cfg.NoColor = *noColorFlag
	cfg.CPUTime = *cpuTimeFlag
	cfg.DiskMiB = *diskFlag
	cfg.DiskPath = *diskPathFlag
	cfg.DiskMulti = *diskMultiFlag
	cfg.IPerfDuration = *iperfDurationFlag
	cfg.SpeedThreads = *threadsFlag
	cfg.HTTPTimeout = *timeoutFlag
	if regions := config.ParseList(*mediaRegionFlag); len(regions) > 0 {
		if err := config.ValidateMediaRegions(regions); err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		cfg.MediaRegions = regions
	}
	if *backtraceCityFlag != "" {
		cities, err := config.ParseBacktraceCities(*backtraceCityFlag)
		if err != nil {
			fmt.Fprintln(stderr, "错误:", err)
			return 1
		}
		cfg.BacktraceTargets = config.BacktraceTargetsFor(cities)
	}
	cfg.Modules = config.SelectModules(cfg.Modules, config.ParseList(*onlyFlag), config.ParseList(*skipFlag))
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}

	terminal := ui.New(stdout, cfg.NoColor)
	terminal.Header(cfg, config.EstimateFor(cfg))
	raw := runner.Run(ctx, cfg, terminal.Progress)
	data := model.RedactedCopy(raw, cfg.Reveal)
	files, writeErr := reporter.WriteFiles(data, cfg.Output, *nameFlag, cfg.Formats)
	if writeErr != nil {
		terminal.Error("报告写入失败：%v", writeErr)
		return 1
	}
	terminal.Summary(data, files)
	if data.Run.Canceled {
		return 130
	}
	if *strictFlag && (data.Summary.Errors > 0 || data.Summary.Warnings > 0) {
		return 2
	}
	return 0
}

func renderCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "ecs JSON 报告路径")
	formats := flags.String("format", "md,html", "输出格式")
	output := flags.String("output", "", "输出目录，默认与输入文件相同")
	name := flags.String("name", "", "报告文件名前缀")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" {
		fmt.Fprintln(stderr, "错误: --input 必填")
		return 1
	}
	data, err := reporter.LoadJSON(*input)
	if err != nil {
		fmt.Fprintln(stderr, "错误: 读取报告:", err)
		return 1
	}
	if *output == "" {
		*output = filepath.Dir(*input)
	}
	if *name == "" {
		base := filepath.Base(*input)
		*name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	written, err := reporter.WriteFiles(data, *output, *name, config.ParseList(*formats))
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	keys := make([]string, 0, len(written))
	for key := range written {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(stdout, "%s %s\n", strings.ToUpper(key), written[key])
	}
	return 0
}

func listCommand(stdout io.Writer) int {
	fmt.Fprintln(stdout, "配置档:")
	fmt.Fprintln(stdout, "  quick     低资源快速筛查，不执行大流量测速和路由")
	fmt.Fprintln(stdout, "  standard  默认综合测试")
	fmt.Fprintln(stdout, "  full      更长性能测试、更大流量并包含路由")
	fmt.Fprintln(stdout, "\n模块:")
	descriptions := map[string]string{
		"system":    "系统、虚拟化、资源、内核网络栈",
		"network":   "IPv4/IPv6、原生/广播、六库风险分与九库风险因子",
		"cpu":       "sysbench 标准 CPU 基准",
		"memory":    "sysbench 标准内存基准",
		"disk":      "fio Direct I/O 标准基准",
		"dns":       "公共 DNS 延迟、失败率与抖动",
		"latency":   "TCP 建连延迟与可达率",
		"speed":     "iperf3 多节点标准吞吐基准",
		"ports":     "Web、SSH、DNS 与邮件出站端口",
		"nat":       "STUN 探测 UDP 映射/过滤行为与 NAT 类型",
		"blacklist": "17 个 DNS 黑名单收录情况 + 反向解析 FCrDNS 校验",
		"apps":      "Telegram DC、代码/镜像/软件源/证书服务可达性",
		"cnspeed":   "中国电信/联通/移动就近节点 HTTP 下载带宽（仅 full 档）",
		"media":     "流媒体与 AI 服务公开页证据（可按 --media-region 筛选）",
		"route":     "NextTrace/系统 traceroute 适配器",
		"backtrace": "三网回程线路识别（电信/联通/移动骨干特征）",
	}
	for _, id := range config.ModuleOrder {
		fmt.Fprintf(stdout, "  %-10s %s\n", id, descriptions[id])
	}
	fmt.Fprintln(stdout, "\nIP 质量数据源:")
	fmt.Fprintln(stdout, "  maxmind, ipinfo, ipregistry, ipapi, ip2location, abuseipdb,")
	fmt.Fprintln(stdout, "  scamalytics, ipqs, dbip, ipdata, ipwhois, ipapicom, ipsb,")
	fmt.Fprintln(stdout, "  virustotal, ipgeolocation, bigdatacloud, getipintel")
	fmt.Fprintln(stdout, "  默认 all；none 关闭附加质量查询（出口发现仍访问 ipapi.is）")
	return 0
}

func configCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "example" {
		fmt.Fprintln(stderr, "用法: ecs config example")
		return 1
	}
	content, err := json.MarshalIndent(config.ExampleFile(), "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "错误:", err)
		return 1
	}
	fmt.Fprintln(stdout, string(content))
	return 0
}

func doctorCommand(ctx context.Context, stdout io.Writer) int {
	fmt.Fprintln(stdout, "ecs 标准基准依赖")
	tools := []struct {
		name     string
		required bool
		purpose  string
		args     []string
	}{
		{name: "sysbench", required: true, purpose: "CPU / 内存", args: []string{"--version"}},
		{name: "fio", required: true, purpose: "磁盘 Direct I/O", args: []string{"--version"}},
		{name: "iperf3", required: true, purpose: "网络吞吐", args: []string{"--version"}},
		{name: "nexttrace", purpose: "高级路由", args: []string{"--version"}},
		{name: "traceroute", purpose: "基础路由", args: []string{"--version"}},
		{name: "mbw", purpose: "内存带宽（补充口径）", args: []string{"-h"}},
		{name: "ioping", purpose: "I/O 延迟（补充口径）", args: []string{"-v"}},
		{name: "smartctl", purpose: "磁盘健康（需 root）", args: []string{"--version"}},
	}
	missingRequired := false
	for _, tool := range tools {
		path, err := exec.LookPath(tool.name)
		if err != nil {
			label := "可选"
			if tool.required {
				label = "缺失"
				missingRequired = true
			}
			fmt.Fprintf(stdout, "  %-11s %-4s %s\n", tool.name, label, tool.purpose)
			continue
		}
		versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		output, runErr := exec.CommandContext(versionCtx, path, tool.args...).CombinedOutput()
		cancel()
		version := strings.TrimSpace(string(output))
		if newline := strings.IndexByte(version, '\n'); newline >= 0 {
			version = version[:newline]
		}
		if runErr != nil || version == "" {
			version = "版本未知"
		}
		fmt.Fprintf(stdout, "  %-11s 就绪  %s · %s\n", tool.name, tool.purpose, version)
	}
	if missingRequired {
		fmt.Fprintln(stdout, "\n安装：./install.sh --with-benchmarks")
		fmt.Fprintln(stdout, "缺失时任何配置档都会明确警告对应标准成绩未运行，不会生成替代分数。")
		return 2
	}
	fmt.Fprintln(stdout, "\n标准性能工具已就绪。")
	return 0
}

func preparse(args []string) (configPath, profile string) {
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--config" && index+1 < len(args):
			configPath = args[index+1]
			index++
		case strings.HasPrefix(value, "--config="):
			configPath = strings.TrimPrefix(value, "--config=")
		case value == "--profile" && index+1 < len(args):
			profile = args[index+1]
			index++
		case strings.HasPrefix(value, "--profile="):
			profile = strings.TrimPrefix(value, "--profile=")
		}
	}
	return configPath, profile
}

func printHelp(writer io.Writer) {
	fmt.Fprintln(writer, `ecs — 无广告、默认零上传的 VPS 综合测试工具

用法:
  ecs [run] [选项]            运行测试（默认 standard）
  ecs list                    查看配置档与模块
  ecs render --input FILE     从 JSON 重新导出 Markdown/HTML
  ecs config example          输出配置文件示例
  ecs doctor                  检查标准基准工具
  ecs version                 显示版本

常用示例:
  ecs
  ecs --profile quick --offline
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html

运行 ecs run --help 查看全部参数。`)
}

func printRunHelp(writer io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(writer, "用法: ecs [run] [选项]")
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n模块:", strings.Join(config.ModuleOrder, ","))
	fmt.Fprintln(writer, "IP 质量数据源: maxmind,ipinfo,ipregistry,ipapi,ip2location,abuseipdb,scamalytics,ipqs,dbip,ipdata,ipwhois,ipapicom,ipsb,virustotal,ipgeolocation,bigdatacloud,getipintel")
}

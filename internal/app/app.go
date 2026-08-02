package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	reporter "ecs/internal/report"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/ui"
)

func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	// 语言要在任何输出之前定下来：帮助文本、错误信息都要用它。
	// 显式 --lang 优先，其次看环境变量，最后回落中文。
	i18n.Set(resolveLanguage(args))
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
	case "baseline":
		return baselineCommand(args, stdout, stderr)
	case "submit":
		return submitCommand(args, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "%s %s commit=%s built=%s go=%s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtime.Version())
		return 0
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "%s %q\n\n", i18n.T("cli.unknownCommand"), command)
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
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
	}
	profile := profileFromCLI
	if profile == "" {
		profile = fileConfig.Profile
	}
	cfg, err := config.Defaults(profile)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if err := config.ApplyFile(&cfg, fileConfig); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if cfg.Output == "" {
		cfg.Output = "./reports"
	}

	flags := flag.NewFlagSet("ecs run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	profileFlag := flags.String("profile", cfg.Profile, i18n.T("flag.profile"))
	configFlag := flags.String("config", configPath, i18n.T("flag.config"))
	onlyFlag := flags.String("only", "", i18n.T("flag.only"))
	skipFlag := flags.String("skip", "", i18n.T("flag.skip"))
	exposureFlag := flags.String("exposure", cfg.Exposure.String(), i18n.T("flag.exposure"))
	acceptFlag := flags.String("accept", strings.Join(cfg.Accepted, ","), i18n.T("flag.accept"))
	revealFlag := flags.Bool("reveal", cfg.Reveal, i18n.T("flag.reveal"))
	ipVersionFlag := flags.String("ip-version", cfg.IPVersion, i18n.T("flag.ipVersion"))
	ipv4Flag := flags.Bool("4", false, i18n.T("flag.ipv4"))
	ipv6Flag := flags.Bool("6", false, i18n.T("flag.ipv6"))
	ipSourcesFlag := flags.String("ip-quality-sources", strings.Join(cfg.IPQualitySources, ","), i18n.T("flag.ipQualitySources"))
	formatsFlag := flags.String("format", strings.Join(cfg.Formats, ","), i18n.T("flag.format"))
	outputFlag := flags.String("output", cfg.Output, i18n.T("flag.output"))
	nameFlag := flags.String("name", "", i18n.T("flag.name"))
	noColorFlag := flags.Bool("no-color", cfg.NoColor, i18n.T("flag.noColor"))
	colorFlag := flags.String("color", "auto", i18n.T("flag.color"))
	baselineFlag := flags.String("score-baseline", "", i18n.T("flag.scoreBaseline"))
	cpuTimeFlag := flags.Duration("cpu-time", cfg.CPUTime, i18n.T("flag.cpuTime"))
	diskFlag := flags.Int("disk-mib", cfg.DiskMiB, i18n.T("flag.diskMiB"))
	diskPathFlag := flags.String("disk-path", cfg.DiskPath, i18n.T("flag.diskPath"))
	diskMultiFlag := flags.Bool("disk-multi", cfg.DiskMulti, i18n.T("flag.diskMulti"))
	iperfDurationFlag := flags.Duration("iperf-duration", cfg.IPerfDuration, i18n.T("flag.iperfDuration"))
	threadsFlag := flags.Int("speed-threads", cfg.SpeedThreads, i18n.T("flag.speedThreads"))
	timeoutFlag := flags.Duration("timeout", cfg.HTTPTimeout, i18n.T("flag.timeout"))
	dnsAttemptsFlag := flags.Int("dns-attempts", cfg.DNSAttempts, i18n.T("flag.dnsAttempts"))
	latencyAttemptsFlag := flags.Int("latency-attempts", cfg.LatencyAttempts, i18n.T("flag.latencyAttempts"))
	dnsResolversFlag := flags.String("dns-resolvers", "", i18n.T("flag.dnsResolvers"))
	latencyTargetsFlag := flags.String("latency-targets", "", i18n.T("flag.latencyTargets"))
	routeTargetsFlag := flags.String("route-targets", "", i18n.T("flag.routeTargets"))
	stunServersFlag := flags.String("stun-servers", "", i18n.T("flag.stunServers"))
	iperfTargetsFlag := flags.String("iperf-targets", "", i18n.T("flag.iperfTargets"))
	mediaRegionFlag := flags.String("media-region", strings.Join(cfg.MediaRegions, ","), i18n.T("flag.mediaRegion"))
	backtraceCityFlag := flags.String("backtrace-city", "", i18n.T("flag.backtraceCity"))
	backtraceTargetsFlag := flags.String("backtrace-targets", "", i18n.T("flag.backtraceTargets"))
	ooklaServersFlag := flags.String("ookla-servers", "", i18n.T("flag.ooklaServers"))
	interactiveFlag := flags.Bool("interactive", false, i18n.T("flag.interactive"))
	yesFlag := flags.Bool("yes", false, i18n.T("flag.yes"))
	strictFlag := flags.Bool("strict", false, i18n.T("flag.strict"))
	versionFlag := flags.Bool("version", false, i18n.T("flag.version"))
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
		fmt.Fprintf(stderr, "%s %s\n", i18n.T("help.extraArgs"), strings.Join(flags.Args(), " "))
		return 1
	}
	_ = configFlag
	cfg.Profile = *profileFlag
	exposure, err := config.ParseExposure(*exposureFlag)
	if err != nil {
		fmt.Fprintf(stderr, "%s: --exposure: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.Exposure = exposure
	cfg.Accepted = config.ParseList(*acceptFlag)
	if err := config.ValidateAccepted(cfg.Accepted); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.Reveal = *revealFlag
	cfg.IPVersion = strings.ToLower(strings.TrimSpace(*ipVersionFlag))
	if *ipv4Flag && *ipv6Flag {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.ipv4AndIPv6"))
		return 1
	}
	if *ipv4Flag {
		cfg.IPVersion = config.IPVersion4
	}
	if *ipv6Flag {
		cfg.IPVersion = config.IPVersion6
	}
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
	cfg.DNSAttempts = *dnsAttemptsFlag
	cfg.LatencyAttempts = *latencyAttemptsFlag
	for _, override := range []struct {
		raw         string
		requirePort bool
		apply       func([]config.Endpoint)
		label       string
	}{
		{*dnsResolversFlag, true, func(e []config.Endpoint) { cfg.DNSResolvers = e }, "dns-resolvers"},
		{*latencyTargetsFlag, true, func(e []config.Endpoint) { cfg.LatencyTargets = e }, "latency-targets"},
		{*routeTargetsFlag, false, func(e []config.Endpoint) { cfg.RouteTargets = e }, "route-targets"},
		{*stunServersFlag, true, func(e []config.Endpoint) { cfg.STUNServers = e }, "stun-servers"},
	} {
		if override.raw == "" {
			continue
		}
		endpoints, err := config.ParseEndpointList(override.raw, override.requirePort)
		if err != nil {
			fmt.Fprintf(stderr, "%s: --%s: %v\n", i18n.T("cli.error"), override.label, err)
			return 1
		}
		override.apply(endpoints)
	}
	if *iperfTargetsFlag != "" {
		targets, err := config.ParseIPerfTargetList(*iperfTargetsFlag)
		if err != nil {
			fmt.Fprintf(stderr, "%s: --iperf-targets: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		cfg.IPerfTargets = targets
	}
	if regions := config.ParseList(*mediaRegionFlag); len(regions) > 0 {
		if err := config.ValidateMediaRegions(regions); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		cfg.MediaRegions = regions
	}
	if *backtraceCityFlag != "" {
		cities, err := config.ParseBacktraceCities(*backtraceCityFlag)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		cfg.BacktraceTargets = config.BacktraceTargetsFor(cities)
	}
	if *backtraceTargetsFlag != "" {
		targets, err := config.ParseEndpointList(*backtraceTargetsFlag, false)
		if err != nil {
			fmt.Fprintf(stderr, "%s: --backtrace-targets: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		cfg.BacktraceTargets = targets
	}
	if *ooklaServersFlag != "" {
		servers, err := config.ParseOoklaServerList(*ooklaServersFlag)
		if err != nil {
			fmt.Fprintf(stderr, "%s: --ookla-servers: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		cfg.OoklaServers = servers
	}
	// --accept 同时表达"我知道这是什么"和"我要跑它"，因此并入模块集。
	cfg.Modules = config.MergeAccepted(cfg.Modules, cfg.Accepted)
	named := config.ParseList(*onlyFlag)
	cfg.Modules = config.SelectModules(cfg.Modules, named, config.ParseList(*skipFlag))
	// 用户亲手点名的模块若越过外联上限就报错；档位带进来的静默过滤。
	if err := config.CheckModuleExposure(named, cfg.Exposure, cfg.Accepted); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.Modules = config.FilterModulesByExposure(cfg.Modules, cfg.Exposure, cfg.Accepted)
	// 交互向导：显式 --interactive 才启动，--yes 永远跳过。
	// run.sh 在检测到可用终端且用户没传测试参数时会自动加上 --interactive，
	// 因此 `curl … | sh` 会进向导，而带参数的调用直接开跑。
	if *interactiveFlag && !*yesFlag {
		if !runWizard(&cfg, stdout) {
			return 0
		}
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	// run.sh 的交互模式先让向导选档位和模块，再按计划准备组件。
	// 计划文件只在临时工作目录中使用，不属于用户报告或持久配置。
	if planPath := os.Getenv("ECS_PLAN_FILE"); planPath != "" {
		if err := writeOneShotPlan(planPath, cfg); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
			return 1
		}
		return 0
	}

	baseline := score.EmbeddedBaseline()
	if *baselineFlag != "" {
		loaded, err := score.LoadBaseline(*baselineFlag)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.baselineLoad", *baselineFlag, err))
			return 1
		}
		baseline = loaded
	}

	terminal := ui.New(stdout, cfg.NoColor)
	terminal.Header(cfg, config.EstimateFor(cfg))
	raw := runner.Run(ctx, cfg, terminal.Progress)
	data := model.RedactedCopy(raw, cfg.Reveal)
	scored := score.Compute(data, baseline)

	// 写进文件的 txt 默认不着色：报告会被 diff、贴进不解析转义序列的地方。
	// --color always 才把颜色写进文件，auto 只影响终端直出。
	textColor := termcolor.LevelNone
	if requested, ok := termcolor.ParseLevel(*colorFlag); ok {
		textColor = requested
	} else if strings.EqualFold(*colorFlag, "always") {
		textColor = termcolor.Detect(true)
		if textColor == termcolor.LevelNone {
			textColor = termcolor.LevelANSI256
		}
	}
	if cfg.NoColor {
		textColor = termcolor.LevelNone
	}
	files, writeErr := reporter.WriteFilesWithOptions(data, cfg.Output, *nameFlag, cfg.Formats,
		reporter.Options{TextColor: textColor, Score: scored})
	if writeErr != nil {
		terminal.Error("%s: %v", i18n.T("term.writeFailed"), writeErr)
		return 1
	}
	terminal.Summary(reporter.Localize(data), files)
	if data.Run.Canceled {
		return 130
	}
	if *strictFlag && (data.Summary.Errors > 0 || data.Summary.Warnings > 0) {
		return 2
	}
	return 0
}

func writeOneShotPlan(path string, cfg config.Runtime) error {
	content := cfg.Profile + "\n" + strings.Join(cfg.Modules, ",") + "\n"
	return os.WriteFile(path, []byte(content), 0o600)
}

func renderCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ecs render", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// 语言已由 Main 扫描原始参数设置，这里定义只为让 --lang 通过解析。
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	input := flags.String("input", "", i18n.T("flag.renderInput"))
	formats := flags.String("format", "md,html", i18n.T("flag.format"))
	output := flags.String("output", "", i18n.T("flag.renderOutput"))
	name := flags.String("name", "", i18n.T("flag.name"))
	renderColor := flags.String("color", "auto", i18n.T("flag.color"))
	renderBaseline := flags.String("score-baseline", "", i18n.T("flag.scoreBaseline"))
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" {
		fmt.Fprintln(stderr, i18n.T("help.renderInputRequired"))
		return 1
	}
	data, err := reporter.LoadJSON(*input)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	if *output == "" {
		*output = filepath.Dir(*input)
	}
	if *name == "" {
		base := filepath.Base(*input)
		*name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	// 重新导出时同样算分：同一份 JSON 换个基线再看分数，是评分能被检验的前提。
	baseline := score.EmbeddedBaseline()
	if *renderBaseline != "" {
		loaded, loadErr := score.LoadBaseline(*renderBaseline)
		if loadErr != nil {
			fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), i18n.Errorf("err.baselineLoad", *renderBaseline, loadErr))
			return 1
		}
		baseline = loaded
	}
	textColor := termcolor.LevelNone
	if requested, ok := termcolor.ParseLevel(*renderColor); ok {
		textColor = requested
	} else if strings.EqualFold(*renderColor, "always") {
		if textColor = termcolor.Detect(true); textColor == termcolor.LevelNone {
			textColor = termcolor.LevelANSI256
		}
	}
	written, err := reporter.WriteFilesWithOptions(data, *output, *name, config.ParseList(*formats),
		reporter.Options{TextColor: textColor, Score: score.Compute(data, baseline)})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
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
	fmt.Fprintln(stdout, i18n.T("list.profilesHeader"))
	for _, profile := range []string{config.ProfileQuick, config.ProfileStandard, config.ProfileFull} {
		fmt.Fprintf(stdout, "  %-10s %s\n", profile, i18n.T("profile."+profile))
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("list.modulesHeader"))
	for _, id := range config.ModuleOrder {
		fmt.Fprintf(stdout, "  %-10s %-11s %s\n", id, config.ModuleExposureName(id), i18n.T("module."+id+".desc"))
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("list.exposureHeader"))
	for _, name := range config.ExposureNames() {
		fmt.Fprintf(stdout, "  %-11s %s\n", name, i18n.T("exposure."+name))
	}
	fmt.Fprintln(stdout, "  "+i18n.T("list.exposureNote"))
	fmt.Fprintln(stdout, "\n"+i18n.T("list.sourcesHeader"))
	fmt.Fprintln(stdout, "  maxmind, ipinfo, ipregistry, ipapi, ip2location, abuseipdb,")
	fmt.Fprintln(stdout, "  scamalytics, ipqs, dbip, ipdata, ipwhois, ipapicom, ipsb")
	fmt.Fprintln(stdout, "  "+i18n.T("list.sourcesNote"))
	return 0
}

func configCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "example" {
		fmt.Fprintln(stderr, i18n.T("help.configUsage"))
		return 1
	}
	content, err := json.MarshalIndent(config.ExampleFile(), "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	fmt.Fprintln(stdout, string(content))
	return 0
}

func doctorCommand(ctx context.Context, stdout io.Writer) int {
	fmt.Fprintln(stdout, i18n.T("doctor.header"))
	tools := []struct {
		name     string
		required bool
		purpose  string
		args     []string
	}{
		{name: "sysbench", required: true, purpose: i18n.T("doctor.purpose.sysbench"), args: []string{"--version"}},
		{name: "fio", required: true, purpose: i18n.T("doctor.purpose.fio"), args: []string{"--version"}},
		{name: "iperf3", required: true, purpose: i18n.T("doctor.purpose.iperf3"), args: []string{"--version"}},
		{name: "nexttrace", purpose: i18n.T("doctor.purpose.nexttrace"), args: []string{"--version"}},
		{name: "traceroute", purpose: i18n.T("doctor.purpose.traceroute"), args: []string{"--version"}},
		{name: "mbw", purpose: i18n.T("doctor.purpose.mbw"), args: []string{"-h"}},
		{name: "ioping", purpose: i18n.T("doctor.purpose.ioping"), args: []string{"-v"}},
		{name: "smartctl", purpose: i18n.T("doctor.purpose.smartctl"), args: []string{"--version"}},
		{name: "speedtest", purpose: i18n.T("doctor.purpose.speedtest"), args: []string{"--version"}},
	}
	missingRequired := false
	for _, tool := range tools {
		path, err := exec.LookPath(tool.name)
		if err != nil {
			label := i18n.T("doctor.optional")
			if tool.required {
				label = i18n.T("doctor.missing")
				missingRequired = true
			}
			fmt.Fprintf(stdout, "  %-11s %s %s\n", tool.name, padDisplay(label, 8), tool.purpose)
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
			version = i18n.T("doctor.unknownVersion")
		}
		fmt.Fprintf(stdout, "  %-11s %s %s · %s\n", tool.name, padDisplay(i18n.T("doctor.ready"), 8), tool.purpose, version)
	}
	if missingRequired {
		fmt.Fprintln(stdout, "\n"+i18n.T("doctor.installHint"))
		fmt.Fprintln(stdout, i18n.T("doctor.noSubstitute"))
		return 2
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("doctor.allReady"))
	return 0
}

// resolveLanguage 在解析命令前先把 --lang 取出来。
//
// flag 包要等到子命令解析时才能拿到值，但帮助与错误输出比那更早，
// 因此这里先扫一遍参数。
func resolveLanguage(args []string) i18n.Lang {
	for index := 0; index < len(args); index++ {
		value := args[index]
		var raw string
		switch {
		case (value == "--lang" || value == "-lang") && index+1 < len(args):
			raw = args[index+1]
		case strings.HasPrefix(value, "--lang="):
			raw = strings.TrimPrefix(value, "--lang=")
		case strings.HasPrefix(value, "-lang="):
			raw = strings.TrimPrefix(value, "-lang=")
		default:
			continue
		}
		if lang, ok := i18n.Parse(raw); ok {
			return lang
		}
	}
	return i18n.DetectFromEnv()
}

// padDisplay 按显示宽度右填充。
//
// %-Ns 按字节计数，中日韩字符一个占三字节却只显示两列，直接用会让中英文两种
// 语言下的列宽都对不齐。
func padDisplay(value string, width int) string {
	display := 0
	for _, character := range value {
		if unicode.Is(unicode.Han, character) || unicode.Is(unicode.Hiragana, character) ||
			unicode.Is(unicode.Katakana, character) || unicode.Is(unicode.Hangul, character) {
			display += 2
			continue
		}
		display++
	}
	if display >= width {
		return value
	}
	return value + strings.Repeat(" ", width-display)
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
  ecs baseline REPORTS...     从多份报告聚合评分基线
  ecs submit --input FILE     导出可公开入库的瘦身提交
  ecs version                 显示版本

常用示例:
  ecs
  ecs --profile quick --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html

运行 ecs run --help 查看全部参数。`)
}

func printRunHelp(writer io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(writer, i18n.T("help.runUsage"))
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(config.ModuleOrder, ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": maxmind,ipinfo,ipregistry,ipapi,ip2location,abuseipdb,scamalytics,ipqs,dbip,ipdata,ipwhois,ipapicom,ipsb")
}

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
	case "compare":
		return compareCommand(args, stdout, stderr)
	case "list":
		return listCommand(args, stdout, stderr)
	case "config":
		return configCommand(args, stdout, stderr)
	case "doctor":
		return doctorCommand(ctx, stdout)
	case "leaderboard":
		return leaderboardCommand(args, stdout, stderr)
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
	named := config.ParseList(*onlyFlag)
	cfg.Modules = config.SelectModules(cfg.Modules, named, config.ParseList(*skipFlag))
	// 用户亲手点名的模块若越过外联上限就报错；档位带进来的静默过滤。
	if err := config.CheckModuleExposure(named, cfg.Exposure); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.Modules = config.FilterModulesByExposure(cfg.Modules, cfg.Exposure)
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

	terminalColor := resolveTerminalColor(*colorFlag, cfg.NoColor, stdout)
	terminal := ui.NewWithColor(stdout, terminalColor)
	terminal.Header(cfg, config.EstimateFor(cfg))
	// runner 只负责收集结果；终端报告必须等所有模块完成后一次性渲染，
	// 否则并行模块的进度行会把完整报告拆成无法复制的碎片。进度视图只显示
	// 模块计数、当前运行模块和总耗时；只有错误会留下进度历史，warning 等
	// 结果统一留给最终报告，不提前渲染任何探针详情。
	progress := terminal.BeginProgress(len(cfg.Modules))
	raw := func() model.Report {
		defer progress.EndProgress()
		return runner.Run(ctx, cfg, progress.Update)
	}()
	data := model.RedactedCopy(raw, cfg.Reveal)
	scored := score.Compute(data, baseline)

	// 写进文件的 txt 默认不着色：报告会被 diff、贴进不解析转义序列的地方。
	// --color always 才把颜色写进文件，auto 只影响终端直出。
	textColor := resolveFileTextColor(*colorFlag, cfg.NoColor)
	files, writeErr := reporter.WriteFilesWithOptions(data, cfg.Output, *nameFlag, cfg.Formats,
		reporter.Options{TextColor: textColor, Score: scored})
	if writeErr != nil {
		terminal.Error("%s: %v", i18n.T("term.writeFailed"), writeErr)
		return 1
	}
	terminal.FullReport(reporter.Localize(data), files, scored, terminalColor)
	if data.Run.Canceled {
		return 130
	}
	if *strictFlag && (data.Summary.Errors > 0 || data.Summary.Warnings > 0) {
		return 2
	}
	return 0
}

// resolveTerminalColor 解析终端完整报告的颜色层级。
//
// auto 遵循终端能力探测，always 即使 stdout 不是 TTY 也强制使用至少 256 色；
// 显式 none 和 --no-color 都彻底关闭颜色，后者优先于所有其他选项。
func resolveTerminalColor(raw string, noColor bool, out io.Writer) termcolor.Level {
	if noColor {
		return termcolor.LevelNone
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return termcolor.Detect(writerIsTerminal(out))
	case "always":
		level := termcolor.Detect(true)
		if level == termcolor.LevelNone {
			level = termcolor.LevelANSI256
		}
		return level
	}
	if level, ok := termcolor.ParseLevel(raw); ok {
		return level
	}
	// 未知值按无色处理；flag 的帮助文本会列出合法值。
	return termcolor.LevelNone
}

// resolveFileTextColor 保留 --color 对 TXT 文件的既有语义：auto 默认无色，
// 只有显式 always 或具体能力档位才把 ANSI 写入文件。
func resolveFileTextColor(raw string, noColor bool) termcolor.Level {
	if noColor {
		return termcolor.LevelNone
	}
	if level, ok := termcolor.ParseLevel(raw); ok {
		return level
	}
	if strings.EqualFold(strings.TrimSpace(raw), "always") {
		level := termcolor.Detect(true)
		if level == termcolor.LevelNone {
			level = termcolor.LevelANSI256
		}
		return level
	}
	return termcolor.LevelNone
}

func writerIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
	formats := flags.String("format", "json,txt,md,html", i18n.T("flag.format"))
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

func listCommand(args []string, stdout, stderr io.Writer) int {
	format := ""
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--machine":
			format = "machine"
		case args[index] == "--format" && index+1 < len(args):
			format = strings.ToLower(strings.TrimSpace(args[index+1]))
			index++
		case strings.HasPrefix(args[index], "--format="):
			format = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(args[index], "--format=")))
		case args[index] == "--lang" && index+1 < len(args):
			// resolveLanguage already handled this before command dispatch.
			index++
		case strings.HasPrefix(args[index], "--lang="):
			// The equals form has already been consumed by language resolution.
		default:
			fmt.Fprintf(stderr, "%s: %s\n", i18n.T("cli.error"), i18n.T("help.extraArgs"))
			return 1
		}
	}
	if format == "machine" || format == "manifest" {
		return writeModuleManifest(stdout)
	}
	if format != "" {
		fmt.Fprintf(stderr, "%s: unsupported list format %q\n", i18n.T("cli.error"), format)
		return 1
	}
	fmt.Fprintln(stdout, i18n.T("list.profilesHeader"))
	for _, profile := range []string{config.ProfileStandard, config.ProfileFull} {
		fmt.Fprintf(stdout, "  %-10s %s\n", profile, i18n.T("profile."+profile))
	}
	fmt.Fprintln(stdout, "\n"+i18n.T("list.modulesHeader"))
	for _, descriptor := range config.ModuleDescriptors() {
		descriptionKey := descriptor.DescriptionKey
		if descriptionKey == "" {
			descriptionKey = "module." + descriptor.ID + ".desc"
		}
		fmt.Fprintf(stdout, "  %-10s %-11s %s\n", descriptor.ID, descriptor.Exposure.String(), i18n.T(descriptionKey))
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

// writeModuleManifest emits a locale-independent, line-oriented descriptor
// view for wrappers such as run.sh.  It is deliberately additive: the normal
// `ecs list` output remains the human-facing interface, while this mode gives
// downloaded wrappers a stable way to plan modules and tools without copying
// the registry into shell code.
func writeModuleManifest(stdout io.Writer) int {
	fmt.Fprintln(stdout, "ecs-module-manifest\t1")
	for _, profile := range []string{config.ProfileStandard, config.ProfileFull} {
		fmt.Fprintf(stdout, "profile\t%s\t%s\n", profile, strings.Join(config.ModulesForProfile(profile), ","))
	}
	for _, descriptor := range config.ModuleDescriptors() {
		fmt.Fprintf(stdout, "module\t%s\t%s\t%s\n", descriptor.ID, descriptor.Exposure.String(), strings.Join(descriptor.RequiredTools, ","))
	}
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
	tools := doctorTools()
	missingRequired := false
	for _, tool := range tools {
		path, err := lookupDoctorTool(tool)
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
		var version string
		var runErr error
		if tool.check != nil {
			// Some tools, notably STREAM, have no version/help mode that is
			// safe to execute: running them starts the benchmark. Identify
			// those binaries from their embedded official markers instead.
			version, runErr = tool.check(versionCtx, path)
		} else {
			var output []byte
			output, runErr = exec.CommandContext(versionCtx, path, tool.args...).CombinedOutput()
			version = strings.TrimSpace(string(output))
		}
		cancel()
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

type doctorTool struct {
	name     string
	required bool
	purpose  string
	args     []string
	lookup   func() (string, error)
	check    func(context.Context, string) (string, error)
}

func lookupDoctorTool(tool doctorTool) (string, error) {
	if tool.lookup != nil {
		return tool.lookup()
	}
	return exec.LookPath(tool.name)
}

func lookupNextTrace() (string, error) {
	path, err := exec.LookPath("nexttrace-tiny")
	if err != nil {
		return "", err
	}
	return path, nil
}

// doctorTools derives the dependency list from the module descriptors.  The
// order and human-facing purpose strings remain stable for existing users,
// while newly described tools are surfaced automatically instead of being
// silently omitted from `ecs doctor`.
func doctorTools() []doctorTool {
	type toolMeta struct {
		name     string
		required bool
		purpose  string
		args     []string
		lookup   func() (string, error)
		check    func(context.Context, string) (string, error)
	}
	catalog := []toolMeta{
		{name: "sysbench", required: true, purpose: "doctor.purpose.sysbench", args: []string{"--version"}},
		{name: "fio", required: true, purpose: "doctor.purpose.fio", args: []string{"--version"}},
		{name: "iperf3", required: true, purpose: "doctor.purpose.iperf3", args: []string{"--version"}},
		{name: "stream", required: true, purpose: "doctor.purpose.stream", check: identifyOfficialStream},
		{name: "nexttrace-tiny", purpose: "doctor.purpose.nexttrace", args: []string{"--version"}, lookup: lookupNextTrace},
		{name: "ping", purpose: "doctor.purpose.ping", args: []string{"-V"}},
		{name: "speedtest", purpose: "doctor.purpose.speedtest", args: []string{"--version"}},
	}
	meta := make(map[string]toolMeta, len(catalog))
	for _, item := range catalog {
		meta[item.name] = item
	}
	descriptors := config.ModuleDescriptors()
	known := make(map[string]bool)
	toolOrder := make([]string, 0)
	for _, descriptor := range descriptors {
		for _, name := range descriptor.RequiredTools {
			name = strings.TrimSpace(name)
			if name == "" || known[name] {
				continue
			}
			known[name] = true
			toolOrder = append(toolOrder, name)
			if _, ok := meta[name]; ok {
				continue
			}
			// Unknown tools are still checked.  A descriptor can add a tool
			// before its dedicated purpose text is added; keeping the id visible
			// is safer than silently treating the dependency as optional.
			meta[name] = toolMeta{name: name, purpose: name, args: []string{"--version"}}
		}
	}
	// Keep required benchmark checks even if descriptor data is incomplete.
	// This gives a clear diagnostic if a descriptor drops a core dependency.
	for _, item := range catalog {
		if item.required {
			known[item.name] = true
		}
	}
	if len(descriptors) == 0 {
		for _, item := range catalog {
			known[item.name] = true
		}
	}
	tools := make([]doctorTool, 0, len(known))
	for _, item := range catalog {
		if !known[item.name] {
			continue
		}
		tools = append(tools, doctorTool{name: item.name, required: item.required, purpose: i18n.T(item.purpose), args: item.args, lookup: item.lookup, check: item.check})
		delete(known, item.name)
	}
	// Preserve descriptor order for tools not in the stable catalog.
	for _, name := range toolOrder {
		if !known[name] {
			continue
		}
		item := meta[name]
		tools = append(tools, doctorTool{name: item.name, required: item.required, purpose: i18n.T(item.purpose), args: item.args, lookup: item.lookup, check: item.check})
	}
	return tools
}

// identifyOfficialStream performs a read-only marker check. The official
// STREAM program is a benchmark executable, not a conventional CLI: invoking
// it with --version or --help starts a full memory run. A build from the
// official source carries these table/header markers in its read-only data.
func identifyOfficialStream(_ context.Context, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	for _, marker := range []string{"STREAM version", "Number of Threads requested", "Best Rate", "Function"} {
		if !strings.Contains(text, marker) {
			return "", fmt.Errorf("official STREAM marker %q not found", marker)
		}
	}
	return "official STREAM", nil
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
	if i18n.Current() == i18n.LangEN {
		fmt.Fprintln(writer, `ecs — ad-free VPS benchmark with local reports by default

Usage:
  ecs [run] [options]         run tests (standard by default)
  ecs list                    show profiles and modules
  ecs render --input FILE     re-export JSON/txt/md/html from JSON
  ecs compare REPORTS...      compare 2 or more JSON reports safely
  ecs config example          print a sample configuration
  ecs doctor                  check standard benchmark tools
  ecs leaderboard REPORTS...  aggregate a leaderboard reference
  ecs baseline REPORTS...     generate a leaderboard reference (same as leaderboard)
  ecs submit --input FILE     export a minimized public submission
  ecs version                 show version

Examples:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,txt,md,html --output ./compare

Run ecs run --help for all test options or ecs compare --help for comparison options.`)
		return
	}
	fmt.Fprintln(writer, `ecs — 无广告、默认零上传的 VPS 综合测试工具

用法:
  ecs [run] [选项]            运行测试（默认 standard）
  ecs list                    查看配置档与模块
  ecs render --input FILE     从 JSON 重新导出 JSON/txt/md/html 四种格式
  ecs compare REPORTS...      安全比较 2 份或更多 JSON 报告
  ecs config example          输出配置文件示例
  ecs doctor                  检查标准基准工具
  ecs leaderboard REPORTS...  从多份报告聚合排行榜参考
  ecs baseline REPORTS...     生成当前排行榜参考（与 leaderboard 相同）
  ecs submit --input FILE     导出可公开入库的瘦身提交
  ecs version                 显示版本

常用示例:
  ecs
  ecs --profile standard --exposure local
  ecs --profile full --exposure public
  ecs --profile full --skip media --output ./reports
  ecs --only system,cpu,memory,disk --format json,html
  ecs compare old.json new.json --format json,txt,md,html --output ./compare

运行 ecs run --help 查看测试参数，或运行 ecs compare --help 查看对比参数。`)
}

func printRunHelp(writer io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(writer, i18n.T("help.runUsage"))
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(config.ModuleIDs(), ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": maxmind,ipinfo,ipregistry,ipapi,ip2location,abuseipdb,scamalytics,ipqs,dbip,ipdata,ipwhois,ipapicom,ipsb")
}

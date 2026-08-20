package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ecs/internal/buildinfo"
	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/model"
	"ecs/internal/probe"
	reporter "ecs/internal/report"
	"ecs/internal/runner"
	"ecs/internal/score"
	"ecs/internal/termcolor"
	"ecs/internal/ui"
)

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
	diskMatrixModeFlag := flags.String("disk-matrix-mode", cfg.DiskMatrixMode, i18n.T("flag.diskMatrixMode"))
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
	diskMatrixMode, err := config.ParseDiskMatrixMode(*diskMatrixModeFlag)
	if err != nil {
		fmt.Fprintf(stderr, "%s: --disk-matrix-mode: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.DiskMatrixMode = diskMatrixMode
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
	if err := config.CheckModuleExposure(named, cfg.Exposure); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
	cfg.Modules = config.FilterModulesByExposure(cfg.Modules, cfg.Exposure)
	if *interactiveFlag && !*yesFlag {
		if !runWizard(&cfg, stdout) {
			return 0
		}
	}
	if err := config.Validate(cfg); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", i18n.T("cli.error"), err)
		return 1
	}
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
	terminal.Header(cfg, probe.EstimateFor(cfg))
	progress := terminal.BeginProgress(len(cfg.Modules))
	raw := func() model.Report {
		defer progress.EndProgress()
		return runner.Run(ctx, cfg, progress.Update)
	}()
	data := model.RedactedCopy(raw, cfg.Reveal)
	scored := score.Compute(data, baseline)

	files, writeErr := reporter.WriteFilesWithOptions(data, cfg.Output, *nameFlag, cfg.Formats,
		reporter.Options{Score: scored})
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

func printRunHelp(writer io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(writer, i18n.T("help.runUsage"))
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(config.ModuleIDs(), ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": maxmind,ipinfo,ipregistry,ipapi,ip2location,abuseipdb,scamalytics,ipqs,dbip,ipdata,ipwhois,ipapicom,ipsb")
}

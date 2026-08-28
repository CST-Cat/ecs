package app

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
)

type resolvedRunConfig struct {
	Runtime       config.Runtime
	Name          string
	Color         string
	ScoreBaseline string
	Interactive   bool
	Yes           bool
	Strict        bool
	Version       bool
}

type runFlagParseError struct{ err error }

func (e runFlagParseError) Error() string { return e.err.Error() }
func (e runFlagParseError) Unwrap() error { return e.err }

// resolveRunConfig is the single CLI/file/defaults resolver for run-like
// commands. It deliberately stops before interactive mutation and execution;
// callers may run the wizard and then validate the resulting Runtime.
func resolveRunConfig(args []string, stderr io.Writer) (resolvedRunConfig, error) {
	configPath, profileFromCLI := preparse(args)
	var fileConfig config.File
	var err error
	if configPath != "" {
		fileConfig, err = config.LoadFile(configPath)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
	}
	profile := profileFromCLI
	if profile == "" {
		profile = fileConfig.Profile
	}
	cfg, err := config.Defaults(profile)
	if err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if err := config.ApplyFile(&cfg, fileConfig); err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if cfg.Output == "" {
		cfg.Output = "./reports"
	}

	flags := flag.NewFlagSet("ecs run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.String("lang", string(i18n.Current()), i18n.T("flag.lang"))
	profileFlag := flags.String("profile", cfg.Profile, i18n.T("flag.profile"))
	flags.String("config", configPath, i18n.T("flag.config"))
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
		return resolvedRunConfig{}, runFlagParseError{err: err}
	}
	// Preserve the historical CLI contract: --version short-circuits before
	// positional-argument and run-specific override validation.
	if *versionFlag {
		cfg.NoColor = *noColorFlag
		return resolvedRunConfig{Runtime: cfg, Color: *colorFlag, Version: true}, nil
	}
	if flags.NArg() != 0 {
		return resolvedRunConfig{}, fmt.Errorf("%s %s", i18n.T("help.extraArgs"), strings.Join(flags.Args(), " "))
	}

	cfg.Profile = *profileFlag
	exposure, err := config.ParseExposure(*exposureFlag)
	if err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: --exposure: %v", i18n.T("cli.error"), err)
	}
	cfg.Exposure = exposure
	cfg.Reveal = *revealFlag
	cfg.IPVersion = strings.ToLower(strings.TrimSpace(*ipVersionFlag))
	if *ipv4Flag && *ipv6Flag {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), i18n.Errorf("err.ipv4AndIPv6"))
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
		return resolvedRunConfig{}, fmt.Errorf("%s: --disk-matrix-mode: %v", i18n.T("cli.error"), err)
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
			return resolvedRunConfig{}, fmt.Errorf("%s: --%s: %v", i18n.T("cli.error"), override.label, err)
		}
		override.apply(endpoints)
	}
	if *iperfTargetsFlag != "" {
		targets, err := config.ParseIPerfTargetList(*iperfTargetsFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --iperf-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.IPerfTargets = targets
	}
	if regions := config.ParseList(*mediaRegionFlag); len(regions) > 0 {
		if err := config.ValidateMediaRegions(regions); err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
		cfg.MediaRegions = regions
	}
	if *backtraceCityFlag != "" {
		cities, err := config.ParseBacktraceCities(*backtraceCityFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
		cfg.BacktraceTargets = config.BacktraceTargetsFor(cities)
	}
	if *backtraceTargetsFlag != "" {
		targets, err := config.ParseBacktraceTargetList(*backtraceTargetsFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --backtrace-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.BacktraceTargets = targets
	}
	if *ooklaServersFlag != "" {
		servers, err := config.ParseOoklaServerList(*ooklaServersFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --ookla-servers: %v", i18n.T("cli.error"), err)
		}
		cfg.OoklaServers = servers
	}
	named := config.ParseList(*onlyFlag)
	cfg.Modules = config.SelectModules(cfg.Modules, named, config.ParseList(*skipFlag))
	if err := config.CheckModuleExposure(named, cfg.Exposure); err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	cfg.Modules = config.FilterModulesByExposure(cfg.Modules, cfg.Exposure)

	return resolvedRunConfig{
		Runtime:       cfg,
		Name:          *nameFlag,
		Color:         *colorFlag,
		ScoreBaseline: *baselineFlag,
		Interactive:   *interactiveFlag,
		Yes:           *yesFlag,
		Strict:        *strictFlag,
		Version:       false,
	}, nil
}

func preparse(args []string) (configPath, profile string) {
	for _, occurrence := range scanEarlyFlags(args, "config", "profile") {
		switch occurrence.Name {
		case "config":
			configPath = occurrence.Value
		case "profile":
			profile = occurrence.Value
		}
	}
	return configPath, profile
}

func printRunHelp(writer io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(writer, i18n.T("help.runUsage"))
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(config.ModuleIDs(), ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": maxmind,ipinfo,ipregistry,ipapi,ip2location,abuseipdb,scamalytics,ipqs,dbip,ipdata,ipwhois,ipapicom,ipsb")
}

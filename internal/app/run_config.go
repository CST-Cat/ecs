package app

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/module"
	"ecs/internal/probe"
)

type resolvedRunConfig struct {
	Runtime       config.Runtime
	Catalog       module.Catalog
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

type runLanguageFlag struct {
	value string
	seen  bool
}

func (f *runLanguageFlag) String() string { return f.value }

func (f *runLanguageFlag) Set(value string) error {
	f.value = value
	f.seen = true
	if strings.HasPrefix(value, "-") {
		return errors.New("--lang requires a value")
	}
	if strings.TrimSpace(value) != "" {
		if language, ok := i18n.Parse(value); ok {
			i18n.Set(language)
		}
	}
	return nil
}

// resolveRunConfig is the single CLI/file/defaults resolver for run-like
// commands. It deliberately stops before interactive mutation and execution;
// callers may run the wizard and then validate the resulting Runtime.
func resolveRunConfig(args []string, stderr io.Writer) (resolvedRunConfig, error) {
	catalog := probe.BuiltinCatalog()
	flags := flag.NewFlagSet("ecs run", flag.ContinueOnError)
	parseOutput := &bytes.Buffer{}
	flags.SetOutput(parseOutput)
	languageFlag := &runLanguageFlag{}
	flags.Var(languageFlag, "lang", "flag.lang")
	helpFlag := flags.Bool("help", false, "")
	hFlag := flags.Bool("h", false, "")
	profileFlag := flags.String("profile", "", "flag.profile")
	configFlag := flags.String("config", "", "flag.config")
	onlyFlag := flags.String("only", "", "flag.only")
	skipFlag := flags.String("skip", "", "flag.skip")
	exposureFlag := flags.String("exposure", "", "flag.exposure")
	revealFlag := flags.Bool("reveal", false, "flag.reveal")
	ipVersionFlag := flags.String("ip-version", "", "flag.ipVersion")
	ipv4Flag := flags.Bool("4", false, "flag.ipv4")
	ipv6Flag := flags.Bool("6", false, "flag.ipv6")
	ipSourcesFlag := flags.String("ip-quality-sources", "", "flag.ipQualitySources")
	formatsFlag := flags.String("format", "", "flag.format")
	outputFlag := flags.String("output", "", "flag.output")
	nameFlag := flags.String("name", "", "flag.name")
	noColorFlag := flags.Bool("no-color", false, "flag.noColor")
	colorFlag := flags.String("color", "auto", "flag.color")
	baselineFlag := flags.String("score-baseline", "", "flag.scoreBaseline")
	cpuTimeFlag := flags.Duration("cpu-time", 0, "flag.cpuTime")
	diskFlag := flags.Int("disk-mib", 0, "flag.diskMiB")
	diskPathFlag := flags.String("disk-path", "", "flag.diskPath")
	diskMultiFlag := flags.Bool("disk-multi", false, "flag.diskMulti")
	diskMatrixModeFlag := flags.String("disk-matrix-mode", "", "flag.diskMatrixMode")
	iperfDurationFlag := flags.Duration("iperf-duration", 0, "flag.iperfDuration")
	threadsFlag := flags.Int("speed-threads", 0, "flag.speedThreads")
	timeoutFlag := flags.Duration("timeout", 0, "flag.timeout")
	dnsAttemptsFlag := flags.Int("dns-attempts", 0, "flag.dnsAttempts")
	latencyAttemptsFlag := flags.Int("latency-attempts", 0, "flag.latencyAttempts")
	dnsResolversFlag := flags.String("dns-resolvers", "", "flag.dnsResolvers")
	latencyTargetsFlag := flags.String("latency-targets", "", "flag.latencyTargets")
	routeTargetsFlag := flags.String("route-targets", "", "flag.routeTargets")
	stunServersFlag := flags.String("stun-servers", "", "flag.stunServers")
	iperfTargetsFlag := flags.String("iperf-targets", "", "flag.iperfTargets")
	mediaRegionFlag := flags.String("media-region", "", "flag.mediaRegion")
	backtraceCityFlag := flags.String("backtrace-city", "", "flag.backtraceCity")
	backtraceTargetsFlag := flags.String("backtrace-targets", "", "flag.backtraceTargets")
	ooklaServersFlag := flags.String("ookla-servers", "", "flag.ooklaServers")
	interactiveFlag := flags.Bool("interactive", false, "flag.interactive")
	yesFlag := flags.Bool("yes", false, "flag.yes")
	strictFlag := flags.Bool("strict", false, "flag.strict")
	versionFlag := flags.Bool("version", false, "flag.version")
	flags.Usage = func() { printRunHelp(catalog, parseOutput, flags) }
	if err := flags.Parse(args); err != nil {
		if *helpFlag || *hFlag || errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			printRunHelp(catalog, stderr, flags)
			return resolvedRunConfig{}, runFlagParseError{err: flag.ErrHelp}
		}
		_, _ = io.Copy(stderr, parseOutput)
		return resolvedRunConfig{}, runFlagParseError{err: err}
	}
	if languageFlag.seen {
		occurrence := languageFlagOccurrence{Value: languageFlag.value}
		if err := validateExplicitLanguage([]languageFlagOccurrence{occurrence}); err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
	}
	if *helpFlag || *hFlag {
		flags.SetOutput(stderr)
		printRunHelp(catalog, stderr, flags)
		return resolvedRunConfig{}, runFlagParseError{err: flag.ErrHelp}
	}
	explicit := make(map[string]bool)
	flags.Visit(func(flag *flag.Flag) { explicit[flag.Name] = true })

	configPath := ""
	if explicit["config"] {
		configPath = *configFlag
	}
	var fileConfig config.File
	var err error
	if configPath != "" {
		fileConfig, err = config.LoadFile(configPath)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
	}
	profile := fileConfig.Profile
	if explicit["profile"] {
		profile = *profileFlag
	}
	// Keep one precedence pipeline: built-in defaults selected by profile, then
	// config-file values, then only explicitly supplied CLI values. Callers
	// apply the final Runtime validation after this resolver returns.
	cfg, err := config.Defaults(catalog, profile)
	if err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if err := config.ApplyFile(catalog, &cfg, fileConfig); err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if cfg.Output == "" {
		cfg.Output = "./reports"
	}
	// Preserve the historical CLI contract: --version short-circuits before
	// positional-argument and run-specific override validation.
	if *versionFlag {
		if explicit["no-color"] {
			cfg.NoColor = *noColorFlag
		}
		return resolvedRunConfig{Runtime: cfg, Catalog: catalog, Color: *colorFlag, Version: true}, nil
	}
	if flags.NArg() != 0 {
		return resolvedRunConfig{}, fmt.Errorf("%s %s", i18n.T("help.extraArgs"), strings.Join(flags.Args(), " "))
	}

	if explicit["exposure"] {
		exposure, err := config.ParseExposure(*exposureFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --exposure: %v", i18n.T("cli.error"), err)
		}
		cfg.Exposure = exposure
	}
	if explicit["reveal"] {
		cfg.Reveal = *revealFlag
	}
	if explicit["ip-version"] {
		cfg.IPVersion = strings.ToLower(strings.TrimSpace(*ipVersionFlag))
	}
	if *ipv4Flag && *ipv6Flag {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), i18n.Errorf("err.ipv4AndIPv6"))
	}
	if *ipv4Flag {
		cfg.IPVersion = config.IPVersion4
	}
	if *ipv6Flag {
		cfg.IPVersion = config.IPVersion6
	}
	if explicit["ip-quality-sources"] {
		cfg.IPQualitySources = config.ParseList(*ipSourcesFlag)
	}
	cfg.Formats = config.ParseList(strings.Join(cfg.Formats, ","))
	if explicit["format"] {
		cfg.Formats = config.ParseList(*formatsFlag)
	}
	if explicit["output"] {
		cfg.Output = *outputFlag
	}
	if explicit["no-color"] {
		cfg.NoColor = *noColorFlag
	}
	if explicit["cpu-time"] {
		cfg.CPUTime = *cpuTimeFlag
	}
	if explicit["disk-mib"] {
		cfg.DiskMiB = *diskFlag
	}
	if explicit["disk-path"] {
		cfg.DiskPath = *diskPathFlag
	}
	if explicit["disk-multi"] {
		cfg.DiskMulti = *diskMultiFlag
	}
	if explicit["disk-matrix-mode"] {
		diskMatrixMode, err := config.ParseDiskMatrixMode(*diskMatrixModeFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --disk-matrix-mode: %v", i18n.T("cli.error"), err)
		}
		cfg.DiskMatrixMode = diskMatrixMode
	}
	if explicit["iperf-duration"] {
		cfg.IPerfDuration = *iperfDurationFlag
	}
	if explicit["speed-threads"] {
		cfg.SpeedThreads = *threadsFlag
	}
	if explicit["timeout"] {
		cfg.HTTPTimeout = *timeoutFlag
	}
	if explicit["dns-attempts"] {
		cfg.DNSAttempts = *dnsAttemptsFlag
	}
	if explicit["latency-attempts"] {
		cfg.LatencyAttempts = *latencyAttemptsFlag
	}
	for _, override := range []struct {
		flagName    string
		raw         string
		requirePort bool
		apply       func([]config.Endpoint)
		label       string
	}{
		{"dns-resolvers", *dnsResolversFlag, true, func(e []config.Endpoint) { cfg.DNSResolvers = e }, "dns-resolvers"},
		{"latency-targets", *latencyTargetsFlag, true, func(e []config.Endpoint) { cfg.LatencyTargets = e }, "latency-targets"},
		{"route-targets", *routeTargetsFlag, false, func(e []config.Endpoint) { cfg.RouteTargets = e }, "route-targets"},
		{"stun-servers", *stunServersFlag, true, func(e []config.Endpoint) { cfg.STUNServers = e }, "stun-servers"},
	} {
		if !explicit[override.flagName] {
			continue
		}
		endpoints, err := config.ParseEndpointList(override.raw, override.requirePort)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --%s: %v", i18n.T("cli.error"), override.label, err)
		}
		override.apply(endpoints)
	}
	if explicit["iperf-targets"] {
		targets, err := config.ParseIPerfTargetList(*iperfTargetsFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --iperf-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.IPerfTargets = targets
	}
	if explicit["media-region"] {
		// 合法性由 config.Validate 无条件校验，这里只负责解析。
		cfg.MediaRegions = config.ParseList(*mediaRegionFlag)
	}
	if explicit["backtrace-city"] {
		if len(config.ParseList(*backtraceCityFlag)) == 0 {
			cfg.BacktraceTargets = []config.Endpoint{}
		} else {
			cities, err := config.ParseBacktraceCities(*backtraceCityFlag)
			if err != nil {
				return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
			}
			cfg.BacktraceTargets = config.BacktraceTargetsFor(cities)
		}
	}
	if explicit["backtrace-targets"] {
		targets, err := config.ParseBacktraceTargetList(*backtraceTargetsFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --backtrace-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.BacktraceTargets = targets
	}
	if explicit["ookla-servers"] {
		servers, err := config.ParseOoklaServerList(*ooklaServersFlag)
		if err != nil {
			return resolvedRunConfig{}, fmt.Errorf("%s: --ookla-servers: %v", i18n.T("cli.error"), err)
		}
		cfg.OoklaServers = servers
	}
	named := config.ParseList(*onlyFlag)
	skipped := config.ParseList(*skipFlag)
	if err := config.ValidateModuleSelection(catalog, named, skipped); err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	cfg.Modules = config.SelectModules(catalog, cfg.Modules, named, skipped)
	if err := config.CheckModuleExposure(catalog, named, cfg.Exposure); err != nil {
		return resolvedRunConfig{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	cfg.Modules = config.FilterModulesByExposure(catalog, cfg.Modules, cfg.Exposure)

	return resolvedRunConfig{
		Runtime:       cfg,
		Catalog:       catalog,
		Name:          *nameFlag,
		Color:         *colorFlag,
		ScoreBaseline: *baselineFlag,
		Interactive:   *interactiveFlag,
		Yes:           *yesFlag,
		Strict:        *strictFlag,
		Version:       false,
	}, nil
}

func printRunHelp(catalog module.Catalog, writer io.Writer, flags *flag.FlagSet) {
	type savedUsage struct {
		parsedFlag *flag.Flag
		usage      string
	}
	var originalUsages []savedUsage
	flags.VisitAll(func(parsedFlag *flag.Flag) {
		if !strings.HasPrefix(parsedFlag.Usage, "flag.") {
			return
		}
		originalUsages = append(originalUsages, savedUsage{parsedFlag: parsedFlag, usage: parsedFlag.Usage})
		parsedFlag.Usage = i18n.T(parsedFlag.Usage)
	})
	defer func() {
		for _, saved := range originalUsages {
			saved.parsedFlag.Usage = saved.usage
		}
	}()
	fmt.Fprintln(writer, i18n.T("help.runUsage"))
	flags.PrintDefaults()
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(config.ModuleIDs(catalog), ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": "+strings.Join(config.IPQualitySourceIDs(), ","))
}

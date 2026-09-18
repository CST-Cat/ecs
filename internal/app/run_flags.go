package app

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/module"
)

type parsedRunFlags struct {
	explicit   map[string]bool
	positional []string

	configPath string
	profile    string
	only       string
	skip       string
	exposure   string
	reveal     bool
	ipVersion  string
	ipv4       bool
	ipv6       bool

	ipQualitySources string
	formats          string
	output           string
	name             string
	noColor          bool
	color            string
	scoreBaseline    string

	cpuTime        time.Duration
	diskMiB        int
	diskPath       string
	diskMulti      bool
	diskMatrixMode string

	iperfDuration   time.Duration
	speedThreads    int
	timeout         time.Duration
	dnsAttempts     int
	latencyAttempts int

	dnsResolvers     string
	latencyTargets   string
	routeTargets     string
	stunServers      string
	iperfTargets     string
	mediaRegion      string
	backtraceCity    string
	backtraceTargets string
	ooklaServers     string

	interactive bool
	yes         bool
	strict      bool
	version     bool
}

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

func parseRunFlags(catalog module.Catalog, args []string, stderr io.Writer) (parsedRunFlags, error) {
	parsed := parsedRunFlags{explicit: make(map[string]bool)}
	flags := flag.NewFlagSet("ecs run", flag.ContinueOnError)
	parseOutput := &bytes.Buffer{}
	flags.SetOutput(parseOutput)
	languageFlag := &runLanguageFlag{}
	flags.Var(languageFlag, "lang", "flag.lang")
	helpFlag := flags.Bool("help", false, "")
	hFlag := flags.Bool("h", false, "")
	flags.StringVar(&parsed.profile, "profile", "", "flag.profile")
	flags.StringVar(&parsed.configPath, "config", "", "flag.config")
	flags.StringVar(&parsed.only, "only", "", "flag.only")
	flags.StringVar(&parsed.skip, "skip", "", "flag.skip")
	flags.StringVar(&parsed.exposure, "exposure", "", "flag.exposure")
	flags.BoolVar(&parsed.reveal, "reveal", false, "flag.reveal")
	flags.StringVar(&parsed.ipVersion, "ip-version", "", "flag.ipVersion")
	flags.BoolVar(&parsed.ipv4, "4", false, "flag.ipv4")
	flags.BoolVar(&parsed.ipv6, "6", false, "flag.ipv6")
	flags.StringVar(&parsed.ipQualitySources, "ip-quality-sources", "", "flag.ipQualitySources")
	flags.StringVar(&parsed.formats, "format", "", "flag.format")
	flags.StringVar(&parsed.output, "output", "", "flag.output")
	flags.StringVar(&parsed.name, "name", "", "flag.name")
	flags.BoolVar(&parsed.noColor, "no-color", false, "flag.noColor")
	flags.StringVar(&parsed.color, "color", "auto", "flag.color")
	flags.StringVar(&parsed.scoreBaseline, "score-baseline", "", "flag.scoreBaseline")
	flags.DurationVar(&parsed.cpuTime, "cpu-time", 0, "flag.cpuTime")
	flags.IntVar(&parsed.diskMiB, "disk-mib", 0, "flag.diskMiB")
	flags.StringVar(&parsed.diskPath, "disk-path", "", "flag.diskPath")
	flags.BoolVar(&parsed.diskMulti, "disk-multi", false, "flag.diskMulti")
	flags.StringVar(&parsed.diskMatrixMode, "disk-matrix-mode", "", "flag.diskMatrixMode")
	flags.DurationVar(&parsed.iperfDuration, "iperf-duration", 0, "flag.iperfDuration")
	flags.IntVar(&parsed.speedThreads, "speed-threads", 0, "flag.speedThreads")
	flags.DurationVar(&parsed.timeout, "timeout", 0, "flag.timeout")
	flags.IntVar(&parsed.dnsAttempts, "dns-attempts", 0, "flag.dnsAttempts")
	flags.IntVar(&parsed.latencyAttempts, "latency-attempts", 0, "flag.latencyAttempts")
	flags.StringVar(&parsed.dnsResolvers, "dns-resolvers", "", "flag.dnsResolvers")
	flags.StringVar(&parsed.latencyTargets, "latency-targets", "", "flag.latencyTargets")
	flags.StringVar(&parsed.routeTargets, "route-targets", "", "flag.routeTargets")
	flags.StringVar(&parsed.stunServers, "stun-servers", "", "flag.stunServers")
	flags.StringVar(&parsed.iperfTargets, "iperf-targets", "", "flag.iperfTargets")
	flags.StringVar(&parsed.mediaRegion, "media-region", "", "flag.mediaRegion")
	flags.StringVar(&parsed.backtraceCity, "backtrace-city", "", "flag.backtraceCity")
	flags.StringVar(&parsed.backtraceTargets, "backtrace-targets", "", "flag.backtraceTargets")
	flags.StringVar(&parsed.ooklaServers, "ookla-servers", "", "flag.ooklaServers")
	flags.BoolVar(&parsed.interactive, "interactive", false, "flag.interactive")
	flags.BoolVar(&parsed.yes, "yes", false, "flag.yes")
	flags.BoolVar(&parsed.strict, "strict", false, "flag.strict")
	flags.BoolVar(&parsed.version, "version", false, "flag.version")
	flags.Usage = func() { printRunHelp(catalog, parseOutput, flags) }
	if err := flags.Parse(args); err != nil {
		if *helpFlag || *hFlag || errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(stderr)
			printRunHelp(catalog, stderr, flags)
			return parsedRunFlags{}, runFlagParseError{err: flag.ErrHelp}
		}
		_, _ = io.Copy(stderr, parseOutput)
		return parsedRunFlags{}, runFlagParseError{err: err}
	}
	if languageFlag.seen {
		occurrence := languageFlagOccurrence{Value: languageFlag.value}
		if err := validateExplicitLanguage([]languageFlagOccurrence{occurrence}); err != nil {
			return parsedRunFlags{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
	}
	if *helpFlag || *hFlag {
		flags.SetOutput(stderr)
		printRunHelp(catalog, stderr, flags)
		return parsedRunFlags{}, runFlagParseError{err: flag.ErrHelp}
	}
	flags.Visit(func(parsedFlag *flag.Flag) { parsed.explicit[parsedFlag.Name] = true })
	parsed.positional = append([]string(nil), flags.Args()...)
	return parsed, nil
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
	fmt.Fprintln(writer, "\n"+i18n.T("cli.modules")+": "+strings.Join(catalog.IDs(), ","))
	fmt.Fprintln(writer, i18n.T("cli.sources")+": "+strings.Join(config.IPQualitySourceIDs(), ","))
}

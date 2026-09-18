package app

import (
	"fmt"
	"io"
	"strings"

	"ecs/internal/config"
	"ecs/internal/i18n"
	"ecs/internal/module"
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
func resolveRunConfig(catalog module.Catalog, args []string, stderr io.Writer) (resolvedRunConfig, error) {
	// Stage 1: parse the complete run/plan flag grammar and remember which
	// values were explicitly supplied.
	flags, err := parseRunFlags(catalog, args, stderr)
	if err != nil {
		return resolvedRunConfig{}, err
	}

	// Stage 2: built-in defaults selected by profile, overlaid by the config
	// file. CLI values are intentionally not applied until the next stage.
	cfg, err := loadRunConfig(catalog, flags)
	if err != nil {
		return resolvedRunConfig{}, err
	}

	// Preserve the historical CLI contract: --version short-circuits before
	// positional-argument and run-specific override validation.
	if flags.version {
		if flags.explicit["no-color"] {
			cfg.NoColor = flags.noColor
		}
		return resolvedRunConfig{Runtime: cfg, Color: flags.color, Version: true}, nil
	}
	if len(flags.positional) != 0 {
		return resolvedRunConfig{}, fmt.Errorf("%s %s", i18n.T("help.extraArgs"), strings.Join(flags.positional, " "))
	}

	// Stage 3: only explicitly supplied CLI values override the loaded
	// runtime. Empty values remain meaningful when their flag was supplied.
	if err := applyRunCLIOverrides(&cfg, flags); err != nil {
		return resolvedRunConfig{}, err
	}

	// Stage 4: resolve explicit module selection and exposure, then return the
	// runtime shared by run and plan.
	cfg, err = resolveRunModules(catalog, cfg, flags)
	if err != nil {
		return resolvedRunConfig{}, err
	}

	return resolvedRunConfig{
		Runtime:       cfg,
		Name:          flags.name,
		Color:         flags.color,
		ScoreBaseline: flags.scoreBaseline,
		Interactive:   flags.interactive,
		Yes:           flags.yes,
		Strict:        flags.strict,
		Version:       false,
	}, nil
}

func loadRunConfig(catalog module.Catalog, flags parsedRunFlags) (config.Runtime, error) {
	var fileConfig config.File
	if flags.explicit["config"] && flags.configPath != "" {
		loaded, err := config.LoadFile(flags.configPath)
		if err != nil {
			return config.Runtime{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
		}
		fileConfig = loaded
	}

	profile := fileConfig.Profile
	if flags.explicit["profile"] {
		profile = flags.profile
	}
	cfg, err := config.Defaults(catalog, profile)
	if err != nil {
		return config.Runtime{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if err := config.ApplyFile(catalog, &cfg, fileConfig); err != nil {
		return config.Runtime{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	if cfg.Output == "" {
		cfg.Output = "./reports"
	}
	return cfg, nil
}

func applyRunCLIOverrides(cfg *config.Runtime, flags parsedRunFlags) error {
	if flags.explicit["exposure"] {
		exposure, err := config.ParseExposure(flags.exposure)
		if err != nil {
			return fmt.Errorf("%s: --exposure: %v", i18n.T("cli.error"), err)
		}
		cfg.Exposure = exposure
	}
	if flags.explicit["reveal"] {
		cfg.Reveal = flags.reveal
	}
	if flags.explicit["ip-version"] {
		cfg.IPVersion = strings.ToLower(strings.TrimSpace(flags.ipVersion))
	}
	if flags.ipv4 && flags.ipv6 {
		return fmt.Errorf("%s: %v", i18n.T("cli.error"), i18n.Errorf("err.ipv4AndIPv6"))
	}
	if flags.ipv4 {
		cfg.IPVersion = config.IPVersion4
	}
	if flags.ipv6 {
		cfg.IPVersion = config.IPVersion6
	}
	if flags.explicit["ip-quality-sources"] {
		cfg.IPQualitySources = config.ParseList(flags.ipQualitySources)
	}
	cfg.Formats = config.ParseList(strings.Join(cfg.Formats, ","))
	if flags.explicit["format"] {
		cfg.Formats = config.ParseList(flags.formats)
	}
	if flags.explicit["output"] {
		cfg.Output = flags.output
	}
	if flags.explicit["no-color"] {
		cfg.NoColor = flags.noColor
	}
	if flags.explicit["cpu-time"] {
		cfg.CPUTime = flags.cpuTime
	}
	if flags.explicit["disk-mib"] {
		cfg.DiskMiB = flags.diskMiB
	}
	if flags.explicit["disk-path"] {
		cfg.DiskPath = flags.diskPath
	}
	if flags.explicit["disk-multi"] {
		cfg.DiskMulti = flags.diskMulti
	}
	if flags.explicit["disk-matrix-mode"] {
		diskMatrixMode, err := config.ParseDiskMatrixMode(flags.diskMatrixMode)
		if err != nil {
			return fmt.Errorf("%s: --disk-matrix-mode: %v", i18n.T("cli.error"), err)
		}
		cfg.DiskMatrixMode = diskMatrixMode
	}
	if flags.explicit["iperf-duration"] {
		cfg.IPerfDuration = flags.iperfDuration
	}
	if flags.explicit["speed-threads"] {
		cfg.SpeedThreads = flags.speedThreads
	}
	if flags.explicit["timeout"] {
		cfg.HTTPTimeout = flags.timeout
	}
	if flags.explicit["dns-attempts"] {
		cfg.DNSAttempts = flags.dnsAttempts
	}
	if flags.explicit["latency-attempts"] {
		cfg.LatencyAttempts = flags.latencyAttempts
	}
	if flags.explicit["dns-resolvers"] {
		endpoints, err := parseRunEndpointOverride(flags.dnsResolvers, true, "dns-resolvers")
		if err != nil {
			return err
		}
		cfg.DNSResolvers = endpoints
	}
	if flags.explicit["latency-targets"] {
		endpoints, err := parseRunEndpointOverride(flags.latencyTargets, true, "latency-targets")
		if err != nil {
			return err
		}
		cfg.LatencyTargets = endpoints
	}
	if flags.explicit["route-targets"] {
		endpoints, err := parseRunEndpointOverride(flags.routeTargets, false, "route-targets")
		if err != nil {
			return err
		}
		cfg.RouteTargets = endpoints
	}
	if flags.explicit["stun-servers"] {
		endpoints, err := parseRunEndpointOverride(flags.stunServers, true, "stun-servers")
		if err != nil {
			return err
		}
		cfg.STUNServers = endpoints
	}
	if flags.explicit["iperf-targets"] {
		targets, err := config.ParseIPerfTargetList(flags.iperfTargets)
		if err != nil {
			return fmt.Errorf("%s: --iperf-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.IPerfTargets = targets
	}
	if flags.explicit["media-region"] {
		// 合法性由 config.Validate 无条件校验，这里只负责解析。
		cfg.MediaRegions = config.ParseList(flags.mediaRegion)
	}
	if flags.explicit["backtrace-city"] {
		if len(config.ParseList(flags.backtraceCity)) == 0 {
			cfg.BacktraceTargets = []config.Endpoint{}
		} else {
			cities, err := config.ParseBacktraceCities(flags.backtraceCity)
			if err != nil {
				return fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
			}
			cfg.BacktraceTargets = config.BacktraceTargetsFor(cities)
		}
	}
	if flags.explicit["backtrace-targets"] {
		targets, err := config.ParseBacktraceTargetList(flags.backtraceTargets)
		if err != nil {
			return fmt.Errorf("%s: --backtrace-targets: %v", i18n.T("cli.error"), err)
		}
		cfg.BacktraceTargets = targets
	}
	if flags.explicit["ookla-servers"] {
		servers, err := config.ParseOoklaServerList(flags.ooklaServers)
		if err != nil {
			return fmt.Errorf("%s: --ookla-servers: %v", i18n.T("cli.error"), err)
		}
		cfg.OoklaServers = servers
	}
	return nil
}

func parseRunEndpointOverride(raw string, requirePort bool, label string) ([]config.Endpoint, error) {
	endpoints, err := config.ParseEndpointList(raw, requirePort)
	if err != nil {
		return nil, fmt.Errorf("%s: --%s: %v", i18n.T("cli.error"), label, err)
	}
	return endpoints, nil
}

func resolveRunModules(catalog module.Catalog, cfg config.Runtime, flags parsedRunFlags) (config.Runtime, error) {
	named := config.ParseList(flags.only)
	skipped := config.ParseList(flags.skip)
	if err := config.ValidateModuleSelection(catalog, named, skipped); err != nil {
		return config.Runtime{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	cfg.Modules = config.SelectModules(catalog, cfg.Modules, named, skipped)
	if err := config.CheckModuleExposure(catalog, named, cfg.Exposure); err != nil {
		return config.Runtime{}, fmt.Errorf("%s: %v", i18n.T("cli.error"), err)
	}
	cfg.Modules = config.FilterModulesByExposure(catalog, cfg.Modules, cfg.Exposure)
	return cfg, nil
}

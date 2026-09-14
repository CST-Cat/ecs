//go:build linux || freebsd

package probe

import (
	"context"
	"math"
	"os"
	"regexp"
	"strconv"
	"time"
)

var (
	pingLossPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%\s+packet loss`)
	// Linux iputils and FreeBSD base ping report min/avg/max/stddev; compact
	// Unix implementations may report only min/avg/max. Four values are
	// preferred and the three-value form keeps standard deviation unknown.
	pingRTTPattern      = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
	pingRTTThreePattern = regexp.MustCompile(`=\s*([0-9.]+)/([0-9.]+)/([0-9.]+)\s*ms`)
)

func icmpAvailable() bool {
	_, err := icmpPingPath()
	return err == nil
}

func runICMPPingFamily(ctx context.Context, host string, count int, timeout time.Duration, family string) icmpStats {
	stats := icmpStats{}
	path, err := icmpPingPath()
	if err != nil {
		stats.Err = err
		return stats
	}
	// Give enough room for count packets, process startup, and summary output.
	budget := time.Duration(count)*timeout + 5*time.Second
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	command := newProbeCommand(runCtx, path, pingArgumentsForFamily(host, count, timeout, family)...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	run := command.RunCombined(probeCommandCombinedLimit)
	stats = parseICMPOutput(sanitizeCommandOutput(run.Combined))
	// A packet-loss exit status still leaves a valid summary, so only an
	// unparseable/unavailable command result owns the execution error.
	if !stats.Available && run.Err != nil {
		stats.Err = run.Err
	}
	return stats
}

func parseICMPOutput(text string) icmpStats {
	stats := icmpStats{}
	if match := pingLossPattern.FindStringSubmatch(text); len(match) == 2 {
		if loss, ok := parsePingFloat(match[1]); ok && loss <= 100 {
			stats.LossPercent = loss
			stats.LossKnown = true
			stats.Available = true
		}
	}
	if match := pingRTTPattern.FindStringSubmatch(text); len(match) == 5 {
		if values, ok := parsePingFloats(match[1:]); ok {
			stats.MinMS, stats.AvgMS, stats.MaxMS, stats.StdDevMS = values[0], values[1], values[2], values[3]
			stats.StdDevKnown = true
			stats.RTTKnown = true
			stats.Available = true
		}
	} else if match := pingRTTThreePattern.FindStringSubmatch(text); len(match) == 4 {
		// A missing standard deviation remains unknown instead of becoming a
		// fabricated zero measurement.
		if values, ok := parsePingFloats(match[1:]); ok {
			stats.MinMS, stats.AvgMS, stats.MaxMS = values[0], values[1], values[2]
			stats.RTTKnown = true
			stats.Available = true
		}
	}
	return stats
}

func parsePingFloat(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func parsePingFloats(values []string) ([]float64, bool) {
	parsed := make([]float64, len(values))
	for index, value := range values {
		item, ok := parsePingFloat(value)
		if !ok {
			return nil, false
		}
		parsed[index] = item
	}
	return parsed, true
}

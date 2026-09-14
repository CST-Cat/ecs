//go:build linux || freebsd

package probe

import (
	"context"
	"strconv"
	"strings"
)

// collectPlatformDisk is the Unix disk boundary. The shared system probe does
// not assume a POSIX df interface; Windows uses its own explicit native boundary.
func collectPlatformDisk(ctx context.Context, diskPath string, s *systemSnapshot) {
	output := commandOutput(ctx, platformDFCommand(), "-Pk", diskPath)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[len(lines)-1])
	parsed, ok := parseDiskDFFields(fields)
	if !ok {
		return
	}
	s.DiskDevice, s.DiskTotal, s.DiskUsed, s.DiskFree = parsed.DiskDevice, parsed.DiskTotal, parsed.DiskUsed, parsed.DiskFree
	s.DiskUsage, s.DiskMount = parsed.DiskUsage, parsed.DiskMount
	// A zero-sized or malformed df record cannot establish the disk facts
	// needed by the system inventory; keep those fields unavailable instead of
	// turning parser defaults into a 0 B measurement.
	s.DiskKnown = parsed.DiskTotal > 0
}

func systemDiskMeasurementMethod() string { return "statfs-v1" }

func parseDiskDFFields(fields []string) (systemSnapshot, bool) {
	var parsed systemSnapshot
	if len(fields) < 6 {
		return parsed, false
	}
	parsed.DiskDevice = fields[0]
	var ok bool
	if parsed.DiskTotal, ok = parseDFBlocks(fields[len(fields)-5]); !ok {
		return systemSnapshot{}, false
	}
	if parsed.DiskUsed, ok = parseDFBlocks(fields[len(fields)-4]); !ok {
		return systemSnapshot{}, false
	}
	if parsed.DiskFree, ok = parseDFBlocks(fields[len(fields)-3]); !ok {
		return systemSnapshot{}, false
	}
	if parsed.DiskTotal > 0 {
		if parsed.DiskUsed > parsed.DiskTotal {
			parsed.DiskUsed = parsed.DiskTotal
		}
		if parsed.DiskFree > parsed.DiskTotal-parsed.DiskUsed {
			parsed.DiskFree = parsed.DiskTotal - parsed.DiskUsed
		}
		parsed.DiskUsage = float64(parsed.DiskUsed) / float64(parsed.DiskTotal) * 100
	} else {
		usage, err := strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-2], "%"), 64)
		if err != nil || usage < 0 || usage > 100 {
			return systemSnapshot{}, false
		}
		parsed.DiskUsage = usage
	}
	parsed.DiskMount = fields[len(fields)-1]
	return parsed, true
}

func parseDFBlocks(value string) (uint64, bool) {
	blocks, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || blocks > ^uint64(0)/1024 {
		return 0, false
	}
	return blocks * 1024, true
}

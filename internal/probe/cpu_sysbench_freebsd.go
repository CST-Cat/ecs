//go:build freebsd

package probe

import (
	"context"
	"strconv"
	"strings"
)

func readLoadAverage1() (float64, bool) {
	value := parseFreeBSDLoadAverage(freeBSDSysctlValue(context.Background(), "vm.loadavg"))
	fields := strings.Fields(strings.ReplaceAll(value, "/", " "))
	if len(fields) == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(fields[0], 64)
	return parsed, err == nil && parsed >= 0
}

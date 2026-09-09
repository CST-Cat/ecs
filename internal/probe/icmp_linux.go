//go:build linux

package probe

import (
	"strconv"
	"time"
)

// pingCommand is the frozen iputils executable supplied by run.sh through the
// private ECS_TOOL_BIN staging directory.
const pingCommand = "ping"

// icmpAvailable reports whether the wrapper supplied the frozen Linux ping.
func icmpAvailable() bool {
	_, err := icmpPingPath()
	return err == nil
}

func icmpPingPath() (string, error) {
	return LookupTool(pingCommand)
}

func pingArgumentsForFamily(host string, count int, timeout time.Duration, family string) []string {
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	args := []string{"-n", "-q", "-c", strconv.Itoa(count), "-W", strconv.Itoa(seconds)}
	if family == "4" || family == "6" {
		args = append(args, "-"+family)
	}
	return append(args, host)
}

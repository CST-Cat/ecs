//go:build freebsd

package probe

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// FreeBSD's ping is a base-system utility whose setuid installation is owned
// and maintained by the OS. ECS must never download, copy, or chmod a network
// utility to recreate that privilege boundary.
const pingCommand = "/sbin/ping"

func icmpAvailable() bool {
	_, err := icmpPingPath()
	return err == nil
}

func icmpPingPath() (string, error) {
	info, err := os.Stat(pingCommand)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("FreeBSD base ping is not executable: %s", pingCommand)
	}
	return pingCommand, nil
}

func pingArgumentsForFamily(host string, count int, timeout time.Duration, family string) []string {
	// FreeBSD -W is the per-packet reply wait in milliseconds. It is not the
	// Linux iputils -W seconds contract.
	waitMilliseconds := timeout.Milliseconds()
	if waitMilliseconds < 1 {
		waitMilliseconds = 1
	}
	args := []string{"-n", "-q", "-c", strconv.Itoa(count), "-W", strconv.FormatInt(waitMilliseconds, 10)}
	if family == "4" || family == "6" {
		args = append(args, "-"+family)
	}
	return append(args, host)
}

//go:build freebsd

package probe

import (
	"context"
	"strconv"
	"strings"

	"ecs/internal/model"
)

// These keys retain the report's stable logical names while their values come
// from FreeBSD's native TCP sysctls.  Linux /proc/sys paths and Linux method
// identifiers are never consulted in this build.
func platformKernelParams() []kernelParam {
	return []kernelParam{
		{Key: "tcp_congestion_control", Path: "net.inet.tcp.cc.algorithm"},
		{Key: "tcp_available_congestion", Path: "net.inet.tcp.cc.available"},
		{Key: "somaxconn", Path: "kern.ipc.somaxconn"},
	}
}

func appendKernelNetworkParams(result *model.Result) {
	values := make(map[string]string)
	for _, param := range platformKernelParams() {
		value := freeBSDSysctlValue(context.Background(), param.Path)
		if value == "" {
			continue
		}
		if param.Key == "tcp_available_congestion" {
			available, ok := parseFreeBSDCCAvailable(value)
			if !ok {
				continue
			}
			values[param.Key] = available
			continue
		}
		values[param.Key] = strings.TrimSpace(value)
	}
	appendKernelNetworkFacts(result, values)
}

// The shared renderer calls this only for platforms with a verified receive
// buffer maximum. FreeBSD has no such fact in this probe, so no method ID is
// defined here.
func kernelRmemMethod() string { return "" }

func platformKernelRmemMaxAvailable() bool { return false }

// parseFreeBSDCCAvailable parses the table emitted by
// net.inet.tcp.cc.available.  The first line is a column header (currently
// "CCmod D PCB count"); subsequent rows identify one congestion-control
// module and the number of PCB references.  A malformed table is not a
// congestion-control fact.
func parseFreeBSDCCAvailable(value string) (string, bool) {
	var modules []string
	headerSeen := false
	seen := make(map[string]bool)
	for _, line := range strings.Split(value, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !headerSeen {
			if strings.EqualFold(strings.Join(fields, " "), "CCmod D PCB count") {
				headerSeen = true
				continue
			}
			return "", false
		}
		if len(fields) != 2 && len(fields) != 3 {
			return "", false
		}
		module := fields[0]
		if !validFreeBSDCCModule(module) {
			return "", false
		}
		countField := fields[len(fields)-1]
		if _, err := strconv.ParseUint(countField, 10, 64); err != nil {
			return "", false
		}
		if len(fields) == 3 && fields[1] != "*" && fields[1] != "-" {
			return "", false
		}
		if !seen[module] {
			seen[module] = true
			modules = append(modules, module)
		}
	}
	if !headerSeen || len(modules) == 0 {
		return "", false
	}
	return strings.Join(modules, " "), true
}

func validFreeBSDCCModule(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

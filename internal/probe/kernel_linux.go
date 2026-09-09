//go:build linux

package probe

import (
	"strings"

	"ecs/internal/model"
)

func kernelParams() []kernelParam {
	return []kernelParam{
		{Key: "tcp_congestion_control", Path: "net/ipv4/tcp_congestion_control"},
		{Key: "tcp_available_congestion", Path: "net/ipv4/tcp_available_congestion_control"},
		{Key: "default_qdisc", Path: "net/core/default_qdisc"},
		{Key: "tcp_fastopen", Path: "net/ipv4/tcp_fastopen"},
		{Key: "rmem_max", Path: "net/core/rmem_max"},
		{Key: "wmem_max", Path: "net/core/wmem_max"},
		{Key: "tcp_rmem", Path: "net/ipv4/tcp_rmem"},
		{Key: "tcp_wmem", Path: "net/ipv4/tcp_wmem"},
		{Key: "ip_forward", Path: "net/ipv4/ip_forward"},
		{Key: "tcp_syncookies", Path: "net/ipv4/tcp_syncookies"},
		{Key: "tcp_mtu_probing", Path: "net/ipv4/tcp_mtu_probing"},
		{Key: "tcp_slow_start_after_idle", Path: "net/ipv4/tcp_slow_start_after_idle"},
		{Key: "disable_ipv6", Path: "net/ipv6/conf/all/disable_ipv6"},
		{Key: "somaxconn", Path: "net/core/somaxconn"},
		{Key: "nf_conntrack_max", Path: "net/netfilter/nf_conntrack_max"},
		{Key: "swappiness", Path: "vm/swappiness"},
	}
}

func platformKernelParams() []kernelParam { return kernelParams() }

func appendKernelNetworkParams(result *model.Result) {
	values := make(map[string]string)
	for _, param := range platformKernelParams() {
		value := readTrimmed("/proc/sys/"+param.Path, "")
		if value == "" {
			continue
		}
		values[param.Key] = strings.Join(strings.Fields(value), " ")
	}
	appendKernelNetworkFacts(result, values)
}

func kernelRmemMethod() string { return "proc-sys-net-core-rmem-max-v1" }

func platformKernelRmemMaxAvailable() bool { return true }

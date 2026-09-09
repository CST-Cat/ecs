//go:build linux

package probe

import (
	"bufio"
	"context"
	"os"
	"strings"
)

func platformDFCommand() string    { return "df" }
func platformUnameCommand() string { return "uname" }

// collectPlatformSystem contains the Linux-only system interfaces.  The
// shared system snapshot deliberately does not know about /proc, cgroups, or
// sysfs; that keeps those facts out of the FreeBSD build and makes the
// provenance boundary explicit at compile time.
func collectPlatformSystem(ctx context.Context, s *systemSnapshot) {
	s.OS = "linux"
	if values := parseOSRelease("/etc/os-release"); len(values) > 0 {
		if pretty := values["PRETTY_NAME"]; pretty != "" {
			s.OS = pretty
		}
	}

	data, _ := os.ReadFile("/proc/cpuinfo")
	cpuText := string(data)
	physical := make(map[string]bool)
	var physicalID, coreID string
	scanner := bufio.NewScanner(strings.NewReader(cpuText))
	for scanner.Scan() {
		line := scanner.Text()
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			if physicalID != "" || coreID != "" {
				physical[physicalID+":"+coreID] = true
			}
			physicalID, coreID = "", ""
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "model name", "Hardware", "Processor":
			if s.CPUModel == "unknown" && value != "" {
				s.CPUModel = value
			}
		case "cpu MHz":
			if s.CPUFrequency == "unknown" {
				s.CPUFrequency = value + " MHz"
			}
		case "cache size":
			if s.CPUCache == "unknown" && value != "" {
				s.CPUCache = value
			}
		case "physical id":
			physicalID = value
		case "core id":
			coreID = value
		case "flags", "Features":
			flags := " " + strings.ToLower(value) + " "
			if strings.Contains(flags, " aes ") {
				s.AES = "available"
			} else if s.AES == "unknown" {
				s.AES = "unavailable"
			}
			if strings.Contains(flags, " vmx ") {
				s.Nested = "VT-x (vmx)"
			} else if strings.Contains(flags, " svm ") {
				s.Nested = "AMD-V (svm)"
			} else if s.Nested == "unknown" {
				s.Nested = "unavailable"
			}
		}
	}
	if physicalID != "" || coreID != "" {
		physical[physicalID+":"+coreID] = true
	}
	if len(physical) > 0 {
		s.PhysicalCores = len(physical)
		s.PhysicalCoresKnown = true
	}

	mem := parseMemInfo("/proc/meminfo")
	if limit, _, ok := cgroupMemoryLimit(); ok {
		s.MemoryLimit = limit
	}
	usage := memoryUsageFromMemInfo(mem, s.MemoryLimit)
	s.MemoryTotal = usage.HostTotalBytes
	s.MemoryUsed = usage.HostUsedBytes
	s.MemoryFree = usage.HostAvailableBytes
	s.MemoryUsage = usage.HostUsagePercent
	s.MemoryTotalKnown = usage.HostTotalBytes > 0
	s.MemoryUsedKnown = usage.HostTotalBytes > 0
	s.MemoryAvailableKnown = usage.AvailableKnown || usage.HostTotalBytes > 0
	s.MemoryMethod = "proc-meminfo-v1"
	if swap, ok := mem["SwapTotal"]; ok {
		s.SwapTotal = swap * 1024
		s.SwapKnown = true
	}
	s.BalloonReclaim = detectBalloonReclaim("/sys", "/proc/vmstat")
	s.KSM = detectKSM("/sys")

	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if seconds, ok := parseUptimeSeconds(data); ok {
			s.UptimeSeconds, s.UptimeKnown = seconds, true
		}
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			s.Load = strings.Join(fields[:3], " / ")
		}
	}
	s.Congestion = readTrimmed("/proc/sys/net/ipv4/tcp_congestion_control", "n/a")
	s.QDisc = readTrimmed("/proc/sys/net/core/default_qdisc", "n/a")
	s.Virtualization = detectLinuxVirtualization(cpuText)
	if kernel := commandOutput(ctx, platformUnameCommand(), "-sr"); kernel != "" {
		s.Kernel = kernel
	}
	if s.Kernel == "" {
		s.Kernel = "linux"
	}
	if sample, ok := readCPUTimes(); ok {
		s.StealPercent, s.StealKnown = cumulativeStealPercent(sample)
	}
}

func detectLinuxVirtualization(cpuinfo string) string {
	candidates := []struct {
		Path  string
		Value string
	}{
		{"/.dockerenv", "Docker"},
		{"/run/.containerenv", "container"},
		{"/proc/xen", "Xen"},
		{"/proc/vz", "OpenVZ"},
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate.Path); err == nil {
			return candidate.Value
		}
	}
	var evidence strings.Builder
	for _, path := range []string{
		"/proc/1/cgroup",
		"/sys/class/dmi/id/product_name",
		"/sys/class/dmi/id/sys_vendor",
		"/sys/class/dmi/id/board_vendor",
	} {
		if data, err := os.ReadFile(path); err == nil {
			evidence.Write(data)
			evidence.WriteByte('\n')
		}
	}
	text := strings.ToLower(evidence.String())
	checks := []struct {
		Needle string
		Name   string
	}{
		{"docker", "Docker"},
		{"kubepods", "Kubernetes"},
		{"containerd", "containerd"},
		{"lxc", "LXC"},
		{"openvz", "OpenVZ"},
		{"kvm", "KVM"},
		{"qemu", "KVM/QEMU"},
		{"vmware", "VMware"},
		{"virtualbox", "VirtualBox"},
		{"microsoft corporation", "Hyper-V"},
		{"amazon ec2", "Amazon EC2"},
		{"google compute engine", "Google Compute Engine"},
	}
	for _, check := range checks {
		if strings.Contains(text, check.Needle) {
			return check.Name
		}
	}
	if strings.Contains(strings.ToLower(cpuinfo), " hypervisor ") {
		return "virtual machine"
	}
	return "none/unknown"
}

func systemMemoryMeasurementMethod(snapshot systemSnapshot) string {
	if snapshot.MemoryMethod != "" {
		return snapshot.MemoryMethod
	}
	return "proc-meminfo-v1"
}

func systemCgroupCPUValue(allowance cpuAllowance) string {
	return cpuAllowanceMachineValue(allowance)
}

func systemMemoryLimitMachineValue(limits resourceLimits) string {
	return systemUintMachineValue(limits.MemoryLimit, "unlimited_or_unavailable")
}

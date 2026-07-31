package probe

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type systemProbe struct{}

func (systemProbe) ID() string         { return "system" }
func (systemProbe) Title() string      { return "系统与资源" }
func (systemProbe) NeedsNetwork() bool { return false }

type systemSnapshot struct {
	Hostname       string
	OS             string
	Kernel         string
	Arch           string
	CPUModel       string
	LogicalCPUs    int
	PhysicalCores  int
	CPUFrequency   string
	AES            string
	Virtualization string
	MemoryTotal    uint64
	MemoryFree     uint64
	SwapTotal      uint64
	DiskTotal      uint64
	DiskFree       uint64
	DiskMount      string
	Uptime         string
	Load           string
	Congestion     string
	QDisc          string
}

func (systemProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	result := model.NewResult("system", "系统与资源")
	result.Description = "操作系统、虚拟化、CPU、内存、磁盘与内核网络栈"
	result.Methodology = model.Methodology{
		Kind:            "inventory",
		Label:           "事实采集",
		Engine:          "OS/runtime inspection",
		Profile:         "host inventory v1",
		ComparisonScope: "资源快照；不是性能基准",
	}

	snapshot := collectSystem(ctx, env.Config.DiskPath)
	result.Fields = []model.Field{
		{Key: "hostname", Label: "主机名", Value: snapshot.Hostname, Sensitive: true},
		{Key: "os", Label: "系统", Value: snapshot.OS},
		{Key: "kernel", Label: "内核", Value: snapshot.Kernel},
		{Key: "arch", Label: "架构", Value: snapshot.Arch},
		{Key: "virtualization", Label: "虚拟化", Value: snapshot.Virtualization},
		{Key: "cpu_model", Label: "CPU", Value: snapshot.CPUModel},
		{Key: "cpu_topology", Label: "CPU 拓扑", Value: fmt.Sprintf("%d 逻辑 / %d 物理核心", snapshot.LogicalCPUs, snapshot.PhysicalCores)},
		{Key: "cpu_frequency", Label: "CPU 频率", Value: snapshot.CPUFrequency},
		{Key: "aes", Label: "AES 指令", Value: snapshot.AES},
		{Key: "memory", Label: "内存", Value: fmt.Sprintf("%s 总计 / %s 可用", model.FormatBytes(snapshot.MemoryTotal), model.FormatBytes(snapshot.MemoryFree))},
		{Key: "swap", Label: "Swap", Value: model.FormatBytes(snapshot.SwapTotal)},
		{Key: "disk", Label: "测试盘", Value: fmt.Sprintf("%s 总计 / %s 可用 (%s)", model.FormatBytes(snapshot.DiskTotal), model.FormatBytes(snapshot.DiskFree), snapshot.DiskMount)},
		{Key: "uptime", Label: "运行时间", Value: snapshot.Uptime},
		{Key: "load", Label: "负载", Value: snapshot.Load},
		{Key: "tcp_congestion", Label: "TCP 拥塞控制", Value: snapshot.Congestion},
		{Key: "qdisc", Label: "默认队列", Value: snapshot.QDisc},
	}
	result.Measurements = []model.Measurement{
		{Key: "logical_cpus", Label: "逻辑 CPU", Value: float64(snapshot.LogicalCPUs), Unit: "线程", Display: strconv.Itoa(snapshot.LogicalCPUs)},
		{Key: "memory_total_bytes", Label: "总内存", Value: float64(snapshot.MemoryTotal), Unit: "bytes", Display: model.FormatBytes(snapshot.MemoryTotal)},
		{Key: "disk_free_bytes", Label: "磁盘可用", Value: float64(snapshot.DiskFree), Unit: "bytes", Display: model.FormatBytes(snapshot.DiskFree)},
	}

	missing := 0
	for _, field := range result.Fields {
		if field.Value == "" || field.Value == "unknown" || strings.Contains(field.Value, "0 B 总计") {
			missing++
		}
	}
	if missing > 3 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "当前平台限制了部分系统信息读取；缺失字段不会影响其余测试。")
	}
	result.Summary = fmt.Sprintf("%d vCPU · %s 内存 · %s 可用盘 · %s", snapshot.LogicalCPUs, model.FormatBytes(snapshot.MemoryTotal), model.FormatBytes(snapshot.DiskFree), snapshot.Virtualization)
	result.Finish(start)
	return result
}

func collectSystem(ctx context.Context, diskPath string) systemSnapshot {
	hostname, _ := os.Hostname()
	s := systemSnapshot{
		Hostname:       hostname,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		LogicalCPUs:    runtime.NumCPU(),
		PhysicalCores:  runtime.NumCPU(),
		CPUModel:       "unknown",
		CPUFrequency:   "unknown",
		AES:            "unknown",
		Virtualization: "unknown",
		Uptime:         "unknown",
		Load:           "unknown",
		Congestion:     "n/a",
		QDisc:          "n/a",
		DiskMount:      diskPath,
	}

	switch runtime.GOOS {
	case "linux":
		collectLinuxSystem(&s)
	case "darwin":
		collectDarwinSystem(ctx, &s)
	case "freebsd", "openbsd", "netbsd":
		collectBSDSystem(ctx, &s)
	case "windows":
		collectWindowsSystem(ctx, &s)
	}
	if kernel := commandOutput(ctx, "uname", "-sr"); kernel != "" {
		s.Kernel = kernel
	}
	if s.Kernel == "" {
		s.Kernel = runtime.GOOS
	}
	collectDisk(ctx, diskPath, &s)
	return s
}

func collectLinuxSystem(s *systemSnapshot) {
	if values := parseOSRelease("/etc/os-release"); len(values) > 0 {
		if pretty := values["PRETTY_NAME"]; pretty != "" {
			s.OS = pretty
		}
	}
	cpuinfo, _ := os.ReadFile("/proc/cpuinfo")
	cpuText := string(cpuinfo)
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
		}
	}
	if physicalID != "" || coreID != "" {
		physical[physicalID+":"+coreID] = true
	}
	if len(physical) > 0 {
		s.PhysicalCores = len(physical)
	}

	mem := parseMemInfo("/proc/meminfo")
	s.MemoryTotal = mem["MemTotal"] * 1024
	if available := mem["MemAvailable"]; available > 0 {
		s.MemoryFree = available * 1024
	} else {
		s.MemoryFree = (mem["MemFree"] + mem["Buffers"] + mem["Cached"]) * 1024
	}
	s.SwapTotal = mem["SwapTotal"] * 1024

	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if seconds, err := strconv.ParseFloat(strings.Fields(string(data))[0], 64); err == nil {
			s.Uptime = formatDuration(time.Duration(seconds) * time.Second)
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
}

func collectDarwinSystem(ctx context.Context, s *systemSnapshot) {
	if value := commandOutput(ctx, "sw_vers", "-productVersion"); value != "" {
		s.OS = "macOS " + value
	}
	s.CPUModel = fallback(commandOutput(ctx, "sysctl", "-n", "machdep.cpu.brand_string"), s.CPUModel)
	if s.CPUModel == "unknown" {
		s.CPUModel = fallback(commandOutput(ctx, "sysctl", "-n", "hw.model"), s.CPUModel)
	}
	s.PhysicalCores = parseIntDefault(commandOutput(ctx, "sysctl", "-n", "hw.physicalcpu"), s.LogicalCPUs)
	s.LogicalCPUs = parseIntDefault(commandOutput(ctx, "sysctl", "-n", "hw.logicalcpu"), s.LogicalCPUs)
	s.MemoryTotal = parseUintDefault(commandOutput(ctx, "sysctl", "-n", "hw.memsize"), 0)
	if vmStat := commandOutput(ctx, "vm_stat"); vmStat != "" {
		pageSize := uint64(4096)
		header := strings.Split(vmStat, "\n")[0]
		headerFields := strings.Fields(header)
		for index, field := range headerFields {
			if field == "bytes)" && index > 0 {
				pageSize = parseUintDefault(headerFields[index-1], pageSize)
			}
		}
		var availablePages uint64
		for _, line := range strings.Split(vmStat, "\n")[1:] {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "Pages free", "Pages inactive", "Pages speculative":
				availablePages += parseUintDefault(strings.TrimSuffix(strings.TrimSpace(value), "."), 0)
			}
		}
		s.MemoryFree = availablePages * pageSize
	}
	if swapUsage := commandOutput(ctx, "sysctl", "-n", "vm.swapusage"); swapUsage != "" {
		fields := strings.Fields(swapUsage)
		if len(fields) >= 3 {
			s.SwapTotal = parseHumanBytes(fields[2])
		}
	}
	if freq := parseUintDefault(commandOutput(ctx, "sysctl", "-n", "hw.cpufrequency"), 0); freq > 0 {
		s.CPUFrequency = fmt.Sprintf("%.0f MHz", float64(freq)/1e6)
	}
	features := strings.ToLower(commandOutput(ctx, "sysctl", "-n", "machdep.cpu.features"))
	if strings.Contains(" "+features+" ", " aes ") {
		s.AES = "available"
	} else if runtime.GOARCH == "arm64" {
		s.AES = "available"
	}
	s.Virtualization = "none/host"
	if load := commandOutput(ctx, "sysctl", "-n", "vm.loadavg"); load != "" {
		s.Load = strings.Trim(load, "{} ")
	}
	if boot := commandOutput(ctx, "sysctl", "-n", "kern.boottime"); boot != "" {
		if idx := strings.Index(boot, "sec = "); idx >= 0 {
			value := boot[idx+6:]
			if end := strings.Index(value, ","); end >= 0 {
				if sec, err := strconv.ParseInt(strings.TrimSpace(value[:end]), 10, 64); err == nil {
					s.Uptime = formatDuration(time.Since(time.Unix(sec, 0)))
				}
			}
		}
	}
}

func collectBSDSystem(ctx context.Context, s *systemSnapshot) {
	s.OS = fallback(commandOutput(ctx, "uname", "-srv"), s.OS)
	s.CPUModel = fallback(commandOutput(ctx, "sysctl", "-n", "hw.model"), s.CPUModel)
	s.PhysicalCores = parseIntDefault(commandOutput(ctx, "sysctl", "-n", "hw.ncpu"), s.LogicalCPUs)
	s.MemoryTotal = parseUintDefault(commandOutput(ctx, "sysctl", "-n", "hw.physmem"), 0)
	s.Virtualization = fallback(commandOutput(ctx, "sysctl", "-n", "kern.vm_guest"), "unknown")
}

func collectWindowsSystem(ctx context.Context, s *systemSnapshot) {
	s.OS = fallback(os.Getenv("OS"), "Windows")
	s.CPUModel = fallback(os.Getenv("PROCESSOR_IDENTIFIER"), s.CPUModel)
	s.Virtualization = "unknown"
	output := commandOutput(ctx, "wmic", "ComputerSystem", "get", "TotalPhysicalMemory", "/value")
	if _, value, ok := strings.Cut(output, "="); ok {
		s.MemoryTotal = parseUintDefault(strings.TrimSpace(value), 0)
	}
}

func collectDisk(ctx context.Context, diskPath string, s *systemSnapshot) {
	if runtime.GOOS == "windows" {
		return
	}
	output := commandOutput(ctx, "df", "-Pk", diskPath)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 6 {
		return
	}
	s.DiskTotal = parseUintDefault(fields[len(fields)-5], 0) * 1024
	s.DiskFree = parseUintDefault(fields[len(fields)-3], 0) * 1024
	s.DiskMount = fields[len(fields)-1]
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

func parseOSRelease(path string) map[string]string {
	values := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values
}

func parseMemInfo(path string) map[string]uint64 {
	values := make(map[string]uint64)
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			values[strings.TrimSuffix(fields[0], ":")] = value
		}
	}
	return values
}

func commandOutput(ctx context.Context, name string, args ...string) string {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func readTrimmed(path, fallbackValue string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallbackValue
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return fallbackValue
	}
	return value
}

func parseIntDefault(value string, defaultValue int) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return defaultValue
	}
	return number
}

func parseUintDefault(value string, defaultValue uint64) uint64 {
	number, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return defaultValue
	}
	return number
}

func parseHumanBytes(value string) uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	multiplier := float64(1)
	suffix := value[len(value)-1]
	switch suffix {
	case 'K', 'k':
		multiplier = 1024
	case 'M', 'm':
		multiplier = 1024 * 1024
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
	case 'T', 't':
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		suffix = 0
	}
	if suffix != 0 {
		value = value[:len(value)-1]
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return uint64(number * multiplier)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		return "unknown"
	}
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%d 天 %d 小时 %d 分", days, hours, minutes)
	}
	return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
}

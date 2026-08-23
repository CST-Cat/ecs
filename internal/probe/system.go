package probe

import (
	"bufio"
	"context"
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
func (systemProbe) Title() string      { return "module.system.title" }
func (systemProbe) NeedsNetwork() bool { return false }

type systemSnapshot struct {
	Hostname      string
	OS            string
	Kernel        string
	Arch          string
	CPUModel      string
	LogicalCPUs   int
	PhysicalCores int
	CPUFrequency  string
	CPUCache      string
	AES           string
	// Nested 表示 CPU 是否暴露了硬件虚拟化指令（vmx/svm），决定能否跑嵌套虚拟化。
	Nested         string
	Virtualization string
	MemoryTotal    uint64
	MemoryUsed     uint64
	MemoryFree     uint64
	MemoryUsage    float64
	SwapTotal      uint64
	DiskTotal      uint64
	DiskUsed       uint64
	DiskFree       uint64
	DiskUsage      float64
	DiskDevice     string
	DiskMount      string
	UptimeSeconds  uint64
	UptimeKnown    bool
	Load           string
	Congestion     string
	QDisc          string
	Hardware       hardwareInventory

	// Allowance 是 cgroup 配额折算后本进程真正可用的 CPU。
	Allowance cpuAllowance
	// MemoryLimit 是 cgroup 内存上限；非零且小于 MemoryTotal 时说明
	// /proc/meminfo 报的是宿主机内存（没有 lxcfs 的 LXC/OpenVZ 常见）。
	MemoryLimit    uint64
	BalloonReclaim memoryFacility
	KSM            memoryFacility
	// StealPercent 是自开机以来被虚拟化层偷走的 CPU 时间占比，
	// 比短窗口采样更能反映长期超售程度。
	StealPercent float64
	StealKnown   bool
}

func (systemProbe) Run(ctx context.Context, env Environment) model.Result {
	start := time.Now()
	snapshot := collectSystem(ctx, env.Config.DiskPath)
	resources := CaptureEnvironmentSnapshot()
	cloud := discoverLocalCloudIdentity()
	result := buildSystemResult(start, snapshot, resources, cloud)
	appendKernelNetworkParams(&result)
	finalizeSystemResult(&result, snapshot)
	result.Finish(start)
	return result
}

func collectSystem(ctx context.Context, diskPath string) systemSnapshot {
	hostname, _ := os.Hostname()
	s := systemSnapshot{
		Hostname:       hostname,
		OS:             "linux",
		Arch:           runtime.GOARCH,
		LogicalCPUs:    runtime.NumCPU(),
		PhysicalCores:  runtime.NumCPU(),
		CPUModel:       "unknown",
		CPUFrequency:   "unknown",
		CPUCache:       "unknown",
		AES:            "unknown",
		Nested:         "unknown",
		Virtualization: "unknown",
		Load:           "unknown",
		Congestion:     "n/a",
		QDisc:          "n/a",
		DiskMount:      diskPath,
		Allowance:      detectCPUAllowance(),
		BalloonReclaim: memoryFacility{Evidence: "unavailable"},
		KSM:            memoryFacility{Evidence: "unavailable"},
	}

	collectLinuxSystem(&s)
	s.Hardware = collectHardwareInventory()
	if kernel := commandOutput(ctx, "uname", "-sr"); kernel != "" {
		s.Kernel = kernel
	}
	if s.Kernel == "" {
		s.Kernel = "linux"
	}
	collectDisk(ctx, diskPath, &s)
	return s
}

func joinHardwareValues(values ...string) string {
	var present []string
	for _, value := range values {
		if value != "" && value != "unknown" {
			present = append(present, value)
		}
	}
	return joinHardwareList(present)
}

func joinHardwareList(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	return strings.Join(values, " · ")
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
			// Intel 是 vmx、AMD 是 svm；两者都没有说明宿主没有透传虚拟化扩展，
			// 该机器上跑不了 KVM 嵌套虚拟化。
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
	}

	mem := parseMemInfo("/proc/meminfo")
	// Read the cgroup limit before computing the effective benchmark view.  The
	// host-visible values remain in the current fields below; the memory
	// probe uses the same helper and applies the limit to allocation decisions.
	if limit, _, ok := cgroupMemoryLimit(); ok {
		s.MemoryLimit = limit
	}
	usage := memoryUsageFromMemInfo(mem, s.MemoryLimit)
	s.MemoryTotal = usage.HostTotalBytes
	s.MemoryUsed = usage.HostUsedBytes
	s.MemoryFree = usage.HostAvailableBytes
	s.MemoryUsage = usage.HostUsagePercent
	s.SwapTotal = mem["SwapTotal"] * 1024
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

	if sample, ok := readCPUTimes(); ok {
		s.StealPercent, s.StealKnown = cumulativeStealPercent(sample)
	}
}

func collectDisk(ctx context.Context, diskPath string, s *systemSnapshot) {
	output := commandOutput(ctx, "df", "-Pk", diskPath)
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
}

func parseDiskDFFields(fields []string) (systemSnapshot, bool) {
	var parsed systemSnapshot
	if len(fields) < 6 {
		return parsed, false
	}
	parsed.DiskDevice = fields[0]
	parsed.DiskTotal = parseUintDefault(fields[len(fields)-5], 0) * 1024
	parsed.DiskUsed = parseUintDefault(fields[len(fields)-4], 0) * 1024
	parsed.DiskFree = parseUintDefault(fields[len(fields)-3], 0) * 1024
	if parsed.DiskTotal > 0 {
		if parsed.DiskUsed > parsed.DiskTotal {
			parsed.DiskUsed = parsed.DiskTotal
		}
		if parsed.DiskFree > parsed.DiskTotal-parsed.DiskUsed {
			parsed.DiskFree = parsed.DiskTotal - parsed.DiskUsed
		}
		parsed.DiskUsage = float64(parsed.DiskUsed) / float64(parsed.DiskTotal) * 100
	} else if usage, err := strconv.ParseFloat(strings.TrimSuffix(fields[len(fields)-2], "%"), 64); err == nil && usage >= 0 {
		parsed.DiskUsage = usage
	}
	parsed.DiskMount = fields[len(fields)-1]
	return parsed, true
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

func parseUintDefault(value string, defaultValue uint64) uint64 {
	number, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return defaultValue
	}
	return number
}

func parseUptimeSeconds(data []byte) (uint64, bool) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	token := fields[0]
	integer := token
	if dot := strings.IndexByte(token, '.'); dot >= 0 {
		integer = token[:dot]
		for _, char := range token[dot+1:] {
			if char < '0' || char > '9' {
				return 0, false
			}
		}
	}
	if integer == "" {
		return 0, false
	}
	for _, char := range integer {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(integer, 10, 64)
	if err != nil {
		return 0, false
	}
	return seconds, true
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}

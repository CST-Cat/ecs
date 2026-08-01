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
	MemoryFree     uint64
	SwapTotal      uint64
	DiskTotal      uint64
	DiskFree       uint64
	DiskMount      string
	Uptime         string
	Load           string
	Congestion     string
	QDisc          string

	// Allowance 是 cgroup 配额折算后本进程真正可用的 CPU。
	Allowance cpuAllowance
	// MemoryLimit 是 cgroup 内存上限；非零且小于 MemoryTotal 时说明
	// /proc/meminfo 报的是宿主机内存（没有 lxcfs 的 LXC/OpenVZ 常见）。
	MemoryLimit    uint64
	MemoryLimitVia string
	// StealPercent 是自开机以来被虚拟化层偷走的 CPU 时间占比，
	// 比短窗口采样更能反映长期超售程度。
	StealPercent float64
	StealKnown   bool
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
	memoryValue := fmt.Sprintf("%s 总计 / %s 可用", model.FormatBytes(snapshot.MemoryTotal), model.FormatBytes(snapshot.MemoryFree))
	if snapshot.MemoryLimit > 0 && snapshot.MemoryTotal > 0 && snapshot.MemoryLimit < snapshot.MemoryTotal {
		memoryValue = fmt.Sprintf("%s 配额（%s 宿主可见）", model.FormatBytes(snapshot.MemoryLimit), model.FormatBytes(snapshot.MemoryTotal))
	}
	result.Fields = []model.Field{
		{Key: "hostname", Label: "主机名", Value: snapshot.Hostname, Sensitive: true},
		{Key: "os", Label: "系统", Value: snapshot.OS},
		{Key: "kernel", Label: "内核", Value: snapshot.Kernel},
		{Key: "arch", Label: "架构", Value: snapshot.Arch},
		{Key: "virtualization", Label: "虚拟化", Value: snapshot.Virtualization},
		{Key: "cpu_model", Label: "CPU", Value: snapshot.CPUModel},
		{Key: "cpu_topology", Label: "CPU 拓扑", Value: fmt.Sprintf("%d 逻辑 / %d 物理核心", snapshot.LogicalCPUs, snapshot.PhysicalCores)},
		{Key: "cpu_allowance", Label: "CPU 配额", Value: describeCPUAllowance(snapshot.Allowance)},
		{Key: "cpu_frequency", Label: "CPU 频率", Value: snapshot.CPUFrequency},
		{Key: "cpu_steal", Label: "CPU steal（累计）", Value: describeSteal(snapshot)},
		{Key: "cpu_cache", Label: "CPU 缓存", Value: snapshot.CPUCache},
		{Key: "aes", Label: "AES 指令", Value: snapshot.AES},
		{Key: "virtualization_ext", Label: "硬件虚拟化", Value: snapshot.Nested},
		{Key: "memory", Label: "内存", Value: memoryValue},
		{Key: "swap", Label: "Swap", Value: model.FormatBytes(snapshot.SwapTotal)},
		{Key: "disk", Label: "测试盘", Value: fmt.Sprintf("%s 总计 / %s 可用 (%s)", model.FormatBytes(snapshot.DiskTotal), model.FormatBytes(snapshot.DiskFree), snapshot.DiskMount)},
		{Key: "uptime", Label: "运行时间", Value: snapshot.Uptime},
		{Key: "load", Label: "负载", Value: snapshot.Load},
		{Key: "tcp_congestion", Label: "TCP 拥塞控制", Value: snapshot.Congestion},
		{Key: "qdisc", Label: "默认队列", Value: snapshot.QDisc},
	}
	result.Measurements = []model.Measurement{
		{Key: "logical_cpus", Label: "逻辑 CPU", Value: float64(snapshot.LogicalCPUs), Unit: "线程", Display: strconv.Itoa(snapshot.LogicalCPUs)},
		{Key: "usable_cpus", Label: "可用 CPU", Value: float64(snapshot.Allowance.Threads), Unit: "线程", Display: strconv.Itoa(snapshot.Allowance.Threads)},
		{Key: "memory_total_bytes", Label: "总内存", Value: float64(snapshot.MemoryTotal), Unit: "bytes", Display: model.FormatBytes(snapshot.MemoryTotal)},
		{Key: "disk_free_bytes", Label: "磁盘可用", Value: float64(snapshot.DiskFree), Unit: "bytes", Display: model.FormatBytes(snapshot.DiskFree)},
	}
	if snapshot.StealKnown {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "cpu_steal_percent_cumulative", Label: "CPU steal（自开机累计）",
			Value: snapshot.StealPercent, Unit: "%", Display: fmt.Sprintf("%.2f %%", snapshot.StealPercent),
			Method: "proc-stat-steal-cumulative-v1", HigherIsBetter: model.BoolPtr(false),
		})
	}
	if snapshot.MemoryLimit > 0 {
		result.Measurements = append(result.Measurements, model.Measurement{
			Key: "memory_limit_bytes", Label: "内存配额",
			Value: float64(snapshot.MemoryLimit), Unit: "bytes", Display: model.FormatBytes(snapshot.MemoryLimit),
		})
	}

	missing := 0
	for _, field := range result.Fields {
		if field.Value == "" || field.Value == "unknown" || strings.Contains(field.Value, "0 B 总计") {
			missing++
		}
	}
	if missing > 3 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, "部分 /proc、/sys 或 DMI 字段不可读（常见于容器与精简镜像）；缺失字段不会影响其余测试。")
	}
	if snapshot.Allowance.Limited() {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"cgroup 限制本机只能用 %.2f 个核（%s），但系统可见 %d 个逻辑核；CPU 与内存基准会按配额开 %d 个线程。",
			snapshot.Allowance.Quota, snapshot.Allowance.Source, snapshot.Allowance.Visible, snapshot.Allowance.Threads,
		))
	}
	if snapshot.MemoryLimit > 0 && snapshot.MemoryTotal > 0 && snapshot.MemoryLimit < snapshot.MemoryTotal {
		result.Notes = append(result.Notes, fmt.Sprintf(
			"/proc/meminfo 报告 %s，但 %s 只有 %s；容器没有挂载 lxcfs 时 meminfo 显示的是宿主机内存，配额才是真实可用值。",
			model.FormatBytes(snapshot.MemoryTotal), snapshot.MemoryLimitVia, model.FormatBytes(snapshot.MemoryLimit),
		))
	}
	if snapshot.StealKnown && snapshot.StealPercent >= 5 {
		result.Status = model.StatusWarning
		result.Notes = append(result.Notes, fmt.Sprintf(
			"自开机以来 CPU steal 累计 %.2f%%，宿主机长期存在资源争抢，通常意味着超售；所有 CPU 成绩都应按此折价理解。",
			snapshot.StealPercent,
		))
	}
	result.Summary = fmt.Sprintf("%d vCPU · %s 内存 · %s 可用盘 · %s", snapshot.LogicalCPUs, model.FormatBytes(snapshot.MemoryTotal), model.FormatBytes(snapshot.DiskFree), snapshot.Virtualization)
	result.Finish(start)
	return result
}

// describeSteal 给出可读的 steal 描述。
func describeSteal(s systemSnapshot) string {
	if !s.StealKnown {
		return "unavailable（/proc/stat 不可读）"
	}
	return fmt.Sprintf("%.2f %%", s.StealPercent)
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
		Uptime:         "unknown",
		Load:           "unknown",
		Congestion:     "n/a",
		QDisc:          "n/a",
		DiskMount:      diskPath,
		Allowance:      detectCPUAllowance(),
	}

	collectLinuxSystem(&s)
	if kernel := commandOutput(ctx, "uname", "-sr"); kernel != "" {
		s.Kernel = kernel
	}
	if s.Kernel == "" {
		s.Kernel = "linux"
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

	// 容器里 /proc/meminfo 通常是宿主机视图，cgroup 上限才是真正拿得到的内存。
	if limit, via, ok := cgroupMemoryLimit(); ok {
		s.MemoryLimit = limit
		s.MemoryLimitVia = via
	}
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

func parseUintDefault(value string, defaultValue uint64) uint64 {
	number, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return defaultValue
	}
	return number
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

package probe

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ecs/internal/model"
)

type systemProbe struct{}

func (systemProbe) ID() string { return "system" }

type systemSnapshot struct {
	Hostname           string
	OS                 string
	Kernel             string
	Arch               string
	CPUModel           string
	LogicalCPUs        int
	PhysicalCores      int
	PhysicalCoresKnown bool
	CPUFrequency       string
	CPUCache           string
	AES                string
	// Nested 表示 CPU 是否暴露了硬件虚拟化指令（vmx/svm），决定能否跑嵌套虚拟化。
	Nested               string
	Virtualization       string
	MemoryTotal          uint64
	MemoryUsed           uint64
	MemoryFree           uint64
	MemoryUsage          float64
	MemoryTotalKnown     bool
	MemoryUsedKnown      bool
	MemoryAvailableKnown bool
	MemoryMethod         string
	SwapTotal            uint64
	SwapKnown            bool
	DiskTotal            uint64
	DiskUsed             uint64
	DiskFree             uint64
	DiskUsage            float64
	DiskKnown            bool
	DiskDevice           string
	DiskMount            string
	UptimeSeconds        uint64
	UptimeKnown          bool
	Load                 string
	Congestion           string
	QDisc                string
	Hardware             hardwareInventory

	// Allowance 是 cgroup 配额折算后本进程真正可用的 CPU。
	Allowance cpuAllowance
	// MemoryLimit 是 Linux cgroup 内存上限；非零且小于 MemoryTotal 时说明
	// Linux host memory reporting is not the process's effective limit. FreeBSD
	// leaves this fact unavailable because it has no Linux cgroup interface.
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
	result.Methodology.Parameters = newComparisonParameters()
	addComparisonParameter(result.Methodology.Parameters, "disk_path", env.Config.DiskPath)
	appendKernelNetworkParams(&result)
	finalizeSystemResult(&result, snapshot)
	result.Finish(start)
	return result
}

func collectSystem(ctx context.Context, diskPath string) systemSnapshot {
	hostname, _ := os.Hostname()
	s := systemSnapshot{
		Hostname:           hostname,
		OS:                 "unknown",
		Arch:               runtime.GOARCH,
		LogicalCPUs:        runtime.NumCPU(),
		PhysicalCores:      runtime.NumCPU(),
		PhysicalCoresKnown: true,
		CPUModel:           "unknown",
		CPUFrequency:       "unknown",
		CPUCache:           "unknown",
		AES:                "unknown",
		Nested:             "unknown",
		Virtualization:     "unknown",
		Load:               "unknown",
		Congestion:         "n/a",
		QDisc:              "n/a",
		DiskMount:          diskPath,
		Allowance:          detectCPUAllowance(),
		BalloonReclaim:     memoryFacility{Evidence: "unavailable"},
		KSM:                memoryFacility{Evidence: "unavailable"},
	}

	collectPlatformSystem(ctx, &s)
	s.Hardware = collectHardwareInventory()
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

func collectDisk(ctx context.Context, diskPath string, s *systemSnapshot) {
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

func parseOSRelease(path string) map[string]string {
	values := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
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
	scanner := bufio.NewScanner(bytes.NewReader(data))
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
	command := newProbeCommand(ctx, name, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	run := command.RunSeparate()
	if run.Err != nil {
		return ""
	}
	return strings.TrimSpace(string(run.Stdout))
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

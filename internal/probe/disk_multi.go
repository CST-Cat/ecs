package probe

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ecs/internal/model"
)

// 多挂载盘 I/O 测试。
//
// 对应 oneclickvirt 的 -diskmc 与 spiritLHLS 的 -mdisk：不少 VPS 会额外挂载
// 数据盘（尤其是"大盘鸡"），系统盘与数据盘的介质、限速策略经常完全不同，
// 只测系统盘会漏掉真正用来存数据的那块。
//
// 默认关闭：多跑一块盘就多一份写入与时间，应当由用户显式要求。

// mountPoint 是一个候选测试挂载点。
type mountPoint struct {
	Path     string
	Device   string
	FSType   string
	ReadOnly bool
}

// virtualFilesystems 是不代表真实块设备的文件系统类型。
//
// tmpfs 尤其要排除：它是内存盘，测出来的是内存带宽而不是磁盘性能，
// 把它当磁盘会得到荒唐的高分（本机 /tmp 就是 tmpfs）。
var virtualFilesystems = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true, "devpts": true,
	"cgroup": true, "cgroup2": true, "securityfs": true, "pstore": true, "bpf": true,
	"autofs": true, "mqueue": true, "hugetlbfs": true, "debugfs": true, "tracefs": true,
	"fusectl": true, "configfs": true, "ramfs": true, "binfmt_misc": true,
	"efivarfs": true, "nsfs": true, "squashfs": true, "overlay": true, "rpc_pipefs": true,
	"fuse.portal": true, "fuse.gvfsd-fuse": true, "fuse.snapfuse": true,
	"iso9660": true, "udf": true, "vfat": true, "msdos": true,
}

// unmountedPrefixes 是不该被当作数据盘的挂载路径前缀。
var unmountedPrefixes = []string{
	"/proc", "/sys", "/dev", "/run", "/boot", "/snap", "/var/lib/docker",
	"/var/lib/kubelet", "/var/lib/containers", "/var/snap",
}

// discoverMountPoints 从 /proc/mounts 读取当前挂载表。
//
// 用内核接口而不是 findmnt/lsblk：这两个命令在精简镜像里未必存在，
// 而 /proc/mounts 在任何 Linux 上都有。
func discoverMountPoints() []mountPoint {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer file.Close()
	var mounts []mountPoint
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// 格式：device mountpoint fstype options dump pass
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		readOnly := false
		for _, option := range strings.Split(fields[3], ",") {
			if option == "ro" {
				readOnly = true
				break
			}
		}
		mounts = append(mounts, mountPoint{
			// /proc/mounts 会把空格等字符转义成八进制，路径里含空格时需要还原。
			Path:     unescapeMountPath(fields[1]),
			Device:   fields[0],
			FSType:   fields[2],
			ReadOnly: readOnly,
		})
	}
	return mounts
}

// unescapeMountPath 还原 /proc/mounts 的八进制转义（\040 空格、\011 制表符等）。
func unescapeMountPath(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+3 < len(value) {
			var code int
			if _, err := fmt.Sscanf(value[index+1:index+4], "%03o", &code); err == nil {
				builder.WriteByte(byte(code))
				index += 3
				continue
			}
		}
		builder.WriteByte(value[index])
	}
	return builder.String()
}

// testableMounts 挑出值得单独测 I/O 的挂载点。
//
// 这里只做纯过滤，不碰文件系统：可写性检查有真实 I/O 副作用，放在调用方，
// 否则这段判定逻辑就没法用构造的挂载表来测。
//
// 过滤依据全部来自实测的挂载表：tmpfs 是内存盘、/boot/efi 是 vfat 小分区、
// 只读挂载写不了、同一设备重复挂载没必要测两次。
func testableMounts(mounts []mountPoint, primaryPath string) []mountPoint {
	primaryAbs, err := filepath.Abs(primaryPath)
	if err != nil {
		primaryAbs = primaryPath
	}
	seenDevice := make(map[string]bool)
	var selected []mountPoint
	for _, mount := range mounts {
		if mount.ReadOnly || virtualFilesystems[mount.FSType] {
			continue
		}
		// 只测真实块设备；网络文件系统与伪设备一律跳过。
		if !strings.HasPrefix(mount.Device, "/dev/") {
			continue
		}
		skip := false
		for _, prefix := range unmountedPrefixes {
			if mount.Path == prefix || strings.HasPrefix(mount.Path, prefix+"/") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		// 同一块设备挂多次（bind mount、subvol）只测一次。
		if seenDevice[mount.Device] {
			continue
		}
		// 主测试路径已由常规 fio 覆盖，不重复。
		if abs, err := filepath.Abs(mount.Path); err == nil && abs == primaryAbs {
			seenDevice[mount.Device] = true
			continue
		}
		seenDevice[mount.Device] = true
		selected = append(selected, mount)
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	return selected
}

// mountWritable 通过实际创建临时文件确认可写。
//
// 只看挂载选项不够：配额耗尽、只读重挂、权限不足都会让写入失败，
// 而这些只有真正写一次才知道。
func mountWritable(path string) bool {
	file, err := os.CreateTemp(path, ".ecs-probe-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

// appendMultiDiskResults 对系统盘之外的挂载点各跑一轮简化 fio。
//
// 刻意用简化作业集而不是完整矩阵：完整矩阵单盘就要十几秒，多块盘会让整个
// 测试时长失控。这里只测顺序写与 4K 随机读，足以看出介质差异与限速。
func appendMultiDiskResults(ctx context.Context, result *model.Result, env Environment, fioPath string) {
	candidates := testableMounts(discoverMountPoints(), env.Config.DiskPath)
	// 可写性只能靠真的写一次确认：配额耗尽、只读重挂、权限不足都不体现在挂载选项里。
	mounts := make([]mountPoint, 0, len(candidates))
	for _, candidate := range candidates {
		if mountWritable(candidate.Path) {
			mounts = append(mounts, candidate)
		}
	}
	if len(mounts) == 0 {
		result.Notes = append(result.Notes,
			"未发现系统盘之外可测的挂载点（已排除 tmpfs 等内存盘、只读挂载与不可写目录）。")
		return
	}

	table := model.Table{
		Title:   "其他挂载盘 I/O",
		Columns: []string{"挂载点", "设备", "文件系统", "顺序写", "4K 随机读", "状态"},
		// 设备路径可能透露存储拓扑，按敏感列处理。
		SensitiveColumns: []int{1},
	}
	engine := detectFIOEngine(ctx, fioPath)
	tested := 0
	for _, mount := range mounts {
		if ctx.Err() != nil {
			break
		}
		sample, err := runMultiDiskFIO(ctx, fioPath, mount.Path, engine)
		if err != nil {
			table.Rows = append(table.Rows, []string{
				mount.Path, mount.Device, mount.FSType, "—", "—", "失败：" + compactError(err),
			})
			continue
		}
		tested++
		writeAvailable := isPositiveFinite(sample.WriteMiB)
		readAvailable := isPositiveFinite(sample.ReadIOPS)
		table.Rows = append(table.Rows, []string{
			mount.Path, mount.Device, mount.FSType,
			formatMatrixRate(sample.WriteMiB, "MiB/s"),
			formatMatrixRate(sample.ReadIOPS, "IOPS"),
			"完成",
		})
		if !writeAvailable || !readAvailable {
			result.Status = model.StatusWarning
			result.Notes = append(result.Notes, "多盘 fio 的一个或多个挂载点只返回部分统计；缺失项不补零。")
		}
		key := strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(strings.TrimPrefix(mount.Path, "/"))
		if key == "" {
			key = "root"
		}
		if writeAvailable {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "fio_mount_" + key + "_write_mib_s", Label: mount.Path + " 顺序写",
				Value: sample.WriteMiB, Unit: "MiB/s", Display: model.FormatRate(sample.WriteMiB, "MiB/s"),
				Method: "fio-direct-1MiB-write-qd1-multidisk-v1", HigherIsBetter: model.BoolPtr(true),
			})
		}
		if readAvailable {
			result.Measurements = append(result.Measurements, model.Measurement{
				Key: "fio_mount_" + key + "_randread_iops", Label: mount.Path + " 4K 随机读",
				Value: sample.ReadIOPS, Unit: "IOPS", Display: model.FormatRate(sample.ReadIOPS, "IOPS"),
				Method:         fmt.Sprintf("fio-direct-4KiB-randread-qd%d-multidisk-v1", engine.EffectiveDepth(32)),
				HigherIsBetter: model.BoolPtr(true),
			})
		}
	}
	if len(table.Rows) > 0 {
		result.Tables = append(result.Tables, table)
	}
	result.Notes = append(result.Notes, fmt.Sprintf(
		"额外测试了 %d 个挂载点，使用简化作业集（顺序写 + 4K 随机读，各 3 秒），"+
			"数值不与主盘的完整矩阵混比；tmpfs 等内存盘已排除，测它们得到的是内存带宽而非磁盘性能。",
		tested))
}

// multiDiskSample 是简化作业集的结果。
type multiDiskSample struct {
	WriteMiB float64
	ReadIOPS float64
}

// runMultiDiskFIO 在指定挂载点跑一轮简化 fio。
func runMultiDiskFIO(ctx context.Context, fioPath, mountPath string, engine fioEngine) (multiDiskSample, error) {
	var sample multiDiskSample
	file, err := os.CreateTemp(mountPath, ".ecs-fio-*")
	if err != nil {
		return sample, fmt.Errorf("创建临时文件: %w", err)
	}
	tempName := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(tempName)
		return sample, err
	}
	defer func() { _ = os.Remove(tempName) }()

	const sizeBytes = int64(128 * 1024 * 1024)
	plan := []fioJobSpec{
		{Name: "seqwrite", RW: "write", BlockSize: "1m", IODepth: 1, NumJobs: 1, EndFsync: true},
		{Name: "randread", RW: "randread", BlockSize: "4k", IODepth: 32, NumJobs: 1},
	}
	args := fioArguments(tempName, sizeBytes, 3*time.Second, engine, plan)
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, fioPath, args...)
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "NO_COLOR=1")
	output, runErr := command.Output()
	if runCtx.Err() != nil {
		return sample, runCtx.Err()
	}
	if runErr != nil {
		return sample, runErr
	}
	jobs, err := parseFIOJobs(output)
	if err != nil {
		return sample, err
	}
	sample.WriteMiB = fioBandwidthMiB(jobs["seqwrite"].Write)
	sample.ReadIOPS = jobs["randread"].Read.IOPS
	if !isPositiveFinite(sample.WriteMiB) && !isPositiveFinite(sample.ReadIOPS) {
		return sample, fmt.Errorf("fio 未返回有效统计")
	}
	return sample, nil
}

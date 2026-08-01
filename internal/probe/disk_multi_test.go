package probe

import "testing"

func TestTestableMountsFiltersVirtualAndReadOnly(t *testing.T) {
	// 样本取自本机 /proc/mounts 的真实内容。
	mounts := []mountPoint{
		{Path: "/", Device: "/dev/nvme0n1p2", FSType: "ext4"},
		{Path: "/tmp", Device: "tmpfs", FSType: "tmpfs"},
		{Path: "/dev/shm", Device: "tmpfs", FSType: "tmpfs"},
		{Path: "/run", Device: "tmpfs", FSType: "tmpfs"},
		{Path: "/boot/efi", Device: "/dev/nvme0n1p1", FSType: "vfat"},
		{Path: "/run/user/1000/doc", Device: "portal", FSType: "fuse.portal"},
		{Path: "/mnt/data", Device: "/dev/sdb1", FSType: "ext4"},
		{Path: "/mnt/backup", Device: "/dev/sdb1", FSType: "ext4"},
		{Path: "/mnt/ro", Device: "/dev/sdc1", FSType: "ext4", ReadOnly: true},
		{Path: "/mnt/nfs", Device: "10.0.0.1:/export", FSType: "nfs4"},
	}
	got := testableMounts(mounts, "/")
	var paths []string
	for _, m := range got {
		paths = append(paths, m.Path)
	}
	// tmpfs 是内存盘（测它得到的是内存带宽）、vfat 是 EFI 小分区、
	// fuse/nfs 不是本地块设备、只读挂载写不了、/dev/sdb1 重复挂载只测一次、
	// "/" 是主测试路径已被常规 fio 覆盖。
	if len(paths) != 1 || paths[0] != "/mnt/data" {
		t.Fatalf("过滤结果 = %v, want [/mnt/data]", paths)
	}
}

func TestUnescapeMountPath(t *testing.T) {
	if got := unescapeMountPath(`/mnt/my\040disk`); got != "/mnt/my disk" {
		t.Fatalf("八进制转义还原 = %q", got)
	}
	if got := unescapeMountPath("/mnt/plain"); got != "/mnt/plain" {
		t.Fatalf("无转义路径被改动 = %q", got)
	}
}

func TestDiscoverMountPointsReadsRealProcMounts(t *testing.T) {
	mounts := discoverMountPoints()
	if len(mounts) == 0 {
		t.Fatal("/proc/mounts 在 Linux 上必然可读")
	}
	rootFound := false
	for _, m := range mounts {
		if m.Path == "/" {
			rootFound = true
			if m.FSType == "" || m.Device == "" {
				t.Fatalf("根挂载点字段缺失：%+v", m)
			}
		}
	}
	if !rootFound {
		t.Fatal("挂载表里必须有根挂载点")
	}
}

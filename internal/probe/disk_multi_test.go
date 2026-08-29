package probe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskMountFilteringAndWritableFixture(t *testing.T) {
	if unescapeMountPath(`/mnt/data\040disk\011one`) != "/mnt/data disk\tone" {
		t.Fatal("mount path octal unescape failed")
	}
	mounts := []mountPoint{
		{Path: "/", Device: "/dev/vda1", FSType: "ext4"},
		{Path: "/mnt/data", Device: "/dev/sdb1", FSType: "ext4"},
		{Path: "/mnt/backup", Device: "/dev/sde1", FSType: "ext4"},
		{Path: "/mnt/data-copy", Device: "/dev/sdb1", FSType: "ext4"},
		{Path: "/tmp", Device: "tmpfs", FSType: "tmpfs"},
		{Path: "/mnt/ro", Device: "/dev/sdc1", FSType: "ext4", ReadOnly: true},
		{Path: "/srv/nfs", Device: "server:/export", FSType: "nfs"},
		{Path: "/proc/data", Device: "/dev/sdd1", FSType: "ext4"},
	}
	got := testableMounts(mounts, "/")
	if len(got) != 2 || got[0].Path != "/mnt/backup" || got[1].Path != "/mnt/data" {
		t.Fatalf("filtered mounts = %+v", got)
	}
	directory := t.TempDir()
	if !mountWritable(directory) {
		t.Fatal("mount writable fixture boundary failed")
	}
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if mountWritable(file) {
		t.Fatal("regular file was treated as writable mount directory")
	}
}

func TestDiskMountRowsUseStableStructuredStatus(t *testing.T) {
	if got := diskTableStatusKey("disk.fio.mounts", true); got != "probe.disk.status.complete" {
		t.Fatalf("complete multi-disk status = %q", got)
	}
	if got := diskTableStatusKey("disk.fio.mounts", false); got != "probe.disk.status.missing" {
		t.Fatalf("missing multi-disk status = %q", got)
	}
	if got := diskMeasurementLabel("fio_mount_mnt_data_write_mib_s"); got != "probe.disk.metric.mount" {
		t.Fatalf("multi-disk measurement label = %q", got)
	}
}

package probe

import "testing"

func TestTestableMountsFiltersFixture(t *testing.T) {
	mounts := []mountPoint{
		{Path: "/", Device: "/dev/vda1", FSType: "ext4"},
		{Path: "/mnt/data", Device: "/dev/sdb1", FSType: "ext4"},
		{Path: "/tmp", Device: "tmpfs", FSType: "tmpfs"},
		{Path: "/mnt/ro", Device: "/dev/sdc1", FSType: "ext4", ReadOnly: true},
	}
	got := testableMounts(mounts, "/")
	if len(got) != 1 || got[0].Path != "/mnt/data" {
		t.Fatalf("filtered mounts = %+v", got)
	}
}

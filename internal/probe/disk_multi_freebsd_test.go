//go:build freebsd

package probe

import "testing"

func TestFreeBSDMountParserRetainsUFSAndZFS(t *testing.T) {
	mounts := parseFreeBSDMountTable(`
/dev/vtbd0p2 / ufs rw 1 1
zroot/ROOT/default /usr zfs rw,noatime 0 0
/dev/vtbd0p3 /readonly ufs ro 2 2
server:/export /net nfs rw 0 0
`)
	if len(mounts) != 4 || mounts[1].FSType != "zfs" || mounts[2].ReadOnly != true {
		t.Fatalf("FreeBSD mounts = %+v", mounts)
	}
	selected := testableMounts(mounts, "/")
	if len(selected) != 1 || selected[0].FSType != "zfs" || selected[0].Path != "/usr" {
		t.Fatalf("UFS/ZFS candidates = %+v", selected)
	}
}

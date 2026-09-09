//go:build freebsd

package probe

import (
	"context"
	"strings"
)

// FreeBSD's base mount command exposes the kernel mount table without relying
// on procfs, findmnt, or lsblk.  `mount -p` prints device, mount point,
// filesystem type and options in a stable fstab-compatible form.
func discoverMountPoints() []mountPoint {
	return parseFreeBSDMountTable(commandOutput(context.Background(), "/sbin/mount", "-p"))
}

func parseFreeBSDMountTable(output string) []mountPoint {
	var mounts []mountPoint
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mounts = append(mounts, mountPoint{
			Device:   unescapeMountPath(fields[0]),
			Path:     unescapeMountPath(fields[1]),
			FSType:   fields[2],
			ReadOnly: mountOptionsReadOnly(fields[3]),
		})
	}
	return mounts
}

func mountOptionsReadOnly(options string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == "ro" {
			return true
		}
	}
	return false
}

//go:build linux

package probe

import (
	"bufio"
	"os"
	"strings"
)

// discoverMountPoints reads Linux's kernel mount table.  This implementation
// is deliberately isolated from the FreeBSD build, where /proc/mounts does
// not exist.
func discoverMountPoints() []mountPoint {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer file.Close()
	var mounts []mountPoint
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		mounts = append(mounts, mountPoint{
			Path:     unescapeMountPath(fields[1]),
			Device:   fields[0],
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

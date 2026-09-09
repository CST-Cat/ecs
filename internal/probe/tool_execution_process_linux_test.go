//go:build linux

package probe

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
)

// Linux keeps the original /proc state check so a reaped or zombie child is
// treated as gone without weakening the process-group cleanup assertion.
func processStateGone(t *testing.T, pid int) bool {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	if end := strings.LastIndexByte(string(data), ')'); end >= 0 {
		fields := strings.Fields(string(data)[end+1:])
		return len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X")
	}
	return false
}

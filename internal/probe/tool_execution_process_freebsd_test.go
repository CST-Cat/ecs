//go:build freebsd

package probe

import (
	"errors"
	"syscall"
	"testing"
)

// FreeBSD does not require procfs for this assertion. The process-group
// check in waitGone is authoritative; this PID check handles a process that
// has already left its group.
func processStateGone(t *testing.T, pid int) bool {
	t.Helper()
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

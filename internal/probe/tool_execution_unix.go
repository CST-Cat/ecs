//go:build linux || freebsd

package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// probeCommand owns the Unix command lifecycle used by benchmark adapters.
// Linux and FreeBSD both support a child process group, so cancellation and
// process-tree cleanup remain one implementation.
type probeCommand struct {
	*exec.Cmd
	parentContext context.Context
	cancel        context.CancelFunc
}

func newProbeCommand(ctx context.Context, path string, args ...string) *probeCommand {
	commandCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(commandCtx, path, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = probeCommandWaitDelay
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return terminateProbeProcessGroup(command.Process.Pid)
	}
	return &probeCommand{Cmd: command, parentContext: ctx, cancel: cancel}
}

func (command *probeCommand) run(stdout, stderr *probeCommandWriter, combined bool) probeCommandResult {
	defer command.cancel()
	command.Cmd.Stdout = stdout
	command.Cmd.Stderr = stderr
	runErr := command.Cmd.Run()
	if command.Process != nil {
		_ = terminateProbeProcessGroup(command.Process.Pid)
	}
	return processProbeCommandOutput(command.parentContext, stdout, stderr, combined, runErr)
}

func terminateProbeProcessGroup(pid int) error {
	if pid <= 1 {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	}
	return err
}

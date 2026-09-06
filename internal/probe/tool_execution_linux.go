package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	// Frozen benchmark output is normally small; this leaves room for verbose
	// metadata while preventing an external tool from retaining unbounded data.
	probeCommandCombinedLimit = 4 * 1024 * 1024
	probeCommandStdoutLimit   = 4 * 1024 * 1024
	probeCommandStderrLimit   = 64 * 1024
	probeCommandWaitDelay     = 200 * time.Millisecond
	probeCommandOoklaLimit    = 512 * 1024
)

var errProbeCommandOutputLimit = errors.New("external command output exceeded its limit")

type probeCommandWriter struct {
	data     []byte
	limit    int
	cancel   context.CancelFunc
	overflow bool
}

func newProbeCommandWriter(limit int, cancel context.CancelFunc) *probeCommandWriter {
	return &probeCommandWriter{limit: limit, cancel: cancel}
}

func (writer *probeCommandWriter) Write(data []byte) (int, error) {
	if writer.overflow {
		return 0, errProbeCommandOutputLimit
	}
	remaining := writer.limit - len(writer.data)
	if len(data) > remaining {
		writer.data = append(writer.data, data[:remaining]...)
		writer.overflow = true
		writer.cancel()
		return remaining, errProbeCommandOutputLimit
	}
	writer.data = append(writer.data, data...)
	return len(data), nil
}

type probeCommandResult struct {
	Stdout   []byte
	Stderr   []byte
	Combined []byte
	Err      error
}

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

func (command *probeCommand) RunCombined(limit int) probeCommandResult {
	output := newProbeCommandWriter(limit, command.cancel)
	return command.run(output, output, true)
}

func (command *probeCommand) RunSeparate() probeCommandResult {
	stdout := newProbeCommandWriter(probeCommandStdoutLimit, command.cancel)
	stderr := newProbeCommandWriter(probeCommandStderrLimit, command.cancel)
	return command.run(stdout, stderr, false)
}

func (command *probeCommand) run(stdout, stderr *probeCommandWriter, combined bool) probeCommandResult {
	defer command.cancel()
	command.Cmd.Stdout = stdout
	command.Cmd.Stderr = stderr
	runErr := command.Cmd.Run()
	if command.Process != nil {
		_ = terminateProbeProcessGroup(command.Process.Pid)
	}
	result := probeCommandResult{Err: runErr}
	if combined {
		result.Combined = stdout.data
	} else {
		result.Stdout = stdout.data
		result.Stderr = stderr.data
	}
	stream, limit, diagnostic := "", 0, []byte(nil)
	if stdout.overflow {
		stream, limit = "stdout", stdout.limit
		if combined {
			stream = "combined"
		} else {
			diagnostic = stderr.data
		}
	} else if !combined && stderr.overflow {
		stream, limit, diagnostic = "stderr", stderr.limit, stderr.data
	}
	if stream != "" {
		result.Stdout = nil
		result.Combined = nil
		limitErr := probeCommandOutputLimitError(stream, limit, diagnostic, runErr)
		if cause := contextCauseError(command.parentContext); cause != nil {
			result.Err = errors.Join(cause, limitErr)
		} else {
			result.Err = limitErr
		}
		return result
	}
	if cause := contextCauseError(command.parentContext); cause != nil {
		result.Err = cause
		return result
	}
	if !combined && runErr != nil && len(stderr.data) > 0 {
		result.Err = fmt.Errorf("%w: command stderr: %s", runErr, sanitizeCommandOutput(stderr.data))
	}
	return result
}

func probeCommandOutputLimitError(stream string, limit int, stderr []byte, runErr error) error {
	err := fmt.Errorf("external command %s exceeded %d-byte limit: %w", stream, limit, errProbeCommandOutputLimit)
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		err = errors.Join(err, exitErr)
	}
	if len(stderr) > 0 {
		err = fmt.Errorf("%w: command stderr: %s", err, sanitizeCommandOutput(stderr))
	}
	return err
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

package probe

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeToolFixture(t *testing.T, directory, name string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLookupToolUsesFrozenStagingDirectory(t *testing.T) {
	hostDirectory := t.TempDir()
	stagedDirectory := t.TempDir()
	writeToolFixture(t, hostDirectory, "fixture-tool")
	want := writeToolFixture(t, stagedDirectory, "fixture-tool")
	t.Setenv("PATH", hostDirectory)
	t.Setenv(ToolBinEnv, stagedDirectory)

	got, err := LookupTool("fixture-tool")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("LookupTool path = %q, want staged path %q", got, want)
	}
}

func TestLookupToolDoesNotUseHostPath(t *testing.T) {
	hostDirectory := t.TempDir()
	writeToolFixture(t, hostDirectory, "fixture-tool")
	t.Setenv("PATH", hostDirectory)
	t.Setenv(ToolBinEnv, "")

	_, err := LookupTool("fixture-tool")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("LookupTool error = %v, want exec.ErrNotFound", err)
	}
}

// These are generic shell process-lifecycle fixtures, not benchmark adapters.
func TestProbeCommandKillsProcessGroups(t *testing.T) {
	writeLifecycleFixture := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "stream-lifecycle.sh")
		script := "#!/bin/sh\n" + body
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}

	waitGone := func(t *testing.T, pid, processGroup int) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(-processGroup, 0); errors.Is(err, syscall.ESRCH) {
				return
			}
			if processStateGone(t, pid) {
				return
			}
			if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("lifecycle process group %d is still running", processGroup)
	}

	t.Run("context cancellation", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child.pid")
		path := writeLifecycleFixture(t, "(while :; do :; done) &\nprintf '%s' \"$!\" > \"$1\"\nwhile :; do :; done\n")
		// Keep enough startup margin for slower real FreeBSD VMs before the
		// cancellation deadline, while retaining a bounded lifecycle check.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		command := newProbeCommand(ctx, path, marker)
		defer func() {
			if command.Process != nil {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
		}()
		started := time.Now()
		result := command.RunCombined(probeCommandCombinedLimit)
		if !errors.Is(result.Err, context.DeadlineExceeded) || time.Since(started) >= time.Second {
			t.Fatalf("cancelled lifecycle command = err:%v elapsed:%s", result.Err, time.Since(started))
		}
		pid := readLifecyclePID(t, marker)
		waitGone(t, pid, command.Process.Pid)
	})

	t.Run("parent exits while child holds pipes", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child.pid")
		path := writeLifecycleFixture(t, "(while :; do :; done) &\nprintf '%s' \"$!\" > \"$1\"\nexit 0\n")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		started := time.Now()
		command := newProbeCommand(ctx, path, marker)
		defer func() {
			if command.Process != nil {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
		}()
		result := command.RunCombined(probeCommandCombinedLimit)
		if !errors.Is(result.Err, exec.ErrWaitDelay) || time.Since(started) >= time.Second {
			t.Fatalf("parent-exit lifecycle command = err:%v elapsed:%s", result.Err, time.Since(started))
		}
		pid := readLifecyclePID(t, marker)
		waitGone(t, pid, command.Process.Pid)
	})

	t.Run("parent exits after child closes pipes", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child.pid")
		path := writeLifecycleFixture(t, "(while :; do :; done) >/dev/null 2>&1 &\nprintf '%s' \"$!\" > \"$1\"\nexit 0\n")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		command := newProbeCommand(ctx, path, marker)
		defer func() {
			if command.Process != nil {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
		}()
		result := command.RunCombined(probeCommandCombinedLimit)
		if result.Err != nil {
			t.Fatalf("closed-pipe lifecycle command = %v", result.Err)
		}
		pid := readLifecyclePID(t, marker)
		waitGone(t, pid, command.Process.Pid)
	})

	t.Run("output limit kills child group", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child.pid")
		path := writeLifecycleFixture(t, "(while :; do :; done) &\nprintf '%s' \"$!\" > \"$1\"\nprintf 123456789012345678901234567890123\nwhile :; do :; done\n")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		command := newProbeCommand(ctx, path, marker)
		defer func() {
			if command.Process != nil {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
		}()
		started := time.Now()
		result := command.RunCombined(32)
		if !errors.Is(result.Err, errProbeCommandOutputLimit) || result.Combined != nil || time.Since(started) >= 2*time.Second {
			t.Fatalf("over-limit lifecycle command = err:%v output:%d elapsed:%s", result.Err, len(result.Combined), time.Since(started))
		}
		pid := readLifecyclePID(t, marker)
		waitGone(t, pid, command.Process.Pid)
	})

	t.Run("normal nonzero exit remains typed", func(t *testing.T) {
		const payload = "fixture nonzero payload"
		path := writeLifecycleFixture(t, "printf '%s' '"+payload+"'\nexit 7\n")
		result := newProbeCommand(context.Background(), path).RunCombined(probeCommandCombinedLimit)
		var exitErr *exec.ExitError
		if !errors.As(result.Err, &exitErr) || exitErr.ExitCode() != 7 {
			t.Fatalf("nonzero lifecycle command = %T %v", result.Err, result.Err)
		}
		if string(result.Combined) != payload || strings.Contains(result.Err.Error(), payload) || strings.Contains(result.Err.Error(), "command stderr") {
			t.Fatalf("nonzero lifecycle payload/error = output:%q err:%v", result.Combined, result.Err)
		}
	})
}

func readLifecyclePID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		t.Fatalf("invalid lifecycle child PID %q", data)
	}
	return pid
}

func TestProbeCommandPreservesProtocolAndContextCause(t *testing.T) {
	t.Run("success preserves environment directory and stdin", func(t *testing.T) {
		directory := t.TempDir()
		command := newProbeCommand(context.Background(), "/bin/sh", "-c", "printf '%s|%s|' \"$PROBE_FIXTURE_ENV\" \"$PWD\"; cat")
		command.Env = append(os.Environ(), "PROBE_FIXTURE_ENV=fixture")
		command.Dir = directory
		command.Stdin = strings.NewReader("stdin")
		result := command.RunCombined(probeCommandCombinedLimit)
		want := "fixture|" + directory + "|stdin"
		if result.Err != nil || string(result.Combined) != want {
			t.Fatalf("command result = err:%v output:%q, want output %q", result.Err, result.Combined, want)
		}
	})

	t.Run("caller cause is retained", func(t *testing.T) {
		cause := errors.New("fixture cancellation cause")
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		result := newProbeCommand(ctx, "/bin/sh", "-c", "exit 0").RunCombined(probeCommandCombinedLimit)
		if !errors.Is(result.Err, cause) || !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("command error = %v, want caller cause and cancellation", result.Err)
		}
	})

	t.Run("merged stream order and exact boundary", func(t *testing.T) {
		ordered := newProbeCommand(context.Background(), "/bin/sh", "-c", "printf 'first\\n' >&2; printf 'second\\n'").RunCombined(probeCommandCombinedLimit)
		if ordered.Err != nil || string(ordered.Combined) != "first\nsecond\n" {
			t.Fatalf("merged result = err:%v output:%q", ordered.Err, ordered.Combined)
		}
		exact := newProbeCommand(context.Background(), "/bin/sh", "-c", "printf 12345678901234567890123456789012").RunCombined(32)
		if exact.Err != nil || len(exact.Combined) != 32 {
			t.Fatalf("exact result = err:%v length:%d", exact.Err, len(exact.Combined))
		}
		over := newProbeCommand(context.Background(), "/bin/sh", "-c", "printf 123456789012345678901234567890123").RunCombined(32)
		if !errors.Is(over.Err, errProbeCommandOutputLimit) || over.Combined != nil {
			t.Fatalf("over-limit result = err:%v output length:%d", over.Err, len(over.Combined))
		}
	})
}

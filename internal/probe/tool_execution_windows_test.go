//go:build windows

package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	windowsTreeRoleEnv   = "ECS_WINDOWS_TREE_ROLE"
	windowsTreeModeEnv   = "ECS_WINDOWS_TREE_MODE"
	windowsTreeMarkerEnv = "ECS_WINDOWS_TREE_MARKER"
)

func TestLookupToolUsesPrivateWindowsExecutable(t *testing.T) {
	hostDirectory := t.TempDir()
	stagedDirectory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := filepath.Join(stagedDirectory, "fixture-tool.exe")
	if err := os.WriteFile(stagedPath, data, 0o700); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(hostDirectory, "path-only.exe")
	if err := os.WriteFile(hostPath, data, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hostDirectory)
	t.Setenv(ToolBinEnv, stagedDirectory)

	got, err := LookupTool("fixture-tool")
	if err != nil {
		t.Fatal(err)
	}
	if got != stagedPath {
		t.Fatalf("LookupTool path = %q, want staged .exe path %q", got, stagedPath)
	}
	if _, err := LookupTool("path-only"); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("LookupTool PATH-only error = %v, want exec.ErrNotFound", err)
	}
}

// TestProbeCommandWindowsProcessTree uses the test binary itself as a real
// helper process. The resulting root -> child -> grandchild hierarchy is
// assigned to the production Job Object implementation, rather than to a
// fake process or a shell script.
func TestProbeCommandWindowsProcessTree(t *testing.T) {
	if role := os.Getenv(windowsTreeRoleEnv); role != "" {
		runWindowsTreeHelper(role)
		return
	}

	t.Run("context cancellation", func(t *testing.T) {
		runWindowsTreeCase(t, "cancel", 0, func(command *probeCommand, ctx context.Context, cancel context.CancelFunc) {
			_ = command
			_ = ctx
			cancel()
		})
	})
	t.Run("interrupt cancellation callback", func(t *testing.T) {
		runWindowsTreeCase(t, "interrupt", 0, func(command *probeCommand, ctx context.Context, cancel context.CancelFunc) {
			// os/signal delivers Ctrl+C to the application context; exec.Cmd then
			// calls this same production callback. Calling it directly keeps the
			// test independent of the runner's console attachment while exercising
			// the actual interrupt cleanup path and all tree assertions.
			if err := command.Cancel(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Fatalf("interrupt cancellation callback: %v", err)
			}
			_ = ctx
			_ = cancel
		})
	})
	t.Run("output overflow", func(t *testing.T) {
		runWindowsTreeCase(t, "overflow", 64, nil)
	})
}

func runWindowsTreeCase(t *testing.T, mode string, outputLimit int, trigger func(*probeCommand, context.Context, context.CancelFunc)) {
	t.Helper()
	markerDirectory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := newProbeCommand(ctx, os.Args[0], "-test.run=TestProbeCommandWindowsProcessTree", "-test.count=1")
	command.Env = append(os.Environ(),
		windowsTreeRoleEnv+"=root",
		windowsTreeModeEnv+"="+mode,
		windowsTreeMarkerEnv+"="+markerDirectory,
	)
	resultChannel := make(chan probeCommandResult, 1)
	go func() {
		if outputLimit > 0 {
			resultChannel <- command.RunCombined(outputLimit)
		} else {
			resultChannel <- command.RunCombined(probeCommandCombinedLimit)
		}
	}()
	finished := false
	defer func() {
		if !finished {
			_ = command.Cancel()
			<-resultChannel
		}
	}()

	childPID, grandchildPID := waitWindowsTreePIDs(t, markerDirectory)
	_ = waitWindowsTreeMarker(t, filepath.Join(markerDirectory, "grandchild.port"))
	if trigger != nil {
		trigger(command, ctx, cancel)
	} else {
		// The overflow writer cancels the command context as soon as the
		// combined limit is crossed.
	}
	result := <-resultChannel
	finished = true

	switch mode {
	case "cancel":
		if !errors.Is(result.Err, context.Canceled) {
			t.Fatalf("context-cancelled tree error = %v, want context.Canceled", result.Err)
		}
	case "interrupt":
		// The direct callback has no parent context cause, but it must still
		// terminate the process and complete Wait without a successful result.
		if result.Err == nil {
			t.Fatal("interrupt callback returned a successful result")
		}
	case "overflow":
		if !errors.Is(result.Err, errProbeCommandOutputLimit) || result.Combined != nil {
			t.Fatalf("over-limit tree result = err:%v output:%d", result.Err, len(result.Combined))
		}
	}
	assertWindowsTreeGone(t, markerDirectory, childPID, grandchildPID)
}

func runWindowsTreeHelper(role string) {
	markerDirectory := os.Getenv(windowsTreeMarkerEnv)
	if markerDirectory == "" {
		panic("missing Windows tree marker directory")
	}
	writeWindowsTreePID(markerDirectory, role, os.Getpid())

	switch role {
	case "root":
		child := startWindowsTreeHelper(markerDirectory, "child")
		if os.Getenv(windowsTreeModeEnv) == "overflow" {
			payload := []byte(strings.Repeat("x", 4096))
			for {
				if _, err := os.Stdout.Write(payload); err != nil {
					return
				}
			}
		}
		_, _ = child.Wait()
	case "child":
		grandchild := startWindowsTreeHelper(markerDirectory, "grandchild")
		_, _ = grandchild.Wait()
	case "grandchild":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(fmt.Sprintf("grandchild listener: %v", err))
		}
		defer listener.Close()
		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok || address.Port <= 0 {
			panic("grandchild listener did not expose a TCP port")
		}
		if err := os.WriteFile(filepath.Join(markerDirectory, "grandchild.port"), []byte(strconv.Itoa(address.Port)), 0o600); err != nil {
			panic(fmt.Sprintf("write grandchild port: %v", err))
		}
		for {
			time.Sleep(time.Second)
		}
	default:
		panic("unknown Windows tree role: " + role)
	}
}

func startWindowsTreeHelper(markerDirectory, role string) *os.Process {
	command := exec.Command(os.Args[0], "-test.run=TestProbeCommandWindowsProcessTree", "-test.count=1")
	command.Env = append(os.Environ(),
		windowsTreeRoleEnv+"="+role,
		windowsTreeModeEnv+"="+os.Getenv(windowsTreeModeEnv),
		windowsTreeMarkerEnv+"="+markerDirectory,
	)
	if err := command.Start(); err != nil {
		panic(fmt.Sprintf("start Windows tree role %s: %v", role, err))
	}
	return command.Process
}

func writeWindowsTreePID(directory, role string, pid int) {
	path := filepath.Join(directory, role+".pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		panic(fmt.Sprintf("write Windows tree role %s PID: %v", role, err))
	}
}

func waitWindowsTreePIDs(t *testing.T, directory string) (int, int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var values [2]int
	for time.Now().Before(deadline) {
		ready := true
		for index, role := range []string{"child", "grandchild"} {
			data, err := os.ReadFile(filepath.Join(directory, role+".pid"))
			if err != nil {
				ready = false
				break
			}
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr != nil || pid <= 1 {
				ready = false
				break
			}
			values[index] = pid
		}
		if ready {
			return values[0], values[1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Windows process-tree PID markers did not appear in %s", directory)
	return 0, 0
}

func assertWindowsTreeGone(t *testing.T, directory string, childPID, grandchildPID int) {
	t.Helper()
	waitWindowsProcessGone(t, childPID)
	waitWindowsProcessGone(t, grandchildPID)

	portData := waitWindowsTreeMarker(t, filepath.Join(directory, "grandchild.port"))
	port, err := strconv.Atoi(strings.TrimSpace(string(portData)))
	if err != nil || port <= 0 {
		t.Fatalf("grandchild port marker = %q", portData)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("grandchild port %d remained bound: %v", port, err)
	}
	_ = listener.Close()

	for _, role := range []string{"child", "grandchild"} {
		path := filepath.Join(directory, role+".pid")
		released := path + ".released"
		if err := os.Rename(path, released); err != nil {
			t.Fatalf("rename %s after tree exit: %v", role, err)
		}
		if err := os.Remove(released); err != nil {
			t.Fatalf("remove released %s marker: %v", role, err)
		}
	}
}

func waitWindowsTreeMarker(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Windows tree marker did not appear: %s", path)
	return nil
}

func waitWindowsProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
		if errors.Is(err, windowsInvalidParameter) {
			return
		}
		if err != nil {
			t.Fatalf("OpenProcess(%d): %v", pid, err)
		}
		state, waitErr := syscall.WaitForSingleObject(handle, 0)
		closeErr := syscall.CloseHandle(handle)
		if waitErr != nil || closeErr != nil {
			t.Fatalf("wait/close process %d: wait=%v close=%v", pid, waitErr, closeErr)
		}
		if state == syscall.WAIT_OBJECT_0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Windows process %d remained alive", pid)
}

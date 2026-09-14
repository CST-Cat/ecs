//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const windowsSignalReadyEnv = "ECS_WINDOWS_SIGNAL_READY"

func TestNotifyContextHandlesConsoleInterrupt(t *testing.T) {
	if readyPath := os.Getenv(windowsSignalReadyEnv); readyPath != "" {
		ctx, stop := notifyContext()
		defer stop()
		if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			t.Fatal("notifyContext did not cancel on console interrupt")
		}
	}

	freeConsole, err := prepareWindowsTestConsole()
	if err != nil {
		t.Fatal(err)
	}
	defer freeConsole()

	readyPath := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=TestNotifyContextHandlesConsoleInterrupt", "-test.count=1")
	command.Env = append(os.Environ(), windowsSignalReadyEnv+"="+readyPath)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForWindowsSignalReady(t, readyPath)

	if err := sendWindowsCtrlBreak(command.Process.Pid); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() {
		wait <- command.Wait()
	}()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("console interrupt helper exited with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("console interrupt helper did not exit")
	}
}

func prepareWindowsTestConsole() (func(), error) {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return func() {}, err
	}
	getConsoleWindow, err := dll.FindProc("GetConsoleWindow")
	if err != nil {
		_ = dll.Release()
		return func() {}, err
	}
	allocConsole, err := dll.FindProc("AllocConsole")
	if err != nil {
		_ = dll.Release()
		return func() {}, err
	}
	freeConsole, err := dll.FindProc("FreeConsole")
	if err != nil {
		_ = dll.Release()
		return func() {}, err
	}
	window, _, _ := getConsoleWindow.Call()
	if window != 0 {
		return func() { _ = dll.Release() }, nil
	}
	result, _, callErr := allocConsole.Call()
	if result == 0 {
		_ = dll.Release()
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return func() {}, callErr
	}
	return func() {
		_, _, _ = freeConsole.Call()
		_ = dll.Release()
	}, nil
}

func sendWindowsCtrlBreak(pid int) error {
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer dll.Release()
	proc, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	result, _, callErr := proc.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(pid))
	if result == 0 {
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return callErr
	}
	return nil
}

func waitForWindowsSignalReady(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == "ready" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("signal helper did not create ready marker %s", path)
}

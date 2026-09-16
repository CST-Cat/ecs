//go:build windows

package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	// Frozen benchmark output is normally small; this leaves room for verbose
	// metadata while preventing an external tool from retaining unbounded data.
	probeCommandCombinedLimit = 4 * 1024 * 1024
	probeCommandStdoutLimit   = 4 * 1024 * 1024
	probeCommandStderrLimit   = 64 * 1024
	probeCommandWaitDelay     = 2 * time.Second
	probeCommandOoklaLimit    = 512 * 1024

	// CREATE_SUSPENDED closes the only process-tree window that cannot be
	// covered by a Job Object alone: a child could create descendants before
	// AssignProcessToJobObject runs. The primary process is resumed only after
	// that assignment succeeds.
	CREATE_SUSPENDED = 0x00000004

	JOB_OBJECT_EXTENDED_LIMIT_INFORMATION = 9
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE    = 0x00002000

	// AssignProcessToJobObject requires PROCESS_SET_QUOTA and
	// PROCESS_TERMINATE. NtResumeProcess additionally requires
	// PROCESS_SUSPEND_RESUME.
	PROCESS_SET_QUOTA       = 0x00000100
	PROCESS_SUSPEND_RESUME  = 0x00000800
	windowsInvalidParameter = syscall.Errno(87)
)

var errProbeCommandOutputLimit = errors.New("external command output exceeded its limit")

var (
	windowsKernel32           = syscall.NewLazyDLL("kernel32.dll")
	windowsCreateJobObject    = windowsKernel32.NewProc("CreateJobObjectW")
	windowsSetInformationJob  = windowsKernel32.NewProc("SetInformationJobObject")
	windowsAssignProcessToJob = windowsKernel32.NewProc("AssignProcessToJobObject")
	windowsTerminateJobObject = windowsKernel32.NewProc("TerminateJobObject")
	windowsNTDLL              = syscall.NewLazyDLL("ntdll.dll")
	windowsNtResumeProcess    = windowsNTDLL.NewProc("NtResumeProcess")
)

// These definitions mirror the Win32 structures used by
// SetInformationJobObject. SIZE_T is uintptr on windows/amd64; using the
// native widths here keeps the structure layout identical to the SDK.
type JOBOBJECT_BASIC_LIMIT_INFORMATION struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type IO_COUNTERS struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type JOBOBJECT_EXTENDED_LIMIT_INFORMATION struct {
	BasicLimitInformation JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                IO_COUNTERS
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func windowsCallError(operation string, err error) error {
	if err == nil {
		err = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func closeWindowsProcessHandle(handle syscall.Handle) error {
	if handle == 0 || handle == syscall.InvalidHandle {
		return nil
	}
	if err := syscall.CloseHandle(handle); err == nil {
		return nil
	} else {
		closeErr := windowsCallError("CloseHandle", err)
		retryErr := syscall.CloseHandle(handle)
		if retryErr == nil {
			return closeErr
		}
		return errors.Join(closeErr, windowsCallError("CloseHandle retry", retryErr))
	}
}

func createWindowsJob() (syscall.Handle, error) {
	handle, _, callErr := windowsCreateJobObject.Call(0, 0)
	if handle == 0 {
		return 0, windowsCallError("CreateJobObjectW", callErr)
	}

	limits := JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	result, _, callErr := windowsSetInformationJob.Call(
		handle,
		JOB_OBJECT_EXTENDED_LIMIT_INFORMATION,
		uintptr(unsafe.Pointer(&limits)),
		unsafe.Sizeof(limits),
	)
	if result != 0 {
		return syscall.Handle(handle), nil
	}

	setErr := windowsCallError("SetInformationJobObject", callErr)
	if closeErr := closeWindowsJobHandle(syscall.Handle(handle)); closeErr != nil {
		return 0, errors.Join(setErr, closeErr)
	}
	return 0, setErr
}

// closeWindowsJobHandle normally relies on KILL_ON_JOB_CLOSE. If closing a
// valid handle itself fails, terminate the job while it is still referenced,
// then retry the close so a failed cleanup cannot leave descendants behind.
func closeWindowsJobHandle(handle syscall.Handle) error {
	if handle == 0 || handle == syscall.InvalidHandle {
		return nil
	}
	if err := syscall.CloseHandle(handle); err == nil {
		return nil
	} else {
		closeErr := windowsCallError("CloseHandle", err)
		terminateErr := terminateWindowsJob(handle)
		retryErr := syscall.CloseHandle(handle)
		if retryErr == nil {
			return errors.Join(closeErr, terminateErr)
		}
		return errors.Join(closeErr, terminateErr, windowsCallError("CloseHandle retry", retryErr))
	}
}

func terminateWindowsJob(handle syscall.Handle) error {
	result, _, callErr := windowsTerminateJobObject.Call(uintptr(handle), 1)
	if result != 0 {
		return nil
	}
	return windowsCallError("TerminateJobObject", callErr)
}

func resumeWindowsProcess(handle syscall.Handle) error {
	// NtResumeProcess resumes the suspended primary process without requiring
	// access to the private primary-thread handle held by os/exec. It is present
	// on the Windows Server versions supported by this target.
	status, _, _ := windowsNtResumeProcess.Call(uintptr(handle))
	if status == 0 {
		return nil
	}
	return fmt.Errorf("NtResumeProcess: NTSTATUS 0x%08x", uint32(status))
}

func terminateWindowsProcess(pid int) error {
	if pid <= 1 {
		return os.ErrProcessDone
	}
	handle, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windowsInvalidParameter) {
			return os.ErrProcessDone
		}
		return windowsCallError("OpenProcess", err)
	}
	terminateErr := syscall.TerminateProcess(handle, 1)
	closeErr := closeWindowsProcessHandle(handle)
	if terminateErr != nil {
		if errors.Is(terminateErr, windowsInvalidParameter) {
			terminateErr = os.ErrProcessDone
		} else {
			terminateErr = windowsCallError("TerminateProcess", terminateErr)
		}
	}
	return errors.Join(terminateErr, closeErr)
}

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

// probeCommand owns the command lifecycle used by benchmark adapters. The
// Job Object remains open until Wait and pipe draining complete, so a normal
// root exit still cleans descendants that retained inherited handles.
type probeCommand struct {
	*exec.Cmd
	parentContext    context.Context
	lifecycleContext context.Context
	cancel           context.CancelFunc

	stateMu         sync.Mutex
	job             syscall.Handle
	jobAssigned     bool
	cancelRequested bool
	processHandle   syscall.Handle
}

func newProbeCommand(ctx context.Context, path string, args ...string) *probeCommand {
	commandCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(commandCtx, path, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: CREATE_SUSPENDED}
	command.WaitDelay = probeCommandWaitDelay
	probe := &probeCommand{
		Cmd:              command,
		parentContext:    ctx,
		lifecycleContext: commandCtx,
		cancel:           cancel,
	}
	command.Cancel = probe.cancelProcess
	return probe
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

	startErr := command.startWithJob()
	runErr := startErr
	if command.Process != nil {
		waitErr := command.Cmd.Wait()
		if startErr == nil {
			runErr = waitErr
		}
	}
	if cleanupErr := command.closeJob(); cleanupErr != nil {
		if runErr == nil {
			runErr = cleanupErr
		} else {
			runErr = errors.Join(runErr, cleanupErr)
		}
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

// startWithJob creates the Job before spawning the process, starts the process
// suspended, assigns it to the configured Job, and resumes it while holding
// stateMu. Cancellation cannot close the Job between assignment and resume.
func (command *probeCommand) startWithJob() error {
	job, err := createWindowsJob()
	if err != nil {
		return err
	}
	command.stateMu.Lock()
	command.job = job
	command.stateMu.Unlock()

	if err := command.Cmd.Start(); err != nil {
		return errors.Join(err, command.closeJob())
	}

	command.stateMu.Lock()
	defer command.stateMu.Unlock()
	if command.cancelRequested || command.lifecycleContext.Err() != nil {
		cause := command.lifecycleContext.Err()
		if cause == nil {
			cause = os.ErrProcessDone
		}
		return errors.Join(cause, command.abortLocked())
	}
	if command.Process == nil {
		return errors.Join(errors.New("windows command started without a process"), command.abortLocked())
	}

	processHandle, err := syscall.OpenProcess(
		PROCESS_SET_QUOTA|syscall.PROCESS_TERMINATE|PROCESS_SUSPEND_RESUME,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		return errors.Join(windowsCallError("OpenProcess", err), command.abortLocked())
	}
	closeProcessHandle := func() error {
		return closeWindowsProcessHandle(processHandle)
	}

	assigned, _, callErr := windowsAssignProcessToJob.Call(uintptr(command.job), uintptr(processHandle))
	if assigned == 0 {
		assignErr := windowsCallError("AssignProcessToJobObject", callErr)
		processCloseErr := closeProcessHandle()
		return errors.Join(assignErr, processCloseErr, command.abortLocked())
	}
	if err := resumeWindowsProcess(processHandle); err != nil {
		processCloseErr := closeProcessHandle()
		return errors.Join(err, processCloseErr, command.abortLocked())
	}

	command.jobAssigned = true
	if err := closeProcessHandle(); err != nil {
		command.processHandle = processHandle
		return errors.Join(err, command.abortLocked())
	}
	return nil
}

// abortLocked is used only before startWithJob returns success. The process
// is still suspended when the Job has not been assigned, so terminating this
// root directly cannot strand descendants; an assigned Job is closed first.
func (command *probeCommand) abortLocked() error {
	command.cancelRequested = true
	assigned := command.jobAssigned
	process := command.Process
	job := command.job
	command.job = 0
	command.jobAssigned = false
	processHandle := command.processHandle
	command.processHandle = 0

	var errs []error
	if job != 0 {
		if err := closeWindowsJobHandle(job); err != nil {
			errs = append(errs, err)
		}
	}
	if processHandle != 0 {
		if err := closeWindowsProcessHandle(processHandle); err != nil {
			errs = append(errs, err)
		}
	}
	if process != nil && !assigned {
		if err := terminateWindowsProcess(process.Pid); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (command *probeCommand) cancelProcess() error {
	command.stateMu.Lock()
	defer command.stateMu.Unlock()
	alreadyRequested := command.cancelRequested
	command.cancelRequested = true
	if alreadyRequested && command.job == 0 {
		return os.ErrProcessDone
	}

	assigned := command.jobAssigned
	process := command.Process
	job := command.job
	command.job = 0
	command.jobAssigned = false
	processHandle := command.processHandle
	command.processHandle = 0
	var errs []error
	if job != 0 {
		if err := closeWindowsJobHandle(job); err != nil {
			errs = append(errs, err)
		}
	}
	if processHandle != 0 {
		if err := closeWindowsProcessHandle(processHandle); err != nil {
			errs = append(errs, err)
		}
	}
	// Before assignment the process is still suspended and has no opportunity
	// to create descendants. Once assigned, closing the Job is the tree cleanup;
	// this direct fallback is only for a failed/unassigned startup.
	if process != nil && !assigned {
		if err := terminateWindowsProcess(process.Pid); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if process == nil {
		return os.ErrProcessDone
	}
	return nil
}

func (command *probeCommand) closeJob() error {
	command.stateMu.Lock()
	job := command.job
	command.job = 0
	command.jobAssigned = false
	processHandle := command.processHandle
	command.processHandle = 0
	command.stateMu.Unlock()

	var errs []error
	if job != 0 {
		if err := closeWindowsJobHandle(job); err != nil {
			errs = append(errs, err)
		}
	}
	if processHandle != 0 {
		if err := closeWindowsProcessHandle(processHandle); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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

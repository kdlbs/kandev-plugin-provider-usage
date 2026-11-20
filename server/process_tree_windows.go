//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procNtResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

// processTreeStartedHook is a test seam run between process creation and job
// assignment. The child is created suspended, so it cannot do anything inside
// this window; tests widen the window to prove that.
var processTreeStartedHook func()

// processTree is a kill-on-close job object holding the command and every
// process it creates.
type processTree struct {
	job windows.Handle
}

func runPlatformCommand(cmd *exec.Cmd) ([]byte, error) {
	tree, err := startProcessTree(cmd)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	return cmd.Wait()
}

// startProcessTree starts cmd suspended, assigns it to a new kill-on-close job
// and only then resumes it, so no descendant can be created outside the job.
func startProcessTree(cmd *exec.Cmd) (*processTree, error) {
	if err := procNtResumeProcess.Find(); err != nil {
		return nil, fmt.Errorf("find NtResumeProcess: %w", err)
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("set job limits: %w", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if processTreeStartedHook != nil {
		processTreeStartedHook()
	}
	fail := func(err error) (*processTree, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(fmt.Errorf("open child process: %w", err))
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return fail(fmt.Errorf("assign child to job: %w", err))
	}
	if status, _, _ := procNtResumeProcess.Call(uintptr(process)); status != 0 {
		return fail(fmt.Errorf("resume child process: NTSTATUS 0x%x", status))
	}
	return &processTree{job: job}, nil
}

// Close terminates every process still in the job.
func (t *processTree) Close() {
	if t != nil && t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

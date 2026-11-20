//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// processTree is the command's own process group.
type processTree struct {
	pgid int
}

// startProcessTree starts cmd as the leader of a new process group, so the
// helpers it spawns can be ended together with it.
func startProcessTree(cmd *exec.Cmd) (*processTree, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &processTree{pgid: cmd.Process.Pid}, nil
}

// Close kills whatever is left of the process group.
func (t *processTree) Close() {
	if t != nil && t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
		t.pgid = 0
	}
}

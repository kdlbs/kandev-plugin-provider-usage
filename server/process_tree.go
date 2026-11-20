package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

const (
	stderrCaptureLimit = 32 << 10
)

// boundedStderr keeps a short stderr prefix while accepting all writes, so the
// process pipe continues to drain after the buffer reaches its limit.
type boundedStderr struct {
	bytes.Buffer
}

func (b *boundedStderr) Write(data []byte) (int, error) {
	written := len(data)
	remaining := stderrCaptureLimit - b.Len()
	if remaining > len(data) {
		remaining = len(data)
	}
	if remaining > 0 {
		_, _ = b.Buffer.Write(data[:remaining])
	}
	return written, nil
}

// runProcessTree runs a command with a bounded wait and returns stdout like
// exec.Cmd.Output. On Windows, it also ends every process in the job object.
//
// Killing only the direct child is not enough. On Windows, Win-CodexBar drives
// agent CLIs through a pseudo console (ConPTY) whose conhost.exe is a child of
// codexbar; when the per-provider deadline kills codexbar, that conhost is
// orphaned, one per timed-out poll, and an orphan can spin at 100% CPU. The
// process-group fallback on Unix cannot safely target descendants after Wait
// reaps the group leader, so those platforms keep exec.Cmd.Output behavior.
func runProcessTree(ctx context.Context, waitDelay time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// --no-color only reaches codexbar's own output; its log lines are colored
	// by the logging layer and reach stderr as escape sequences even through a
	// pipe. NO_COLOR turns those off, so a failure reason lifted from stderr is
	// readable on the Settings card.
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	// Killing the process on a context deadline doesn't unblock Wait: it keeps
	// reading the output pipe until every process holding the write end exits.
	// WaitDelay prevents a child that keeps output pipes open from holding the
	// poller, which holds pollMu.
	cmd.WaitDelay = waitDelay
	var stdout bytes.Buffer
	stderr := &boundedStderr{}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	err := runPlatformCommand(cmd)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitErr.Stderr = stderr.Bytes()
	}
	return stdout.Bytes(), err
}

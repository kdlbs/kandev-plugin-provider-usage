package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// runProcessTree runs a command as a process tree and ends the whole tree when
// the command exits or ctx expires, returning stdout like exec.Cmd.Output (an
// *exec.ExitError carries stderr).
//
// Killing only the direct child is not enough. On Windows, Win-CodexBar drives
// agent CLIs through a pseudo console (ConPTY) whose conhost.exe is a child of
// codexbar; when the per-provider deadline kills codexbar, that conhost is
// orphaned, one per timed-out poll, and an orphan can spin at 100% CPU. The
// same applies to helpers such as node or MCP servers on any platform.
func runProcessTree(ctx context.Context, waitDelay time.Duration, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// --no-color only reaches codexbar's own output; its log lines are colored
	// by the logging layer and reach stderr as escape sequences even through a
	// pipe. NO_COLOR turns those off, so a failure reason lifted from stderr is
	// readable on the Settings card.
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	// Killing the process on a context deadline doesn't unblock Wait: it keeps
	// reading the output pipe until every process holding the write end exits.
	// WaitDelay bounds that, so a run that leaks its handles to a child can't
	// wedge the poller (which holds pollMu); the tree is ended right after.
	cmd.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	tree, err := startProcessTree(cmd)
	if err != nil {
		return nil, err
	}
	err = cmd.Wait()
	tree.Close()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitErr.Stderr = stderr.Bytes()
	}
	return stdout.Bytes(), err
}

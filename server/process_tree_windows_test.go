//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The child is created suspended, so it cannot run before it joins the job.
// The hook widens the creation-to-assignment window to 1.5 s: a child that ran
// inside it would start its grandchild outside the job, and that grandchild
// would survive the tree.
func TestRunChildCannotRunBeforeJobAssignment(t *testing.T) {
	processTreeStartedHook = func() { time.Sleep(1500 * time.Millisecond) }
	t.Cleanup(func() { processTreeStartedHook = nil })
	stop := filepath.Join(t.TempDir(), "release")
	t.Setenv(helperStopEnv, stop)
	t.Setenv(helperEnv, "spawn-grandchild-sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := newPlugin().run(ctx, os.Args[0])
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, ctx.Err(), "the runner must reach its deadline")
	require.FileExists(t, stop+".started", "the helper must start its grandchild")
	requireGrandchildDead(t, stop)
}

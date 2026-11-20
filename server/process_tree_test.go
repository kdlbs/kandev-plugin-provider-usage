package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireGrandchildDead releases a surviving grandchild via the stop file and
// fails if it announces itself: a grandchild ended with the tree never does.
func requireGrandchildDead(t *testing.T, stop string) {
	t.Helper()
	require.NoError(t, os.WriteFile(stop, nil, 0o644))
	time.Sleep(1500 * time.Millisecond)
	_, err := os.Stat(stop + ".released")
	require.True(t, os.IsNotExist(err), "the codexbar grandchild outlived run")
}

// The codexbar runner must end codexbar's whole process tree, on a normal exit
// and on a timeout. A plain exec.CommandContext kills only codexbar itself.
func TestRunEndsCodexbarProcessTree(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    string
		timeout time.Duration
	}{
		{"normal exit", "spawn-grandchild-exit", 30 * time.Second},
		{"timeout", "spawn-grandchild-sleep", 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop := filepath.Join(t.TempDir(), "release")
			t.Setenv(helperStopEnv, stop)
			t.Setenv(helperEnv, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			start := time.Now()
			_, err := newPlugin().run(ctx, os.Args[0])
			if tc.mode == "spawn-grandchild-exit" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Less(t, time.Since(start), 20*time.Second)
			}
			requireGrandchildDead(t, stop)
		})
	}
}

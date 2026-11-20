//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoundedStderrCapture(t *testing.T) {
	capture := &boundedStderr{}
	output := bytes.Repeat([]byte("x"), 2*stderrCaptureLimit)
	if written, err := capture.Write(output); err != nil || written != len(output) {
		t.Fatalf("write stderr: got %d bytes, %v", written, err)
	}

	got := capture.Bytes()
	if len(got) != stderrCaptureLimit {
		t.Fatalf("captured %d bytes, want at most %d", len(got), stderrCaptureLimit)
	}
	if !bytes.Equal(got, output[:stderrCaptureLimit]) {
		t.Fatal("stderr prefix was not preserved")
	}
}

// requireGrandchildDead releases a surviving grandchild via the stop file and
// fails if it announces itself: a grandchild ended with the tree never does.
func requireGrandchildDead(t *testing.T, stop string) {
	t.Helper()
	require.NoError(t, os.WriteFile(stop, nil, 0o644))
	time.Sleep(1500 * time.Millisecond)
	_, err := os.Stat(stop + ".released")
	require.True(t, os.IsNotExist(err), "the codexbar grandchild outlived run")
}

func cancelWhenFileExists(ctx context.Context, cancel context.CancelFunc, path string) <-chan bool {
	started := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				started <- false
				return
			case <-ticker.C:
				if _, err := os.Stat(path); err == nil {
					cancel()
					started <- true
					return
				}
			}
		}
	}()
	return started
}

// The codexbar runner must end codexbar's whole process tree, on a normal exit
// and on a timeout. A plain exec.CommandContext kills only codexbar itself.
func TestRunEndsCodexbarProcessTree(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
	}{
		{"normal exit", "spawn-grandchild-exit"},
		{"timeout", "spawn-grandchild-sleep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop := filepath.Join(t.TempDir(), "release")
			t.Setenv(helperStopEnv, stop)
			t.Setenv(helperEnv, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var grandchildStarted <-chan bool
			if tc.mode == "spawn-grandchild-sleep" {
				grandchildStarted = cancelWhenFileExists(ctx, cancel, stop+".started")
			}
			start := time.Now()
			_, err := newPlugin().run(ctx, os.Args[0])
			if tc.mode == "spawn-grandchild-exit" {
				require.NoError(t, err)
			} else {
				require.True(t, <-grandchildStarted, "the helper must start its grandchild before cancellation")
				require.Error(t, err)
				require.Equal(t, context.Canceled, ctx.Err(), "the runner must stop after the helper starts")
				require.Less(t, time.Since(start), 20*time.Second)
			}
			require.FileExists(t, stop+".started", "the helper must start its grandchild")
			requireGrandchildDead(t, stop)
		})
	}
}

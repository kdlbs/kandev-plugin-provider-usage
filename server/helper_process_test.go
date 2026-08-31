package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// helperEnv turns this test binary into a stand-in for codexbar. Re-execing
// os.Args[0] is the portable way to get a real *exec.ExitError, and a real
// process holding an inherited pipe, without depending on a shell — Windows,
// the platform these behaviors matter most on, has none.
const helperEnv = "KANDEV_PROVIDER_USAGE_TEST_HELPER"

// helperStopEnv names a file whose appearance releases the pipe holder. Windows
// will not let `go test` delete a running binary, so the holder has to be gone
// before the test binary is cleaned up rather than sleeping out a fixed delay.
const helperStopEnv = "KANDEV_PROVIDER_USAGE_TEST_STOP"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "stderr-exit":
		fmt.Fprintln(os.Stderr, "config file is corrupt")
		os.Exit(3)
	case "tracing-stderr":
		// Verbatim bytes from codexbar-cli.exe 0.55.0, escape sequences included:
		// its logging layer colors stderr regardless of --no-color.
		fmt.Fprint(os.Stderr, sampleTracingStderr)
		fmt.Fprintln(os.Stderr, "provider claude is not signed in")
		os.Exit(1)
	case "spawn-holder":
		// Leak the output pipe to a process that outlives us: Wait cannot see EOF
		// until it exits, which is what WaitDelay has to bound.
		grandchild := exec.Command(os.Args[0])
		grandchild.Env = append(os.Environ(), helperEnv+"=hold-stdout")
		grandchild.Stdout = os.Stdout
		grandchild.Stderr = os.Stderr
		if err := grandchild.Start(); err != nil {
			os.Exit(9)
		}
		os.Exit(0)
	case "hold-stdout":
		stop := os.Getenv(helperStopEnv)
		for range 3000 {
			if _, err := os.Stat(stop); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Announce the release before exiting: Windows keeps the test binary
		// locked while this process runs it, and `go test` deletes that binary as
		// soon as the run ends.
		_ = os.WriteFile(stop+".released", nil, 0o644)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sampleTracingStderr is one captured line of codexbar-cli.exe 0.55.0 stderr,
// byte for byte. The escapes are the point: the logging layer colors its output
// even through a pipe and even with --no-color, which is what hides the
// timestamp from a naive tracing-line check.
const sampleTracingStderr = "\x1b[2m2026-08-29T15:18:12.238504Z\x1b[0m \x1b[33m WARN\x1b[0m " +
	"\x1b[2mcodexbar::browser::cookies\x1b[0m\x1b[2m:\x1b[0m Chromium App-Bound Encryption (ABE) " +
	"detected: all 10 cookies failed to decrypt \x1b[3mbrowser\x1b[0m\x1b[2m=\x1b[0mMicrosoft Edge\n"

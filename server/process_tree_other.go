//go:build !windows

package main

import "os/exec"

// runPlatformCommand keeps the established Unix process and stderr behavior.
// A process group ID can be reused after cmd.Wait reaps its leader, so delayed
// signaling can affect an unrelated group.
func runPlatformCommand(cmd *exec.Cmd) error {
	return cmd.Run()
}

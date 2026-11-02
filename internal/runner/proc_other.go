//go:build !unix

package runner

import "os/exec"

// Process groups are a unix concept; elsewhere only the process is signalled.
func setProcessGroup(*exec.Cmd) {}

func signalGroup(cmd *exec.Cmd, _ bool) {
	_ = cmd.Process.Kill()
}

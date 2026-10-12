//go:build !windows

package hub

import (
	"os/exec"
	"syscall"
)

// ownGroup runs cmd in its own process group, and a cancel kills the
// whole group, so nothing the update started outlives it.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

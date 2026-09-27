//go:build !windows

package core

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ownerDead reports whether the owner line ("<time> pid <n>") names a process
// that no longer exists.
func ownerDead(owner []byte) bool {
	f := strings.Fields(string(owner))
	if len(f) < 3 || f[len(f)-2] != "pid" {
		return false
	}
	pid, err := strconv.Atoi(f[len(f)-1])
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return false
	}
	return syscall.Kill(pid, 0) == syscall.ESRCH
}

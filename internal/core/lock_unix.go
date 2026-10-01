//go:build !windows

package core

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ownerDead reports whether the owner line ("<time> pid <n> [start <s>]")
// names a process that no longer exists, or whose pid now belongs to a
// process started at another time.
func ownerDead(owner []byte) bool {
	f := strings.Fields(string(owner))
	pid, start := 0, ""
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "pid":
			pid, _ = strconv.Atoi(f[i+1])
		case "start":
			start = f[i+1]
		}
	}
	if pid <= 0 || pid == os.Getpid() {
		return false
	}
	if syscall.Kill(pid, 0) == syscall.ESRCH {
		return true
	}
	return start != "" && ProcStart(pid) != "" && ProcStart(pid) != start
}

// ProcStart is a process's start time as one token, or "" if unknown. With
// the pid it tells a live process from a later one that reused its pid.
func ProcStart(pid int) string {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), "_")
}

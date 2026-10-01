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
	if start == "" {
		return false
	}
	now := ProcStart(pid)
	// Locks written by v0.8.0 to v0.8.2 used the local time and locale.
	return now != "" && now != start && procStartLocal(pid) != start
}

func procStartLocal(pid int) string {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), "_")
}

// ProcStart is a process's start time as one token, or "" if unknown. With
// the pid it tells a live process from a later one that reused its pid.
func ProcStart(pid int) string {
	// ps prints the start time in local time and the locale's words; fix
	// both, so every caller writes and compares the same text.
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), "_")
}

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Lock attempts, spaced by lockWait. A leftover lock whose owner process is
// gone (interrupt, kill -9) is removed once and retried; _local is per host,
// so the recorded pid is on this machine and a dead process cannot be
// mid-rename. A lock with no readable owner is reported as busy (design §8).
var (
	lockTries = 10
	lockWait  = 200 * time.Millisecond
)

// lock takes the per-ID mkdir lock and returns its release function.
func (p *Project) lock(id string) (func(), error) {
	root := p.local("locks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	dir := filepath.Join(root, id)
	if err := p.noSymlink(dir); err != nil {
		return nil, err
	}
	reclaimed := false
	for i := 0; ; i++ {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		if i+1 >= lockTries {
			owner, _ := os.ReadFile(filepath.Join(dir, "owner"))
			if !reclaimed && ownerDead(owner) {
				os.RemoveAll(dir)
				reclaimed = true
				i = -1
				continue
			}
			if len(owner) == 0 {
				owner = []byte("unknown")
			}
			return nil, fail(ExitClaim, "busy", "%s is locked by another operation (%s); retry, or run sunstack health", id, trimNL(owner))
		}
		time.Sleep(lockWait)
	}
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(fmt.Sprintf("%s pid %d\n", now(), os.Getpid())), 0o644)
	return func() { os.RemoveAll(dir) }, nil
}

func trimNL(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}

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

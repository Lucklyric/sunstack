package core

import (
	"fmt"
	"os"
	"path/filepath"
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
	return lockDir(dir, id)
}

// LockDir takes the mkdir lock at dir for code outside a project (the org
// hub); name is used in the busy message.
func LockDir(dir, name string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	return lockDir(dir, name)
}

// lockDir takes the mkdir lock at dir; name is used in the busy message.
func lockDir(dir, id string) (func(), error) {
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
	owner := fmt.Sprintf("%s pid %d", now(), os.Getpid())
	if st := ProcStart(os.Getpid()); st != "" {
		owner += " start " + st
	}
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(owner+"\n"), 0o644)
	return func() { os.RemoveAll(dir) }, nil
}

func trimNL(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}

// LockDirOnce takes the mkdir lock at dir without waiting: a lock held by a
// live process is reported as busy at once, one left by a dead process is
// taken over. The org connector uses it to run once per host.
func LockDirOnce(dir string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	err := os.Mkdir(dir, 0o755)
	if os.IsExist(err) {
		owner, _ := os.ReadFile(filepath.Join(dir, "owner"))
		if !ownerDead(owner) {
			return nil, fail(ExitClaim, "busy", "%s is held (%s)", filepath.Base(dir), trimNL(owner))
		}
		os.RemoveAll(dir)
		err = os.Mkdir(dir, 0o755)
	}
	if err != nil {
		return nil, fail(ExitClaim, "busy", "%v", err)
	}
	owner := fmt.Sprintf("%s pid %d", now(), os.Getpid())
	if st := ProcStart(os.Getpid()); st != "" {
		owner += " start " + st
	}
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(owner+"\n"), 0o644)
	return func() { os.RemoveAll(dir) }, nil
}

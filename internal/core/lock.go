package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Lock attempts, spaced by lockWait. A leftover lock (interrupt, kill -9, power
// loss) is reported as busy and never removed automatically (design §8).
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
			if len(owner) == 0 {
				owner = []byte("unknown")
			}
			return nil, fail(ExitClaim, "busy", "%s is locked by another operation (%s); retry, or run sunstack health", id, trimNL(owner))
		}
		time.Sleep(lockWait)
	}
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(fmt.Sprintf("%s pid %d\n", now(), os.Getpid())), 0o644)
	// An interrupted command leaves its lock behind on purpose: a writer may
	// still be mid-rename. health reports it for manual removal (design §8).
	return func() { os.RemoveAll(dir) }, nil
}

func trimNL(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}

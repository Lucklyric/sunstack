package core

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Lock attempts, spaced by lockWait. A leftover lock (kill -9, power loss) is
// reported as busy and never removed automatically (design §8).
var (
	lockTries = 10
	lockWait  = 200 * time.Millisecond
)

var (
	heldMu sync.Mutex
	held   = map[string]bool{}
	sigsOn sync.Once
)

// releaseAllOnSignal removes every lock this process holds when it is
// interrupted, then exits.
func releaseAllOnSignal() {
	sigsOn.Do(func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		go func() {
			<-c
			heldMu.Lock()
			for dir := range held {
				os.RemoveAll(dir)
			}
			os.Exit(ExitFail)
		}()
	})
}

// lock takes the per-ID mkdir lock and returns its release function.
func (p *Project) lock(id string) (func(), error) {
	root := p.local("locks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	releaseAllOnSignal()
	dir := filepath.Join(root, id)
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
	heldMu.Lock()
	held[dir] = true
	heldMu.Unlock()
	_ = os.WriteFile(filepath.Join(dir, "owner"), []byte(fmt.Sprintf("%s pid %d\n", now(), os.Getpid())), 0o644)
	return func() {
		heldMu.Lock()
		delete(held, dir)
		heldMu.Unlock()
		os.RemoveAll(dir)
	}, nil
}

func trimNL(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}

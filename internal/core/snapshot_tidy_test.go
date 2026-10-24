package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// §32: a snapshot that waits for the agent's lock to tidy, while a release
// removes its claim, refuses instead of writing as the released session.
func TestSnapshotTidyAfterRelease(t *testing.T) {
	p := secTeam(t)
	id, token := "builder.ann", "0123456789abcdef"
	board := "## Now\n## Next\n## Done\n- 2020-01-01 KR1 [O1] completed (verified: test)\n"
	path := filepath.Join(p.AgentDir(id), "board.md")
	if err := os.WriteFile(path, []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	live := &Live{Token: token, Claimed: now(), Tool: "claude"}
	if err := p.writeLive(id, live); err != nil {
		t.Fatal(err)
	}
	unlock, err := p.lock(id)
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			unlock()
		}
	}()
	ch := make(chan error, 1)
	go func() { _, e := p.Snapshot(id, "board.md", token); ch <- e }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b := make([]byte, 1<<20)
		n := runtime.Stack(b, true)
		if strings.Contains(string(b[:n]), ".Snapshot(") && strings.Contains(string(b[:n]), ".lockDir(") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshot never waited for the agent's lock")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.removeLive(live); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(p.sessionTmp(id, token))
	unlock()
	held = false
	err = <-ch
	var ce *Error
	if !errors.As(err, &ce) || ce.Reason != "token" {
		t.Fatalf("snapshot after release: %v, want a token error", err)
	}
	if got, _ := os.ReadFile(path); string(got) != board {
		t.Errorf("a released snapshot tidied the board:\n%s", got)
	}
	if _, err := os.Stat(p.sessionTmp(id, token)); err == nil {
		t.Error("a released snapshot recreated its scratch folder")
	}
}

// §32: when the lock wait runs out while a release still holds the lock, the
// snapshot checks its claim again instead of going on with the old one.
func TestSnapshotTidyLockTimeoutAfterRelease(t *testing.T) {
	p := secTeam(t)
	defer SetLockWait(10, 20*time.Millisecond)()
	id, token := "builder.ann", "0123456789abcdef"
	path := filepath.Join(p.AgentDir(id), "board.md")
	if err := os.WriteFile(path, []byte("## Now\n## Next\n## Done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	live := &Live{Token: token, Claimed: now(), Tool: "claude"}
	if err := p.writeLive(id, live); err != nil {
		t.Fatal(err)
	}
	unlock, err := p.lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ch := make(chan error, 1)
	go func() { _, e := p.Snapshot(id, "board.md", token); ch <- e }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b := make([]byte, 1<<20)
		n := runtime.Stack(b, true)
		if strings.Contains(string(b[:n]), ".Snapshot(") && strings.Contains(string(b[:n]), ".lockDir(") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshot never waited for the agent's lock")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.removeLive(live); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(p.sessionTmp(id, token))
	err = <-ch
	var ce *Error
	if !errors.As(err, &ce) || ce.Reason != "token" {
		t.Fatalf("snapshot after release and lock timeout: %v, want a token error", err)
	}
	if _, err := os.Stat(p.sessionTmp(id, token)); err == nil {
		t.Error("a released snapshot recreated its scratch folder")
	}
}

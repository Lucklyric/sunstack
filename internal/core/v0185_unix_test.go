//go:build !windows

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A tmux server that stopped answering does not keep the agent's lock: as
// and release return, and a send to the agent is not refused as busy.
func TestStuckTmuxDoesNotHoldAgentLock(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	p := secTeam(t)
	sock := filepath.Join(t.TempDir(), "sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	standIns(t, sleep, "block", "x")
	done := make(chan *AsResult, 1)
	go func() {
		r, _ := p.As(AsOptions{Arg: "builder.ann", Tool: "claude", Pane: "%1", Socket: sock})
		done <- r
	}()
	var r *AsResult
	select {
	case r = <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("as still waits on tmux under the agent lock")
	}
	if r == nil {
		t.Fatal("as failed")
	}
	rel := make(chan error, 1)
	go func() { rel <- p.Release("builder.ann", r.Token) }()
	select {
	case <-rel:
	case <-time.After(8 * time.Second):
		t.Fatal("release still waits on tmux under the agent lock")
	}
}

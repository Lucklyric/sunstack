package main

// v0.17.2: fixes from the whole-code review (§34).

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// §34.1: one Claude session in two live panes. clean leaves the busy claimed
// pane alone, whoami from the other pane does not hand over the claim, and
// health names both panes.
func TestDuplicateSessionPanes(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	sid := "00000000-0000-4000-8000-000000000079"
	var panes []string
	var entries []map[string]any
	ttys := map[string]string{}
	for _, status := range []string{"idle", "busy"} {
		pane := s.tmux(t, "new-window", "-d", "-P", "-F", "#{pane_id}", "-c", p, "/bin/sh")
		panes = append(panes, pane)
		q := *s
		q.pane = pane
		q.start(t, "claude", "idle")
		eventually(t, "stand-in launches", func() bool { return len(launches(t, s.env)) == len(panes) })
		tty := strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", pane, "#{pane_tty}"), "/dev/")
		pid := 0
		for _, ev := range launches(t, s.env) {
			b, _ := exec.Command("/bin/ps", "-o", "tty=", "-p", strconv.Itoa(ev.PID)).Output()
			if strings.TrimPrefix(strings.TrimSpace(string(b)), "tty") == strings.TrimPrefix(tty, "tty") {
				pid = ev.PID
			}
		}
		if pid == 0 {
			t.Fatal("stand-in PID not found")
		}
		ttys[strconv.Itoa(pid)] = tty
		entries = append(entries, map[string]any{"pid": pid, "sessionId": sid, "cwd": p, "kind": "interactive", "status": status})
	}
	mapping, _ := json.Marshal(ttys)
	s.env = append(s.env, "SUNSTACK_TEST_PROCESS_TTYS="+string(mapping))
	claudeList(t, s.env, entries...)
	old := time.Now().Add(-48 * time.Hour)
	must(t, os.Chtimes(transcript(t, s.env, "claude", sid, p), old, old))
	claimEnv := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid, "TMUX_PANE="+panes[1])
	r := sh(t, p, claimEnv, "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "claim in the busy pane")
	tok := field(tokenRe, r.out)
	cp := filepath.Join(p, "sunstack", "_local", "live", "reviewer", tok+".json")
	b, err := os.ReadFile(cp)
	must(t, err)
	var c core.Live
	must(t, json.Unmarshal(b, &c))
	c.Claimed = old.UTC().Format(time.RFC3339)
	c.LastContact = c.Claimed
	b, _ = json.Marshal(c)
	must(t, os.WriteFile(cp, b, 0o600))

	sh(t, p, s.env, "clean", "--yes")
	if !strings.Contains(s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}"), panes[1]) {
		t.Error("clean closed the busy claimed pane")
	}
	dupEnv := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid, "TMUX_PANE="+panes[0])
	if r := sh(t, p, dupEnv, "whoami"); strings.Contains(r.out, tok) {
		t.Error("whoami from the duplicate pane handed over the claim")
	}
	requireContains(t, sh(t, p, s.env, "health").out, "session "+sid+" runs in panes")
}

package main

// v0.12.7: agent sessions idle over 24 hours show in next, and kill --idle
// closes them after one confirmation.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

func TestIdleSessions(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	spawn := func(task string) string {
		r := sh(t, p, s.env, "spawn", "reviewer", "--task", task, "--tool", "claude", "--place", "here")
		expect(t, r, 0, "spawn "+task)
		return field(paneRe, r.out)
	}
	oldPane, freshPane, busyPane := spawn("old"), spawn("fresh"), spawn("busy")

	// The scan sees each stand-in at its prompt, in its pane.
	ttys := map[string]string{}
	var list []map[string]any
	// Each stand-in's launch names its task; its pane is that spawn's.
	paneOf := map[string]string{"--task old": oldPane, "--task fresh": freshPane, "--task busy": busyPane}
	for _, e := range launches(t, s.env) {
		for task, pn := range paneOf {
			if strings.Contains(strings.Join(e.Args, " "), task+" ") {
				ttys[strconv.Itoa(e.PID)] = strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", pn, "#{pane_tty}"), "/dev/")
				list = append(list, map[string]any{"pid": e.PID, "cwd": p, "kind": "interactive", "status": "idle"})
			}
		}
	}
	b, _ := json.Marshal(ttys)
	s.env = append(s.env, "SUNSTACK_TEST_PROCESS_TTYS="+string(b))
	claudeList(t, s.env, list...)

	// old and busy were last active two days ago; busy still holds a message.
	stale := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	claims, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "live", "reviewer", "*.json"))
	for _, f := range claims {
		var c core.Live
		b, err := os.ReadFile(f)
		must(t, err)
		must(t, json.Unmarshal(b, &c))
		if c.Task == "fresh" {
			continue
		}
		c.Claimed, c.LastContact = stale, stale
		b, _ = json.Marshal(c)
		must(t, os.WriteFile(f, b, 0o600))
		if c.Task == "busy" {
			d := filepath.Join(p, "sunstack", "_local", "inbox", "reviewer", ".taken", c.Token)
			must(t, os.MkdirAll(d, 0o755))
			must(t, os.WriteFile(filepath.Join(d, "20261016T000000Z-user-abcdef.md"), []byte("---\nfrom: user\nto: reviewer\ntype: task\n---\nGoal: x\n"), 0o644))
		}
	}

	r := sh(t, p, s.env, "next", "--all")
	requireContains(t, r.out, "1 agent session(s) idle over 24h: reviewer_old", "sunstack clean")
	if strings.Contains(r.out, "reviewer_fresh") || strings.Contains(r.out, "reviewer_busy") {
		t.Errorf("only reviewer_old is idle:\n%s", r.out)
	}

	r = sh(t, p, s.env, "kill", "--idle")
	expect(t, r, 2, "kill --idle needs a confirmation")
	requireContains(t, r.stderr, "reviewer_old", "--yes")
	r = sh(t, p, s.env, "kill", "--idle", "--yes")
	expect(t, r, 0, "kill --idle --yes")
	requireContains(t, r.out, "closed reviewer_old")
	panes := s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}")
	if strings.Contains(panes, oldPane+"\n") || strings.HasSuffix(panes, oldPane) || !strings.Contains(panes, freshPane) || !strings.Contains(panes, busyPane) {
		t.Errorf("panes after kill --idle: %q (old %s)", panes, oldPane)
	}
	if r := sh(t, p, s.env, "next", "--all"); strings.Contains(r.out, "idle over 24h") {
		t.Errorf("idle line after kill --idle:\n%s", r.out)
	}
	r = sh(t, p, s.env, "kill", "--idle", "--yes")
	expect(t, r, 0, "nothing idle")
	requireContains(t, r.out, "nothing to close")
}

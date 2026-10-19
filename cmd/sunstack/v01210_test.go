package main

// v0.12.10: kill --idle also closes free sessions idle over 24 hours and the
// panes sunstack opened whose CLI exited.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKillIdleFreeAndExited(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	spawn := func(args ...string) string {
		r := sh(t, p, s.env, append([]string{"spawn"}, args...)...)
		expect(t, r, 0, "spawn "+strings.Join(args, " "))
		return field(paneRe, r.out)
	}
	gone := spawn("reviewer", "--task", "gone", "--tool", "claude", "--place", "here")
	scratch := spawn("--free", "--tool", "claude", "--name", "scratch", "--place", "here")
	recent := spawn("--free", "--tool", "claude", "--name", "recent", "--place", "here")
	left := spawn("--free", "--tool", "claude", "--name", "left", "--place", "here")

	// Each pane's stand-in, found by its terminal.
	pidOf := map[string]int{}
	ttyOf := map[string]string{}
	for _, pn := range []string{gone, scratch, recent, left} {
		ttyOf[pn] = strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", pn, "#{pane_tty}"), "/dev/")
	}
	for i := 0; i < 50 && len(launches(t, s.env)) < 4; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	for _, e := range launches(t, s.env) {
		// The real ps, not the stand-in on PATH. It shortens ttys005 to
		// s005 on macOS; pts/5 stays as is on Linux.
		out, _ := exec.Command("/bin/ps", "-o", "tty=", "-p", strconv.Itoa(e.PID)).Output()
		got := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/"), "tty")
		for pn, tty := range ttyOf {
			if got != "" && strings.TrimPrefix(tty, "tty") == got {
				pidOf[pn] = e.PID
			}
		}
	}
	if len(pidOf) != 4 {
		t.Fatalf("stand-ins by pane: %v (terminals %v)", pidOf, ttyOf)
	}

	// scratch was last active two days ago, recent just now.
	home := envValue(s.env, "HOME")
	dir := filepath.Join(home, ".claude", "projects", "fixture")
	must(t, os.MkdirAll(dir, 0o755))
	sids := map[string]string{scratch: "00000000-0000-4000-8000-000000000031", recent: "00000000-0000-4000-8000-000000000032"}
	ttys := map[string]string{}
	var list []map[string]any
	for pn, sid := range sids {
		f := filepath.Join(dir, sid+".jsonl")
		must(t, os.WriteFile(f, []byte("{}\n"), 0o600))
		if pn == scratch {
			old := time.Now().Add(-48 * time.Hour)
			must(t, os.Chtimes(f, old, old))
		}
		ttys[strconv.Itoa(pidOf[pn])] = ttyOf[pn]
		list = append(list, map[string]any{"pid": pidOf[pn], "sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})
	}
	b, _ := json.Marshal(ttys)
	s.env = append(s.env, "SUNSTACK_TEST_PROCESS_TTYS="+string(b))
	claudeList(t, s.env, list...)

	// The CLIs in gone and left exit, leaving their panes at the shell.
	for _, pn := range []string{gone, left} {
		proc, err := os.FindProcess(pidOf[pn])
		must(t, err)
		must(t, proc.Kill())
	}
	for i := 0; i < 50; i++ {
		if s.tmux(t, "display-message", "-p", "-t", gone, "#{pane_current_command}") == "sh" &&
			s.tmux(t, "display-message", "-p", "-t", left, "#{pane_current_command}") == "sh" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	r := sh(t, p, s.env, "next", "--all")
	requireContains(t, r.out, "1 free session(s) idle over 24h: scratch (free) (2d)", "2 pane(s) whose CLI exited",
		"reviewer_gone (claude exited)", "left (free, claude exited)", "sunstack kill --idle")
	if strings.Contains(r.out, "recent") {
		t.Errorf("recent is not idle:\n%s", r.out)
	}

	r = sh(t, p, s.env, "kill", "--idle")
	expect(t, r, 2, "kill --idle needs a confirmation")
	requireContains(t, r.stderr, "scratch", "reviewer_gone", "left", "--yes")
	r = sh(t, p, s.env, "kill", "--idle", "--yes")
	expect(t, r, 0, "kill --idle --yes")
	requireContains(t, r.out, "closed scratch", "closed reviewer_gone", "closed left")
	panes := strings.Fields(s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}"))
	has := func(pn string) bool {
		for _, x := range panes {
			if x == pn {
				return true
			}
		}
		return false
	}
	if has(gone) || has(scratch) || has(left) || !has(recent) || !has(s.pane) {
		t.Errorf("panes after kill --idle: %v (closed %s %s %s, kept %s %s)", panes, gone, scratch, left, recent, s.pane)
	}
	if r := sh(t, p, s.env, "team", "--sessions"); strings.Contains(r.out, "reviewer_gone") {
		t.Errorf("the exited session's claim is still there:\n%s", r.out)
	}
	r = sh(t, p, s.env, "kill", "--idle", "--yes")
	expect(t, r, 0, "nothing idle")
	requireContains(t, r.out, "nothing to close")
}

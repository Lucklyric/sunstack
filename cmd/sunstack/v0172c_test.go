package main

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

func review172Sessions(t *testing.T) (string, *tmuxFixture, []string, []map[string]any) {
	t.Helper()
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	var panes []string
	var entries []map[string]any
	ttys := map[string]string{}
	sid := "00000000-0000-4000-8000-000000000093"
	for i := 0; i < 2; i++ {
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
			t.Fatal("missing stand-in PID")
		}
		ttys[strconv.Itoa(pid)] = tty
		entries = append(entries, map[string]any{"pid": pid, "sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})
	}
	mapping, _ := json.Marshal(ttys)
	s.env = append(s.env, "SUNSTACK_TEST_PROCESS_TTYS="+string(mapping))
	claudeList(t, s.env, entries...)
	transcript(t, s.env, "claude", sid, p)
	return p, s, panes, entries
}

func review172Identity(s *tmuxFixture, pane, sid string) []string {
	return append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid, "TMUX_PANE="+pane)
}

func TestReview172DuplicateFreeNudgeByPane(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	r := sh(t, p, s.env, "send", sid, "--type", "question", "review the pending item")
	expect(t, r, 0, "session-addressed send")
	requireContains(t, r.out, "runs in panes")
	if got := submitted(t, s.env); len(got) != 0 {
		t.Fatalf("session-ID send nudged: %v", got)
	}
	r = sh(t, p, s.env, "send", panes[0], "--type", "question", "review the pending item")
	expect(t, r, 0, "pane-addressed send")
	time.Sleep(150 * time.Millisecond)
	t.Logf("pane send=%q submitted=%q", r.out, submitted(t, s.env))
	if len(submitted(t, s.env)) > 0 {
		t.Error("pane-addressed host send bypassed duplicate-session nudge suppression")
	}
}

func TestReview172DuplicateAgentNudgeAndRecovery(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	env := review172Identity(s, panes[1], sid)
	r := sh(t, p, env, "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "claim")
	token := field(tokenRe, r.out)
	expect(t, sh(t, p, review172Identity(s, panes[0], sid), "whoami"), 1, "duplicate recovery blocked")
	r = sh(t, p, env, "whoami")
	t.Logf("claimed-pane recovery exit=%d returnsToken=%v", r.code, strings.Contains(r.out, token))
	r = sh(t, p, s.env, "send", "reviewer", "--type", "question", "review the pending item")
	time.Sleep(150 * time.Millisecond)
	t.Logf("agent send=%q submitted=%q", r.out, submitted(t, s.env))
	if len(submitted(t, s.env)) > 0 {
		t.Error("agent-addressed send still nudges an ambiguous session")
	}
	for _, args := range [][]string{{"health"}, {"next", "--all"}} {
		r := sh(t, p, s.env, args...)
		requireContains(t, r.out, "session "+sid+" runs in panes")
	}
	r = sh(t, p, s.env, "org", "--by", "host", "--all")
	t.Logf("org warns about duplicate=%v", strings.Contains(r.out, "runs in panes"))
}

func TestReview172MovedSessionRecovery(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	r := sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "original claim")
	token := field(tokenRe, r.out)
	r = sh(t, p, review172Identity(s, panes[0], ""), "whoami")
	expect(t, r, 0, "same-pane recovery after lost session ID")
	requireContains(t, r.out, token)
	hook := exec.Command(bin, "hook")
	hook.Env = append(cleanEnv(), review172Identity(s, panes[0], sid)...)
	payload, _ := json.Marshal(map[string]string{"session_id": sid, "cwd": p, "hook_event_name": "SessionStart", "source": "compact"})
	hook.Stdin = strings.NewReader(string(payload))
	output, err := hook.Output()
	must(t, err)
	requireContains(t, string(output), token)
	t.Log("same-pane whoami and SessionStart compact hook restored the token")
	entries[0]["sessionId"] = "00000000-0000-4000-8000-000000000094"
	claudeList(t, s.env, entries...)
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	t.Logf("moved-session recovery with unrelated Claude in old pane: exit=%d out=%q stderr=%q", r.code, r.out, r.stderr)
	if r.code != 0 || !strings.Contains(r.out, token) {
		t.Error("unique resumed session cannot recover while old pane runs an unrelated Claude session")
	}
	s.tmux(t, "kill-pane", "-t", panes[0])
	claudeList(t, s.env, entries[1])
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	expect(t, r, 0, "recovery after old pane closes")
	requireContains(t, r.out, token)
}

func TestReview172ReleaseStrandedCLIAndRecovery(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	r := sh(t, p, env, "as", "reviewer")
	expect(t, r, 0, "claim")
	token := field(tokenRe, r.out)
	r = sh(t, p, env, "send", "reviewer", "--type", "handoff", "--no-nudge", "finish the review")
	expect(t, r, 0, "send")
	files, err := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", "reviewer", "*.md"))
	must(t, err)
	if len(files) != 1 {
		t.Fatalf("message list: %v", files)
	}
	id := strings.TrimSuffix(filepath.Base(files[0]), ".md")
	expect(t, sh(t, p, env, "take", "reviewer", id, "--token", token), 0, "take")
	inbox := filepath.Dir(files[0])
	must(t, os.Chmod(inbox, 0500))
	defer os.Chmod(inbox, 0700)
	r = sh(t, p, env, "release", "reviewer", "--token", token)
	expect(t, r, 1, "stranded release")
	requireContains(t, r.stderr, "stranded:")
	requireContains(t, r.stderr, id)
	t.Logf("release exit=%d stderr=%q", r.code, r.stderr)
	must(t, os.Chmod(inbox, 0700))
	r = sh(t, p, env, "as", "reviewer")
	expect(t, r, 0, "new claim")
	token = field(tokenRe, r.out)
	r = sh(t, p, env, "check", "reviewer", "--token", token)
	expect(t, r, 0, "retry check")
	requireContains(t, r.out, id)
	t.Log("stranded message recovered after permissions repaired and a new claim acquired")
}

func TestReview172UserKRNextCLI(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	day := time.Now().Format("2006-01-02")
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "BOARD.md"), []byte("## Objectives\n- "+day+" O1 Ship\n## User\n- "+day+" KR1 [O1] Approve release\n"), 0600))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "reviewer", "board.md"), []byte("## Now\n- "+day+" KR1 [O1] Deploy (needs: user#KR1)\n"), 0600))
	r := sh(t, p, env, "next", "--all")
	expect(t, r, 0, "next user dependency")
	requireContains(t, r.out, "waiting on your user#KR1", "the board skill: finish or update your user#KR1")
	if strings.Contains(r.out, "ask user with the send skill") {
		t.Error("next still directs a message to user")
	}
	t.Logf("next user dependency output=%q", r.out)
}

package main

// v0.8.11: fixes from the Codex review of v0.8.4 to v0.8.10.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Only sunstack answer (approval-gated) speaks for the user on an ask.
func TestAnswerOnlyFromAnswerCommand(t *testing.T) {
	p, env := team086(t)
	tok := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	commitBoard(t, p, env, "builder.alice", tok, "## Now\n## Next\n## Asks\n- 2026-10-05 Q1 CSV? (default: csv after 2999-01-01)\n## Done\n- 2026-10-04 Q4 UTC? (answered: default)\n")
	r := sh(t, p, env, "send", "builder.alice", "Q1: parquet", "--type", "answer")
	expect(t, r, 2, "send --type answer")
	requireContains(t, r.stderr, "sunstack answer")
	// A defaulted ask can be overruled, as next suggests.
	expect(t, sh(t, p, env, "answer", "builder.alice", "Q4", "local time"), 0, "overrule a default")
	expect(t, sh(t, p, env, "answer", "builder.alice", "Q1", "parquet"), 0, "answer an open ask")
}

// The cap check never removes claims, and two spawns at once respect it.
func TestCapIsReadOnlyAndSerialized(t *testing.T) {
	p := fixture(t)
	writeFile(t, p, "sunstack/TEAM", "id: 00000000000000ab\nname: capped\nmax_sessions: 5\n")
	dir := filepath.Join(p, "sunstack", "_local", "live", "reviewer")
	must(t, os.MkdirAll(dir, 0o755))
	claim := filepath.Join(dir, "eeeeeeeeeeeeeeee.json")
	must(t, os.WriteFile(claim, []byte(`{"tool":"claude","host":"h","token":"eeeeeeeeeeeeeeee","claimed":"2026-09-01T00:00:00Z","last_contact":"2026-09-01T00:00:00Z","tmux_pane":"%1","tmux_socket":"`+filepath.Join(p, "gone.sock")+`","tmux_server":"1"}`), 0o644))
	sh(t, p, nil, "spawn", "builder.alice")
	if _, err := os.Stat(claim); err != nil {
		t.Error("the cap check removed a claim; only release --stale may")
	}

	env := isolatedEnv(t)
	s := privateTmux(t, p, env)
	must(t, os.Remove(claim))
	writeFile(t, p, "sunstack/TEAM", "id: 00000000000000ab\nname: capped\nmax_sessions: 1\n")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i, id := range []string{"builder.alice", "builder.bob"} {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			codes[i] = sh(t, p, s.env, "spawn", id, "--tool", "claude").code
		}(i, id)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 0 {
			ok++
		}
	}
	if ok != 1 {
		t.Errorf("max_sessions 1 with two spawns at once: %d started (exit codes %v)", ok, codes)
	}
}

// An unreadable BOARD.md does not quietly end a halt.
func TestHaltSurvivesUnreadableBoard(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs Unix permissions and a non-root user")
	}
	p, env := team086(t)
	expect(t, sh(t, p, env, "halt", "stop"), 0, "halt")
	board := filepath.Join(p, "sunstack", "BOARD.md")
	must(t, os.Chmod(board, 0))
	defer os.Chmod(board, 0o644)
	r := sh(t, p, env, "spawn", "builder.bob")
	expect(t, r, 1, "spawn with an unreadable board")
	requireContains(t, r.stderr, "halted")
}

// The ledger: only the recipient's reply closes a task, a verified: line
// must say something, replies are judged in time order, and rounds count.
func TestLedgerReplies(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	tb := field(tokenRe, sh(t, p, env, "as", "builder.bob").out)
	tr := field(tokenRe, sh(t, p, env, "as", "reviewer.rita").out)
	brief := writeFile(t, t.TempDir(), "b.md", fullBrief)
	t1 := field(msgRe, sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief, "--from", "builder.alice", "--token", ta).out)

	// Someone else's reply does not close it.
	expect(t, sh(t, p, env, "send", "reviewer.rita", "done", "--type", "done", "--reply-to", t1, "--from", "reviewer.rita", "--token", tr), 0, "unrelated reply")
	requireContains(t, sh(t, p, env, "tasks").out, t1)

	// "not verified:" is a failed round.
	r := sh(t, p, env, "send", "builder.alice", "not verified: could not run anything", "--type", "done", "--reply-to", t1, "--from", "builder.bob", "--token", tb)
	f1 := field(msgRe, r.out)
	expect(t, sh(t, p, env, "ack", "builder.alice", f1, "--token", ta), 0, "archive the first reply")
	requireContains(t, sh(t, p, env, "tasks", "--all").out, "done without verified:")

	// Round two fails, then is fixed: the latest reply counts, so one failed round.
	t2 := field(msgRe, sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief, "--follows", t1, "--from", "builder.alice", "--token", ta).out)
	f2 := field(msgRe, sh(t, p, env, "send", "builder.alice", "still broken", "--type", "done", "--reply-to", t2, "--from", "builder.bob", "--token", tb).out)
	expect(t, sh(t, p, env, "ack", "builder.alice", f2, "--token", ta), 0, "archive the second")
	expect(t, sh(t, p, env, "send", "builder.alice", "Fixed.\nverified: go test ./... @ abc123", "--type", "done", "--reply-to", t2, "--from", "builder.bob", "--token", tb), 0, "verified reply")
	if r := sh(t, p, env, "board"); strings.Contains(r.out, "failed twice") {
		t.Errorf("the latest reply is verified, so the chain did not fail twice:\n%s", r.out)
	}
}

// spawn --brief: the brief is in the inbox before the CLI starts, and an
// agent's spawn keeps its reply route.
func TestSpawnBriefBeforeLaunch(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	sid := "00000000-0000-4000-8000-000000000811"
	caller := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	expect(t, sh(t, p, caller, "as", "builder.alice"), 0, "caller holds alice")
	expect(t, sh(t, p, caller, "spawn", "reviewer", "--task", "rev", "--tool", "claude", "--brief", writeFile(t, t.TempDir(), "b.md", fullBrief)), 0, "spawn")
	files, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", "reviewer", "*.md"))
	if len(files) != 1 {
		t.Fatalf("brief: %v", files)
	}
	b, _ := os.ReadFile(files[0])
	requireContains(t, string(b), "from_session: "+sid, "via: ", "builder.alice")
	brief, _ := os.Stat(files[0])
	launches, _ := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "launch-*.json"))
	if len(launches) == 0 {
		t.Fatal("no launch recorded")
	}
	launch, _ := os.Stat(launches[0])
	if brief.ModTime().After(launch.ModTime()) {
		t.Errorf("the brief was written after the CLI started (%v > %v)", brief.ModTime(), launch.ModTime())
	}
}

var _ = regexp.MustCompile

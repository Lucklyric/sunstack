package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, strings.ReplaceAll(msg, " ", "-")+".txt"), []byte(msg), 0o644))
	for _, args := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", msg}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
}

// checkpoint works without a team: git state, a wrap note it stores and
// reads back, and the SessionStart hook pointing at it (§52).
func TestCheckpointAndWrapNote(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	gitCommit(t, dir, "first change")
	home := "SUNSTACK_HOME=" + filepath.Join(t.TempDir(), "home")
	const sid = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"
	me := []string{home, "SUNSTACK_NO_UPDATE_CHECK=1", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=" + sid}

	r := sh(t, dir, me, "checkpoint")
	expect(t, r, 0, "checkpoint before any note")
	requireContains(t, r.out, "== wrap note", "none for this folder", "last commits:", "first change", "uncommitted: none")

	body := filepath.Join(t.TempDir(), "note.md")
	must(t, os.WriteFile(body, []byte("Read first: plan.md\nOpen: finish the parser, next step tests\n"), 0o644))
	r = sh(t, dir, me, "checkpoint", "--save", body)
	expect(t, r, 0, "save the note")
	requireContains(t, r.out, "saved the wrap note", filepath.Join("home", "wrap"))

	gitCommit(t, dir, "second change")
	must(t, os.WriteFile(filepath.Join(dir, "draft.txt"), []byte("wip"), 0o644))
	r = sh(t, dir, me, "checkpoint")
	expect(t, r, 0, "checkpoint after the note")
	requireContains(t, r.out, "by this session", "finish the parser", "commit .: ", "commits since the note", "second change", "uncommitted (1)", "draft.txt")
	if since := r.out[strings.Index(r.out, "commits since the note"):]; strings.Contains(since, "first change") {
		t.Errorf("a commit from before the note is listed as since:\n%s", r.out)
	}

	other := []string{home, "SUNSTACK_NO_UPDATE_CHECK=1", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=1a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"}
	requireContains(t, sh(t, dir, other, "checkpoint").out, "by another session", "finish the parser")

	// Saving needs the session's ID; reading never fails.
	r = sh(t, dir, []string{home, "SUNSTACK_NO_UPDATE_CHECK=1"}, "checkpoint", "--save", body)
	expect(t, r, 2, "save without a session")
	requireContains(t, r.stderr+r.out, "no_session")
	plain := t.TempDir()
	r = sh(t, plain, me, "checkpoint")
	expect(t, r, 0, "checkpoint outside git")
	requireContains(t, r.out, "not a git repository", "none for this folder")

	// After a compaction the hook points at the note, with no team here.
	hook := exec.Command(bin, "hook")
	hook.Env = append(cleanEnv(), home)
	hook.Stdin = strings.NewReader(`{"session_id":"` + sid + `","cwd":"` + filepath.ToSlash(dir) + `","hook_event_name":"SessionStart","source":"compact"}`)
	out, _ := hook.Output()
	requireContains(t, string(out), `"hookEventName":"SessionStart"`, "a wrap note for this folder", "sunstack checkpoint")
	hook = exec.Command(bin, "hook")
	hook.Env = append(cleanEnv(), home)
	hook.Stdin = strings.NewReader(`{"session_id":"` + sid + `","cwd":"` + filepath.ToSlash(plain) + `","hook_event_name":"SessionStart","source":"compact"}`)
	if out, _ := hook.Output(); strings.Contains(string(out), "wrap note") {
		t.Errorf("hook names a note for a folder without one: %s", out)
	}
}

// In a team, checkpoint shows the board's Now and Done entries, and the
// SessionStart hook names the wrap note for a session that holds no claim.
func TestCheckpointInTeam(t *testing.T) {
	p := t.TempDir()
	home := "SUNSTACK_HOME=" + filepath.Join(t.TempDir(), "home")
	env := []string{home, "SUNSTACK_NO_UPDATE_CHECK=1"}
	expect(t, sh(t, p, env, "init"), 0, "init")
	expect(t, sh(t, p, env, "hire", "builder", "alice"), 0, "hire")
	board := filepath.Join(p, "sunstack", "builder.alice", "board.md")
	if _, err := os.Stat(board); err != nil {
		t.Fatalf("no agent board at %s: %v", board, err)
	}
	must(t, os.WriteFile(board, []byte("## Now\n- 2026-11-20 KR2 [O1] Parser in progress\n## Next\n## Done\n- 2026-11-19 KR1 [O1] Scaffold (verified: go test ./... @ abc1234)\n"), 0o644))

	const sid = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"
	me := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	r := sh(t, p, me, "checkpoint")
	expect(t, r, 0, "checkpoint in a team")
	requireContains(t, r.out, "in progress", "builder.alice KR2 Parser in progress", "done, latest", "KR1 Scaffold", "go test ./... @ abc1234")
	expect(t, sh(t, p, me, "checkpoint", "--root", t.TempDir()), 1, "a --root with no team")

	body := filepath.Join(t.TempDir(), "note.md")
	must(t, os.WriteFile(body, []byte("Open: the parser\n"), 0o644))
	expect(t, sh(t, p, me, "checkpoint", "--save", body), 0, "save in a team")
	hook := exec.Command(bin, "hook")
	hook.Env = append(cleanEnv(), home)
	hook.Stdin = strings.NewReader(`{"session_id":"` + sid + `","cwd":"` + filepath.ToSlash(p) + `","hook_event_name":"SessionStart","source":"clear"}`)
	out, _ := hook.Output()
	requireContains(t, string(out), `"hookEventName":"SessionStart"`, "a wrap note for this folder")
}

package main

// v0.8.5: task briefs, proof of done, pillars after compaction, asks,
// the resume block, and the session cap.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fullBrief = `Goal: the export runs on an empty board
Scope: internal/export only, not the CLI flags
Done when:
  - an empty board exports an empty list
  - go test ./... passes
Verify: go test ./internal/export/...
Report: what changed, the verified line, follow-ups
Timebox: one hour
`

func writeFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	must(t, os.WriteFile(path, []byte(text), 0o644))
	return path
}

func TestTaskBriefs(t *testing.T) {
	p := fixture(t)
	tr := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	tb := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	dir := t.TempDir()

	// A complete brief, from a file, is delivered as a task.
	r := sh(t, p, nil, "send", "builder.alice", "--type", "task", "--file", writeFile(t, dir, "ok.md", fullBrief), "--from", "reviewer", "--token", tr)
	expect(t, r, 0, "send a full brief")
	r = sh(t, p, nil, "check", "builder.alice", "--token", tb)
	requireContains(t, r.out, "type: task", "Goal: the export runs", "  - go test ./... passes")

	// Each missing required label is named.
	for _, label := range []string{"Goal", "Scope", "Done when", "Verify", "Report"} {
		var keep []string
		skip := false
		for _, l := range strings.Split(fullBrief, "\n") {
			if strings.HasPrefix(l, label+":") {
				skip = true
				continue
			}
			if skip && strings.HasPrefix(l, "  ") {
				continue
			}
			skip = false
			keep = append(keep, l)
		}
		r := sh(t, p, nil, "send", "builder.alice", strings.Join(keep, "\n"), "--type", "task")
		expect(t, r, 2, "brief without "+label)
		if !strings.Contains(r.stderr, "missing_brief") || !strings.Contains(r.stderr, "missing: "+label) {
			t.Errorf("brief without %s: %s", label, r.stderr)
		}
	}
	// A label with nothing after it is missing too.
	r = sh(t, p, nil, "send", "builder.alice", strings.Replace(fullBrief, "Verify: go test ./internal/export/...", "Verify:", 1), "--type", "task")
	expect(t, r, 2, "empty Verify")
	requireContains(t, r.stderr, "missing_brief", "missing: Verify")

	// Other types stay free text; --file works for them and excludes the text.
	expect(t, sh(t, p, nil, "send", "builder.alice", "--type", "fyi", "--file", writeFile(t, dir, "fyi.md", "just so you know\n")), 0, "fyi from a file")
	expect(t, sh(t, p, nil, "send", "builder.alice", "text", "--file", writeFile(t, dir, "x.md", "x")), 2, "text and --file together")
	expect(t, sh(t, p, nil, "send", "builder.alice", "--file", filepath.Join(dir, "nope.md")), 1, "missing file")

	// spawn --brief checks the brief before anything else, even without tmux.
	r = sh(t, p, nil, "spawn", "reviewer", "--brief", writeFile(t, dir, "bad.md", "Goal: something\n"))
	expect(t, r, 2, "spawn with a bad brief")
	requireContains(t, r.stderr, "missing_brief", "missing: Scope")
	expect(t, sh(t, p, nil, "spawn", "reviewer", "--brief", filepath.Join(dir, "nope.md")), 1, "spawn with a missing brief file")
}

func TestVerifiedDone(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, home, "init"), 0, "init")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire")
	tok := field(tokenRe, sh(t, p, home, "as", "builder.alice").out)
	r := sh(t, p, home, "snapshot", "builder.alice", "board.md", "--token", tok)
	cand := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok, "board.md")
	day := "2026-10-05"
	doc := "## Now\n## Next\n## Done\n" +
		"- " + day + " KR1 [O1] Export fixed (verified: go test ./... @ f51abe4)\n" +
		"- " + day + " KR2 [O1] Picked the format (verified: none, a decision)\n" +
		"- " + day + " KR3 [O1] Claimed but unchecked\n"
	must(t, os.WriteFile(cand, []byte(doc), 0o644))
	expect(t, sh(t, p, home, "commit", "builder.alice", "board.md", cand, field(sumRe, r.out), "--token", tok), 0, "commit board")

	r = sh(t, p, home, "board")
	requireContains(t, r.out, "builder.alice#KR3 is done without a verified: line")
	for _, kr := range []string{"KR1 is done without", "KR2 is done without"} {
		if strings.Contains(r.out, kr) {
			t.Errorf("verified entry flagged (%s):\n%s", kr, r.out)
		}
	}
	r = sh(t, p, home, "org", "--by", "agent")
	requireContains(t, r.out, "verified: go test ./... @ f51abe4")
}

func TestPillarsAfterCompaction(t *testing.T) {
	p := fixture(t)
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder.alice", "pillars.md"), []byte("- 2026-10-01 never push to main without a review\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "BOARD.md"), []byte("## Objectives\n## User\n## Directives\n- 2026-10-04 D1 Freeze the API this week (to: builder)\n"), 0o644))
	sid := "00000000-0000-4000-8000-000000000085"
	env := []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=" + sid}
	expect(t, sh(t, p, env, "as", "builder.alice"), 0, "claim alice")
	expect(t, sh(t, p, env, "as", "reviewer"), 0, "claim reviewer in the same session")

	ctx := hookContextText(t, runHook(t, nil, "SessionStart", "compact", sid, p), "SessionStart")
	requireContains(t, ctx,
		"pillars of builder.alice", "[team] 2026-09-22 every migration must be reversible", "[builder.alice] 2026-10-01 never push to main",
		"pillars of reviewer", "not aligned: D1 2026-10-04 Freeze the API this week",
		"reload the identity files")
	if strings.Count(ctx, "[builder.alice]") != 1 {
		t.Errorf("alice's own pillar should appear once: %s", ctx)
	}

	// Long pillars are cut at a line boundary, with a pointer to the rest.
	var long strings.Builder
	for i := 0; long.Len() < 6000; i++ {
		long.WriteString("- 2026-10-01 rule number " + strings.Repeat("x", 60) + "\n")
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "reviewer", "pillars.md"), []byte(long.String()), 0o644))
	ctx = hookContextText(t, runHook(t, nil, "SessionStart", "compact", sid, p), "SessionStart")
	requireContains(t, ctx, "run sunstack pillar reviewer for the rest")
	if strings.Count(ctx, "rule number") > 70 {
		t.Errorf("reviewer pillars not cut: %d rules", strings.Count(ctx, "rule number"))
	}
}

func TestSpawnWithBrief(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	brief := writeFile(t, t.TempDir(), "brief.md", fullBrief)
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "export", "--tool", "claude", "--brief", brief)
	expect(t, r, 0, "spawn with a brief")
	files, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", "reviewer", "*.md"))
	if len(files) != 1 {
		t.Fatalf("want one message for the spawned session, got %v", files)
	}
	b, _ := os.ReadFile(files[0])
	requireContains(t, string(b), "type: task", "session: reviewer_export", "Goal: the export runs")
}

// commitBoard writes an agent's board through snapshot and commit.
func commitBoard(t *testing.T, p string, env []string, id, tok, doc string) {
	t.Helper()
	r := sh(t, p, env, "snapshot", id, "board.md", "--token", tok)
	expect(t, r, 0, "snapshot "+id)
	cand := filepath.Join(p, "sunstack", "_local", "tmp", id, tok, "board.md")
	must(t, os.WriteFile(cand, []byte(doc), 0o644))
	expect(t, sh(t, p, env, "commit", id, "board.md", cand, field(sumRe, r.out), "--token", tok), 0, "commit "+id)
}

func TestAsks(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, home, "init"), 0, "init")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire")
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "BOARD.md"), []byte("## Objectives\n- 2026-10-01 O1 Ship the export\n## User\n## Directives\n"), 0o644))
	tok := field(tokenRe, sh(t, p, home, "as", "builder.alice").out)
	commitBoard(t, p, home, "builder.alice", tok, "## Now\n"+
		"- 2026-10-05 KR1 [O1] Export to CSV (needs: user#Q1)\n"+
		"## Next\n## Asks\n"+
		"- 2026-10-05 Q1 CSV or Parquet for the export? (options: csv | parquet) (default: csv after 2999-01-01)\n"+
		"- 2026-10-01 Q2 Keep the old flag? (default: yes after 2026-10-02)\n"+
		"- 2026-10-05 Q3 Which region?\n"+
		"## Done\n"+
		"- 2026-10-04 Q4 Use UTC? (answered: default)\n")

	r := sh(t, p, home, "board")
	requireContains(t, r.out,
		"builder.alice#Q1 asks the user: CSV or Parquet for the export? (options: csv | parquet) (default: csv after 2999-01-01)",
		"builder.alice#KR1 is waiting on the user (Q1)",
		"builder.alice#Q2 passed its default date 2026-10-02",
		"builder.alice#Q3 has no default",
		"builder.alice#Q4 was decided by default")
	for _, bad := range []string{"Q1 serves no objective", "user#Q1, which does not exist", "Q4 is done without a verified"} {
		if strings.Contains(r.out, bad) {
			t.Errorf("board should not say %q:\n%s", bad, r.out)
		}
	}
	r = sh(t, p, home, "org", "--attention")
	requireContains(t, r.out, "builder.alice#Q1 asks the user", "builder.alice#Q4 was decided by default")

	// answer sends the agent a message; the agent edits its own board.
	r = sh(t, p, home, "answer", "builder.alice", "Q1", "parquet")
	expect(t, r, 0, "answer")
	r = sh(t, p, home, "check", "builder.alice", "--token", tok)
	requireContains(t, r.out, "type: answer", "Q1: parquet")
	expect(t, sh(t, p, home, "answer", "builder.alice", "Q9", "x"), 1, "no such open ask")
	expect(t, sh(t, p, home, "answer", "builder.alice", "Q4", "x"), 1, "an answered ask is closed")
	expect(t, sh(t, p, home, "answer", "builder.alice", "KR1", "x"), 2, "not an ask key")
	expect(t, sh(t, p, home, "answer", "builder.alice", "Q1"), 2, "no answer text")
}

func TestResumeBlock(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	p := fixture(t)
	var when []string // environment for the next git command
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", p, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Env = append(os.Environ(), when...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	two := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	when = []string{"GIT_AUTHOR_DATE=" + two, "GIT_COMMITTER_DATE=" + two}
	git("commit", "-q", "-m", "chore: start")
	when = nil

	// A new claim of an agent that never saved has no block.
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	commitBoard(t, p, nil, "builder.alice", tok, "## Now\n- 2026-10-05 KR1 [O1] Export\n## Next\n## Done\n")
	hour := time.Now().Add(-time.Hour)
	must(t, os.Chtimes(filepath.Join(p, "sunstack", "builder.alice", "board.md"), hour, hour))

	// Since then: a commit, another agent's need on alice, a message.
	writeFile(t, p, "export.go", "package export\n")
	git("add", "export.go")
	git("commit", "-q", "-m", "feat: export skeleton")
	tb := field(tokenRe, sh(t, p, nil, "as", "builder.bob").out)
	commitBoard(t, p, nil, "builder.bob", tb, "## Now\n- "+time.Now().Format("2006-01-02")+" KR1 [O1] Wire the API (needs: builder.alice#KR1)\n## Next\n## Done\n")
	expect(t, sh(t, p, nil, "send", "builder.alice", "the API is ready"), 0, "message")

	r := sh(t, p, nil, "as", "builder.alice", "--token", tok)
	expect(t, r, 0, "resume")
	requireContains(t, r.out, "Since your last save", "feat: export skeleton", "builder.bob#KR1 Wire the API (needs builder.alice#KR1)", "1 new message(s)")
	if strings.Index(r.out, "Since your last save") > strings.Index(r.out, "===== session state") {
		t.Errorf("the resume block should come before the bundle:\n%s", r.out)
	}
	if strings.Contains(r.out, "chore: start") {
		t.Errorf("commits before the last save are not news:\n%s", r.out)
	}
	if r := sh(t, p, nil, "as", "reviewer"); strings.Contains(r.out, "Since your last save") {
		t.Errorf("an agent with no board has no resume block:\n%s", r.out)
	}
}

func TestSessionCap(t *testing.T) {
	p := fixture(t)
	writeFile(t, p, "sunstack/TEAM", "id: 00000000000000aa\nname: capped\nmax_sessions: 1\n")
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--task", "api").out)
	expect(t, sh(t, p, nil, "doing", "builder.alice", "writing the export", "--token", tok), 0, "doing")
	r := sh(t, p, nil, "spawn", "reviewer")
	expect(t, r, 4, "spawn at the cap")
	requireContains(t, r.stderr, "team_full", "max_sessions: 1", "builder.alice_api", "writing the export")
	r = sh(t, p, nil, "spawn", "reviewer", "--over-cap")
	expect(t, r, 1, "--over-cap passes the cap and stops at tmux")
	requireContains(t, r.stderr, "no_tmux")
	// A session the user opened by hand is never refused.
	expect(t, sh(t, p, nil, "as", "builder.bob"), 0, "as at the cap")
	// No max_sessions means the default of 6.
	writeFile(t, p, "sunstack/TEAM", "id: 00000000000000aa\nname: capped\n")
	requireContains(t, sh(t, p, nil, "spawn", "reviewer").stderr, "no_tmux")
}

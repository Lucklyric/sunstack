package main

// v0.8.6: next, the delegation ledger, halt, follow-up tasks and the default
// tool per agent.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// team086 is a project with two builders and a reviewer, each with a board.
func team086(t *testing.T) (p string, env []string) {
	t.Helper()
	p = t.TempDir()
	env = []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, env, "init"), 0, "init")
	for _, h := range [][2]string{{"builder", "alice"}, {"builder", "bob"}, {"reviewer", "rita"}} {
		expect(t, sh(t, p, env, "hire", h[0], h[1]), 0, "hire "+h[1])
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "BOARD.md"), []byte("## Objectives\n- 2026-10-01 O1 Ship the export\n## User\n## Directives\n"), 0o644))
	return p, env
}

var nextLineRe = regexp.MustCompile(`(?m)^\d+\. \[(\w+)\] (.*)$`)

func TestNext(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	commitBoard(t, p, env, "builder.alice", ta, "## Now\n"+
		"- 2026-10-05 KR1 [O1] Export to CSV (needs: builder.bob#KR1)\n"+
		"## Next\n- 2026-10-05 KR2 [O1] Export to Parquet\n"+
		"## Asks\n- 2026-10-05 Q1 CSV or Parquet first? (options: csv | parquet) (default: csv after 2999-01-01)\n"+
		"## Done\n- 2026-10-04 KR0 [O1] Scaffold\n")
	sid := "00000000-0000-4000-8000-000000000086"
	bobEnv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	tb := field(tokenRe, sh(t, p, bobEnv, "as", "builder.bob").out)
	commitBoard(t, p, bobEnv, "builder.bob", tb, "## Now\n- 2026-10-05 KR1 [O1] Data model\n## Next\n## Done\n"+
		"- 2026-10-04 KR7 [O1] Old work A\n- 2026-10-04 KR8 [O1] Old work B\n- 2026-10-04 KR9 [O1] Old work C\n")

	// Ranked: waiting on the user, then blocked work, then drift.
	r := sh(t, p, env, "next", "--all")
	expect(t, r, 0, "next --all")
	var tiers []string
	for _, m := range nextLineRe.FindAllStringSubmatch(r.out, -1) {
		tiers = append(tiers, m[1])
	}
	order := map[string]int{"broken": 0, "user": 1, "blocked": 2, "drift": 3, "yours": 4}
	for i := 1; i < len(tiers); i++ {
		if order[tiers[i]] < order[tiers[i-1]] {
			t.Errorf("not ranked: %v\n%s", tiers, r.out)
		}
	}
	requireContains(t, r.out,
		"[user] builder.alice#Q1 asks the user: CSV or Parquet first?",
		`sunstack answer builder.alice Q1 "<answer>"`,
		"[blocked] builder.alice#KR1 is waiting on builder.bob#KR1",
		"[drift] builder.alice#KR0 is done without a verified: line")
	if len(tiers) < 6 {
		t.Fatalf("want at least 6 items for the cut: %s", r.out)
	}
	if r := sh(t, p, env, "next"); len(nextLineRe.FindAllString(r.out, -1)) != 5 || !strings.Contains(r.out, "more: sunstack next --all") {
		t.Errorf("next shows the top five:\n%s", r.out)
	}

	// A session that holds an agent sees its own items first in each tier,
	// and its own next step last.
	r = sh(t, p, bobEnv, "next", "--all")
	first := ""
	for _, m := range nextLineRe.FindAllStringSubmatch(r.out, -1) {
		if m[1] == "drift" {
			first = m[2]
			break
		}
	}
	if !strings.HasPrefix(first, "builder.bob#") {
		t.Errorf("bob's drift should come first, got %q:\n%s", first, r.out)
	}
	requireContains(t, r.out, "[yours] builder.bob: KR1 Data model")

	var items []struct{ Tier, Owner, Text, Do string }
	r = sh(t, p, env, "next", "--json")
	must(t, json.Unmarshal([]byte(r.out), &items))
	if len(items) != len(tiers) || items[0].Tier != "user" || items[0].Do == "" {
		t.Errorf("json: %+v", items)
	}
}

func TestTaskLedger(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	tb := field(tokenRe, sh(t, p, env, "as", "builder.bob").out)
	dir := t.TempDir()
	brief := writeFile(t, dir, "b.md", fullBrief)

	r := sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief, "--from", "builder.alice", "--token", ta)
	expect(t, r, 0, "task 1")
	t1 := field(msgRe, r.out)
	r = sh(t, p, env, "tasks")
	requireContains(t, r.out, t1, "builder.alice -> builder.bob", "Goal: the export runs on an empty board", "pending")

	// A reply without a verified: line closes the round but counts as failed.
	expect(t, sh(t, p, env, "send", "builder.alice", "Blocked on the schema.", "--type", "done", "--reply-to", t1, "--from", "builder.bob", "--token", tb), 0, "reply 1")
	if r := sh(t, p, env, "tasks"); strings.Contains(r.out, t1) {
		t.Errorf("a replied task is closed:\n%s", r.out)
	}
	requireContains(t, sh(t, p, env, "tasks", "--all").out, t1, "done without verified")

	// The follow-up quotes the earlier brief and reply.
	r = sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief, "--follows", t1, "--from", "builder.alice", "--token", ta)
	expect(t, r, 0, "task 2")
	t2 := field(msgRe, r.out)
	r = sh(t, p, env, "check", "builder.bob", "--token", tb)
	requireContains(t, r.out, "follows: "+t1, "Context: earlier round "+t1, "  > Goal: the export runs", "  > Blocked on the schema.")
	expect(t, sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief, "--follows", "20260101T000000Z-user-abcdef"), 1, "follows an unknown message")
	expect(t, sh(t, p, env, "send", "builder.bob", "x", "--type", "fyi", "--follows", t1), 2, "follows is for tasks")

	// Two failed rounds in one chain: send no third, make it an ask.
	expect(t, sh(t, p, env, "send", "builder.alice", "Still blocked.", "--type", "done", "--reply-to", t2, "--from", "builder.bob", "--token", tb), 0, "reply 2")
	requireContains(t, sh(t, p, env, "board").out, "failed twice")
	requireContains(t, sh(t, p, env, "tasks", "--all").out, t2, "follows "+t1)

	// A verified reply is a success; an old open task is flagged.
	r = sh(t, p, env, "send", "reviewer.rita", "--type", "task", "--file", brief, "--from", "builder.alice", "--token", ta)
	expect(t, r, 0, "task 3")
	t3 := field(msgRe, r.out)
	path := filepath.Join(p, "sunstack", "_local", "inbox", "reviewer.rita", t3+".md")
	b, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, regexp.MustCompile(`(?m)^at: .*$`).ReplaceAll(b, []byte("at: 2026-01-01T00:00:00Z")), 0o644))
	requireContains(t, sh(t, p, env, "board").out, "task "+t3+" from builder.alice to reviewer.rita has been open since 2026-01-01")
	requireContains(t, sh(t, p, env, "tasks", "--from", "builder.alice").out, t3)
	if r := sh(t, p, env, "tasks", "--from", "builder.bob"); strings.Contains(r.out, t3) {
		t.Errorf("--from filters by sender:\n%s", r.out)
	}

	// A task from the user stays open until it is acked.
	r = sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", brief)
	t4 := field(msgRe, r.out)
	requireContains(t, sh(t, p, env, "tasks").out, t4, "user -> builder.bob")
	expect(t, sh(t, p, env, "ack", "builder.bob", t4, "--token", tb), 0, "ack the user's task")
	if r := sh(t, p, env, "tasks"); strings.Contains(r.out, t4) {
		t.Errorf("an acked task from the user is closed:\n%s", r.out)
	}
}

func TestHalt(t *testing.T) {
	p, env := team086(t)
	sid := "00000000-0000-4000-8000-000000000087"
	cenv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	tok := field(tokenRe, sh(t, p, cenv, "as", "builder.alice").out)
	expect(t, sh(t, p, env, "halt"), 2, "halt needs a reason")
	expect(t, sh(t, p, env, "halt", "the schema is wrong, stop until fixed"), 0, "halt")
	b, _ := os.ReadFile(filepath.Join(p, "sunstack", "BOARD.md"))
	requireContains(t, string(b), "halt: ", "the schema is wrong, stop until fixed", "O1 Ship the export")
	// Other writers of BOARD.md keep the halt line.
	expect(t, sh(t, p, env, "direct", "Keep the export small"), 0, "direct while halted")
	expect(t, sh(t, p, env, "tidy", "--team"), 0, "tidy --team while halted")
	expect(t, sh(t, p, env, "tidy", "--all"), 0, "tidy --all while halted")
	b, _ = os.ReadFile(filepath.Join(p, "sunstack", "BOARD.md"))
	requireContains(t, string(b), "halt: ", "the schema is wrong", "Keep the export small")

	r := sh(t, p, env, "spawn", "builder.bob")
	expect(t, r, 1, "spawn while halted")
	requireContains(t, r.stderr, "halted", "the schema is wrong")
	requireContains(t, sh(t, p, env, "as", "builder.bob").out, "TEAM HALTED")
	requireContains(t, sh(t, p, env, "check", "builder.alice", "--token", tok).out, "TEAM HALTED")
	requireContains(t, sh(t, p, env, "next").out, "[broken] the team is halted")
	if out := runHook(t, env, "UserPromptSubmit", "", sid, p); !strings.Contains(out, "TEAM HALTED") {
		t.Errorf("prompt hook: %s", out)
	}
	requireContains(t, hookContextText(t, runHook(t, env, "SessionStart", "compact", sid, p), "SessionStart"), "TEAM HALTED")

	expect(t, sh(t, p, env, "halt", "--off"), 0, "halt --off")
	b, _ = os.ReadFile(filepath.Join(p, "sunstack", "BOARD.md"))
	if strings.Contains(string(b), "halt:") || !strings.Contains(string(b), "O1 Ship the export") {
		t.Errorf("halt --off:\n%s", b)
	}
	// Past the halt check: refused only because the label is taken.
	requireContains(t, sh(t, p, env, "spawn", "builder.bob").stderr, "task_taken")
	expect(t, sh(t, p, env, "halt", "--off"), 0, "halt --off twice is fine")
}

func TestToolDefault(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	agent := filepath.Join(p, "sunstack", "reviewer", "AGENT.md")
	must(t, os.WriteFile(agent, []byte("---\ntitle: reviewer\nfrom: custom\ntool: codex\n---\n## Role\n"), 0o644))
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "rev")
	expect(t, r, 0, "spawn uses the agent's tool")
	requireContains(t, r.out, "(codex)")
	r = sh(t, p, s.env, "spawn", "builder.alice", "--task", "b", "--tool", "claude")
	expect(t, r, 0, "--tool wins")
	requireContains(t, r.out, "(claude)")
	must(t, os.WriteFile(agent, []byte("---\ntitle: reviewer\nfrom: custom\ntool: emacs\n---\n## Role\n"), 0o644))
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--task", "x"), 2, "an unknown tool in AGENT.md")
}

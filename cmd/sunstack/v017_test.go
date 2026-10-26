package main

// §33: a third pass over pstack.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A question is open in the ledger until its recipient replies done.
func TestQuestionLedger(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	tb := field(tokenRe, sh(t, p, env, "as", "builder.bob").out)
	asAlice := []string{"--from", "builder.alice", "--token", ta}

	r := sh(t, p, env, append([]string{"send", "builder.bob", "CSV or Parquet first?\nThe export needs one.", "--type", "question"}, asAlice...)...)
	expect(t, r, 0, "question")
	q := field(msgRe, r.out)
	r = sh(t, p, env, "tasks")
	requireContains(t, r.out, q, "question", "builder.alice -> builder.bob", "CSV or Parquet first?")
	if strings.Contains(sh(t, p, env, "tasks", "--type", "task").out, q) {
		t.Error("--type task lists a question")
	}
	requireContains(t, sh(t, p, env, "tasks", "--type", "question").out, q)
	expect(t, sh(t, p, env, "tasks", "--type", "fyi"), 2, "--type takes task or question")

	// An old open question is flagged under its sender.
	path := filepath.Join(p, "sunstack", "_local", "inbox", "builder.bob", q+".md")
	b, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, regexp.MustCompile(`(?m)^at: .*$`).ReplaceAll(b, []byte("at: 2026-01-01T00:00:00Z")), 0o644))
	requireContains(t, sh(t, p, env, "next", "--all").out, "question "+q+" from builder.alice to builder.bob has been open since 2026-01-01")

	// A done reply from its recipient closes it.
	expect(t, sh(t, p, env, "send", "builder.alice", "CSV.", "--type", "done", "--reply-to", q, "--from", "builder.bob", "--token", tb), 0, "answer")
	if strings.Contains(sh(t, p, env, "tasks").out, q) {
		t.Error("an answered question is still open")
	}
	if strings.Contains(sh(t, p, env, "next", "--all").out, "question "+q) {
		t.Error("an answered question is still flagged")
	}
	// --replaces is still for tasks only.
	expect(t, sh(t, p, env, append([]string{"send", "builder.bob", "--type", "task", "--file", writeFile(t, t.TempDir(), "b.md", fullBrief), "--replaces", q}, asAlice...)...), 2, "replaces a question")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// A verified reply names its commit; tasks counts the project's commits
// since then, and not the parent repo's commits elsewhere.
func TestVerifiedCommitsSince(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "proj")
	must(t, os.MkdirAll(p, 0o755))
	env := []string{"SUNSTACK_HOME=" + filepath.Join(root, ".home")}
	expect(t, sh(t, p, env, "init"), 0, "init")
	for _, n := range []string{"alice", "bob"} {
		expect(t, sh(t, p, env, "hire", "builder", n), 0, "hire "+n)
	}
	git(t, root, "init", "-q")
	writeFile(t, p, "a.txt", "1")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "one")
	sha := git(t, root, "rev-parse", "--short", "HEAD")

	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	tb := field(tokenRe, sh(t, p, env, "as", "builder.bob").out)
	r := sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", writeFile(t, t.TempDir(), "b.md", fullBrief), "--from", "builder.alice", "--token", ta)
	t1 := field(msgRe, r.out)
	expect(t, sh(t, p, env, "send", "builder.alice", "Done.\nverified: go test ./... @ "+sha, "--type", "done", "--reply-to", t1, "--from", "builder.bob", "--token", tb), 0, "reply")

	for i, f := range []string{"proj/b.txt", "proj/c.txt", "outside.txt"} {
		writeFile(t, root, f, "x")
		git(t, root, "add", "-A")
		git(t, root, "commit", "-qm", "c"+string(rune('0'+i)))
	}
	requireContains(t, sh(t, p, env, "tasks", "--all").out, "verified @ "+sha+", 2 commits since")

	// A reply without @ shows no count.
	r = sh(t, p, env, "send", "builder.bob", "--type", "task", "--file", writeFile(t, t.TempDir(), "b.md", fullBrief), "--from", "builder.alice", "--token", ta)
	t2 := field(msgRe, r.out)
	expect(t, sh(t, p, env, "send", "builder.alice", "Done.\nverified: read the diff", "--type", "done", "--reply-to", t2, "--from", "builder.bob", "--token", tb), 0, "reply 2")
	out := sh(t, p, env, "tasks", "--all").out
	block := out[strings.Index(out, t2):]
	if strings.Contains(block, "commits since") {
		t.Errorf("a reply without @ shows a count:\n%s", block)
	}
}

// issues --triaged records the time; --since triaged starts there; next
// asks for a triage only when there are new entries and the last one is
// over 7 days old.
func TestIssuesTriage(t *testing.T) {
	p, env := team086(t)
	home := envValue(env, "SUNSTACK_HOME")
	expect(t, sh(t, p, env, "issue", "the save skill stopped at step 4"), 0, "note")
	requireContains(t, sh(t, p, env, "next", "--all").out, "not triaged")
	requireContains(t, sh(t, p, env, "issues", "--since", "triaged").out, "the save skill stopped")

	time.Sleep(1100 * time.Millisecond) // past the note's second, which still counts as new
	expect(t, sh(t, p, env, "issues", "--triaged"), 0, "mark triaged")
	if _, err := os.Stat(filepath.Join(home, "log", "triaged")); err != nil {
		t.Fatal("no triaged mark:", err)
	}
	requireContains(t, sh(t, p, env, "issues", "--since", "triaged").out, "No issues")
	expect(t, sh(t, p, env, "issue", "a new note"), 0, "new note")
	if strings.Contains(sh(t, p, env, "next", "--all").out, "not triaged") {
		t.Error("triage asked for within 7 days of the last one")
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	must(t, os.Chtimes(filepath.Join(home, "log", "triaged"), old, old))
	requireContains(t, sh(t, p, env, "next", "--all").out, "not triaged", "/sunstack:issues")
}

// Skill and template text for §33.
func TestPstackPassText(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		must(t, err)
		return strings.Join(strings.Fields(string(b)), " ")
	}
	for path, wants := range map[string][]string{
		"../../plugins/sunstack/skills/check/SKILL.md": {
			"out of time or context, a smaller scope",
			"a tool or model failure, the other CLI",
		},
		"../../plugins/sunstack/skills/verify/SKILL.md": {
			"from an empty data dir",
			"ready means usable, not that a process answers",
			"never weaken the journey",
			"the stand-in did that the target will not",
		},
		"../../internal/assets/library/reviewer/AGENT.md": {
			"Prove each blocker",
			"List every defect I can prove",
		},
		"../../plugins/sunstack/skills/issues/SKILL.md": {
			"sunstack issues --since triaged --json",
			"Never loosen the check",
			"sunstack issues --triaged",
		},
		"../../README.md": {
			"## Credits",
			"https://github.com/backnotprop/pstack",
		},
	} {
		s := read(path)
		for _, w := range wants {
			if !strings.Contains(s, w) {
				t.Errorf("%s lacks %q", path, w)
			}
		}
	}
}

// Review r33-r1: malformed tasks arguments are usage errors, not panics, and
// --type filters the remote-mail section too.
func TestTasksArgsAndSentFilter(t *testing.T) {
	p, env := team086(t)
	for _, args := range [][]string{{"tasks", "--type"}, {"tasks", "--root"}, {"tasks", "--bogus"}} {
		r := sh(t, p, env, args...)
		expect(t, r, 2, strings.Join(args, " "))
		if strings.Contains(r.stderr, "panic") {
			t.Errorf("%v panicked:\n%s", args, r.stderr)
		}
	}
	out := filepath.Join(envValue(env, "SUNSTACK_HOME"), "outbox")
	must(t, os.MkdirAll(out, 0o700))
	writeFile(t, out, "20260101T000000Z-user-aaaaaa.json", `{"mail":{"id":"20260101T000000Z-user-aaaaaa"},"letter":{"type":"task","to":"proj/builder"},"to_name":"remote","status":"queued"}`)
	if r := sh(t, p, env, "tasks", "--type", "question"); strings.Contains(r.out, "20260101T000000Z-user-aaaaaa") {
		t.Errorf("--type question lists a remote task:\n%s", r.out)
	}
	requireContains(t, sh(t, p, env, "tasks", "--type", "task").out, "20260101T000000Z-user-aaaaaa")
}

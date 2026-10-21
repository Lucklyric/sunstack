package main

// §26: every host keeps a bounded log of what went wrong.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func issueLog(t *testing.T, env []string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(envValue(env, "SUNSTACK_HOME"), "log", "issues.jsonl"))
	return string(b)
}

func TestIssueLogRecordsFailures(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)

	expect(t, sh(t, p, env, "team"), 0, "team")
	if log := issueLog(t, env); log != "" {
		t.Fatalf("a successful command was recorded:\n%s", log)
	}

	// A refusal: one entry with its code and exit, and none of the input.
	r := sh(t, p, env, "send", "laptop:demo/builder.alice", "SECRET-BODY-TEXT", "--op", "retry-1", "--token", "SECRET-TOKEN-VALUE")
	expect(t, r, 2, "remote send with --op")
	log := issueLog(t, env)
	for _, want := range []string{`"source":"cli"`, `"cmd":"send"`, `"code":"usage"`, `"exit":2`} {
		requireContains(t, log, want)
	}
	if strings.Count(log, "\n") != 1 || strings.Contains(log, "SECRET") {
		t.Fatalf("want one entry without the input:\n%s", log)
	}
	// Usage errors quote their arguments: only the code is kept.
	expect(t, sh(t, p, env, "send", "builder.alice", "body", "--token=SECRET-TOKEN-2"), 2, "unknown option")
	expect(t, sh(t, p, env, "issues", "SECRET-PROMPT"), 2, "surplus argument")
	if log := issueLog(t, env); strings.Count(log, "\n") != 3 || strings.Contains(log, "SECRET") {
		t.Fatalf("usage errors leaked their input:\n%s", log)
	}
	// No failure keeps its message: refusals quote tokens and arguments too.
	expect(t, sh(t, p, env, "as", "builder.alice", "--takeover", "--expect", "SECRET-CLAIM-TOKEN"), 4, "takeover of an unclaimed agent")
	expect(t, sh(t, p, env, "--token=SECRET-AS-COMMAND"), 2, "unknown command")
	log = issueLog(t, env)
	if strings.Count(log, "\n") != 5 || strings.Contains(log, "SECRET") {
		t.Fatalf("failures leaked their input:\n%s", log)
	}

	// A command aimed at another team with --root is recorded under that team.
	other := fixture(t)
	expect(t, sh(t, p, env, "tidy", "builder.missing", "--root", other), 1, "tidy of an unknown agent")
	if log := issueLog(t, env); !strings.Contains(log[strings.LastIndex(strings.TrimSpace(log), "\n")+1:], `"team":"`+filepath.Base(other)+`"`) {
		t.Fatalf("--root failure not under its team:\n%s", log)
	}

	// A note, then the grouped view.
	expect(t, sh(t, p, env, "issue", "the save skill stopped at step 4"), 0, "issue")
	r = sh(t, p, env, "issues")
	expect(t, r, 0, "issues")
	for _, want := range []string{"usage", "4 times", "send", "not_found", "note", "the save skill stopped at step 4"} {
		requireContains(t, r.out, want)
	}
	expect(t, sh(t, p, env, "issue", "a build broke", "--code", "Build.Failed"), 0, "issue with a code")
	requireContains(t, sh(t, p, env, "issues", "--code", "Build.Failed").out, "a build broke")
	r = sh(t, p, env, "issues", "--code", "note")
	if strings.Contains(r.out, "usage") {
		t.Fatalf("--code note shows other codes:\n%s", r.out)
	}
	expect(t, sh(t, p, env, "issue"), 2, "issue without text")
}

// A log that cannot be written never changes the command's result.
func TestIssueLogFailureKeepsExit(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	home := envValue(env, "SUNSTACK_HOME")
	must(t, os.MkdirAll(home, 0o755))
	must(t, os.WriteFile(filepath.Join(home, "log"), []byte("not a folder"), 0o644))
	r := sh(t, p, env, "send", "laptop:demo/builder.alice", "hi", "--op", "x")
	expect(t, r, 2, "remote send with --op")
	requireContains(t, r.stderr, "--op is for messages within this host")
}

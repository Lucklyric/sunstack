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
	for _, want := range []string{`"source":"cli"`, `"cmd":"send"`, `"code":"usage"`, `"exit":2`, `--op is for messages within this host`} {
		requireContains(t, log, want)
	}
	if strings.Count(log, "\n") != 1 || strings.Contains(log, "SECRET") {
		t.Fatalf("want one entry without the input:\n%s", log)
	}

	// A note, then the grouped view.
	expect(t, sh(t, p, env, "issue", "the save skill stopped at step 4"), 0, "issue")
	r = sh(t, p, env, "issues")
	expect(t, r, 0, "issues")
	for _, want := range []string{"usage", "1 time", "send", "note", "the save skill stopped at step 4"} {
		requireContains(t, r.out, want)
	}
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

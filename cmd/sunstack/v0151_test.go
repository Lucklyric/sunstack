package main

// §32: an fyi from an agent does not nudge, and a snapshot tidies first.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An fyi from an agent is delivered without a nudge; --nudge, an fyi from
// the user and the other types still nudge.
func TestFyiFromAgentNotNudged(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	tok := field(tokenRe, sh(t, p, s.env, "as", "reviewer", "--tool", "claude").out)
	ta := field(tokenRe, sh(t, p, s.env, "as", "builder.alice").out)
	asAlice := []string{"--from", "builder.alice", "--token", ta}

	r := sh(t, p, s.env, append([]string{"send", "reviewer", "progress note"}, asAlice...)...)
	expect(t, r, 0, "agent fyi")
	requireContains(t, r.out, "not nudged (an fyi waits for the next check)")
	r = sh(t, p, s.env, append([]string{"send", "reviewer", "typed note", "--type", "fyi"}, asAlice...)...)
	expect(t, r, 0, "typed agent fyi")
	requireContains(t, r.out, "not nudged (an fyi waits for the next check)")
	time.Sleep(300 * time.Millisecond)
	if got := len(submitted(t, s.env)); got != 0 {
		t.Fatalf("an agent fyi was typed: %d prompts", got)
	}

	nudged := 0
	want := func(args ...string) {
		t.Helper()
		r := sh(t, p, s.env, args...)
		expect(t, r, 0, strings.Join(args, " "))
		requireContains(t, r.out, "nudged session reviewer")
		nudged++
		eventually(t, "nudge submitted", func() bool { return len(submitted(t, s.env)) == nudged })
		expect(t, sh(t, p, s.env, "check", "reviewer", "--token", tok), 0, "check")
	}
	want(append([]string{"send", "reviewer", "look now", "--nudge"}, asAlice...)...)
	want("send", "reviewer", "from the user")
	want(append([]string{"send", "reviewer", "which one?", "--type", "question"}, asAlice...)...)
}

// The same rule holds for a free session's host inbox.
func TestHostFyiFromAgentNotNudged(t *testing.T) {
	p := fixture(t)
	sid := "00000000-0000-4000-8000-000000000002"
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	claudeList(t, s.env, privateSession(t, s, sid, p))
	ta := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)

	r := sh(t, p, s.env, "send", sid, "progress note", "--from", "builder.alice", "--token", ta)
	expect(t, r, 0, "agent fyi")
	requireContains(t, r.out, "not nudged (an fyi waits for the next check)")
	time.Sleep(300 * time.Millisecond)
	if got := len(submitted(t, s.env)); got != 0 {
		t.Fatalf("an agent fyi was typed: %d prompts", got)
	}
	r = sh(t, p, s.env, "send", sid, "look now", "--from", "builder.alice", "--token", ta, "--nudge")
	expect(t, r, 0, "agent fyi --nudge")
	requireContains(t, r.out, "nudged session claude "+s.pane)
	eventually(t, "host nudge", func() bool { return len(submitted(t, s.env)) == 1 })
}

func TestSendNudgeUsage(t *testing.T) {
	p := fixture(t)
	expect(t, sh(t, p, nil, "send", "reviewer", "x", "--nudge", "--no-nudge"), 2, "--nudge with --no-nudge")
	expect(t, sh(t, p, nil, "send", "otherhost:team/reviewer", "x", "--nudge"), 2, "--nudge across hosts")
}

// A snapshot of an agent's board or context tidies the agent first, and a
// commit against that snapshot's checksum succeeds.
func TestSnapshotTidiesFirst(t *testing.T) {
	p := fixture(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	old := time.Now().AddDate(0, 0, -20).Format("2006-01-02")
	var b strings.Builder
	b.WriteString("## Now\n## Next\n## Done\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "- %s KR%d [O1] Finished %d (verified: test)\n", old, i+1, i+1)
	}
	must(t, os.WriteFile(filepath.Join(dir, "board.md"), []byte(b.String()), 0o644))
	ctx := "## Processed messages\n"
	for i := 0; i < 12; i++ {
		ctx += fmt.Sprintf("- %s 20261001T0000%02dZ-pm.lead-dddd%02d from pm.lead: old %d\n", old, i, i, i)
	}
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte(ctx), 0o644))

	// A wrong token writes nothing.
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", "wrong"), 4, "wrong token")
	if c, _ := os.ReadFile(filepath.Join(dir, "context.md")); string(c) != ctx {
		t.Fatalf("a rejected snapshot tidied:\n%s", c)
	}

	r := sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", tok)
	expect(t, r, 0, "snapshot")
	if strings.Contains(r.out, "Finished 1 ") || strings.Contains(r.out, "KR12") {
		t.Errorf("snapshot shows untidied Done entries:\n%s", r.out)
	}
	c, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	if strings.Contains(string(c), "old 0") {
		t.Errorf("context not tidied:\n%s", c)
	}
	if !strings.Contains(eventsLog(t, p), "tidy") {
		t.Errorf("no tidy event:\n%s", eventsLog(t, p))
	}
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok)
	must(t, os.WriteFile(filepath.Join(tmp, "b.md"), []byte("## Now\n## Next\n## Done\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "board.md", filepath.Join(tmp, "b.md"), field(sumRe, r.out), "--token", tok), 0, "commit after tidy")

	// A second snapshot has nothing to tidy and logs nothing more.
	n := strings.Count(eventsLog(t, p), " tidy ")
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok), 0, "second snapshot")
	if got := strings.Count(eventsLog(t, p), " tidy "); got != n {
		t.Errorf("tidy logged again with nothing to do: %d -> %d", n, got)
	}

	// A snapshot that cannot tidy still prints.
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte("<<<<<<< HEAD\n- a\n=======\n- b\n>>>>>>> x\n"), 0o644))
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", tok), 0, "snapshot with an untidyable context")
}

// The check skill nudges the holder of a replaced task, and never acks an
// early-acked fyi a second time.
func TestCheckSkillFyiRules(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/check/SKILL.md")
	must(t, err)
	s := strings.Join(strings.Fields(string(b)), " ")
	for _, want := range []string{
		"send that session an `fyi` with `--reply-to <older id>` and `--nudge`",
		"not acked already",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("check skill lacks %q", want)
		}
	}
}

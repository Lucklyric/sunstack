package main

import (
	"reflect"
	"strings"
	"testing"
)

// Review of v0.18.4 (design §45): what callers type and what they are told.

// --name=value, a lone -- before text, text that starts with dashes, and a
// valued option whose value was left out.
func TestParseForms(t *testing.T) {
	const valued, switches = "type from", "no-nudge all"
	for _, c := range []struct {
		in    []string
		pos   []string
		flags map[string]string
		err   string
	}{
		{in: []string{"a.b", "--type=fyi", "hello"}, pos: []string{"a.b", "hello"}, flags: map[string]string{"type": "fyi"}},
		{in: []string{"a.b", "--type", "fyi", "--no-nudge", "hello"}, pos: []string{"a.b", "hello"}, flags: map[string]string{"type": "fyi", "no-nudge": "1"}},
		{in: []string{"a.b", "--type=a=b"}, pos: []string{"a.b"}, flags: map[string]string{"type": "a=b"}},
		{in: []string{"a.b", "--- separator first"}, pos: []string{"a.b", "--- separator first"}, flags: map[string]string{}},
		{in: []string{"a.b", "--type", "fyi", "--", "--all", "--bogus"}, pos: []string{"a.b", "--all", "--bogus"}, flags: map[string]string{"type": "fyi"}},
		{in: []string{"a.b", "--type", "--weird text"}, pos: []string{"a.b"}, flags: map[string]string{"type": "--weird text"}},
		{in: []string{"a.b", "--type", "--no-nudge"}, err: "--type needs a value"},
		{in: []string{"a.b", "--type"}, err: "--type needs a value"},
		{in: []string{"a.b", "--all=yes"}, err: "--all takes no value"},
		{in: []string{"a.b", "--bogus"}, err: "unknown option: --bogus"},
		{in: []string{"a.b", "--bogus=SECRET"}, err: "unknown option: --bogus"},
		{in: []string{"a.b", "--no_nudge"}, err: "unknown option: --no_nudge"},
	} {
		a, err := parse(c.in, valued, switches)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, want %q", c.in, err, c.err)
			}
			if err != nil && strings.Contains(err.Error(), "SECRET") {
				t.Errorf("%q: the error quotes the value: %v", c.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(a.pos, c.pos) || !reflect.DeepEqual(a.flags, c.flags) {
			t.Errorf("%q: pos %q flags %v, want %q %v", c.in, a.pos, a.flags, c.pos, c.flags)
		}
	}
}

// What a caller is told: the command's own usage from help <cmd>, the way
// to reach a team from another folder, a skill typed as a command, and the
// arguments a delete really needs. A bare `as`, which lists the agents to
// choose from, is not recorded as an issue.
func TestCallerGuidance(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)

	r := sh(t, p, env, "help", "send")
	expect(t, r, 0, "help send")
	requireContains(t, r.out, "sunstack send <id|title", "[--root DIR]")
	if strings.Contains(r.out, "sunstack board") {
		t.Errorf("help send printed the overview:\n%s", r.out)
	}
	r = sh(t, p, env, "sessions", "--help")
	requireContains(t, r.out, "sunstack team --sessions")
	r = sh(t, p, env, "next", "--help")
	requireContains(t, r.out, "[--team]")

	r = sh(t, p, env, "send", "builder.alice", "--type=fyi", "--", "--- a line that starts with dashes")
	expect(t, r, 0, "send with --type=fyi and dashed text")

	r = sh(t, p, env, "save")
	expect(t, r, 2, "a skill typed as a command")
	requireContains(t, r.stderr, "is a skill", "/sunstack:save")

	r = sh(t, p, env, "send", "builder.alice", "two", "words")
	expect(t, r, 2, "unquoted text")
	requireContains(t, r.stderr, "quote the text as one argument")

	r = sh(t, p, env, "commit", "builder.alice", "board.md", "--delete", "--token", "x")
	expect(t, r, 2, "delete with too few arguments")
	requireContains(t, r.stderr, "missing id target checksum")

	outside := t.TempDir()
	r = sh(t, outside, env, "team")
	expect(t, r, 1, "team outside a team")
	requireContains(t, r.stderr, "--root", "<team>/<id>")

	before := issueLog(t, env)
	r = sh(t, p, env, "as")
	expect(t, r, 2, "bare as")
	requireContains(t, r.out, "builder.alice")
	if after := issueLog(t, env); after != before {
		t.Errorf("a bare as was recorded as an issue:\n%s", strings.TrimPrefix(after, before))
	}
	r = sh(t, p, env, "take")
	expect(t, r, 2, "take with nothing")
	if after := issueLog(t, env); after == before {
		t.Error("a real missing argument was not recorded")
	}
}

// A join refused because the name is taken leaves the code good: the host
// joins with another name and the same code. Then the code is spent.
func TestJoinCodeSurvivesNameClash(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "spare", "third")
	hs[0].join(t, hub, addr)
	r := hub.run(t, "hub", "invite")
	expect(t, r, 0, "invite")
	code := field(codeRe, r.out)

	clash := &orgHost{name: "laptop", home: hs[1].home}
	for _, e := range hs[1].env {
		if strings.HasPrefix(e, "SUNSTACK_HOST_NAME=") {
			e = "SUNSTACK_HOST_NAME=laptop"
		}
		clash.env = append(clash.env, e)
	}
	r = clash.run(t, "org", "join", addr, "--code", code)
	if r.code == 0 || !strings.Contains(r.out+r.stderr, "already used") {
		t.Fatalf("join under a taken name: exit %d\n%s%s", r.code, r.out, r.stderr)
	}
	expect(t, hs[1].run(t, "org", "join", addr, "--code", code), 0, "the same code after the name clash")
	r = hs[2].run(t, "org", "join", addr, "--code", code)
	if r.code == 0 {
		t.Error("a spent code let a second host in")
	}
}

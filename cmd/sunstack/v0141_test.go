package main

// §29: fixes from the issue log.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// `<command> --help` prints that command's lines from help all.
func TestHelpPerCommand(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, nil, "send", "--help")
	expect(t, r, 0, "send --help")
	// send has lines in two sections: this host's and another host's.
	requireContains(t, r.out, "sunstack send <id|title|id_task", "--reply-to MSG", "sunstack send <host>:<team>/<agent>")
	if strings.Contains(r.out, "sunstack commit") {
		t.Errorf("other commands' lines printed:\n%s", r.out)
	}
	r = sh(t, p, nil, "commit", "-h")
	expect(t, r, 0, "commit -h")
	requireContains(t, r.out, "sunstack commit <id> <target> <candidate> <checksum>", "--delete <checksum>")
	// check|take|ack share one line.
	requireContains(t, sh(t, p, nil, "take", "--help").out, "sunstack take <id> <msg>", "sunstack check|take|ack --session")
	r = sh(t, p, nil, "hub", "--help")
	expect(t, r, 0, "hub --help")
	requireContains(t, r.out, "sunstack hub init <org name>")
	// --help after other arguments is not help.
	expect(t, sh(t, p, nil, "send", "builder.alice", "--help"), 2, "send x --help")
}

// A usage error adds the command's lines on stderr; stdout is unchanged.
func TestUsageErrorsShowSyntax(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	r := sh(t, p, nil, "snapshot", "builder.alice", "--token", tok)
	expect(t, r, 2, "snapshot without target")
	if r.out != "missing_arguments: id target\n" {
		t.Errorf("stdout changed: %q", r.out)
	}
	requireContains(t, r.stderr, "usage:\n", "sunstack snapshot <id> <context.md|board.md")
	r = sh(t, p, nil, "commit", "builder.alice", "board.md", "x.md", "--expect", "abc", "--token", tok)
	expect(t, r, 2, "commit --expect")
	requireContains(t, r.stderr, "unknown option: --expect", "sunstack commit <id> <target> <candidate> <checksum>")
	// hub subcommands too, without repeating a list the error already has.
	r = sh(t, p, nil, "hub", "init")
	expect(t, r, 2, "hub init without a name")
	requireContains(t, r.stderr, "usage:\n", "sunstack hub init <org name>")
	r = sh(t, p, nil, "hub", "bogus")
	if strings.Count(r.stderr, "sunstack hub init <org name>") != 1 {
		t.Errorf("hub list repeated:\n%s", r.stderr)
	}
	// Other failures add nothing.
	r = sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", "wrong")
	expect(t, r, 4, "bad token")
	if strings.Contains(r.stderr, "usage:") {
		t.Errorf("a claim error printed usage:\n%s", r.stderr)
	}
}

// A candidate given as a relative path inside the session folder is taken.
func TestCommitRelativeCandidate(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	sum := field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok).out)
	rel := filepath.Join("sunstack", "_local", "tmp", "builder.alice", tok, "context.md")
	must(t, os.WriteFile(filepath.Join(p, rel), []byte("## Current state\n- relative\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", rel, sum, "--token", tok), 0, "relative candidate")
	// A relative path to another folder is still refused.
	must(t, os.WriteFile(filepath.Join(p, "elsewhere.md"), []byte("- x\n"), 0o644))
	sum = field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok).out)
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", "elsewhere.md", sum, "--token", tok), 2, "candidate outside the session folder")
}

// A board snapshot names the next free keys on stderr, counting the archive.
func TestBoardSnapshotNextKeys(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	board := filepath.Join(p, "sunstack", "builder.alice", "board.md")
	must(t, os.WriteFile(board, []byte("## Now\n- 2026-10-21 KR3 [O1] work\n## Asks\n- 2026-10-21 Q2 which one?\n"), 0o644))
	arch := filepath.Join(p, "sunstack", "builder.alice", "archive")
	must(t, os.MkdirAll(arch, 0o755))
	must(t, os.WriteFile(filepath.Join(arch, "2026-09.md"), []byte("## Done\n- 2026-09-01 KR9 [O1] archived work\n"), 0o644))
	r := sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", tok)
	expect(t, r, 0, "board snapshot")
	if !strings.Contains(r.stderr, "next keys: KR10 Q3\n") {
		t.Errorf("stderr: %q", r.stderr)
	}
	lines := strings.SplitN(r.out, "\n", 4)
	if !strings.HasPrefix(lines[0], "checksum: ") || !strings.HasPrefix(lines[1], "candidate_dir: ") || lines[2] != "----- content -----" || !strings.HasPrefix(lines[3], "## Now\n") {
		t.Errorf("stdout format changed:\n%s", r.out)
	}
	if r := sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok); r.stderr != "" {
		t.Errorf("a context snapshot printed: %q", r.stderr)
	}
}

// update restarts the connector service only when one is installed and the
// update was not run by the connector itself.
func TestUpdateRestartsConnector(t *testing.T) {
	calls := 0
	var out strings.Builder
	restart := func(err error) func() error { return func() error { calls++; return err } }

	t.Setenv("SUNSTACK_SERVICE", "1")
	restartConnector(&out, restart(nil))
	if calls != 0 {
		t.Fatal("restarted from inside the connector")
	}
	t.Setenv("SUNSTACK_SERVICE", "")
	restartConnector(&out, restart(&core.Error{Reason: "no_service"}))
	if calls != 1 || out.String() != "" {
		t.Fatalf("no service should be silent: %q", out.String())
	}
	restartConnector(&out, restart(nil))
	requireContains(t, out.String(), "restarted the hub connector")
	out.Reset()
	restartConnector(&out, restart(errors.New("boom")))
	requireContains(t, out.String(), "restart the hub connector yourself", "boom")
}

// The new binary restarts the connector as it finishes an update (update
// --skip-binary, run by the old one after replacing it), so an older updater
// needs no restart of its own; inside the connector it does not.
func TestUpdateFinishRestartsService(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the connector service is for macOS and Linux")
	}
	env := isolatedEnv(t)
	home := envValue(env, "HOME")
	svc := filepath.Join(home, ".config", "systemd", "user", "sunstack-hub-connect.service")
	if runtime.GOOS == "darwin" {
		svc = filepath.Join(home, "Library", "LaunchAgents", "org.sunstack.hub-connect.plist")
	}
	must(t, os.MkdirAll(filepath.Dir(svc), 0o755))
	must(t, os.WriteFile(svc, []byte("service"), 0o644))
	bin := t.TempDir()
	marker := filepath.Join(bin, "calls")
	for _, name := range []string{"launchctl", "systemctl", "claude"} {
		must(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho "+name+" \"$@\" >> "+marker+"\n"), 0o755))
	}
	env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p := t.TempDir()

	r := sh(t, p, env, "update", "--skip-binary", "--claude")
	expect(t, r, 0, "update --skip-binary")
	requireContains(t, r.out, "restarted the hub connector")
	b, _ := os.ReadFile(marker)
	if !strings.Contains(string(b), "launchctl kickstart -k") && !strings.Contains(string(b), "systemctl --user restart") {
		t.Fatalf("no restart:\n%s", b)
	}

	must(t, os.Remove(marker))
	r = sh(t, p, append(env, "SUNSTACK_SERVICE=1"), "update", "--skip-binary", "--claude")
	expect(t, r, 0, "update inside the connector")
	if b, _ := os.ReadFile(marker); strings.Contains(string(b), "launchctl") || strings.Contains(string(b), "systemctl") {
		t.Fatalf("restarted from inside the connector:\n%s", b)
	}
}

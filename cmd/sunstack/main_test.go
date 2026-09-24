package main

// End-to-end checks against the built binary, ported from the step 1 spike
// (tests/spike-test.sh): resolution, claims, takeover, snapshot/commit, races,
// guards, leftover locks and release. Separate processes make the races real.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sunstack-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "sunstack")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code        int
	out, stderr string
}

// sh runs the binary in dir with extra environment entries.
func sh(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), env...)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, so.String(), se.String()}
}

// cleanEnv drops the caller's agent and tmux variables so tests are hermetic.
func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "TMUX") || strings.HasPrefix(e, "CLAUDE") || strings.HasPrefix(e, "CODEX") {
			continue
		}
		env = append(env, e)
	}
	return env
}

func expect(t *testing.T, r result, code int, label string) {
	t.Helper()
	if r.code != code {
		t.Errorf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", label, r.code, code, r.out, r.stderr)
	}
}

var tokenRe = regexp.MustCompile(`(?m)^token: (\S+)$`)
var sumRe = regexp.MustCompile(`(?m)^checksum: (\S+)$`)

func field(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func fixture(t *testing.T) string {
	t.Helper()
	p := t.TempDir()
	for _, id := range []string{"builder.alice", "builder.bob", "reviewer"} {
		d := filepath.Join(p, "sunstack", id)
		must(t, os.MkdirAll(d, 0o755))
		title := strings.SplitN(id, ".", 2)[0]
		must(t, os.WriteFile(filepath.Join(d, "AGENT.md"), []byte("---\ntitle: "+title+"\nfrom: custom\n---\n## Role\n"), 0o644))
		must(t, os.WriteFile(filepath.Join(d, "context.md"), []byte("## Current state\n## Decisions\n"), 0o644))
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PILLARS.md"), []byte("- 2026-09-22 every migration must be reversible\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PROTOCOL.md"), []byte("# Sunstack protocol\n"), 0o644))
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolution(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, nil, "as")
	expect(t, r, 2, "as with no id")
	if !strings.HasPrefix(r.out, "missing_arguments: id") || !strings.Contains(r.out, "reviewer\tfree") {
		t.Errorf("roster not listed: %q", r.out)
	}
	expect(t, sh(t, p, nil, "as", "builder"), 2, "title with two instances asks which")
	expect(t, sh(t, p, nil, "as", "nobody"), 1, "unknown title")
	expect(t, sh(t, p, nil, "as", "Bad/Name"), 2, "bad name is a usage error")
	expect(t, sh(t, p, nil, "as", "reviewer", "--bogus"), 2, "unknown option")
	expect(t, sh(t, t.TempDir(), nil, "as", "reviewer"), 1, "no sunstack/ found")
	// A subdirectory resolves to the project root.
	sub := filepath.Join(p, "src", "deep")
	must(t, os.MkdirAll(sub, 0o755))
	expect(t, sh(t, sub, nil, "as", "reviewer"), 0, "as from a subdirectory")
}

func TestClaimResumeTakeover(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, []string{"CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=s-1"}, "as", "builder.alice")
	expect(t, r, 0, "claim")
	t1 := field(tokenRe, r.out)
	if t1 == "" || !strings.Contains(r.out, "PILLARS.md (team)") || !strings.Contains(r.out, "protocol: 2") {
		t.Fatalf("bundle incomplete: %s", r.out)
	}
	live, _ := os.ReadFile(filepath.Join(p, "sunstack", "_local", "live", "builder.alice", t1+".json"))
	if !strings.Contains(string(live), `"tool": "claude"`) || !strings.Contains(string(live), `"session": "s-1"`) {
		t.Errorf("tool/session not detected: %s", live)
	}

	r = sh(t, p, nil, "as", "builder.alice")
	expect(t, r, 4, "second claim refused")
	if !strings.Contains(r.out, "claim="+t1) {
		t.Errorf("claim line missing: %q", r.out)
	}
	r = sh(t, p, nil, "as", "builder.alice", "--token", t1)
	expect(t, r, 0, "resume with token")
	if field(tokenRe, r.out) != t1 {
		t.Error("resume changed the token")
	}
	expect(t, sh(t, p, nil, "as", "builder.alice", "--token", "deadbeef"), 4, "wrong token")
	r = sh(t, p, nil, "as", "builder.alice", "--takeover", "--expect", "deadbeef")
	expect(t, r, 4, "stale --expect refused")
	if !strings.Contains(r.out, "claim="+t1) {
		t.Errorf("changed-claim refusal must print the current claim line: %q", r.out)
	}
	expect(t, sh(t, p, nil, "as", "builder.alice", "--takeover"), 2, "--takeover without --expect")
	r = sh(t, p, nil, "as", "builder.alice", "--takeover", "--expect", t1)
	expect(t, r, 0, "takeover")
	t2 := field(tokenRe, r.out)
	if t2 == "" || t2 == t1 {
		t.Error("takeover did not rotate the token")
	}
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", t1), 4, "old token rejected")

	log, _ := os.ReadFile(filepath.Join(p, "sunstack", "_local", "log", "events.log"))
	for _, ev := range []string{"claim", "resume", "takeover"} {
		if !regexp.MustCompile(`(?m)^\S+ ` + ev + ` +builder\.alice`).Match(log) {
			t.Errorf("events.log lacks %s: %s", ev, log)
		}
	}
}

func TestSamePane(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, []string{"TMUX_PANE=%99"}, "as", "reviewer")
	expect(t, r, 0, "claim from pane %99")
	tr := field(tokenRe, r.out)
	r = sh(t, p, []string{"TMUX_PANE=%99"}, "as", "reviewer")
	expect(t, r, 4, "same pane without token")
	if !strings.Contains(r.stderr, "occupied_same_pane") {
		t.Errorf("want occupied_same_pane: %s", r.stderr)
	}
	r = sh(t, p, []string{"TMUX_PANE=%12"}, "as", "reviewer")
	if r.code != 4 || strings.Contains(r.stderr, "same_pane") {
		t.Errorf("another pane must get plain occupied: %d %s", r.code, r.stderr)
	}
	expect(t, sh(t, p, nil, "release", "reviewer", "--token", tr), 0, "release")
}

// race runs n processes at once and returns their exit codes.
func race(t *testing.T, n int, dir string, args func(i int) []string) []int {
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(bin, args(i)...)
			cmd.Dir, cmd.Env = dir, cleanEnv()
			if err := cmd.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					codes[i] = ee.ExitCode()
				} else {
					codes[i] = -1
				}
			}
		}(i)
	}
	wg.Wait()
	return codes
}

func count(codes []int, want int) int {
	n := 0
	for _, c := range codes {
		if c == want {
			n++
		}
	}
	return n
}

func TestConcurrentClaims(t *testing.T) {
	p := fixture(t)
	codes := race(t, 8, p, func(int) []string { return []string{"as", "reviewer"} })
	if count(codes, 0) != 1 {
		t.Errorf("8 concurrent claims: want exactly 1 winner, got codes %v", codes)
	}
}

func TestSnapshotCommit(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok)
	ctx := filepath.Join(p, "sunstack", "builder.alice", "context.md")

	r := sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok)
	expect(t, r, 0, "snapshot")
	c1 := field(sumRe, r.out)
	must(t, os.WriteFile(filepath.Join(tmp, "a.md"), []byte("## Current state\n- A\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(tmp, "b.md"), []byte("## Current state\n- B\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", filepath.Join(tmp, "a.md"), c1, "--token", tok), 0, "commit A")
	r = sh(t, p, nil, "commit", "builder.alice", "context.md", filepath.Join(tmp, "b.md"), c1, "--token", tok)
	expect(t, r, 3, "commit B from the old snapshot")
	if !strings.Contains(r.out, "- A") || field(sumRe, r.out) == "" {
		t.Errorf("mismatch must return fresh content and checksum: %q", r.out)
	}
	if b, _ := os.ReadFile(ctx); string(b) != "## Current state\n- A\n" {
		t.Errorf("file should hold A only: %q", b)
	}

	// Concurrent commits from one snapshot: exactly one lands.
	c2 := field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok).out)
	for i := 0; i < 6; i++ {
		must(t, os.WriteFile(filepath.Join(tmp, fmt.Sprintf("r%d.md", i)), []byte(fmt.Sprintf("- race %d\n", i)), 0o644))
	}
	codes := race(t, 6, p, func(i int) []string {
		return []string{"commit", "builder.alice", "context.md", filepath.Join(tmp, fmt.Sprintf("r%d.md", i)), c2, "--token", tok}
	})
	if count(codes, 0) != 1 || count(codes, 3) != 5 {
		t.Errorf("6 concurrent commits: want 1 ok and 5 mismatches, got %v", codes)
	}

	// Threads: create from absent, then delete.
	r = sh(t, p, nil, "snapshot", "builder.alice", "threads/auth-refactor.md", "--token", tok)
	expect(t, r, 0, "snapshot a missing thread")
	if field(sumRe, r.out) != "absent" {
		t.Errorf("missing file checksum should be absent: %q", r.out)
	}
	must(t, os.WriteFile(filepath.Join(tmp, "auth-refactor.md"), []byte("Goal: refactor auth\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "threads/auth-refactor.md", filepath.Join(tmp, "auth-refactor.md"), "absent", "--token", tok), 0, "create thread")
	c3 := field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "threads/auth-refactor.md", "--token", tok).out)
	expect(t, sh(t, p, nil, "commit", "builder.alice", "threads/auth-refactor.md", "--delete", c3, "--token", tok), 0, "delete thread")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "builder.alice", "threads", "auth-refactor.md")); !os.IsNotExist(err) {
		t.Error("thread still exists")
	}
}

func TestGuards(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok)
	sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok)

	outside := filepath.Join(p, "outside.md")
	must(t, os.WriteFile(outside, []byte("x\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", outside, "absent", "--token", tok), 2, "candidate outside tmp")
	must(t, os.MkdirAll(filepath.Join(tmp, "threads"), 0o755))
	nested := filepath.Join(tmp, "threads", "x.md")
	must(t, os.WriteFile(nested, []byte("x\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", nested, "absent", "--token", tok), 2, "candidate in a subfolder")
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "notes.md", "--token", tok), 2, "target outside the allowed set")
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "threads/../x.md", "--token", tok), 2, "traversal in topic")
	conflict := filepath.Join(tmp, "c.md")
	must(t, os.WriteFile(conflict, []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> b\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", conflict, "absent", "--token", tok), 1, "conflict markers in candidate")

	// A conflicted context blocks as.
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder.bob", "context.md"), []byte("<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n"), 0o644))
	expect(t, sh(t, p, nil, "as", "builder.bob"), 1, "as refuses a conflicted context")

	// Symlinked thread target.
	tb := field(tokenRe, sh(t, p, nil, "as", "reviewer").out)
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "reviewer", "threads"), 0o755))
	if err := os.Symlink(outside, filepath.Join(p, "sunstack", "reviewer", "threads", "evil.md")); err == nil {
		expect(t, sh(t, p, nil, "snapshot", "reviewer", "threads/evil.md", "--token", tb), 1, "symlinked target")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}

	// A leftover lock (kill -9) gives busy, and is not removed.
	lock := filepath.Join(p, "sunstack", "_local", "locks", "reviewer")
	must(t, os.MkdirAll(lock, 0o755))
	r := sh(t, p, nil, "release", "reviewer", "--token", tb)
	expect(t, r, 4, "leftover lock")
	if !strings.Contains(r.stderr, "busy") {
		t.Errorf("want busy: %s", r.stderr)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("leftover lock must not be removed automatically")
	}
	os.RemoveAll(lock)

	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", "deadbeef"), 4, "release with a wrong token")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", tok), 0, "release")
	expect(t, sh(t, p, nil, "as", "builder.alice"), 0, "free again")
	if entries, _ := os.ReadDir(filepath.Join(p, "sunstack", "_local", "locks")); len(entries) != 0 {
		t.Errorf("locks left behind: %v", entries)
	}
}

func TestMissingArguments(t *testing.T) {
	p := fixture(t)
	for _, args := range [][]string{{"snapshot"}, {"commit", "builder.alice"}, {"release"}} {
		r := sh(t, p, nil, args...)
		expect(t, r, 2, strings.Join(args, " "))
		if !strings.HasPrefix(r.out, "missing_arguments:") {
			t.Errorf("%v: want missing_arguments on stdout, got %q", args, r.out)
		}
	}
	expect(t, sh(t, p, nil, "nope"), 2, "unknown command")
	r := sh(t, p, nil, "version")
	expect(t, r, 0, "version")
	if !strings.Contains(r.out, "protocol: 2") {
		t.Errorf("version output: %q", r.out)
	}
}

func TestAmend(t *testing.T) {
	p := fixture(t)
	tok := field(tokenRe, sh(t, p, nil, "as", "builder.alice").out)
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice")
	teamTmp := filepath.Join(p, "sunstack", "_local", "tmp", "_team")

	// Rule files cannot go through commit.
	must(t, os.MkdirAll(tmp, 0o755))
	must(t, os.WriteFile(filepath.Join(tmp, "p.md"), []byte("- rule\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "pillars.md", filepath.Join(tmp, "p.md"), "absent", "--token", tok), 2, "commit refuses pillars.md")

	// Agent pillars: snapshot needs no token; amend needs a summary.
	r := sh(t, p, nil, "snapshot", "builder.alice", "pillars.md")
	expect(t, r, 0, "snapshot agent pillars without token")
	if field(sumRe, r.out) != "absent" {
		t.Errorf("new pillars.md should be absent: %q", r.out)
	}
	expect(t, sh(t, p, nil, "amend", "builder.alice", "pillars.md", filepath.Join(tmp, "p.md"), "absent"), 2, "amend without --summary")
	expect(t, sh(t, p, nil, "amend", "builder.alice", "pillars.md", filepath.Join(tmp, "p.md"), "absent", "--summary", "add rule"), 0, "amend agent pillars")
	if b, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.alice", "pillars.md")); string(b) != "- rule\n" {
		t.Errorf("pillars.md = %q", b)
	}

	// Team pillars through --team, with a stale checksum first.
	r = sh(t, p, nil, "snapshot", "--team")
	expect(t, r, 0, "snapshot team pillars")
	sum := field(sumRe, r.out)
	must(t, os.WriteFile(filepath.Join(teamTmp, "PILLARS.md"), []byte("- 2026-09-23 new team rule\n"), 0o644))
	r = sh(t, p, nil, "amend", "--team", filepath.Join(teamTmp, "PILLARS.md"), "deadbeef", "--summary", "x")
	expect(t, r, 3, "stale team checksum")
	if !strings.Contains(r.out, "reversible") {
		t.Errorf("mismatch must return the current team pillars: %q", r.out)
	}
	expect(t, sh(t, p, nil, "amend", "--team", filepath.Join(teamTmp, "PILLARS.md"), sum, "--summary", "replace team rules"), 0, "amend team pillars")

	// AGENT.md must keep frontmatter with the right title.
	r = sh(t, p, nil, "snapshot", "builder.alice", "AGENT.md")
	asum := field(sumRe, r.out)
	must(t, os.WriteFile(filepath.Join(tmp, "AGENT.md"), []byte("no frontmatter\n"), 0o644))
	expect(t, sh(t, p, nil, "amend", "builder.alice", "AGENT.md", filepath.Join(tmp, "AGENT.md"), asum, "--summary", "x"), 1, "AGENT.md without frontmatter")
	must(t, os.WriteFile(filepath.Join(tmp, "AGENT.md"), []byte("---\ntitle: builder\nfrom: custom\n---\n## Role\nImproved\n"), 0o644))
	expect(t, sh(t, p, nil, "amend", "builder.alice", "AGENT.md", filepath.Join(tmp, "AGENT.md"), asum, "--summary", "tighten duties"), 0, "amend AGENT.md")

	log, _ := os.ReadFile(filepath.Join(p, "sunstack", "_local", "log", "events.log"))
	for _, want := range []string{`amend    builder.alice  pillars.md  summary="add rule"`, `amend    team  PILLARS.md  summary="replace team rules"`, `summary="tighten duties"`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("events.log lacks %s:\n%s", want, log)
		}
	}
}

func TestTeamCommands(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	must(t, os.WriteFile(filepath.Join(p, "AGENTS.md"), []byte("# Project\n\nKeep this.\n"), 0o644))
	r := sh(t, p, home, "init")
	expect(t, r, 0, "init")
	agents, _ := os.ReadFile(filepath.Join(p, "AGENTS.md"))
	if !strings.Contains(string(agents), "Keep this.") || strings.Count(string(agents), "<!-- sunstack:begin -->") != 1 {
		t.Errorf("AGENTS.md after init:\n%s", agents)
	}
	if r = sh(t, p, home, "init"); !strings.Contains(r.out, "nothing to change") {
		t.Errorf("second init should be a no-op: %q", r.out)
	}
	gi, _ := os.ReadFile(filepath.Join(p, ".gitignore"))
	if !strings.Contains(string(gi), "sunstack/_local/") {
		t.Error(".gitignore line missing")
	}
	// An old PROTOCOL.md is kept by init and replaced by init --refresh; PILLARS.md never is.
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PROTOCOL.md"), []byte("old\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PILLARS.md"), []byte("- 2026-09-23 keep me\n"), 0o644))
	sh(t, p, home, "init")
	if b, _ := os.ReadFile(filepath.Join(p, "sunstack", "PROTOCOL.md")); string(b) != "old\n" {
		t.Error("plain init must not replace PROTOCOL.md")
	}
	if r := sh(t, p, home, "health"); !strings.Contains(r.out, "PROTOCOL.md differs") ||
		!strings.Contains(r.out, "next steps:") || !strings.Contains(r.out, "[next] no agents hired yet") {
		t.Errorf("health should flag the old protocol and suggest hiring: %q", r.out)
	}
	// Staging files of an unclaimed agent are reported as leftovers.
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "_local", "tmp", "builder.gone"), 0o755))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "_local", "tmp", "builder.gone", "context.md"), []byte("x"), 0o644))
	if r := sh(t, p, home, "health"); !strings.Contains(r.out, "leftover staging file(s) in tmp/builder.gone") {
		t.Errorf("health should report leftover tmp: %q", r.out)
	}
	must(t, os.RemoveAll(filepath.Join(p, "sunstack", "_local", "tmp", "builder.gone")))
	sh(t, p, home, "init", "--refresh")
	if b, _ := os.ReadFile(filepath.Join(p, "sunstack", "PROTOCOL.md")); !strings.HasPrefix(string(b), "# Sunstack protocol") {
		t.Errorf("init --refresh should replace PROTOCOL.md: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(p, "sunstack", "PILLARS.md")); string(b) != "- 2026-09-23 keep me\n" {
		t.Error("PILLARS.md must never be replaced")
	}

	// A malformed block is refused without writing.
	bad := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bad, "AGENTS.md"), []byte("<!-- sunstack:begin -->\nhalf\n"), 0o644))
	expect(t, sh(t, bad, home, "init"), 1, "malformed block")
	if _, err := os.Stat(filepath.Join(bad, "sunstack")); err == nil {
		t.Error("init must not write anything when the block is malformed")
	}

	r = sh(t, p, home, "hire")
	expect(t, r, 2, "hire without a title")
	if !strings.Contains(r.out, "builder\tbuilt-in") {
		t.Errorf("hire should list templates: %q", r.out)
	}
	expect(t, sh(t, p, home, "hire", "nobody", "xx"), 1, "unknown template")
	expect(t, sh(t, p, home, "hire", "builder"), 2, "every agent needs a name")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire builder.alice")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 1, "duplicate id")
	doc, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.alice", "AGENT.md"))
	if !strings.Contains(string(doc), "name: alice") || !strings.Contains(string(doc), "from: builder@2") || !strings.Contains(string(doc), "hired: ") {
		t.Errorf("AGENT.md frontmatter:\n%s", doc)
	}

	// Recruit: an approved draft becomes a custom agent.
	draft := filepath.Join(p, "draft.md")
	must(t, os.WriteFile(draft, []byte("---\ntitle: security-reviewer\n---\n## Role\nReviews security only\n"), 0o644))
	expect(t, sh(t, p, home, "hire", "security-reviewer", "sec", "--file", draft), 0, "hire from a draft")
	doc, _ = os.ReadFile(filepath.Join(p, "sunstack", "security-reviewer.sec", "AGENT.md"))
	if !strings.Contains(string(doc), "from: custom") {
		t.Errorf("draft hire should be from: custom:\n%s", doc)
	}
	if _, err := os.Stat(draft); err != nil {
		t.Error("a draft outside _local/tmp/ belongs to the user and must be kept")
	}
	expect(t, sh(t, p, home, "hire", "other", "xx", "--file", draft), 1, "draft title must match")

	// A staged recruit draft is removed once hired; a second analyst copies the
	// role from the first because there is no template.
	staged := filepath.Join(p, "sunstack", "_local", "tmp", "_recruit", "analyst-1.md")
	must(t, os.MkdirAll(filepath.Dir(staged), 0o755))
	must(t, os.WriteFile(staged, []byte("---\ntitle: analyst\n---\n## Role\nAnalyzes\n"), 0o644))
	expect(t, sh(t, p, home, "hire", "analyst", "one", "--file", staged), 0, "hire a staged draft")
	if _, err := os.Stat(filepath.Dir(staged)); !os.IsNotExist(err) {
		t.Error("the staged draft and its empty folder should be removed after hire")
	}
	expect(t, sh(t, p, home, "hire", "analyst", "two"), 0, "hire from a colleague")
	doc, _ = os.ReadFile(filepath.Join(p, "sunstack", "analyst.two", "AGENT.md"))
	if !strings.Contains(string(doc), "from: analyst.one") || !strings.Contains(string(doc), "name: two") || !strings.Contains(string(doc), "Analyzes") {
		t.Errorf("colleague hire:\n%s", doc)
	}
	expect(t, sh(t, p, home, "hire", "analyst", "three", "--from", "builder.alice"), 1, "--from must share the title")

	// Personal library round trip.
	expect(t, sh(t, p, home, "library", "save", "security-reviewer.sec"), 0, "library save")
	r = sh(t, p, home, "library")
	if !strings.Contains(r.out, "security-reviewer") || !strings.Contains(r.out, "personal") {
		t.Errorf("library should list the personal template: %q", r.out)
	}
	expect(t, sh(t, p, home, "library", "save", "security-reviewer.sec"), 1, "library save refuses to overwrite")
	expect(t, sh(t, p, home, "hire", "security-reviewer", "bob"), 0, "hire from the personal template")

	// team, pillar, inbox, log.
	tok := field(tokenRe, sh(t, p, append(home, "TMUX_PANE=%0"), "as", "builder.alice").out)
	r = sh(t, p, home, "team")
	if !strings.Contains(r.out, "builder.alice") || !strings.Contains(r.out, "1 session(s)") {
		t.Errorf("team: %q", r.out)
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PILLARS.md"), []byte("<!-- - not a rule -->\n- 2026-09-23 team rule\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder.alice", "pillars.md"), []byte("- 2026-09-23 own rule\n"), 0o644))
	r = sh(t, p, home, "pillar", "builder.alice")
	if !strings.Contains(r.out, "[team] 2026-09-23 team rule") || !strings.Contains(r.out, "[builder.alice] 2026-09-23 own rule") || strings.Contains(r.out, "not a rule") {
		t.Errorf("pillar: %q", r.out)
	}
	// An unnamed agent from an older version still works, health asks to
	// migrate it, and rename gives it a name.
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "builder"), 0o755))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder", "AGENT.md"), []byte("---\ntitle: builder\nhired: 2026-09-01\n---\n## Role\nOld\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder", "context.md"), []byte("## Current state\n- 2026-09-01 kept\n"), 0o644))
	expect(t, sh(t, p, home, "pillar", "builder"), 0, "pillar on the exact id builder")
	if r := sh(t, p, home, "health"); !strings.Contains(r.out, "migrate  builder has no name") {
		t.Errorf("health should ask to migrate the unnamed agent: %q", r.out)
	}
	expect(t, sh(t, p, home, "rename", "builder", "reviewer.x"), 2, "rename keeps the title")
	expect(t, sh(t, p, home, "rename", "builder", "builder.alice"), 1, "rename onto an existing id")
	expect(t, sh(t, p, home, "rename", "builder", "builder.old"), 0, "rename")
	doc, _ = os.ReadFile(filepath.Join(p, "sunstack", "builder.old", "AGENT.md"))
	ctx, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.old", "context.md"))
	if !strings.Contains(string(doc), "name: old") || !strings.Contains(string(ctx), "kept") {
		t.Errorf("renamed agent:\n%s\n%s", doc, ctx)
	}
	expect(t, sh(t, p, home, "pillar", "nobody"), 1, "pillar unknown id")
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice"), 0o755))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice", "m1.md"), []byte("---\nfrom: pm\ntype: handoff   # q\nat: 2026-09-23T10:00:00Z\n---\nbody\n"), 0o644))
	r = sh(t, p, home, "inbox", "builder.alice")
	if !strings.Contains(r.out, "handoff") || !strings.Contains(r.out, "from pm") {
		t.Errorf("inbox: %q", r.out)
	}
	r = sh(t, p, home, "log", "--id", "builder.alice")
	if !strings.Contains(r.out, "hire     builder.alice") || !strings.Contains(r.out, "claim    builder.alice") || strings.Contains(r.out, "security-reviewer") {
		t.Errorf("log --id: %q", r.out)
	}

	// fire and rename refuse a claimed agent; fire refuses pending messages.
	expect(t, sh(t, p, home, "rename", "builder.alice", "builder.al"), 4, "rename a claimed agent")
	expect(t, sh(t, p, home, "fire", "builder.alice"), 4, "fire a claimed agent")
	sh(t, p, home, "release", "builder.alice", "--token", tok)
	expect(t, sh(t, p, home, "fire", "builder.alice"), 1, "fire with a pending message")
	expect(t, sh(t, p, home, "fire", "builder.alice", "--discard"), 0, "fire --discard")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "builder.alice")); !os.IsNotExist(err) {
		t.Error("agent folder still there")
	}

	r = sh(t, p, home, "health")
	if !strings.Contains(r.out, "AGENTS.md has one current sunstack block") {
		t.Errorf("health: %q", r.out)
	}
	expect(t, sh(t, t.TempDir(), home, "health"), 1, "health outside a project fails")

	// A folder that merely happens to be named sunstack is not a team.
	other := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(other, "sunstack", "notes"), 0o755))
	must(t, os.WriteFile(filepath.Join(other, "sunstack", "README.md"), []byte("my notes\n"), 0o644))
	expect(t, sh(t, filepath.Join(other, "sunstack", "notes"), home, "team"), 1, "a sunstack folder without PROTOCOL.md is not a team")
	expect(t, sh(t, other, home, "init"), 1, "init refuses a non-team sunstack folder")
	if _, err := os.Stat(filepath.Join(other, "sunstack", "PROTOCOL.md")); err == nil {
		t.Error("init wrote into a folder that is not a team")
	}
}

func TestReviewFixes(t *testing.T) {
	p := fixture(t)
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PROTOCOL.md"), []byte("# protocol\n"), 0o644))

	// A damaged claim is an error, not "free": no fresh claim, no fire.
	must(t, os.MkdirAll(filepath.Join(p, "sunstack", "_local", "live"), 0o755))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "_local", "live", "reviewer.json"), []byte("{broken"), 0o644))
	r := sh(t, p, nil, "as", "reviewer")
	expect(t, r, 1, "as on a damaged claim")
	if !strings.Contains(r.stderr, "bad_claim") {
		t.Errorf("want bad_claim: %s", r.stderr)
	}
	expect(t, sh(t, p, nil, "fire", "reviewer", "--discard"), 1, "fire on a damaged claim")
	if !strings.Contains(sh(t, p, nil, "team").out, "claim file damaged") {
		t.Error("team should flag the damaged claim")
	}
	os.Remove(filepath.Join(p, "sunstack", "_local", "live", "reviewer.json"))

	// A missing PROTOCOL.md stops as before any claim.
	os.Rename(filepath.Join(p, "sunstack", "PROTOCOL.md"), filepath.Join(p, "PROTOCOL.bak"))
	expect(t, sh(t, p, nil, "as", "reviewer"), 1, "as without PROTOCOL.md")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "_local", "live", "reviewer.json")); err == nil {
		t.Error("no claim should be written when identity files are missing")
	}
	os.Rename(filepath.Join(p, "PROTOCOL.bak"), filepath.Join(p, "sunstack", "PROTOCOL.md"))

	// A symlinked agent directory cannot be written through.
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "AGENT.md"), []byte("---\ntitle: evil\n---\n"), 0o644))
	if err := os.Symlink(outside, filepath.Join(p, "sunstack", "evil")); err == nil {
		expect(t, sh(t, p, nil, "snapshot", "evil", "pillars.md"), 1, "snapshot through a symlinked agent dir")
		must(t, os.MkdirAll(filepath.Join(p, "sunstack", "_local", "tmp", "evil"), 0o755))
		must(t, os.WriteFile(filepath.Join(p, "sunstack", "_local", "tmp", "evil", "c.md"), []byte("- x\n"), 0o644))
		expect(t, sh(t, p, nil, "amend", "evil", "pillars.md", filepath.Join(p, "sunstack", "_local", "tmp", "evil", "c.md"), "absent", "--summary", "x"), 1, "amend through a symlinked agent dir")
		if _, err := os.Stat(filepath.Join(outside, "pillars.md")); err == nil {
			t.Error("amend wrote outside the project")
		}
		os.Remove(filepath.Join(p, "sunstack", "evil"))

		// A symlinked candidate file is refused.
		tok := field(tokenRe, sh(t, p, nil, "as", "builder.bob").out)
		tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.bob", tok)
		sh(t, p, nil, "snapshot", "builder.bob", "context.md", "--token", tok)
		secret := filepath.Join(outside, "secret")
		must(t, os.WriteFile(secret, []byte("secret\n"), 0o644))
		must(t, os.Symlink(secret, filepath.Join(tmp, "link.md")))
		expect(t, sh(t, p, nil, "commit", "builder.bob", "context.md", filepath.Join(tmp, "link.md"), "x", "--token", tok), 1, "symlinked candidate")
	}

	// Outside git, fire refuses without --discard.
	r = sh(t, p, nil, "fire", "reviewer")
	expect(t, r, 1, "fire outside git")
	if !strings.Contains(r.stderr, "git cannot confirm") {
		t.Errorf("want the git warning: %s", r.stderr)
	}
	expect(t, sh(t, p, nil, "fire", "reviewer", "--discard"), 0, "fire --discard outside git")

	// Surplus arguments are errors, and pillar does not pretend to write.
	expect(t, sh(t, p, nil, "fire", "builder.bob", "extra"), 2, "fire with an extra argument")
	expect(t, sh(t, p, nil, "pillar", "builder.alice", "always run tests"), 2, "pillar with a requirement")

	// Concurrent hires of one ID: exactly one wins, and nothing is overwritten.
	codes := race(t, 6, p, func(int) []string { return []string{"hire", "builder", "carol"} })
	if count(codes, 0) != 1 {
		t.Errorf("6 concurrent hires: want exactly 1 success, got %v", codes)
	}

	// library show prints a built-in template.
	r = sh(t, p, nil, "library", "show", "builder")
	if r.code != 0 || !strings.Contains(r.out, "## Role") {
		t.Errorf("library show: %d %q", r.code, r.out)
	}
}

func TestMultiSession(t *testing.T) {
	p := fixture(t)
	r := sh(t, p, nil, "as", "builder.alice", "--task", "auth")
	expect(t, r, 0, "first session")
	t1 := field(tokenRe, r.out)
	if !strings.Contains(r.out, "session_name: builder.alice_auth") {
		t.Errorf("session name missing: %s", r.out)
	}
	expect(t, sh(t, p, nil, "as", "builder.alice", "--task", "this-is-too-long"), 2, "task over 10 characters")
	r = sh(t, p, nil, "as", "builder.alice")
	expect(t, r, 4, "second session needs --join")
	if !strings.Contains(r.stderr, "active") || !strings.Contains(r.out, "task=auth") {
		t.Errorf("active refusal: %s %s", r.out, r.stderr)
	}
	r = sh(t, p, nil, "as", "builder.alice", "--join", "--task", "docs")
	expect(t, r, 0, "join")
	t2 := field(tokenRe, r.out)
	if t2 == t1 || !strings.Contains(r.out, "builder.alice_auth") {
		t.Errorf("join must get its own token and list the other session: %s", r.out)
	}
	// Each session stages candidates in its own folder.
	s1 := sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", t1).out
	s2 := sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", t2).out
	if !strings.Contains(s1, t1) || !strings.Contains(s2, t2) {
		t.Errorf("candidate dirs should be per session:\n%s\n%s", s1, s2)
	}
	expect(t, sh(t, p, nil, "fire", "builder.alice", "--discard"), 4, "fire with active sessions")
	expect(t, sh(t, p, nil, "release", "builder.alice", "--token", t1), 0, "release one session")
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", t2), 0, "the other session keeps working")
	expect(t, sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", t1), 4, "released token is gone")
	if r := sh(t, p, nil, "team"); !strings.Contains(r.out, "1 session(s)") || !strings.Contains(r.out, "builder.alice_docs") {
		t.Errorf("team after one release: %s", r.out)
	}

	// A claim file from protocol 1 still counts, and resuming moves it.
	legacy := filepath.Join(p, "sunstack", "_local", "live", "reviewer.json")
	must(t, os.MkdirAll(filepath.Dir(legacy), 0o755))
	must(t, os.WriteFile(legacy, []byte(`{"tool":"claude","host":"h","token":"00112233445566aa","claimed":"2026-09-01T00:00:00Z","last_contact":"2026-09-01T00:00:00Z"}`), 0o644))
	expect(t, sh(t, p, nil, "as", "reviewer"), 4, "a legacy claim is honored")
	expect(t, sh(t, p, nil, "as", "reviewer", "--token", "00112233445566aa"), 0, "resume a legacy claim")
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy claim file should move to the per-session layout")
	}
}

func TestBoards(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, home, "init"), 0, "init")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "BOARD.md")); err != nil {
		t.Fatal("init should create BOARD.md")
	}
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire")
	expect(t, sh(t, p, home, "hire", "reviewer", "bob"), 0, "hire")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "builder.alice", "board.md")); err != nil {
		t.Fatal("hire should create board.md")
	}

	// Objectives change through amend --team BOARD.md.
	r := sh(t, p, home, "snapshot", "--team", "BOARD.md")
	expect(t, r, 0, "snapshot team board")
	sum := field(sumRe, r.out)
	teamTmp := filepath.Join(p, "sunstack", "_local", "tmp", "_team")
	day := "2026-09-24"
	board := "## Objectives\n- " + day + " O1 Ship v1 (due: 2026-12-01)\n## User\n- " + day + " KR1 [O1] Approve the release plan\n## Directives\n"
	must(t, os.WriteFile(filepath.Join(teamTmp, "b.md"), []byte(board), 0o644))
	expect(t, sh(t, p, home, "amend", "--team", "BOARD.md", filepath.Join(teamTmp, "b.md"), sum, "--summary", "set O1"), 0, "amend BOARD.md")

	r = sh(t, p, home, "direct", "Focus on (tests)")
	expect(t, r, 2, "parentheses are refused")
	r = sh(t, p, home, "direct", "Focus on tests first", "--to", "builder")
	expect(t, r, 0, "direct")
	if !strings.Contains(r.out, "D1") {
		t.Errorf("direct: %s", r.out)
	}
	expect(t, sh(t, p, home, "direct", "x", "--to", "nobody"), 1, "unknown addressee")

	// An agent writes its board through snapshot and commit.
	tok := field(tokenRe, sh(t, p, home, "as", "builder.alice", "--task", "tests").out)
	r = sh(t, p, home, "as", "reviewer.bob")
	if !strings.Contains(r.out, "O1 Ship v1") || strings.Contains(r.out, "directives not aligned yet") {
		t.Errorf("reviewer bundle should load BOARD.md and not list D1 (addressed to builder) as unaligned: %s", r.out)
	}
	if r := sh(t, p, home, "as", "builder.alice", "--token", tok); !strings.Contains(r.out, "directives not aligned yet") || !strings.Contains(r.out, "D1 ") {
		t.Errorf("builder bundle should list D1 as unaligned: %s", r.out)
	}
	r = sh(t, p, home, "snapshot", "builder.alice", "board.md", "--token", tok)
	cand := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok, "board.md")
	old := "2026-01-02"
	doc := "aligned: D1\n## Now\n- " + day + " KR1 [O1] Test suite green (needs: reviewer.bob#KR1)\n- " + old + " KR2 [O9] Old work\n## Next\n- no date here\n## Done\n- " + old + " KR0 [O1] Scaffold\n"
	must(t, os.WriteFile(cand, []byte(doc), 0o644))
	expect(t, sh(t, p, home, "commit", "builder.alice", "board.md", cand, field(sumRe, r.out), "--token", tok), 0, "commit board")

	r = sh(t, p, home, "board")
	expect(t, r, 0, "board")
	for _, want := range []string{
		"O1 2026-09-24 Ship v1", "builder.alice#KR1", "user#KR1",
		"builder.alice#KR1 needs reviewer.bob#KR1, which does not exist",
		"builder.alice#KR2 serves O9, which is not an objective",
		"builder.alice#KR2 has not been updated since " + old,
		"builder.alice/board.md has 1 undated",
	} {
		if !strings.Contains(r.out, want) {
			t.Errorf("board output lacks %q:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out, "not aligned yet: builder.alice") {
		t.Errorf("builder.alice aligned with D1: %s", r.out)
	}

	// tidy archives old Done entries by month; the board keeps the rest.
	r = sh(t, p, home, "tidy", "builder.alice")
	expect(t, r, 0, "tidy")
	b, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.alice", "board.md"))
	a, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.alice", "archive", "2026-01.md"))
	if strings.Contains(string(b), "Scaffold") || !strings.Contains(string(a), "KR0 [O1] Scaffold") || !strings.Contains(string(b), "Old work") {
		t.Errorf("tidy:\nboard:\n%s\narchive:\n%s", b, a)
	}
	if r := sh(t, p, home, "as", "builder.alice", "--token", tok); !strings.Contains(r.out, "archive (not loaded") {
		t.Errorf("bundle should mention the archive: %s", r.out)
	}
	expect(t, sh(t, p, home, "snapshot", "builder.alice", "archive/2026-01.md", "--token", tok), 0, "archive is a commit target")
	expect(t, sh(t, p, home, "snapshot", "builder.alice", "archive/jan.md", "--token", tok), 2, "archive names are months")

	// health: a missing board is a migration step.
	must(t, os.Remove(filepath.Join(p, "sunstack", "reviewer.bob", "board.md")))
	if r := sh(t, p, home, "health"); !strings.Contains(r.out, "reviewer.bob has no board.md") {
		t.Errorf("health: %s", r.out)
	}
	expect(t, sh(t, p, home, "tidy", "--all"), 0, "tidy --all")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "reviewer.bob", "board.md")); err != nil {
		t.Error("tidy should create a missing board")
	}
}

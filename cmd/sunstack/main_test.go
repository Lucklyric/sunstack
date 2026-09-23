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
		must(t, os.WriteFile(filepath.Join(d, "AGENT.md"), []byte("---\ntitle: "+title+"\nfrom: custom\n---\n## 职责\n"), 0o644))
		must(t, os.WriteFile(filepath.Join(d, "context.md"), []byte("## 当前状态\n## 决策\n"), 0o644))
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PILLARS.md"), []byte("- 2026-09-22 所有 migration 必须可回滚\n"), 0o644))
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
	if t1 == "" || !strings.Contains(r.out, "PILLARS.md (team)") || !strings.Contains(r.out, "protocol: 1") {
		t.Fatalf("bundle incomplete: %s", r.out)
	}
	live, _ := os.ReadFile(filepath.Join(p, "sunstack", "_local", "live", "builder.alice.json"))
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
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice")
	ctx := filepath.Join(p, "sunstack", "builder.alice", "context.md")

	r := sh(t, p, nil, "snapshot", "builder.alice", "context.md", "--token", tok)
	expect(t, r, 0, "snapshot")
	c1 := field(sumRe, r.out)
	must(t, os.WriteFile(filepath.Join(tmp, "a.md"), []byte("## 当前状态\n- A\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(tmp, "b.md"), []byte("## 当前状态\n- B\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "context.md", filepath.Join(tmp, "a.md"), c1, "--token", tok), 0, "commit A")
	r = sh(t, p, nil, "commit", "builder.alice", "context.md", filepath.Join(tmp, "b.md"), c1, "--token", tok)
	expect(t, r, 3, "commit B from the old snapshot")
	if !strings.Contains(r.out, "- A") || field(sumRe, r.out) == "" {
		t.Errorf("mismatch must return fresh content and checksum: %q", r.out)
	}
	if b, _ := os.ReadFile(ctx); string(b) != "## 当前状态\n- A\n" {
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
	must(t, os.WriteFile(filepath.Join(tmp, "auth-refactor.md"), []byte("目标：重构 auth\n"), 0o644))
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
	tmp := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice")
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
	if !strings.Contains(r.out, "protocol: 1") {
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
	if !strings.Contains(r.out, "migration") {
		t.Errorf("mismatch must return the current team pillars: %q", r.out)
	}
	expect(t, sh(t, p, nil, "amend", "--team", filepath.Join(teamTmp, "PILLARS.md"), sum, "--summary", "replace team rules"), 0, "amend team pillars")

	// AGENT.md must keep frontmatter with the right title.
	r = sh(t, p, nil, "snapshot", "builder.alice", "AGENT.md")
	asum := field(sumRe, r.out)
	must(t, os.WriteFile(filepath.Join(tmp, "AGENT.md"), []byte("no frontmatter\n"), 0o644))
	expect(t, sh(t, p, nil, "amend", "builder.alice", "AGENT.md", filepath.Join(tmp, "AGENT.md"), asum, "--summary", "x"), 1, "AGENT.md without frontmatter")
	must(t, os.WriteFile(filepath.Join(tmp, "AGENT.md"), []byte("---\ntitle: builder\nfrom: custom\n---\n## 职责\n改进后\n"), 0o644))
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
	expect(t, sh(t, p, home, "hire", "nobody"), 1, "unknown template")
	expect(t, sh(t, p, home, "hire", "builder"), 0, "hire builder")
	expect(t, sh(t, p, home, "hire", "builder"), 2, "second builder needs a name")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire builder.alice")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 1, "duplicate id")
	doc, _ := os.ReadFile(filepath.Join(p, "sunstack", "builder.alice", "AGENT.md"))
	if !strings.Contains(string(doc), "name: alice") || !strings.Contains(string(doc), "from: builder@1") || !strings.Contains(string(doc), "hired: ") {
		t.Errorf("AGENT.md frontmatter:\n%s", doc)
	}

	// Recruit: an approved draft becomes a custom agent.
	draft := filepath.Join(p, "draft.md")
	must(t, os.WriteFile(draft, []byte("---\ntitle: security-reviewer\n---\n## 职责\n只看安全问题\n"), 0o644))
	expect(t, sh(t, p, home, "hire", "security-reviewer", "--file", draft), 0, "hire from a draft")
	doc, _ = os.ReadFile(filepath.Join(p, "sunstack", "security-reviewer", "AGENT.md"))
	if !strings.Contains(string(doc), "from: custom") {
		t.Errorf("draft hire should be from: custom:\n%s", doc)
	}
	expect(t, sh(t, p, home, "hire", "other", "--file", draft), 1, "draft title must match")

	// Personal library round trip.
	expect(t, sh(t, p, home, "library", "save", "security-reviewer"), 0, "library save")
	r = sh(t, p, home, "library")
	if !strings.Contains(r.out, "security-reviewer") || !strings.Contains(r.out, "personal") {
		t.Errorf("library should list the personal template: %q", r.out)
	}
	expect(t, sh(t, p, home, "library", "save", "security-reviewer"), 1, "library save refuses to overwrite")
	expect(t, sh(t, p, home, "hire", "security-reviewer", "bob"), 0, "hire from the personal template")

	// team, pillar, inbox, log.
	tok := field(tokenRe, sh(t, p, append(home, "TMUX_PANE=%0"), "as", "builder.alice").out)
	r = sh(t, p, home, "team")
	if !strings.Contains(r.out, "builder.alice") || !strings.Contains(r.out, "claimed by") {
		t.Errorf("team: %q", r.out)
	}
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "PILLARS.md"), []byte("<!-- - not a rule -->\n- 2026-09-23 team rule\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "builder.alice", "pillars.md"), []byte("- 2026-09-23 own rule\n"), 0o644))
	r = sh(t, p, home, "pillar", "builder.alice")
	if !strings.Contains(r.out, "[team] 2026-09-23 team rule") || !strings.Contains(r.out, "[builder.alice] 2026-09-23 own rule") || strings.Contains(r.out, "not a rule") {
		t.Errorf("pillar: %q", r.out)
	}
	expect(t, sh(t, p, home, "pillar", "builder"), 0, "pillar on the exact id builder")
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

	// fire refuses a claimed agent and pending messages.
	expect(t, sh(t, p, home, "fire", "builder.alice"), 4, "fire a claimed agent")
	sh(t, p, home, "release", "builder.alice", "--token", tok)
	expect(t, sh(t, p, home, "fire", "builder.alice"), 1, "fire with a pending message")
	expect(t, sh(t, p, home, "fire", "builder.alice", "--discard"), 0, "fire --discard")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "builder.alice")); !os.IsNotExist(err) {
		t.Error("agent folder still there")
	}

	r = sh(t, p, home, "health")
	if !strings.Contains(r.out, "AGENTS.md has one sunstack block") {
		t.Errorf("health: %q", r.out)
	}
	expect(t, sh(t, t.TempDir(), home, "health"), 1, "health outside a project fails")
}

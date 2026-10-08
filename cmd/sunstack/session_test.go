package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

var testTools, realTmux string

func installTestTools(dir string) {
	realTmux, _ = exec.LookPath("tmux")
	realPS, _ := exec.LookPath("ps")
	testTools = filepath.Join(dir, "tools")
	if err := os.MkdirAll(testTools, 0o755); err != nil {
		panic(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	helper := filepath.Join(testTools, "claude"+ext)
	if out, err := exec.Command("go", "build", "-o", helper, "./testdata/cli").CombinedOutput(); err != nil {
		panic(string(out))
	}
	b, err := os.ReadFile(helper)
	if err != nil {
		panic(err)
	}
	for _, name := range []string{"codex", "tmux", "ps", "lsof"} {
		if err := os.WriteFile(filepath.Join(testTools, name+ext), b, 0o755); err != nil {
			panic(err)
		}
	}
	os.Setenv("SUNSTACK_TEST_REAL_TMUX", realTmux)
	os.Setenv("SUNSTACK_TEST_REAL_PS", realPS)
	// Drop inherited test controls too: no caller-supplied socket can enter tests.
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(key, "SUNSTACK_TEST_") && key != "SUNSTACK_TEST_REAL_TMUX" && key != "SUNSTACK_TEST_REAL_PS" {
			os.Unsetenv(key)
		}
	}
	os.Setenv("PATH", testTools+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func isolatedEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	agents := filepath.Join(home, "agents.json")
	events := filepath.Join(home, "events")
	must(t, os.MkdirAll(events, 0o755))
	must(t, os.WriteFile(agents, []byte("[]"), 0o600))
	return []string{"HOME=" + home, "USERPROFILE=" + home, "SUNSTACK_HOME=" + filepath.Join(home, ".sunstack"), "SUNSTACK_CLAUDE_AGENTS=" + agents, "SUNSTACK_CODEX_SCAN=off", "SUNSTACK_TEST_EVENTS=" + events}
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], key+"=") {
			return strings.TrimPrefix(env[i], key+"=")
		}
	}
	return ""
}

type tmuxFixture struct {
	socket, pid, pane string
	env               []string
}

func privateTmux(t *testing.T, cwd string, env []string) *tmuxFixture {
	t.Helper()
	if runtime.GOOS == "windows" || realTmux == "" {
		t.Skip("private tmux tests require tmux on Unix")
	}
	name := fmt.Sprintf("sunstack-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	cmd := exec.Command("tmux", "-L", name, "-f", os.DevNull, "new-session", "-d", "-s", "tests", "-x", "200", "-y", "60", "-c", cwd, "/bin/sh")
	cmd.Env = append(cleanEnv(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create private tmux server: %v: %s", err, out)
	}
	// Register cleanup immediately, including when metadata retrieval fails.
	t.Cleanup(func() {
		q := exec.Command("tmux", "-L", name, "display-message", "-p", "#{socket_path}")
		q.Env = append(cleanEnv(), env...)
		sock, _ := q.Output()
		cmd := exec.Command("tmux", "-L", name, "kill-server")
		cmd.Env = append(cleanEnv(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("clean up private server: %v: %s", err, out)
		}
		// tmux leaves the socket file behind; remove it.
		if p := strings.TrimSpace(string(sock)); strings.Contains(filepath.Base(p), "sunstack-test-") {
			os.Remove(p)
		}
	})
	cmd = exec.Command("tmux", "-L", name, "display-message", "-p", "#{socket_path}\t#{pid}\t#{pane_id}")
	cmd.Env = append(cleanEnv(), env...)
	out, err := cmd.Output()
	must(t, err)
	fields := strings.Fields(string(out))
	if len(fields) != 3 {
		t.Fatalf("tmux metadata: %q", out)
	}
	s := &tmuxFixture{fields[0], fields[1], fields[2], env}
	s.env = append(append([]string{}, env...), "TMUX="+s.socket+","+s.pid+",0", "TMUX_PANE="+s.pane, "SUNSTACK_TMUX_SOCKET="+s.socket)
	// Prevent user shell startup files from influencing spawned panes.
	s.tmux(t, "set-option", "-g", "default-shell", "/bin/sh")
	s.tmux(t, "set-option", "-g", "default-command", "/bin/sh")
	return s
}

func (s *tmuxFixture) tmux(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("tmux", append([]string{"-S", s.socket}, args...)...)
	cmd.Env = append(cleanEnv(), s.env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("private tmux %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func eventually(t *testing.T, label string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out: " + label)
}

func (s *tmuxFixture) start(t *testing.T, tool, mode string) {
	t.Helper()
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "SUNSTACK_TEST_MODE="+mode+" "+tool)
	s.tmux(t, "send-keys", "-t", s.pane, "Enter")
	eventually(t, "stand-in input box", func() bool {
		screen := s.tmux(t, "capture-pane", "-p", "-t", s.pane)
		if mode == "menu" {
			return strings.Contains(screen, "Enter to confirm")
		}
		return core.ReadyForInput(tool, screen)
	})
}

type launchEvent struct {
	PID  int
	Cwd  string
	Args []string
}

func launches(t *testing.T, env []string) []launchEvent {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(envValue(env, "SUNSTACK_TEST_EVENTS"), "launch-*.json"))
	must(t, err)
	var out []launchEvent
	for _, path := range files {
		b, err := os.ReadFile(path)
		must(t, err)
		var e launchEvent
		must(t, json.Unmarshal(b, &e))
		out = append(out, e)
	}
	return out
}

func submitted(t *testing.T, env []string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(envValue(env, "SUNSTACK_TEST_EVENTS"), "input-*.jsonl"))
	must(t, err)
	var out []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		must(t, err)
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var e struct{ Line string }
			must(t, json.Unmarshal([]byte(line), &e))
			out = append(out, e.Line)
		}
	}
	return out
}

func claudeList(t *testing.T, env []string, entries ...map[string]any) {
	t.Helper()
	if entries == nil {
		entries = []map[string]any{}
	}
	b, err := json.Marshal(entries)
	must(t, err)
	must(t, os.WriteFile(envValue(env, "SUNSTACK_CLAUDE_AGENTS"), b, 0o600))
}

func privateSession(t *testing.T, s *tmuxFixture, sid, cwd string) map[string]any {
	t.Helper()
	events := launches(t, s.env)
	if len(events) != 1 {
		t.Fatalf("want one stand-in launch, got %v", events)
	}
	tty := strings.TrimPrefix(s.tmux(t, "display-message", "-p", "-t", s.pane, "#{pane_tty}"), "/dev/")
	// Fixtures supply process->TTY discovery; the nudge still checks the real
	// foreground process on this private terminal via ps.
	mapping, err := json.Marshal(map[string]string{strconv.Itoa(events[0].PID): tty})
	must(t, err)
	s.env = append(s.env, "SUNSTACK_TEST_PROCESS_TTYS="+string(mapping))
	return map[string]any{"pid": events[0].PID, "sessionId": sid, "cwd": cwd, "kind": "interactive", "status": "idle"}
}

func requireContains(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestAgentPaneNudge(t *testing.T) {
	for _, tool := range []string{"claude", "codex"} {
		for _, mode := range []string{"idle", "menu", "draft"} {
			t.Run(tool+"/"+mode, func(t *testing.T) {
				p := fixture(t)
				s := privateTmux(t, p, isolatedEnv(t))
				s.start(t, tool, mode)
				claim := sh(t, p, s.env, "as", "reviewer", "--tool", tool)
				expect(t, claim, 0, "claim private pane")
				r := sh(t, p, s.env, "send", "reviewer", "please check")
				expect(t, r, 0, "send")
				tok := field(tokenRe, claim.out)
				expect(t, sh(t, p, s.env, "check", "reviewer", "--token", tok), 0, "inbox")
				requireContains(t, sh(t, p, s.env, "check", "reviewer", "--token", tok).out, "please check")
				if mode == "idle" {
					requireContains(t, r.out, "nudged session reviewer")
					eventually(t, "nudge submitted with Enter", func() bool { return len(submitted(t, s.env)) == 1 })
					want := core.NudgePrompt(tool, &core.Message{Type: "fyi"})
					if got := submitted(t, s.env); len(got) != 1 || got[0] != want {
						t.Errorf("submitted %v, want %q", got, want)
					}
				} else {
					requireContains(t, r.out, "not nudged")
					eventually(t, "screen preserved", func() bool {
						screen := s.tmux(t, "capture-pane", "-p", "-t", s.pane)
						if mode == "draft" {
							cursor := "❯"
							if tool == "codex" {
								cursor = "›"
							}
							return strings.Contains(screen, "\n"+cursor+" my unsent draft\n") && !strings.Contains(screen, "sunstack:check")
						}
						return strings.Contains(screen, "Enter to confirm") && !strings.Contains(screen, "sunstack:check")
					})
					if got := submitted(t, s.env); len(got) != 0 {
						t.Errorf("unsafe Enter: %v", got)
					}
					if mode == "menu" {
						files, err := filepath.Glob(filepath.Join(envValue(s.env, "SUNSTACK_TEST_EVENTS"), "keys-*.txt"))
						must(t, err)
						if len(files) != 1 {
							t.Fatalf("missing keyboard log: %v", files)
						}
						keys, err := os.ReadFile(files[0])
						must(t, err)
						if len(keys) != 0 {
							t.Errorf("typed into a menu: %q", keys)
						}
					}
				}
			})
		}
	}
}

func TestFreeSessionPaneNudge(t *testing.T) {
	p := t.TempDir()
	sid := "00000000-0000-4000-8000-000000000001"
	s := privateTmux(t, p, isolatedEnv(t))
	s.start(t, "claude", "idle")
	entry := privateSession(t, s, sid, p)
	claudeList(t, s.env, entry)
	r := sh(t, p, s.env, "send", sid, "host inbox ping")
	expect(t, r, 0, "send to free session")
	requireContains(t, r.out, "nudged session claude "+s.pane)
	eventually(t, "host nudge submitted", func() bool { return len(submitted(t, s.env)) == 1 })
	if got := submitted(t, s.env); got[0] != core.NudgePrompt("claude", &core.Message{Type: "fyi"}) {
		t.Errorf("nudge %v", got)
	}
	me := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	requireContains(t, sh(t, p, me, "check", "--session").out, "host inbox ping")
}

var paneRe = regexp.MustCompile(`\bpane (%\d+)`)

func TestSpawnPaneLifecycle(t *testing.T) {
	for _, window := range []bool{false, true} {
		t.Run(fmt.Sprintf("window=%t", window), func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			args := []string{"spawn", "reviewer", "--task", "tests", "--tool", "claude", "--note", "check this fixture", "--place", "here"}
			if window {
				args = append(args, "--window")
			}
			r := sh(t, p, s.env, args...)
			expect(t, r, 0, "spawn")
			pane := field(paneRe, r.out)
			if pane == "" || pane == s.pane {
				t.Fatalf("spawn output: %s", r.out)
			}
			if strings.Contains(r.out, "warning:") {
				t.Errorf("stand-in not recognized: %s", r.out)
			}
			callerWindow := s.tmux(t, "display-message", "-p", "-t", s.pane, "#{window_id}")
			childWindow := s.tmux(t, "display-message", "-p", "-t", pane, "#{window_id}")
			if (callerWindow != childWindow) != window {
				t.Errorf("wrong split/window: caller %s child %s", callerWindow, childWindow)
			}
			events := launches(t, s.env)
			if len(events) != 1 {
				t.Fatalf("launches %v", events)
			}
			root, _ := filepath.EvalSymlinks(p)
			if events[0].Cwd != root {
				t.Errorf("spawn cwd %q, want %q", events[0].Cwd, root)
			}
			requireContains(t, strings.Join(events[0].Args, " "), "/sunstack:as reviewer --token ", "--task tests", "check this fixture")
			r = sh(t, p, s.env, args...)
			expect(t, r, 4, "task_taken")
			requireContains(t, r.stderr, "task_taken")
			panes := strings.Fields(s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}"))
			if len(panes) != 2 {
				t.Errorf("duplicate spawn leaked pane: %v", panes)
			}
			own := append(append([]string{}, s.env...), "TMUX_PANE="+pane)
			r = sh(t, p, own, "kill", "reviewer_tests", "--yes")
			expect(t, r, 2, "refuse own pane")
			requireContains(t, r.stderr, "this pane")
			r = sh(t, p, s.env, "dismiss", "reviewer_tests")
			expect(t, r, 0, "dismiss")
			requireContains(t, r.out, "asked reviewer_tests to finish")
			eventually(t, "shutdown nudge Enter", func() bool { return len(submitted(t, s.env)) == 1 })
			if got := submitted(t, s.env); got[0] != core.NudgePrompt("claude", &core.Message{Type: "shutdown"}) {
				t.Errorf("shutdown nudge %v", got)
			}
			// Take the shutdown message, then ensure kill requeues it.
			claims, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "live", "reviewer", "*.json"))
			if len(claims) != 1 {
				t.Fatalf("claims %v", claims)
			}
			b, err := os.ReadFile(claims[0])
			must(t, err)
			var c core.Live
			must(t, json.Unmarshal(b, &c))
			check := sh(t, p, s.env, "check", "reviewer", "--token", c.Token)
			id := field(regexp.MustCompile(`(?m)^===== (\S+) \(pending\) =====$`), check.out)
			if id == "" {
				t.Fatalf("check output %s", check.out)
			}
			expect(t, sh(t, p, s.env, "take", "reviewer", id, "--token", c.Token), 0, "take shutdown")
			expect(t, sh(t, p, s.env, "kill", "reviewer_tests", "--yes"), 0, "kill")
			if got := strings.Fields(s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}")); len(got) != 1 || got[0] != s.pane {
				t.Errorf("kill pane list %v", got)
			}
			if _, err := os.Stat(claims[0]); !os.IsNotExist(err) {
				t.Errorf("claim not removed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(p, "sunstack", "_local", "inbox", "reviewer", id+".md")); err != nil {
				t.Errorf("taken message not requeued: %v", err)
			}
		})
	}
}

func TestSpawnNotRunningWarning(t *testing.T) {
	env := append(isolatedEnv(t), "SUNSTACK_TEST_MODE=exit")
	p := fixture(t)
	s := privateTmux(t, p, env)
	r := sh(t, p, s.env, "spawn", "reviewer", "--task", "exited", "--tool", "codex")
	expect(t, r, 0, "exited stand-in")
	requireContains(t, r.out, "warning: codex is not running", "sunstack kill reviewer_exited")
	expect(t, sh(t, p, s.env, "kill", "reviewer_exited", "--yes"), 0, "marked pane can be killed after tool exits")
}

func transcript(t *testing.T, env []string, tool, sid, cwd string) string {
	t.Helper()
	home := envValue(env, "HOME")
	var path, line string
	if tool == "claude" {
		path = filepath.Join(home, ".claude", "projects", "fixture", sid+".jsonl")
		b, err := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "message": map[string]any{"role": "user", "content": "hello"}})
		must(t, err)
		line = string(b)
	} else {
		path = filepath.Join(home, ".codex", "sessions", "2026", "10", "01", "rollout-fixture-"+sid+".jsonl")
		b, err := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sid, "cwd": cwd, "source": "cli"}})
		must(t, err)
		line = string(b)
	}
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(line+"\n"), 0o600))
	return path
}

func TestReopenPrivatePane(t *testing.T) {
	for _, tool := range []string{"claude", "codex"} {
		t.Run(tool, func(t *testing.T) {
			p := fixture(t)
			env := isolatedEnv(t)
			sid := "00000000-0000-4000-8000-000000000010"
			cwd := filepath.Join(p, "transcript folder")
			must(t, os.MkdirAll(cwd, 0o755))
			transcript(t, env, tool, sid, cwd)
			caller := privateTmux(t, p, env)
			asEnv := append(append([]string{}, env...), "CODEX_THREAD_ID="+sid)
			if tool == "claude" {
				asEnv = append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
			}
			claim := sh(t, p, asEnv, "as", "reviewer", "--tool", tool)
			expect(t, claim, 0, "claim outside tmux")
			tok := field(tokenRe, claim.out)
			r := sh(t, p, caller.env, "reopen", sid)
			expect(t, r, 0, "reopen")
			requireContains(t, r.out, "reopened "+tool+" session "+sid, cwd)
			pane := field(paneRe, r.out)
			if pane == "" || pane == caller.pane {
				t.Fatalf("reopen output %s", r.out)
			}
			eventually(t, "resume command reached stand-in", func() bool { return len(launches(t, env)) == 1 })
			event := launches(t, env)[0]
			actualCwd, _ := filepath.EvalSymlinks(cwd)
			launchCwd, err := filepath.EvalSymlinks(event.Cwd)
			must(t, err)
			if launchCwd != actualCwd {
				t.Errorf("resume cwd %q, want %q", event.Cwd, actualCwd)
			}
			want := "--resume " + sid
			if tool == "codex" {
				want = "resume " + sid
			}
			if strings.Join(event.Args, " ") != want {
				t.Errorf("resume args %v, want %s", event.Args, want)
			}
			b, err := os.ReadFile(filepath.Join(p, "sunstack", "_local", "live", "reviewer", tok+".json"))
			must(t, err)
			var c core.Live
			must(t, json.Unmarshal(b, &c))
			if c.Token != tok || c.Session != sid || c.TmuxPane != pane || c.TmuxSocket != caller.socket || c.TmuxServer != caller.pid {
				t.Errorf("claim did not move to reopened pane")
			}
			if got := caller.tmux(t, "display-message", "-p", "-t", pane, "#{pane_title}"); got != "reviewer" {
				t.Errorf("pane title %q", got)
			}
		})
	}
}

func TestReopenScanRefusals(t *testing.T) {
	for _, mode := range []string{"running", "failed"} {
		t.Run(mode, func(t *testing.T) {
			p := fixture(t)
			env := isolatedEnv(t)
			sid := "00000000-0000-4000-8000-000000000011"
			transcript(t, env, "claude", sid, p)
			s := privateTmux(t, p, env)
			if mode == "running" {
				claudeList(t, env, map[string]any{"sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})
			} else {
				must(t, os.WriteFile(envValue(env, "SUNSTACK_CLAUDE_AGENTS"), []byte("{bad JSON"), 0o600))
			}
			r := sh(t, p, s.env, "reopen", sid)
			expect(t, r, 1, "refuse reopen")
			if mode == "running" {
				requireContains(t, r.stderr, "still open")
			} else {
				requireContains(t, r.stderr, "scan_failed", "could not check")
			}
			if panes := strings.Fields(s.tmux(t, "list-panes", "-a", "-F", "#{pane_id}")); len(panes) != 1 {
				t.Errorf("refusal created a pane: %v", panes)
			}
			if len(launches(t, env)) != 0 {
				t.Error("refused reopen started a CLI")
			}
		})
	}
}

func readOrg(t *testing.T, p string, env []string) core.Org {
	t.Helper()
	r := sh(t, p, env, "org", "--json")
	expect(t, r, 0, "org json")
	var o core.Org
	must(t, json.Unmarshal([]byte(r.out), &o))
	return o
}

func orgSession(o core.Org, sid string) *core.HostSession {
	for _, team := range o.Teams {
		for _, s := range team.Free {
			if s.SessionID == sid {
				return s
			}
		}
		for _, a := range team.Agents {
			for _, s := range a.Sessions {
				if s.SessionID == sid {
					return s
				}
			}
		}
	}
	for _, group := range o.Free {
		for _, s := range group.Sessions {
			if s.SessionID == sid {
				return s
			}
		}
	}
	return nil
}

func TestClaimDoesNotMatchOtherServer(t *testing.T) {
	checkClaimOtherServer(t, false)
}

func TestLegacyClaimDoesNotMatchOtherServer(t *testing.T) {
	checkClaimOtherServer(t, true)
}

func checkClaimOtherServer(t *testing.T, legacy bool) {
	t.Helper()
	p := fixture(t)
	env := isolatedEnv(t)
	a := privateTmux(t, p, env)
	claim := sh(t, p, a.env, "as", "reviewer", "--tool", "claude")
	expect(t, claim, 0, "claim on server A")
	if legacy {
		// Older releases persisted the socket, but did not record the PID.
		token := field(tokenRe, claim.out)
		path := filepath.Join(p, "sunstack", "_local", "live", "reviewer", token+".json")
		data, err := os.ReadFile(path)
		must(t, err)
		var c core.Live
		must(t, json.Unmarshal(data, &c))
		c.TmuxServer = ""
		data, err = json.Marshal(c)
		must(t, err)
		must(t, os.WriteFile(path, data, 0o600))
	}
	b := privateTmux(t, p, isolatedEnv(t))
	b.start(t, "claude", "idle")
	if a.pane != b.pane {
		t.Fatalf("fixture needs identical pane IDs: %s %s", a.pane, b.pane)
	}
	sid := "00000000-0000-4000-8000-000000000020"
	entry := privateSession(t, b, sid, p)
	claudeList(t, env, entry)
	scanEnv := append(append([]string{}, a.env...), "SUNSTACK_TEST_DEFAULT_SOCKET="+b.socket, "SUNSTACK_TMUX_SOCKET="+b.socket, "SUNSTACK_TEST_PROCESS_TTYS="+envValue(b.env, "SUNSTACK_TEST_PROCESS_TTYS"))
	s := orgSession(readOrg(t, p, scanEnv), sid)
	if s == nil || s.Agent != "" || s.Pane != b.pane {
		t.Fatalf("server B session should be free: %+v", s)
	}
}

func TestStaleClaimDoesNotWinOverFreeSession(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	s := privateTmux(t, p, env)
	s.start(t, "claude", "idle")
	old := "00000000-0000-4000-8000-000000000021"
	live := "00000000-0000-4000-8000-000000000022"
	claimEnv := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+old)
	expect(t, sh(t, p, claimEnv, "as", "reviewer"), 0, "stale session claim")
	entry := privateSession(t, s, live, p)
	claudeList(t, env, entry)
	session := orgSession(readOrg(t, p, s.env), live)
	if session == nil || session.Agent != "" {
		t.Fatalf("stale session ID captured a live free session: %+v", session)
	}
}

func TestPaneRecipientStaysOnCallerServer(t *testing.T) {
	p := t.TempDir()
	a := privateTmux(t, p, isolatedEnv(t))
	a.start(t, "claude", "idle")
	b := privateTmux(t, p, isolatedEnv(t))
	b.start(t, "claude", "idle")
	if a.pane != b.pane {
		t.Fatalf("fixture pane numbers differ: %s %s", a.pane, b.pane)
	}
	sidA := "00000000-0000-4000-8000-000000000023"
	sidB := "00000000-0000-4000-8000-000000000024"
	ea := privateSession(t, a, sidA, p)
	eb := privateSession(t, b, sidB, p)
	mapping, _ := json.Marshal(map[string]string{strconv.Itoa(ea["pid"].(int)): a.tmux(t, "display-message", "-p", "-t", a.pane, "#{pane_tty}"), strconv.Itoa(eb["pid"].(int)): b.tmux(t, "display-message", "-p", "-t", b.pane, "#{pane_tty}")})
	claudeList(t, a.env, ea, eb)
	env := append(append([]string{}, a.env...), "SUNSTACK_TEST_DEFAULT_SOCKET="+b.socket, "SUNSTACK_TMUX_SOCKET="+b.socket, "SUNSTACK_TEST_PROCESS_TTYS="+string(mapping))
	r := sh(t, p, env, "send", a.pane, "only server A", "--no-nudge")
	expect(t, r, 0, "send pane")
	home := envValue(env, "SUNSTACK_HOME")
	if files, _ := filepath.Glob(filepath.Join(home, "inbox", sidA, "*.md")); len(files) != 1 {
		t.Errorf("caller inbox %v", files)
	}
	if files, _ := filepath.Glob(filepath.Join(home, "inbox", sidB, "*.md")); len(files) != 0 {
		t.Errorf("other server got message: %v", files)
	}
	// Also test B as caller, sharing the exact same host scan and inbox.
	env = append(env, "TMUX="+b.socket+","+b.pid+",0", "TMUX_PANE="+b.pane, "SUNSTACK_TEST_DEFAULT_SOCKET="+a.socket, "SUNSTACK_TMUX_SOCKET="+a.socket)
	expect(t, sh(t, p, env, "send", b.pane, "only server B", "--no-nudge"), 0, "send pane from B")
	if files, _ := filepath.Glob(filepath.Join(home, "inbox", sidB, "*.md")); len(files) != 1 {
		t.Errorf("B caller inbox %v", files)
	}
}

func TestCodexScanWithFakeProcessTools(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		t.Skip("lsof discovery branch is used on Unix without /proc")
	}
	for _, failing := range []bool{false, true} {
		t.Run(fmt.Sprintf("lsof-fails=%t", failing), func(t *testing.T) {
			p := t.TempDir()
			env := isolatedEnv(t)
			sid := "00000000-0000-4000-8000-000000000030"
			path := transcript(t, env, "codex", sid, p)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			must(t, err)
			_, err = f.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"check the fake rollout\"}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n")
			must(t, err)
			must(t, f.Close())
			env = append(env, "SUNSTACK_CODEX_SCAN=on", "SUNSTACK_TEST_SCAN_PID=4242", "SUNSTACK_TEST_SCAN_CWD="+p, "SUNSTACK_TEST_ROLLOUT="+path)
			if failing {
				env = append(env, "SUNSTACK_TEST_LSOF_FAIL=1")
			}
			o := readOrg(t, p, env)
			if failing {
				requireContains(t, strings.Join(o.Notes, "\n"), "Codex sessions not listed", "could not inspect codex process(es) 4242")
				if orgSession(o, sid) != nil {
					t.Error("failed lsof fabricated a session")
				}
			} else {
				s := orgSession(o, sid)
				if s == nil || s.Tool != "codex" || s.Status != "idle" || s.Activity.LastPrompt != "check the fake rollout" || s.Cwd != p {
					t.Errorf("codex process fixture: %+v", s)
				}
				if len(o.Notes) != 0 {
					t.Errorf("unexpected scan notes %v", o.Notes)
				}
			}
		})
	}
}

func runHook(t *testing.T, env []string, event, source, sid, cwd string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"hook_event_name": event, "source": source, "session_id": sid, "cwd": cwd})
	must(t, err)
	cmd := exec.Command(bin, "hook")
	cmd.Env = append(cleanEnv(), env...)
	cmd.Stdin = strings.NewReader(string(b))
	out, err := cmd.CombinedOutput()
	must(t, err)
	return string(out)
}

func hookContextText(t *testing.T, out, event string) string {
	t.Helper()
	// Use the wire keys explicitly, so malformed JSON/incorrect event fails.
	var wire struct {
		HookSpecificOutput struct {
			HookEventName     string
			AdditionalContext string
		}
	}
	must(t, json.Unmarshal([]byte(out), &wire))
	if wire.HookSpecificOutput.HookEventName != event {
		t.Errorf("hook event %q, want %s", wire.HookSpecificOutput.HookEventName, event)
	}
	return wire.HookSpecificOutput.AdditionalContext
}

func TestSessionStartHooks(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	s := privateTmux(t, p, env)
	old := "00000000-0000-4000-8000-000000000040"
	claimed := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+old)
	r := sh(t, p, claimed, "as", "reviewer", "--task", "hooks")
	expect(t, r, 0, "claim")
	tok := field(tokenRe, r.out)
	for _, source := range []string{"compact", "clear", "resume"} {
		t.Run(source, func(t *testing.T) {
			sid := old
			if source == "clear" {
				sid = "00000000-0000-4000-8000-000000000041"
			}
			out := runHook(t, s.env, "SessionStart", source, sid, p)
			ctx := hookContextText(t, out, "SessionStart")
			requireContains(t, ctx, "reviewer", tok, "session_name: reviewer_hooks", "reload the identity files")
			if source == "clear" {
				requireContains(t, ctx, "claimed from a different CLI session ID")
			}
		})
	}
	if out := runHook(t, env, "SessionStart", "resume", "unclaimed", p); out != "" {
		t.Errorf("unclaimed hook output %q", out)
	}
}

func TestPromptHooksBothInboxes(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	sid := "00000000-0000-4000-8000-000000000042"
	asEnv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	claim := sh(t, p, asEnv, "as", "reviewer")
	expect(t, claim, 0, "claim")
	if out := runHook(t, env, "UserPromptSubmit", "", sid, p); out != "" {
		t.Errorf("empty inbox printed %q", out)
	}
	expect(t, sh(t, p, env, "send", "reviewer", "agent inbox", "--no-nudge"), 0, "agent message")
	// Deliver while the session is still free to create a genuine host inbox
	// entry, then restore the claim before invoking the combined hook.
	tok := field(tokenRe, claim.out)
	expect(t, sh(t, p, env, "release", "reviewer", "--token", tok), 0, "temporarily free session")
	claudeList(t, env, map[string]any{"sessionId": sid, "cwd": p, "kind": "interactive", "status": "idle"})
	expect(t, sh(t, p, env, "send", sid, "host inbox", "--no-nudge"), 0, "host message")
	expect(t, sh(t, p, asEnv, "as", "reviewer"), 0, "claim again")
	out := runHook(t, env, "UserPromptSubmit", "", sid, p)
	ctx := hookContextText(t, out, "UserPromptSubmit")
	requireContains(t, ctx, "reviewer", "1 message(s) for this session itself", "sunstack check --session")
	// Outside any team the hook still reports the host inbox alone.
	out = runHook(t, env, "UserPromptSubmit", "", sid, t.TempDir())
	requireContains(t, hookContextText(t, out, "UserPromptSubmit"), "1 message(s) are waiting for this session")
	if out := runHook(t, env, "UserPromptSubmit", "", "00000000-0000-4000-8000-000000000043", t.TempDir()); out != "" {
		t.Errorf("empty free session printed %q", out)
	}
}

func TestOldTeamMigrationSequence(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	// fixture is intentionally older: no TEAM/BOARD, old protocol and an
	// unnamed reviewer. Preserve its identity and context throughout migration.
	agentPath := filepath.Join(p, "sunstack", "reviewer", "AGENT.md")
	contextPath := filepath.Join(p, "sunstack", "reviewer", "context.md")
	agent, err := os.ReadFile(agentPath)
	must(t, err)
	context, err := os.ReadFile(contextPath)
	must(t, err)
	oldProto, err := os.ReadFile(filepath.Join(p, "sunstack", "PROTOCOL.md"))
	must(t, err)
	r := sh(t, p, env, "migrate")
	expect(t, r, 0, "migration plan")
	requireContains(t, r.out, "[auto] add sunstack/TEAM", "[auto] create sunstack/BOARD.md", "[ask ] refresh sunstack/PROTOCOL.md")
	if _, err := os.Stat(filepath.Join(p, "sunstack", "TEAM")); !os.IsNotExist(err) {
		t.Error("plan mutated old layout")
	}
	expect(t, sh(t, p, env, "migrate", "--apply", "safe"), 0, "safe migration")
	for _, name := range []string{"TEAM", "BOARD.md"} {
		if _, err := os.Stat(filepath.Join(p, "sunstack", name)); err != nil {
			t.Errorf("safe did not create %s: %v", name, err)
		}
	}
	proto, err := os.ReadFile(filepath.Join(p, "sunstack", "PROTOCOL.md"))
	must(t, err)
	if string(proto) != string(oldProto) {
		t.Error("safe rewrote old protocol")
	}
	claim := sh(t, p, env, "as", "reviewer")
	expect(t, claim, 0, "old unnamed agent remains usable")
	tok := field(tokenRe, claim.out)
	for _, result := range []result{claim, sh(t, p, env, "as", "reviewer", "--token", tok)} {
		expect(t, result, 0, "as old team")
		if n := strings.Count(result.out, "===== migration ("); n != 1 {
			t.Errorf("migration section count %d, want exactly one", n)
		}
		if n := strings.Count(result.out, "needs the user's OK: refresh sunstack/PROTOCOL.md"); n != 1 {
			t.Errorf("protocol ask count %d", n)
		}
	}
	expect(t, sh(t, p, env, "migrate", "--apply", "all"), 0, "all migration")
	r = sh(t, p, env, "migrate")
	expect(t, r, 0, "migrated plan")
	requireContains(t, r.out, "up to date")
	r = sh(t, p, env, "as", "reviewer", "--token", tok)
	expect(t, r, 0, "as migrated")
	if strings.Contains(r.out, "===== migration (") {
		t.Error("migration section persists after all steps")
	}
	for path, want := range map[string][]byte{agentPath: agent, contextPath: context} {
		b, err := os.ReadFile(path)
		must(t, err)
		if string(b) != string(want) {
			t.Errorf("migration altered %s", path)
		}
	}
	proto, err = os.ReadFile(filepath.Join(p, "sunstack", "PROTOCOL.md"))
	must(t, err)
	if string(proto) == string(oldProto) {
		t.Error("all migration did not refresh protocol")
	}
}

// Without --place, a session goes to its team's home on the default tmux
// server: one tmux session per team, one window per agent (§20.1).
func TestSpawnTeamHome(t *testing.T) {
	p := fixture(t)
	must(t, os.WriteFile(filepath.Join(p, "sunstack", "TEAM"), []byte("id: a1b2000000000000\nname: alpha.team\n"), 0o644))
	home := privateTmux(t, p, isolatedEnv(t))   // stands in for the default server
	caller := privateTmux(t, p, isolatedEnv(t)) // the caller's own server
	env := append(append([]string{}, caller.env...), "SUNSTACK_TMUX_SOCKET="+home.socket)
	where := func(pane string) string {
		return home.tmux(t, "display-message", "-p", "-t", pane, "#{session_name}\t#{window_name}\t#{window_id}")
	}

	r := sh(t, p, env, "spawn", "reviewer", "--task", "one", "--tool", "claude")
	expect(t, r, 0, "spawn into the home")
	requireContains(t, r.out, "ss-alpha-team-a1b2")
	one := field(paneRe, r.out)
	w1 := strings.Split(where(one), "\t")
	if w1[0] != "ss-alpha-team-a1b2" || w1[1] != "reviewer" {
		t.Fatalf("spawned into %v", w1)
	}
	if o := home.tmux(t, "show-options", "-v", "-t", "ss-alpha-team-a1b2", "@sunstack_home"); o != "a1b2000000000000" {
		t.Errorf("home owner %q", o)
	}
	if n := len(strings.Fields(caller.tmux(t, "list-panes", "-a", "-F", "#{pane_id}"))); n != 1 {
		t.Errorf("the caller's server got %d panes", n)
	}

	// A second task of the agent splits its window; another agent gets its own.
	r = sh(t, p, env, "spawn", "reviewer", "--task", "two", "--tool", "claude")
	expect(t, r, 0, "second task")
	if w := strings.Split(where(field(paneRe, r.out)), "\t"); w[2] != w1[2] {
		t.Errorf("second task in %v, want window %s", w, w1[2])
	}
	r = sh(t, p, env, "spawn", "builder.alice", "--tool", "codex")
	expect(t, r, 0, "another agent")
	if w := strings.Split(where(field(paneRe, r.out)), "\t"); w[0] != "ss-alpha-team-a1b2" || w[1] != "builder.alice" {
		t.Errorf("builder.alice in %v", w)
	}

	// Outside tmux it still goes home.
	var plain []string
	for _, e := range env {
		if !strings.HasPrefix(e, "TMUX=") && !strings.HasPrefix(e, "TMUX_PANE=") {
			plain = append(plain, e)
		}
	}
	r = sh(t, p, plain, "spawn", "builder.bob", "--tool", "claude")
	expect(t, r, 0, "spawn outside tmux")
	if w := strings.Split(where(field(paneRe, r.out)), "\t"); w[1] != "builder.bob" {
		t.Errorf("builder.bob in %v", w)
	}

	// The claim names the home's server, so kill finds the pane.
	expect(t, sh(t, p, env, "kill", "reviewer_two", "--yes"), 0, "kill a session in its home")

	// A tmux session with a home's name that sunstack did not make is refused.
	q := fixture(t)
	must(t, os.WriteFile(filepath.Join(q, "sunstack", "TEAM"), []byte("id: c3d4000000000000\nname: beta\n"), 0o644))
	home.tmux(t, "new-session", "-d", "-s", "ss-beta-c3d4")
	r = sh(t, q, env, "spawn", "reviewer", "--tool", "claude")
	expect(t, r, 1, "unowned home")
	requireContains(t, r.stderr, "home_taken")
}

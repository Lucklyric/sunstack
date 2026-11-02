package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func review172cEditClaim(t *testing.T, p string, f func(map[string]any)) map[string]any {
	t.Helper()
	var found map[string]any
	err := filepath.WalkDir(filepath.Join(p, "sunstack", "_local"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var v map[string]any
		if json.Unmarshal(b, &v) == nil && v["claimed"] != nil && v["token"] != nil {
			f(v)
			found = v
			b, _ = json.Marshal(v)
			return os.WriteFile(path, b, 0600)
		}
		return nil
	})
	must(t, err)
	if found == nil {
		t.Fatal("claim not found")
	}
	return found
}

func TestReview172cPeekLegacySameSecond(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "claude", "idle")
	soon(t, "peer pinned", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "grant peek")
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000031")
	target := time.Now().Truncate(time.Second).Add(time.Second + 10*time.Millisecond)
	time.Sleep(time.Until(target))
	r := sh(t, p, claim, "as", "builder.alice")
	expect(t, r, 0, "claim")
	old := review172cEditClaim(t, p, func(v map[string]any) { delete(v, "cli_pid") })
	before := launches(t, s.env)
	s.tmux(t, "respawn-pane", "-k", "-t", s.pane, "/bin/sh")
	s.start(t, "claude", "idle")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "B3-LEGACY-REPLACEMENT-SECRET")
	after := launches(t, s.env)
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "10")
	t.Logf("legacy claim=%v launches=%v -> %v peek exit=%d out=%q err=%q", old, before, after, r.code, r.out, r.stderr)
	if r.code == 0 && strings.Contains(r.out, "B3-LEGACY-REPLACEMENT-SECRET") {
		t.Error("legacy claim exposed a replacement CLI")
	}
}

func TestReview172cPeekInspectionFailure(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "fail")
	ps := "#!/bin/sh\ncase \"$*\" in *stat=,pid=,lstart=,comm=*) if test -f '" + flag + "'; then exit 1; fi;; esac\nexec '" + filepath.Join(testTools, "ps") + "' \"$@\"\n"
	must(t, os.WriteFile(filepath.Join(dir, "ps"), []byte(ps), 0700))
	path := "PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events, path})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket, path}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "claude", "idle")
	soon(t, "peer pinned", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "grant peek")
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000032")
	r := sh(t, p, claim, "as", "builder.alice")
	expect(t, r, 0, "claim")
	old := review172cEditClaim(t, p, func(v map[string]any) {})
	if old["cli_pid"] == nil {
		t.Fatal("claim did not bind PID")
	}
	s.tmux(t, "respawn-pane", "-k", "-t", s.pane, "/bin/sh")
	s.start(t, "claude", "idle")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "B3-INSPECTION-FAILURE-SECRET")
	must(t, os.WriteFile(flag, []byte("fail extended ps only"), 0600))
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "10")
	t.Logf("claimPID=%v extended ps fails; peek exit=%d out=%q err=%q", old["cli_pid"], r.code, r.out, r.stderr)
	if r.code == 0 && strings.Contains(r.out, "B3-INSPECTION-FAILURE-SECRET") {
		t.Error("failed identity check allowed replacement capture")
	}
}

func TestReview172cCodexClaimResumePeek(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "codex", "idle")
	soon(t, "peer pinned", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "grant peek")
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CODEX_THREAD_ID=00000000-0000-4000-8000-000000000033")
	r := sh(t, p, claim, "as", "builder.alice", "--tool", "codex")
	expect(t, r, 0, "claim")
	token := field(tokenRe, r.out)
	old := review172cEditClaim(t, p, func(v map[string]any) {})
	if old["cli_pid"] == nil {
		t.Fatal("Codex claim has no PID")
	}
	expect(t, laptop.run(t, "peek", "hub-host:shop/builder.alice"), 0, "peek Codex original")
	s.tmux(t, "respawn-pane", "-k", "-t", s.pane, "/bin/sh")
	s.start(t, "codex", "idle")
	expect(t, sh(t, p, claim, "as", "builder.alice", "--tool", "codex", "--token", token), 0, "resume")
	newClaim := review172cEditClaim(t, p, func(v map[string]any) {})
	if old["cli_pid"] == newClaim["cli_pid"] {
		t.Fatal("resume did not change PID")
	}
	time.Sleep(2100 * time.Millisecond)
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice")
	expect(t, r, 0, "peek resumed Codex")
	t.Logf("Codex PID %v -> %v; resumed peek=%d", old["cli_pid"], newClaim["cli_pid"], r.code)
}

func TestReview172cMovedRecoveryFailedScanOldPaneGone(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	r := sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "claim")
	token := field(tokenRe, r.out)
	s.tmux(t, "kill-pane", "-t", panes[0])
	claudeList(t, s.env, entries[1])
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	expect(t, r, 0, "moved recovery with complete scan")
	requireContains(t, r.out, token)
	must(t, os.WriteFile(envValue(s.env, "SUNSTACK_CLAUDE_AGENTS"), []byte("invalid json"), 0600))
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	t.Logf("old pane closed, scanner fails: exit=%d tokenRecovered=%v err=%q", r.code, strings.Contains(r.out, token), r.stderr)
	if r.code != 0 {
		t.Error("scan failure blocks recovery even though claimed pane is demonstrably gone")
	}
}

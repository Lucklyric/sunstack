package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReview172bPeekSameSecondReplacement(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, addr, hosts := newOrgEnv(t, []string{"SUNSTACK_TMUX_SOCKET=" + s.socket}, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	p := hubHost.team(t, "shop")
	s.start(t, "claude", "idle")
	soon(t, "laptop pinned", func() bool { return strings.Contains(hubHost.run(t, "org", "keys").out, "laptop") })
	expect(t, hubHost.run(t, "org", "allow", "laptop", "peek"), 0, "grant peek")
	claim := append(append(append([]string{}, hubHost.env...), s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000031")
	// Make the real claim and replacement early in the same wall-clock second.
	target := time.Now().Truncate(time.Second).Add(time.Second + 10*time.Millisecond)
	time.Sleep(time.Until(target))
	r := sh(t, p, claim, "as", "builder.alice")
	expect(t, r, 0, "claim original process")
	before := launches(t, s.env)
	s.tmux(t, "respawn-pane", "-k", "-t", s.pane, "/bin/sh")
	s.start(t, "claude", "idle")
	s.tmux(t, "send-keys", "-t", s.pane, "-l", "SAME-SECOND-REPLACEMENT-SECRET")
	after := launches(t, s.env)
	if len(after) <= len(before) {
		t.Fatal("replacement did not start")
	}
	// Record the real claim timestamp and native ps timestamp.
	var files []string
	filepath.WalkDir(filepath.Join(p, "sunstack", "_local"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), `"claimed"`) {
				files = append(files, path)
				t.Logf("claim state=%s", b)
			}
		}
		return nil
	})
	_ = files
	pid := after[len(after)-1].PID
	pc := exec.Command("/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	pc.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C")
	started, _ := pc.Output()
	t.Logf("originalPID=%d replacementPID=%d replacementStart=%q", before[len(before)-1].PID, pid, strings.TrimSpace(string(started)))
	r = laptop.run(t, "peek", "hub-host:shop/builder.alice", "--lines", "10")
	t.Logf("peek exit=%d out=%q err=%q", r.code, r.out, r.stderr)
	if r.code == 0 && strings.Contains(r.out, "SAME-SECOND-REPLACEMENT-SECRET") {
		t.Error("same-second replacement exposed under the original claim")
	}
}
func TestReview172bUserUpdateStillMigrates(t *testing.T) {
	p := fixture(t)
	expect(t, sh(t, p, nil, "init"), 0, "init")
	team := filepath.Join(p, "sunstack", "TEAM")
	must(t, os.Remove(team))
	r := sh(t, p, []string{"SUNSTACK_TEST_MODE=exit"}, "update", "--skip-binary", "--claude")
	expect(t, r, 0, "user update")
	if _, e := os.Stat(team); e != nil {
		t.Errorf("user update no longer migrates: %s", r.out)
	}
}
func TestReview172bLeaveServiceAndRejoin(t *testing.T) {
	hubHost, addr, hosts := newOrg(t, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	expect(t, laptop.run(t, "hub", "connect", "--install"), 0, "write private unloaded service")
	soon(t, "initial roster", func() bool { _, e := os.Stat(laptop.file("remote/roster.json")); return e == nil })
	expect(t, laptop.run(t, "org", "leave"), 0, "leave")
	path := filepath.Join(laptop.home, "Library", "LaunchAgents", "org.sunstack.hub-connect.plist")
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Errorf("private service file remains: %v", e)
	}
	expect(t, hubHost.run(t, "hub", "revoke", "laptop"), 0, "remove old enrollment")
	laptop.join(t, hubHost, addr)
	laptop.connector(t)
	soon(t, "rejoined roster", func() bool { _, e := os.Stat(laptop.file("remote/roster.json")); return e == nil })
}
func TestReview172bFirstKeysProcesses(t *testing.T) {
	h := t.TempDir()
	env := append(isolatedEnv(t), "SUNSTACK_HOME="+filepath.Join(h, ".sunstack"))
	type resultKey struct {
		out []byte
		err error
	}
	ch := make(chan resultKey, 16)
	for i := 0; i < 16; i++ {
		go func() {
			c := exec.Command(bin, "org", "keys")
			c.Env = append(cleanEnv(), env...)
			b, e := c.CombinedOutput()
			ch <- resultKey{b, e}
		}()
	}
	outputs := map[string]bool{}
	busy := 0
	for i := 0; i < 16; i++ {
		r := <-ch
		if r.err != nil {
			if !strings.Contains(string(r.out), "busy:") {
				t.Fatalf("keys child: %v %s", r.err, r.out)
			}
			busy++
			continue
		}
		outputs[string(r.out)] = true
	}
	retry := sh(t, h, env, "org", "keys")
	expect(t, retry, 0, "keys after contention")
	outputs[retry.out] = true
	var raw map[string]any
	b, e := os.ReadFile(filepath.Join(h, ".sunstack", "keys.json"))
	must(t, e)
	must(t, json.Unmarshal(b, &raw))
	t.Logf("16 processes: busy=%d distinct successful key outputs=%d persisted=%v", busy, len(outputs), len(raw) > 0)
	if len(outputs) != 1 {
		t.Error("processes returned different keys")
	}
}
func TestReview172bDuplicateRecoveryFailedScan(t *testing.T) {
	p, s, panes, entries := review172Sessions(t)
	sid := entries[0]["sessionId"].(string)
	r := sh(t, p, review172Identity(s, panes[0], sid), "as", "reviewer", "--tool", "claude")
	expect(t, r, 0, "claim original pane")
	token := field(tokenRe, r.out)
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	expect(t, r, 1, "known duplicate refused")
	// Simulate a failed Claude session-list response with both real stand-ins still alive.
	path := envValue(s.env, "SUNSTACK_CLAUDE_AGENTS")
	must(t, os.WriteFile(path, []byte("not valid json"), 0600))
	r = sh(t, p, review172Identity(s, panes[1], sid), "whoami")
	t.Logf("failed scan: whoami exit=%d returnedOriginalToken=%v stderr=%q", r.code, strings.Contains(r.out, token), r.stderr)
	if r.code == 0 && strings.Contains(r.out, token) {
		t.Error("failed scan let the other live duplicate recover the original pane's claim")
	}
}

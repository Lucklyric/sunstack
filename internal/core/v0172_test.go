package core

// v0.17.2: fixes from the whole-code review (§34), batch 1.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func must172(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func tmux172(t *testing.T) (socket, server, pane string) {
	t.Helper()
	name := fmt.Sprintf("sunstack-test-v0172-%d-%d", os.Getpid(), time.Now().UnixNano())
	b, err := exec.Command("tmux", "-L", name, "-f", "/dev/null", "new-session", "-d", "-s", "t", "/bin/sh").CombinedOutput()
	if err != nil {
		t.Skipf("tmux: %v %s", err, b)
	}
	t.Cleanup(func() { exec.Command("tmux", "-L", name, "kill-server").Run() })
	out, err := exec.Command("tmux", "-L", name, "display-message", "-p", "#{socket_path} #{pid} #{pane_id}").Output()
	must172(t, err)
	f := strings.Fields(string(out))
	return f[0], f[1], f[2]
}

func waitLock172(t *testing.T, method string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b := make([]byte, 1<<20)
		n := runtime.Stack(b, true)
		for _, st := range strings.Split(string(b[:n]), "\n\n") {
			if strings.Contains(st, method) && strings.Contains(st, ".lockDir(") {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s did not reach the lock", method)
}

// §34.1: with one session ID in two panes, the claim matches only its own
// pane, and an ambiguous session is never offered for clean.
func TestDuplicateSessionClassify(t *testing.T) {
	p := secTeam(t)
	sock, server, pane := tmux172(t)
	sid := "00000000-0000-4000-8000-000000000078"
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	c := &Live{Token: NewToken(), Session: sid, Tool: "claude", TmuxPane: pane, TmuxSocket: sock, TmuxServer: server, Claimed: old, LastContact: old}
	must172(t, p.writeLive("builder.ann", c))
	sessions := []*HostSession{
		{Tool: "claude", SessionID: sid, Pane: "%99", socket: sock, Cwd: p.Root, Status: "idle", Activity: Activity{LastActive: old}},
		{Tool: "claude", SessionID: sid, Pane: pane, socket: sock, Cwd: p.Root, Status: "idle", Activity: Activity{LastActive: old}},
	}
	if idle := p.idleAgents(sessions, time.Now()); len(idle) > 0 {
		t.Errorf("an ambiguous session was offered for clean: %+v", idle)
	}
	sessions[1].Status = "busy"
	for _, list := range classify(sessions, []*Project{p})[p.Root] {
		for _, s := range list {
			if s.Pane != pane {
				t.Errorf("the claim matched pane %s, not its own %s", s.Pane, pane)
			}
		}
	}
}

// §34.1: a session ID in two panes is reported, and FindSession marks it.
func TestDuplicateSessionFind(t *testing.T) {
	sid := "00000000-0000-4000-8000-000000000079"
	all := []*HostSession{{SessionID: sid, Pane: "%1"}, {SessionID: sid, Pane: "%2"}, {SessionID: "other", Pane: "%3"}}
	d := DuplicateSessions(all)
	if got := strings.Join(d[sid], ","); got != "%1,%2" {
		t.Errorf("duplicates: %v", d)
	}
	if _, ok := d["other"]; ok {
		t.Error("a unique session is listed as duplicate")
	}
}

// §34.3: take and ack never follow a symlinked message folder.
func TestMessageMoveSymlinks(t *testing.T) {
	for _, ack := range []bool{false, true} {
		t.Run(fmt.Sprint("ack=", ack), func(t *testing.T) {
			p := secTeam(t)
			r, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
			must172(t, err)
			m, err := p.Send(SendOptions{To: r.ID, Type: "fyi", Body: "private message", NoNudge: true})
			must172(t, err)
			outside := t.TempDir()
			dst := p.takenDir(r.ID, r.Token)
			if ack {
				dst = p.messageArchive()
			}
			must172(t, os.MkdirAll(filepath.Dir(dst), 0o755))
			must172(t, os.Symlink(outside, dst))
			victim := filepath.Join(outside, m.ID+".md")
			must172(t, os.WriteFile(victim, []byte("unrelated external file"), 0o600))
			if ack {
				err = p.moveMessage(r.ID, r.Token, m.ID, true)
			} else {
				os.Remove(victim)
				err = p.Take(r.ID, r.Token, m.ID)
			}
			if err == nil {
				t.Error("a message move through a symlinked folder succeeded")
			}
			if got, _ := os.ReadFile(victim); ack && string(got) != "unrelated external file" {
				t.Error("ack overwrote a file outside the project")
			}
			if !exists(p.msgPath(p.inboxDir(r.ID), m.ID)) {
				t.Error("the message left the inbox")
			}
		})
	}
}

// §34.4: kill rereads the claim under the lock and refuses a moved one.
func TestKillAfterClaimMoves(t *testing.T) {
	p := secTeam(t)
	sock, server, pane := tmux172(t)
	b, err := exec.Command("tmux", "-S", sock, "new-window", "-d", "-P", "-F", "#{pane_id}", "/bin/sh").Output()
	must172(t, err)
	newPane := strings.TrimSpace(string(b))
	must172(t, exec.Command("tmux", "-S", sock, "set-option", "-p", "-t", pane, "@sunstack", p.Root+"|builder.ann").Run())
	c := &Live{Token: NewToken(), Tool: "claude", TmuxPane: pane, TmuxSocket: sock, TmuxServer: server, Spawned: true, Claimed: now(), LastContact: now()}
	must172(t, p.writeLive("builder.ann", c))
	unlock, err := p.lock("builder.ann")
	must172(t, err)
	held := true
	defer func() {
		if held {
			unlock()
		}
	}()
	ch := make(chan error, 1)
	go func() { _, e := p.Kill("builder.ann"); ch <- e }()
	waitLock172(t, ".Kill(")
	c.TmuxPane = newPane // a resume moved it while kill waited
	must172(t, p.writeLive("builder.ann", c))
	unlock()
	held = false
	err = <-ch
	cs, _ := p.Claims("builder.ann")
	if err == nil || len(cs) != 1 {
		t.Errorf("kill after the claim moved: err=%v claims=%d", err, len(cs))
	}
}

// §34.2: tidy refuses a destination archive with conflict markers and keeps
// the source entries.
func TestTidyArchiveConflict(t *testing.T) {
	for _, ctx := range []bool{false, true} {
		t.Run(fmt.Sprint("context=", ctx), func(t *testing.T) {
			p := secTeam(t)
			id := "builder.ann"
			src := p.boardPath(id)
			body := "## Now\n## Next\n## Done\n- 2020-01-01 KR1 [O1] preserve this completed result\n"
			if ctx {
				src = filepath.Join(p.AgentDir(id), "context.md")
				body = "## Processed messages\n" + strings.Repeat("- 2020-01-01 20200101T000000Z-user-aaaaaa from user: preserve this receipt\n", 1) +
					strings.Repeat("- 2020-01-02 20200102T000000Z-user-bbbbbb from user: filler\n", 11)
			}
			must172(t, os.WriteFile(src, []byte(body), 0o644))
			ap := filepath.Join(p.archiveDir(id), "2020-01.md")
			must172(t, os.MkdirAll(filepath.Dir(ap), 0o755))
			conflict := "<<<<<<< HEAD\n## Board\nleft\n=======\n## Notes\nright\n>>>>>>> other\n"
			must172(t, os.WriteFile(ap, []byte(conflict), 0o644))
			_, err := p.Tidy(id)
			got, _ := os.ReadFile(src)
			arch, _ := os.ReadFile(ap)
			var ce *Error
			if !errors.As(err, &ce) || ce.Reason != "conflict" {
				t.Errorf("tidy into a conflicted archive: %v, want a conflict error", err)
			}
			if string(got) != body || string(arch) != conflict {
				t.Error("tidy changed the source or the conflicted archive")
			}
		})
	}
}

// §34.5: release reports a taken message it could not put back.
func TestReleaseStranded(t *testing.T) {
	p := secTeam(t)
	r, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
	must172(t, err)
	m, err := p.Send(SendOptions{To: r.ID, Type: "handoff", Body: "finish the review", NoNudge: true})
	must172(t, err)
	must172(t, p.Take(r.ID, r.Token, m.ID))
	inbox := p.inboxDir(r.ID)
	must172(t, os.Chmod(inbox, 0o500))
	defer os.Chmod(inbox, 0o755)
	err = p.Release(r.ID, r.Token)
	var ce *Error
	if !errors.As(err, &ce) || ce.Reason != "stranded" || !strings.Contains(ce.Msg, m.ID) {
		t.Errorf("release with a stranded message: %v, want stranded naming %s", err, m.ID)
	}
}

// §34.6: a dependency on the user's key result waits on the user.
func TestUserKRDependency(t *testing.T) {
	p := secTeam(t)
	day := time.Now().Format("2006-01-02")
	must172(t, os.WriteFile(p.teamBoardPath(), []byte("## Objectives\n- "+day+" O1 Ship\n## User\n- "+day+" KR1 [O1] Approve release\n"), 0o644))
	must172(t, os.WriteFile(p.boardPath("builder.ann"), []byte("## Now\n- "+day+" KR1 [O1] Deploy (needs: user#KR1)\n"), 0o644))
	for _, is := range p.Findings(time.Now()) {
		if strings.Contains(is.Text, "user#KR1") {
			if is.Kind != "user" || strings.Contains(is.Do, "send skill") {
				t.Errorf("tier=%s do=%q", is.Kind, is.Do)
			}
			return
		}
	}
	t.Fatal("dependency not found")
}

// §34.2: health warns about a conflicted archive.
func TestHealthArchiveConflict(t *testing.T) {
	p := secTeam(t)
	ap := filepath.Join(p.archiveDir("builder.ann"), "2020-01.md")
	must172(t, os.MkdirAll(filepath.Dir(ap), 0o755))
	must172(t, os.WriteFile(ap, []byte("<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\n"), 0o644))
	for _, c := range p.Health() {
		if strings.Contains(c.Msg, "archive/2020-01.md has merge conflict markers") {
			return
		}
	}
	t.Error("health does not warn about a conflicted archive")
}

// §34.5: the release skill handles a stranded release.
func TestReleaseSkillStranded(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/release/SKILL.md")
	must172(t, err)
	if !strings.Contains(string(b), "**1 `stranded`**") {
		t.Error("the release skill does not handle exit 1 stranded")
	}
}

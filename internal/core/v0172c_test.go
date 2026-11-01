package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview172TidyPreflightBothSources(t *testing.T) {
	p := secTeam(t)
	id := "builder.ann"
	board := "## Done\n- 2020-01-01 KR1 [O1] completed board result\n"
	context := "## Processed messages\n" + strings.Repeat("- 2020-02-01 20200201T000000Z-user-aaaaaa from user: completed receipt\n", 12)
	bp, cp := p.boardPath(id), filepath.Join(p.AgentDir(id), "context.md")
	must172(t, os.WriteFile(bp, []byte(board), 0600))
	must172(t, os.WriteFile(cp, []byte(context), 0600))
	ap := filepath.Join(p.archiveDir(id), "2020-01.md")
	must172(t, os.MkdirAll(filepath.Dir(ap), 0700))
	conflict := "<<<<<<< HEAD\n## Board\nleft\n=======\nright\n>>>>>>> branch\n"
	must172(t, os.WriteFile(ap, []byte(conflict), 0600))
	n, err := p.Tidy(id)
	afterBoard, _ := os.ReadFile(bp)
	afterContext, _ := os.ReadFile(cp)
	archived, _ := os.ReadFile(filepath.Join(p.archiveDir(id), "2020-02.md"))
	t.Logf("tidy count=%d err=%v boardChanged=%v contextChanged=%v FebruaryArchiveBytes=%d", n, err, string(afterBoard) != board, string(afterContext) != context, len(archived))
	if string(afterContext) != context {
		t.Error("tidy modified context before refusing the conflicted board archive")
	}
}

func TestReview172ReleaseUnreadableTakenDirectory(t *testing.T) {
	p := secTeam(t)
	r, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
	must172(t, err)
	m, err := p.Send(SendOptions{To: r.ID, Type: "handoff", Body: "unfinished work", NoNudge: true})
	must172(t, err)
	must172(t, p.Take(r.ID, r.Token, m.ID))
	taken := p.takenDir(r.ID, r.Token)
	must172(t, os.Chmod(taken, 0100))
	defer os.Chmod(taken, 0700)
	if _, err := os.ReadDir(taken); err == nil {
		t.Fatal("fixture must prohibit directory reads")
	}
	err = p.Release(r.ID, r.Token)
	cs, _ := p.Claims(r.ID)
	_, remaining := os.Stat(p.msgPath(taken, m.ID))
	t.Logf("release=%v claims=%d takenRemains=%v pending=%v", err, len(cs), remaining == nil, exists(p.msgPath(p.inboxDir(r.ID), m.ID)))
	if err == nil && len(cs) == 0 && remaining == nil {
		t.Error("release silently stranded work after it could not enumerate the taken directory")
	}
}

func TestReview172SnapshotAndSaveConflict(t *testing.T) {
	p := secTeam(t)
	r, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
	must172(t, err)
	body := "## Now\n## Next\n## Done\n- 2020-01-01 KR1 [O1] keep result\n"
	must172(t, os.WriteFile(p.boardPath(r.ID), []byte(body), 0600))
	ap := filepath.Join(p.archiveDir(r.ID), "2020-01.md")
	must172(t, os.MkdirAll(filepath.Dir(ap), 0700))
	conflict := "<<<<<<< HEAD\nleft\n=======\nright\n>>>>>>> branch\n"
	must172(t, os.WriteFile(ap, []byte(conflict), 0600))
	snap, err := p.Snapshot(r.ID, "board.md", r.Token)
	must172(t, err)
	t.Logf("snapshot succeeds with intact board=%v", strings.Contains(snap, body))
	if !strings.Contains(snap, body) {
		t.Error("snapshot lost source")
	}
	b, _ := os.ReadFile(ap)
	if string(b) != conflict {
		t.Error("snapshot altered conflicted archive")
	}
	candidate := filepath.Join(p.sessionTmp(r.ID, r.Token), "review-board.md")
	next := strings.Replace(body, "## Next\n", "## Next\n- 2026-11-01 KR2 [O1] continue work\n", 1)
	must172(t, os.WriteFile(candidate, []byte(next), 0600))
	must172(t, p.Commit(CommitOptions{ID: r.ID, Rel: "board.md", Candidate: candidate, Sum: Checksum([]byte(body), true), Token: r.Token}))
	_, err = p.Tidy(r.ID)
	t.Logf("save commit succeeds; final tidy returns=%v", err)
	if err == nil {
		t.Error("save final tidy did not report conflict")
	}
	b, _ = os.ReadFile(p.boardPath(r.ID))
	if string(b) != next {
		t.Error("save did not preserve completed source and new entry")
	}
}

func TestReview172ClassifySamePaneDifferentServers(t *testing.T) {
	p := secTeam(t)
	a, aserver, pane := tmux172(t)
	b, _, paneB := tmux172(t)
	if pane != paneB {
		t.Fatal("expected each new server to start at the same pane ID")
	}
	sid := "00000000-0000-4000-8000-000000000091"
	c := &Live{Token: NewToken(), Session: sid, Tool: "claude", TmuxPane: pane, TmuxSocket: a, TmuxServer: aserver, Claimed: now(), LastContact: now()}
	must172(t, p.writeLive("builder.ann", c))
	sessions := []*HostSession{{Tool: "claude", SessionID: sid, Pane: paneB, socket: b, Cwd: p.Root}, {Tool: "claude", SessionID: sid, Pane: pane, socket: a, Cwd: p.Root}}
	classify(sessions, []*Project{p})
	t.Logf("claimSocket=%s selectedSocket=%s wrongServerAgent=%q actualServerAgent=%q", a, b, sessions[0].Agent, sessions[1].Agent)
	if sessions[0].Agent != "" {
		t.Error("classification assigned the claim to the duplicate on another tmux server")
	}
}

func TestReview172IdleFreeDuplicate(t *testing.T) {
	p := secTeam(t)
	sock, _, pane := tmux172(t)
	t.Setenv("SUNSTACK_TMUX_SOCKET", sock)
	for k, v := range map[string]string{"@sunstack_launch": "abcdef", "@sunstack_scope": p.HomeScope(), "@sunstack_label": "review", "@sunstack_tool": "claude"} {
		must172(t, exec.Command("tmux", "-S", sock, "set-option", "-p", "-t", pane, k, v).Run())
	}
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	sid := "00000000-0000-4000-8000-000000000092"
	sessions := []*HostSession{{SessionID: sid, Pane: pane, socket: sock, Status: "idle", Activity: Activity{LastActive: old}}, {SessionID: sid, Pane: "%99", socket: sock, Status: "idle", Activity: Activity{LastActive: old}}}
	if got := p.idleFree(sessions, time.Now()); len(got) > 0 {
		t.Errorf("duplicate free sessions offered for clean: %+v", got)
	}
	if got := p.idleFree(sessions[:1], time.Now()); len(got) != 1 {
		t.Errorf("unique free session missing from idle list: %+v", got)
	}
}

func TestReview172MessageMoveSourceSymlinks(t *testing.T) {
	for _, taken := range []bool{false, true} {
		for _, ack := range []bool{false, true} {
			p := secTeam(t)
			r, err := p.As(AsOptions{Arg: "builder.ann", Tool: "claude"})
			must172(t, err)
			m, err := p.Send(SendOptions{To: r.ID, Type: "fyi", Body: "original message", NoNudge: true})
			must172(t, err)
			src := p.msgPath(p.inboxDir(r.ID), m.ID)
			if taken {
				must172(t, p.Take(r.ID, r.Token, m.ID))
				src = p.msgPath(p.takenDir(r.ID, r.Token), m.ID)
			}
			outside := filepath.Join(t.TempDir(), "external-message.md")
			content, err := os.ReadFile(src)
			must172(t, err)
			must172(t, os.Rename(src, outside))
			must172(t, os.Symlink(outside, src))
			err = p.moveMessage(r.ID, r.Token, m.ID, ack)
			after, _ := os.ReadFile(outside)
			if err == nil || !strings.Contains(err.Error(), "symlink") || string(after) != string(content) {
				t.Errorf("taken=%v ack=%v err=%v external changed=%v", taken, ack, err, string(after) != string(content))
			}
		}
	}
}

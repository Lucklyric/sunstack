package main

// v0.8.7: fixes found by looking at the TUI and org on a real host.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A claim made before its tmux server restarted names a pane ID that now
// belongs to someone else: org must say the pane is closed, not show the
// new pane as the session's place.
func TestOrgShowsRestartedPaneAsClosed(t *testing.T) {
	env := isolatedEnv(t)
	s := privateTmux(t, t.TempDir(), env)
	p := fixture(t)
	dir := filepath.Join(p, "sunstack", "_local", "live", "reviewer")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "cccccccccccccccc.json"), []byte(`{"tool":"claude","host":"h","token":"cccccccccccccccc","claimed":"2026-09-01T00:00:00Z","last_contact":"2026-09-01T00:00:00Z","session":"00000000-0000-4000-8000-000000000870","tmux_pane":"`+s.pane+`","tmux_socket":"`+s.socket+`","tmux_server":"1","task":"old"}`), 0o644))
	expect(t, sh(t, p, s.env, "teams", "--scan", p), 0, "index the team")
	r := sh(t, p, s.env, "org", "--by", "host")
	expect(t, r, 0, "org")
	requireContains(t, r.out, "reviewer_old", "pane "+s.pane+" closed")
	if strings.Contains(r.out, "tmux tests:0.0") {
		t.Errorf("the restarted server's pane is shown as the claim's place:\n%s", r.out)
	}
}

// needs: KR8 on an agent's own board means its own KR8.
func TestBareNeedIsOwnKeyResult(t *testing.T) {
	p, env := team086(t)
	tok := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	commitBoard(t, p, env, "builder.alice", tok, "## Now\n"+
		"- 2026-10-05 KR1 [O1] Ship it (needs: KR2)\n"+
		"- 2026-10-05 KR3 [O1] Announce it (needs: KR9)\n"+
		"## Next\n- 2026-10-05 KR2 [O1] Build it\n## Done\n")
	r := sh(t, p, env, "board")
	requireContains(t, r.out, "builder.alice#KR1 is waiting on builder.alice#KR2 (next)", "builder.alice#KR3 needs builder.alice#KR9, which does not exist")
	if strings.Contains(r.out, "needs KR2, which does not exist") {
		t.Errorf("a bare need is resolved on the agent's own board:\n%s", r.out)
	}
}

// A claim whose pane is still there but now runs a shell is not shown as the
// session's place.
func TestOrgShowsLeftPane(t *testing.T) {
	env := isolatedEnv(t)
	s := privateTmux(t, t.TempDir(), env)
	p := fixture(t)
	dir := filepath.Join(p, "sunstack", "_local", "live", "reviewer")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "dddddddddddddddd.json"), []byte(`{"tool":"claude","host":"h","token":"dddddddddddddddd","claimed":"2026-09-01T00:00:00Z","last_contact":"2026-09-01T00:00:00Z","session":"00000000-0000-4000-8000-000000000871","tmux_pane":"`+s.pane+`","tmux_socket":"`+s.socket+`","tmux_server":"`+s.pid+`","task":"gone"}`), 0o644))
	expect(t, sh(t, p, s.env, "teams", "--scan", p), 0, "index the team")
	r := sh(t, p, s.env, "org", "--by", "host")
	requireContains(t, r.out, "reviewer_gone", "pane "+s.pane+" no longer runs claude")
}

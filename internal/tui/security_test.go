package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Text from boards, pillars and messages reaches the screen without its
// control sequences: OSC 52 would set the clipboard, OSC 8 plant a link.
func TestNoControlSequencesOnScreen(t *testing.T) {
	m, p := boardTeam(t)
	osc := "\x1b]52;c;ZXZpbA==\x07"
	for rel, text := range map[string]string{
		"builder.a/board.md":   "## Now\n- 2026-01-01 KR1 [O1] Write " + osc + " it\n",
		"builder.a/pillars.md": "- care " + osc + "\n",
		"_local/inbox/builder.a/20260101T110000Z-user-ccccc1.md": "---\nid: 20260101T110000Z-user-ccccc1\nfrom: user\nto: builder.a\nat: 2026-01-01T11:00:00Z\ntype: task\n---\nGoal: evil \x1b]8;;http://x\x07link\n",
	} {
		if err := os.WriteFile(filepath.Join(p.Dir, rel), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.reload()
	for _, k := range []string{"", "B", "T"} {
		if k != "" {
			m.Update(key(k))
		}
		for i := 0; i < 3; i++ {
			v := m.View()
			if strings.Contains(v, "\x1b]") || strings.Contains(v, "\x07") {
				t.Fatalf("view %q shows a control sequence: %q", k, v)
			}
			m.Update(key("down"))
		}
	}
}

// Updating hosts is for the user, as on the command line.
func TestHostUpdateIsUserOnly(t *testing.T) {
	m := hostsModel(t, 200)
	m.key("h")
	t.Setenv("CLAUDECODE", "1")
	for _, k := range []string{"u", "U"} {
		m.confirm, m.note = "", ""
		m.key(k)
		if m.confirm != "" || !strings.Contains(m.note, "for the user") {
			t.Errorf("%s from an agent session: confirm %q note %q", k, m.confirm, m.note)
		}
	}
}

// A session of a host that stopped reporting is shown as of its last
// snapshot, dimmed, with the host's state, never as live.
func TestRemoteSessionsShowHostState(t *testing.T) {
	m := hostsModel(t, 200)
	for _, state := range []string{"stale", "offline"} {
		m.hosts.Hosts[2].State = state
		found := false
		for _, n := range m.treeNodes() {
			if n.kind == "session" && n.host != nil && n.host.Name == "server" {
				found = true
				if r := ansi.Strip(m.row(n, 100)); !strings.Contains(r, ", "+state) {
					t.Errorf("%s host's session row: %q", state, r)
				}
			}
		}
		if !found {
			t.Fatal("no session of server in the tree")
		}
	}
	m.hosts.Hosts[2].State = "live"
	for _, n := range m.treeNodes() {
		if n.kind == "session" && n.host != nil && strings.Contains(ansi.Strip(m.row(n, 100)), ", live") {
			t.Error("a live host's session is marked")
		}
	}
}

// Pasting into a prompt adds the text, on one line.
func TestPasteInPrompt(t *testing.T) {
	m, _ := boardTeam(t)
	m.Update(key("B"))
	m.Update(key("/"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("par\nser\x1b]52;c;x\x07"), Paste: true})
	if m.inputText != "par ser]52;c;x" {
		t.Errorf("pasted text: %q", m.inputText)
	}
}

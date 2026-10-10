package tui

import (
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

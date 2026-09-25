package core

import (
	"os"
	"path/filepath"
	"testing"
)

func screen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "screens", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The screens in testdata are real captures of Claude Code 2.1 and Codex 0.155
// panes (paths scrubbed).
func TestReadyForInput(t *testing.T) {
	claudeTrust := `─────────────────────────────
 Accessing workspace:
 /tmp/project
 Quick safety check: Is this a project you created or one you trust?
 Security guide
 ❯ No, exit
   Yes, I trust this folder
 Enter to confirm · Esc to cancel`
	codexHooksCursorMoved := `  Hooks need review
  1 hook is new or changed.
  Hooks can run outside the sandbox after you trust them.
  1. Review hooks
› 2. Trust all and continue
  3. Continue without trusting (hooks won't run)`
	claudeNormalMode := `──────────────────
❯
──────────────────
  -- NORMAL --`
	claudeListAbove := `  Next steps:
  1. Fix the test
  2. Rerun it
──────────────────
❯ Try "fix typecheck errors"
──────────────────
  -- INSERT --`
	for _, c := range []struct {
		name, tool, screen string
		want               bool
	}{
		{"claude idle (real)", "claude", screen(t, "claude-idle"), true},
		{"claude draft (real)", "claude", screen(t, "claude-draft"), true},
		{"codex idle (real)", "codex", screen(t, "codex-idle"), true},
		{"codex draft (real)", "codex", screen(t, "codex-draft"), true},
		{"codex trust menu (real)", "codex", screen(t, "codex-trust"), false},
		{"claude trust menu (real text)", "claude", claudeTrust, false},
		{"codex menu with the cursor moved, no footer", "codex", codexHooksCursorMoved, false},
		{"claude vim normal mode", "claude", claudeNormalMode, false},
		{"numbered list in output is not a menu", "claude", claudeListAbove, true},
		{"claude screen for codex", "codex", screen(t, "claude-idle"), false},
		{"empty", "claude", "", false},
	} {
		if got := ReadyForInput(c.tool, c.screen); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// After typing, the input line must hold exactly the nudge; a draft in front
// of it means the user was typing.
func TestInputLineAfterTyping(t *testing.T) {
	nudge := "/sunstack:check a new fyi message is waiting"
	for _, c := range []struct {
		name, tool, screen, want string
	}{
		{"claude, empty before", "claude", screen(t, "claude-nudge-typed"), nudge},
		{"claude, draft before", "claude", screen(t, "claude-draft-plus-nudge"), "my draft" + nudge},
		{"codex, empty before", "codex", screen(t, "codex-nudge-typed"), "$" + nudge[1:]},
		{"codex, draft before", "codex", screen(t, "codex-draft-plus-nudge"), "my draft$" + nudge[1:]},
	} {
		got, ok := inputLine(c.tool, screenTail(c.screen, 15))
		if !ok || got != c.want {
			t.Errorf("%s: got %q (%v), want %q", c.name, got, ok, c.want)
		}
	}
	if p := NudgePrompt("claude", &Message{Type: "fyi", From: "researcher.kr2"}); p != nudge {
		t.Errorf("nudge text: %q", p)
	}
}

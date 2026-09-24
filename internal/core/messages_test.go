package core

import "testing"

func TestReadyForInput(t *testing.T) {
	claudeIdle := `  Done. Next, check that window.
────────────────────────────────────────────── todo ─
❯ 
──────────────────────────────────────────────────────
  Opus 5.5 (1M context) | main
  -- INSERT -- ⏵⏵ auto mode on (shift+tab to cycle)`
	claudeTrust := `─────────────────────────────
 Accessing workspace:
 /tmp/x
 Quick safety check: Is this a project you created or one you trust?
 Security guide
 ❯ No, exit
   Yes, I trust this folder
 Enter to confirm · Esc to cancel`
	claudePermission := `╭──────────────────────────────╮
│ Bash command                 │
│   sunstack amend x           │
│ Do you want to proceed?      │
│ ❯ 1. Yes                     │
│   2. No                      │
╰──────────────────────────────╯`
	claudeNormalMode := `──────────────────
❯ 
──────────────────
  -- NORMAL --`
	codexIdle := `• DONE
  Worked for 4m 53s
› Ask Codex to do anything
  gpt-6-astra high · ~/project`
	codexApproval := `Would you like to run the following command?
  $ sunstack fire x
› 1. Yes, proceed (y)
  2. No, and tell Codex what to do differently (esc)
Press enter to confirm or esc to cancel`
	for _, c := range []struct {
		name, tool, screen string
		want               bool
	}{
		{"claude idle", "claude", claudeIdle, true},
		{"claude trust menu", "claude", claudeTrust, false},
		{"claude permission", "claude", claudePermission, false},
		{"claude vim normal mode", "claude", claudeNormalMode, false},
		{"codex idle", "codex", codexIdle, true},
		{"codex approval", "codex", codexApproval, false},
		{"claude screen for codex", "codex", claudeIdle, false},
		{"empty", "claude", "", false},
	} {
		if got := ReadyForInput(c.tool, c.screen); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if p := NudgePrompt("claude", &Message{Type: "question", From: "researcher.kr2"}); p != "/sunstack:check a new question message is waiting" {
		t.Errorf("nudge text must carry no digits: %q", p)
	}
}

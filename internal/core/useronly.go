package core

import (
	"os"
	"os/exec"
	"strings"
)

// UserOnly refuses a command run from an agent session (§20.7): a process
// with a CLI session's variables, or in a tmux pane sunstack recorded for a
// session. It stops agents acting on their own; it is not a boundary
// against another process of the same user.
func UserOnly(what string) error {
	refuse := func() error {
		return fail(ExitUsage, "user_only", "%s is for the user: run it yourself in a terminal, not from a Claude Code or Codex session", what)
	}
	for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID"} {
		if os.Getenv(k) != "" {
			return refuse()
		}
	}
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return nil
	}
	socket := strings.SplitN(os.Getenv("TMUX"), ",", 2)[0]
	for _, opt := range []string{"@sunstack", "@sunstack_launch"} {
		out, _ := exec.Command("tmux", TmuxArgs(socket, "show-options", "-p", "-v", "-t", pane, opt)...).Output()
		if strings.TrimSpace(string(out)) != "" {
			return refuse()
		}
	}
	return nil
}

package core

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// TmuxPlace is a pane with its server's socket and process ID.
type TmuxPlace struct{ Pane, Socket, Server string }

// CodexCaller is CallerPane for a Codex caller whose shell has no tmux
// variables, found once per command; empty for any other caller.
var CodexCaller = sync.OnceValue(func() TmuxPlace {
	if os.Getenv("TMUX") != "" || os.Getenv("TMUX_PANE") != "" {
		return TmuxPlace{}
	}
	if os.Getenv("CODEX_THREAD_ID") == "" && os.Getenv("CODEX_SESSION_ID") == "" {
		return TmuxPlace{}
	}
	pane, socket, server := CallerPane()
	return TmuxPlace{pane, socket, server}
})

// selfPane is the pane this command runs in, so a session never closes
// its own pane.
func selfPane() string {
	if p := os.Getenv("TMUX_PANE"); p != "" {
		return p
	}
	return CodexCaller().Pane
}

// CallerPane finds the tmux pane this command runs under when its shell has
// no tmux variables: Codex with shell_environment_policy inherit = "core"
// drops TMUX and TMUX_PANE, but its process still runs on the pane's
// terminal. It walks up the parent processes to the first one with a
// terminal and returns that terminal's pane, with its server's socket and
// process ID; all empty when none is found.
func CallerPane() (pane, socket, server string) {
	panes := tmuxPanes()
	if len(panes) == 0 {
		return "", "", ""
	}
	pid := os.Getppid()
	for i := 0; i < 16 && pid > 1; i++ {
		cmd, done := quick("ps", "-o", "ppid=,tty=", "-p", strconv.Itoa(pid))
		b, err := cmd.Output()
		done()
		f := strings.Fields(string(b))
		if err != nil || len(f) != 2 {
			return "", "", ""
		}
		if tty := f[1]; tty != "?" && tty != "??" {
			if p, ok := panes[strings.TrimPrefix(tty, "/dev/")]; ok {
				server, _ = serverPID(p.socket)
				return p.id, p.socket, server
			}
			return "", "", "" // a terminal outside tmux
		}
		pid, _ = strconv.Atoi(f[0])
	}
	return "", "", ""
}

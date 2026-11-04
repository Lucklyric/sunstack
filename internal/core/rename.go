package core

// Renaming a Claude or Codex session from the dashboard (§40.7): the TUI
// types the CLI's own /rename into the session's pane, so the name is the
// one the CLI keeps and Sunstack reads back.

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RenameSession renames s through its pane. It types only while the CLI
// shows its normal input box with no draft (typeInto), so a busy session or
// one with a prompt open is left alone and the error says why.
func RenameSession(s *HostSession, name string) error {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\n", " "))
	switch {
	case name == "":
		return fmt.Errorf("no name given")
	case s.Pane == "":
		return fmt.Errorf("it has no tmux pane")
	case s.Status != "idle":
		return fmt.Errorf("it is %s", s.Status)
	}
	sock := s.Socket()
	switch s.Tool {
	case "claude":
		return typeInto(sock, s.Pane, s.Tool, "/rename "+name)
	case "codex":
		// Codex's /rename opens a one-line dialog for the name.
		if err := typeInto(sock, s.Pane, s.Tool, "/rename"); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
		// The box is the last thing on screen; older text above it does not count.
		if !strings.Contains(strings.Join(screenTail(paneScreen(sock, s.Pane), 8), "\n"), "Type a name and press Enter") {
			return fmt.Errorf("Codex did not open its rename box")
		}
		if err := exec.Command("tmux", TmuxArgs(sock, "send-keys", "-t", s.Pane, "-l", "--", name)...).Run(); err != nil {
			return err
		}
		return exec.Command("tmux", TmuxArgs(sock, "send-keys", "-t", s.Pane, "Enter")...).Run()
	}
	return fmt.Errorf("%s has no rename command", s.Tool)
}

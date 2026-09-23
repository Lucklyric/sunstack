// Package setup installs, updates and removes the Sunstack plugin in Claude
// Code and Codex, and updates the sunstack binary itself (design §6, §10).
package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

const (
	Repo        = "Lucklyric/sunstack"
	Marketplace = "sunstack"
	Plugin      = "sunstack@sunstack"
	AllowRule   = "Bash(sunstack *)"
	AskRule     = "Bash(sunstack amend *)"
)

// AskRules are the sunstack commands the user always approves: changing an
// agent's rules, and creating or deleting an agent (design §6).
var AskRules = []string{AskRule, "Bash(sunstack hire *)", "Bash(sunstack fire *)"}

// Targets says which CLIs to act on.
type Targets struct{ Claude, Codex bool }

// Detect fills in the CLIs found on PATH when neither was requested.
func Detect(t Targets) Targets {
	if t.Claude || t.Codex {
		return t
	}
	_, e1 := exec.LookPath("claude")
	_, e2 := exec.LookPath("codex")
	return Targets{Claude: e1 == nil, Codex: e2 == nil}
}

// run shows and runs one command, streaming its output.
func run(out io.Writer, name string, args ...string) error {
	fmt.Fprintf(out, "$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// Confirm asks a yes/no question on a terminal. Without a terminal it returns
// def, so a non-interactive call never blocks.
func Confirm(in io.Reader, out io.Writer, q string, def bool) bool {
	if f, ok := in.(*os.File); !ok || !isTerminal(f) {
		return def
	}
	fmt.Fprintf(out, "%s [y/N] ", q)
	line, _ := bufio.NewReader(in).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool { return isTerminal(f) }

// InstallClaude adds the marketplace if needed, refreshes it, and installs or
// updates the plugin, so running it again always ends on the latest version.
func InstallClaude(out io.Writer) error {
	_ = run(out, "claude", "plugin", "marketplace", "add", Repo) // fails harmlessly when already added
	if err := run(out, "claude", "plugin", "marketplace", "update", Marketplace); err != nil {
		return err
	}
	if err := run(out, "claude", "plugin", "install", Plugin); err != nil {
		return err
	}
	return run(out, "claude", "plugin", "update", Plugin)
}

// InstallCodex adds the marketplace if needed, refreshes its snapshot, and
// installs the plugin from it.
func InstallCodex(out io.Writer) error {
	_ = run(out, "codex", "plugin", "marketplace", "add", Repo) // fails harmlessly when already added
	if err := run(out, "codex", "plugin", "marketplace", "upgrade", Marketplace); err != nil {
		return err
	}
	return run(out, "codex", "plugin", "add", Plugin)
}

// UpdateClaude refreshes the marketplace and updates the plugin.
func UpdateClaude(out io.Writer) error {
	if err := run(out, "claude", "plugin", "marketplace", "update", Marketplace); err != nil {
		return err
	}
	return run(out, "claude", "plugin", "update", Plugin)
}

// UpdateCodex refreshes the marketplace snapshot and reinstalls the plugin.
func UpdateCodex(out io.Writer) error {
	if err := run(out, "codex", "plugin", "marketplace", "upgrade", Marketplace); err != nil {
		return err
	}
	return run(out, "codex", "plugin", "add", Plugin)
}

// UninstallClaude removes the plugin and the marketplace.
func UninstallClaude(out io.Writer) error {
	e1 := run(out, "claude", "plugin", "uninstall", Plugin)
	e2 := run(out, "claude", "plugin", "marketplace", "remove", Marketplace)
	if e1 != nil {
		return e1
	}
	return e2
}

// UninstallCodex removes the plugin and the marketplace.
func UninstallCodex(out io.Writer) error {
	e1 := run(out, "codex", "plugin", "remove", Plugin)
	e2 := run(out, "codex", "plugin", "marketplace", "remove", Marketplace)
	if e1 != nil {
		return e1
	}
	return e2
}

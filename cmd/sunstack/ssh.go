package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
	"github.com/Lucklyric/sunstack/internal/setup"
)

// runSSH is sunstack ssh (§23): the status of shared SSH connections, and
// the user's scan, map and check.
func runSSH(rest []string, stdin io.Reader, stdout io.Writer) error {
	sub := ""
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	a, err := parse(rest, "os user address alias key", "yes")
	if err != nil {
		return err
	}
	switch sub {
	case "":
		return sshStatus(stdout)
	case "scan":
		if err := a.atMost(0, "ssh scan"); err != nil {
			return err
		}
		if err := core.UserOnly("sunstack ssh scan"); err != nil {
			return err
		}
		s, err := core.SSHScan()
		if err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		fmt.Fprintf(stdout, "sunstack: %d aliases in ~/.ssh/config\n", len(s.Aliases))
		return sshStatus(stdout)
	case "map":
		if err := a.atMost(2, "ssh map"); err != nil {
			return err
		}
		if len(a.pos) < 2 {
			return missing("org host, then its SSH alias (\"-\" to remove)")
		}
		if err := core.UserOnly("sunstack ssh map"); err != nil {
			return err
		}
		id, name, ok := hub.HostID(a.pos[0])
		if !ok {
			return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "no host " + a.pos[0] + " in the org (sunstack org --refresh, then sunstack org --by host)"}
		}
		alias := a.pos[1]
		if alias == "-" {
			alias = ""
		} else if _, known := core.LoadSSH().Aliases[alias]; !known {
			return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "no SSH alias " + alias + " (sunstack ssh scan reads ~/.ssh/config)"}
		}
		if err := core.SSHMap(id, alias); err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		if alias == "" {
			fmt.Fprintf(stdout, "sunstack: %s has no SSH alias now\n", name)
		} else {
			fmt.Fprintf(stdout, "sunstack: %s is SSH alias %s\n", name, alias)
		}
		return nil
	case "check":
		if err := a.atMost(1, "ssh check"); err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("SSH alias or org host")
		}
		if err := core.UserOnly("sunstack ssh check"); err != nil {
			return err
		}
		s := core.LoadSSH()
		alias := a.pos[0]
		if id, _, ok := hub.HostID(alias); ok && s.Hosts[id] != "" {
			alias = s.Hosts[id]
		}
		if _, known := s.Aliases[alias]; !known {
			return &core.Error{Code: core.ExitFail, Reason: "not_found", Msg: "no SSH alias " + alias + " (sunstack ssh scan reads ~/.ssh/config)"}
		}
		result, err := core.RunSSHCheck(alias)
		if err != nil {
			return &core.Error{Code: core.ExitFail, Reason: "fs", Msg: err.Error()}
		}
		fmt.Fprintf(stdout, "%s: %s\n", alias, result)
		if result != "ok" {
			return &core.Error{Code: core.ExitFail, Reason: "ssh", Msg: alias + ": " + result}
		}
		return nil
	case "start", "stop":
		if err := a.atMost(1, "ssh "+sub); err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("SSH alias or org host")
		}
		if err := core.UserOnly("sunstack ssh " + sub); err != nil {
			return err
		}
		alias := sshAlias(a.pos[0])
		if sub == "start" {
			if !setup.IsTerminal(os.Stdin) {
				return &core.Error{Code: core.ExitUsage, Reason: "terminal", Msg: "sunstack ssh start runs in a terminal, so a passphrase or key prompt can be answered"}
			}
			opened, err := core.SSHStart(alias, os.Stdin, stdout, os.Stderr)
			if err != nil {
				return err
			}
			if opened {
				fmt.Fprintf(stdout, "sunstack: connection to %s open; ssh %s reuses it until sunstack ssh stop %s\n", alias, alias, alias)
			} else {
				fmt.Fprintf(stdout, "sunstack: a connection to %s is already open\n", alias)
			}
			return nil
		}
		path, err := core.SSHMasterPath(alias)
		if err != nil {
			return err
		}
		if !core.MasterOpen(path) {
			fmt.Fprintf(stdout, "sunstack: no open connection to %s\n", alias)
			return nil
		}
		if !a.has("yes") {
			plan := "close the shared connection " + alias + " (" + path + "), which also ends every shell and tmux attach running through it"
			if !setup.IsTerminal(os.Stdin) {
				return &core.Error{Code: core.ExitUsage, Reason: "confirm", Msg: "this will " + plan + "; rerun with --yes once the user has confirmed"}
			}
			fmt.Fprintf(stdout, "To keep running shells and refuse only new ones, run ssh -O stop %s instead.\n", alias)
			if !setup.Confirm(stdin, stdout, "This will "+plan+". Continue?", false) {
				fmt.Fprintln(stdout, "sunstack: nothing closed")
				return nil
			}
		}
		closed, err := core.SSHStop(alias)
		if err != nil {
			return err
		}
		if closed {
			fmt.Fprintf(stdout, "sunstack: connection to %s closed\n", alias)
		} else {
			fmt.Fprintf(stdout, "sunstack: no open connection to %s\n", alias)
		}
		return nil
	case "setup":
		if err := a.atMost(1, "ssh setup"); err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("the host to reach")
		}
		return sshSetup(stdout, a.pos[0], a.flags)
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "sunstack ssh [scan | map <host> <alias> | check <alias|host> | start <alias|host> | stop <alias|host> [--yes] | setup <host>]"}
}

// sshAlias is the alias an argument names: an org host's mapped alias, or
// the alias itself.
func sshAlias(name string) string {
	if id, _, ok := hub.HostID(name); ok {
		if alias := core.LoadSSH().Hosts[id]; alias != "" {
			return alias
		}
	}
	return name
}

var shSafe = regexp.MustCompile(`^[A-Za-z0-9._@%+=:,/~-]+$`)

// shQuote quotes a value for a POSIX shell command line.
func shQuote(v string) string {
	if shSafe.MatchString(v) {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// sshSetup prints the steps to reach host from this host (§23.2). It changes
// nothing and runs no ssh, so an agent may run it to show the user.
func sshSetup(w io.Writer, host string, flags map[string]string) error {
	osName := flags["os"]
	if osName != "" && osName != "macos" && osName != "linux" {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--os is macos or linux"}
	}
	val := func(flag, placeholder string) string {
		if v := flags[flag]; v != "" {
			return shQuote(v)
		}
		return placeholder
	}
	alias := flags["alias"]
	if alias == "" {
		alias = strings.ToLower(host)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(alias) {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "pick an SSH alias of letters, digits, dot, dash and underscore with --alias"}
	}
	user, address := val("user", "<user>"), val("address", "<address>")
	key := flags["key"]
	if key == "" {
		key = "~/.ssh/id_ed25519"
	}
	key = shQuote(strings.TrimSuffix(key, ".pub"))
	me := core.ThisHost().Name

	fmt.Fprintf(w, "Reach %q from %q over SSH (this prints steps and changes nothing)\n\n", host, me)
	fmt.Fprintf(w, "1. On %q, allow logins:\n", host)
	if osName != "linux" {
		fmt.Fprintln(w, "   macOS: System Settings, General, Sharing, Remote Login on. Under \"Allow access for\", include the account you log in as (a standard account is shut out when only Administrators are allowed).")
	}
	if osName != "macos" {
		fmt.Fprintln(w, "   Linux: sshd installed and running (systemctl enable --now ssh, or sshd).")
	}
	fmt.Fprintln(w, "   Tailscale SSH instead needs no keys, but only with open-source tailscaled (Linux, or macOS without the App Store app) and a tailnet SSH policy that allows it.")
	if user == "<user>" || address == "<address>" {
		fmt.Fprintln(w, "   Note the login name there (whoami) and an address this host can reach: a Tailscale name or IP, or <machine>.local on the same network.")
		if m := hub.HostMachine(host); m != "" {
			fmt.Fprintf(w, "   Its machine name is %q.\n", m)
		}
	}
	fmt.Fprintln(w, "\n2. Here, a key and its copy there (ls ~/.ssh/*.pub shows the keys you have):")
	fmt.Fprintf(w, "   ssh-keygen -t ed25519 -f %s     # only if you have no key; a passphrase is recommended\n", key)
	fmt.Fprintf(w, "   ssh-copy-id -i %s.pub %s@%s   # asks for the password there once\n", key, user, address)
	fmt.Fprintln(w, "\n3. Here, a private folder for shared connections:")
	fmt.Fprintln(w, "   mkdir -p -m 700 ~/.ssh/cm")
	fmt.Fprintln(w, "\n4. Here, this entry in ~/.ssh/config, above any Host * or Match block:")
	fmt.Fprintf(w, "   Host %s\n     HostName %s\n     User %s\n     IdentityFile %s\n     ControlMaster auto\n     ControlPath ~/.ssh/cm/%%C\n     ControlPersist 10m\n", alias, address, user, key)
	fmt.Fprintln(w, "   ControlPersist closes the shared connection after 10 idle minutes; the key logs in again when needed.")
	fmt.Fprintln(w, "\n5. Then, here:")
	fmt.Fprintf(w, "   ssh %s true                      # accept the host key once\n", alias)
	fmt.Fprintln(w, "   sunstack ssh scan")
	if _, _, ok := hub.HostID(host); ok {
		fmt.Fprintf(w, "   sunstack ssh map %s %s\n", shQuote(host), alias)
	}
	fmt.Fprintf(w, "   sunstack ssh check %s\n", alias)
	fmt.Fprintf(w, "\nFor the other direction, run on %q: sunstack ssh setup %s\n", host, shQuote(me))
	return nil
}

func sshStatus(stdout io.Writer) error {
	s := core.LoadSSH()
	if s.Scanned == "" {
		fmt.Fprintln(stdout, "No SSH aliases recorded yet: run sunstack ssh scan in a terminal (it reads ~/.ssh/config).")
		return nil
	}
	rows, other := core.SSHStatus()
	fmt.Fprintf(stdout, "Shared SSH connections (scanned %s)\n", s.Scanned)
	for _, r := range rows {
		name := strings.Join(r.Aliases, ", ")
		var hosts []string
		for _, id := range r.HostIDs {
			hosts = append(hosts, hub.HostName(id))
		}
		if len(hosts) > 0 {
			name += " (org host " + strings.Join(hosts, ", ") + ")"
		}
		state := "no shared connection"
		switch {
		case r.Shared && r.Open:
			state = "open, reuse it: ssh " + r.Aliases[0]
		case r.Shared:
			state = "closed; the next ssh " + r.Aliases[0] + " opens it"
		}
		if r.Open && core.StartedBySunstack(r.Aliases[0], r.Path) {
			state += " (sunstack ssh start, " + core.SSHAge(s.Started[r.Aliases[0]]) + ")"
		}
		line := "  " + name + ": " + state
		if r.Check != nil {
			line += "; last check " + r.Check.Result + " at " + r.Check.At
		}
		fmt.Fprintln(stdout, line)
	}
	if len(other) > 0 {
		fmt.Fprintln(stdout, "Other open connections in the same folders:")
		for _, p := range other {
			fmt.Fprintln(stdout, "  "+p)
		}
	}
	return nil
}

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

// runSSH is sunstack ssh (§23): the status of shared SSH connections, and
// the user's scan, map and check.
func runSSH(rest []string, stdout io.Writer) error {
	sub := ""
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	a, err := parse(rest, "", "")
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
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "sunstack ssh [scan | map <host> <alias> | check <alias|host>]"}
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

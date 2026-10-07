package main

// v0.9: hosts connected through an org hub (design §18).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

const hubUsage = `sunstack hub init <org name>                    make this host the hub of a new org (user only)
sunstack hub allow <name> --id ID --key "<pub>" [--send]
                                                add a host and its dedicated key (user only, on the hub)
sunstack hub revoke <name>                      remove a host and end its connections (user only, on the hub)
sunstack hub hosts [--json]                     the roster, on the hub
sunstack hub connect [--install|--uninstall]    keep one connection to the hub open
sunstack hub serve --host ID                    what a host's key runs on the hub (from authorized_keys)`

func hubCommand(rest []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(rest) == 0 {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "sunstack hub init|allow|revoke|hosts|connect|serve\n" + hubUsage}
	}
	sub, rest := rest[0], rest[1:]
	switch sub {
	case "init":
		a, err := parse(rest, "", "")
		if err == nil {
			err = a.atMost(1, "hub init")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("org name")
		}
		c, err := hub.Init(a.pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: this host is the hub of org %s (id %s)\nnext: sunstack hub connect --install, then on each other host: sunstack org join <this host> --send\n", c.OrgName, c.OrgID)
		return nil

	case "allow":
		a, err := parse(rest, "id key", "send")
		if err == nil {
			err = a.atMost(1, "hub allow")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 || a.flags["id"] == "" || a.flags["key"] == "" {
			return missing("host name, --id and --key")
		}
		note, err := hub.Allow(a.pos[0], a.flags["id"], a.flags["key"], a.has("send"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s %s\n", a.pos[0], note)
		return nil

	case "revoke":
		a, err := parse(rest, "", "")
		if err == nil {
			err = a.atMost(1, "hub revoke")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("host name")
		}
		if err := hub.Revoke(a.pos[0]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s revoked; its key, snapshot and mail are gone\n", a.pos[0])
		return nil

	case "hosts":
		a, err := parse(rest, "", "json")
		if err == nil {
			err = a.atMost(0, "hub hosts")
		}
		if err != nil {
			return err
		}
		r, err := hub.Hosts()
		if err != nil {
			return err
		}
		if a.has("json") {
			b, _ := json.MarshalIndent(r, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		fmt.Fprintf(stdout, "Org %s\n", r.OrgName)
		for _, h := range r.Hosts {
			role := ""
			if h.Hub {
				role = " (hub)"
			}
			send := "read only"
			if h.CanSend {
				send = "can send"
			}
			contact := h.Contact
			if contact == "" {
				contact = "never"
			}
			fmt.Fprintf(stdout, "  %s%s  %s, last contact %s, %d waiting\n", h.Name, role, send, contact, h.Waiting)
		}
		return nil

	case "connect":
		a, err := parse(rest, "", "install uninstall")
		if err == nil {
			err = a.atMost(0, "hub connect")
		}
		if err != nil {
			return err
		}
		switch {
		case a.has("install"):
			return hub.Install(stdout)
		case a.has("uninstall"):
			return hub.Uninstall(stdout)
		}
		return hub.Connect(stderr)

	case "serve":
		a, err := parse(rest, "host", "")
		if err == nil {
			err = a.atMost(0, "hub serve")
		}
		if err != nil {
			return err
		}
		// The verb comes from the key's SSH session, as one exact word;
		// it is never given to a shell.
		return hub.Serve(a.flags["host"], strings.TrimSpace(os.Getenv("SSH_ORIGINAL_COMMAND")), stdin, stdout)
	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "unknown hub command " + sub + "\n" + hubUsage}
}

// orgMembership is org join and org leave.
func orgMembership(rest []string, stdin io.Reader, stdout io.Writer) error {
	if rest[0] == "leave" {
		a, err := parse(rest[1:], "", "")
		if err == nil {
			err = a.atMost(0, "org leave")
		}
		if err != nil {
			return err
		}
		if err := hub.Leave(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "sunstack: this host left its org; on the hub, sunstack hub revoke <name> removes it there")
		return nil
	}
	a, err := parse(rest[1:], "", "send")
	if err == nil {
		err = a.atMost(1, "org join")
	}
	if err != nil {
		return err
	}
	if len(a.pos) < 1 {
		return missing("the hub's ssh target")
	}
	c, err := hub.Join(a.pos[0], a.has("send"), stdin, stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "sunstack: joined org %s through %s\nnext: sunstack hub connect --install\n", c.OrgName, c.Hub)
	return nil
}

// sendRemote queues a message for another host and, when no connector runs,
// hands it to the hub at once (§18.8).
func sendRemote(p *core.Project, o core.SendOptions, stdout io.Writer) error {
	from := "user"
	if o.From != "" {
		if p == nil {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--from needs a team folder"}
		}
		if err := p.VerifySender(o.From, o.Token); err != nil {
			return err
		}
		from = p.TeamLabel(o.From)
	}
	if o.Follows != "" {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--follows is for tasks within this host's teams"}
	}
	s, err := hub.Queue(o.To, from, o.Type, o.Body, o.ReplyTo)
	if err != nil {
		return err
	}
	where := "queued; the connector takes it to the hub"
	if !hub.ConnectorRunning() {
		if err := hub.FlushOnce(); err != nil {
			where = "queued; " + err.Error() + "; it goes at the next sunstack hub connect or send"
		} else {
			where = "at the hub"
			for _, x := range hub.SentMail() {
				if x.Mail.ID == s.Mail.ID {
					where = x.Status
					if x.Reason != "" {
						where += ": " + x.Reason
					}
				}
			}
		}
	}
	fmt.Fprintf(stdout, "sunstack: sent %s to %s:%s (%s)\n", s.Mail.ID, s.ToName, s.Mail.To, where)
	return nil
}

// sentText lists messages sent to other hosts with their place.
func sentText(all bool) string {
	var b strings.Builder
	for _, s := range hub.SentMail() {
		if !all && s.Status == "delivered" && s.Mail.Type != "task" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\nSent to other hosts\n")
		}
		line := fmt.Sprintf("  %s  %s -> %s:%s  %s", s.Mail.ID, s.Mail.Type, s.ToName, s.Mail.To, s.Status)
		if s.Reason != "" {
			line += " (" + s.Reason + ")"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

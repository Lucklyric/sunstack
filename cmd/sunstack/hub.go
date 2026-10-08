package main

// v0.9: hosts connected through an org hub (design §18).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
	"github.com/Lucklyric/sunstack/internal/setup"
)

const hubUsage = `sunstack hub init <org name>                    make this host the hub of a new org (user only)
sunstack hub invite [--read-only]               a one-time join code for another host (user only, on the hub)
sunstack hub revoke <name>                      remove a host and end its connection (user only, on the hub)
sunstack hub hosts [--json]                     the roster, on the hub
sunstack hub connect [--install|--uninstall]    keep one connection to the hub open; on the hub, also listen`

func hubCommand(rest []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(rest) == 0 {
		return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "sunstack hub init|invite|revoke|hosts|connect\n" + hubUsage}
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
		fmt.Fprintf(stdout, "sunstack: this host is the hub of org %s (id %s)\nnext: sunstack hub connect --install, then sunstack hub invite for each other host\n", c.OrgName, c.OrgID)
		return nil

	case "invite":
		a, err := parse(rest, "", "read-only")
		if err == nil {
			err = a.atMost(0, "hub invite")
		}
		if err != nil {
			return err
		}
		code, err := hub.Invite(a.has("read-only"))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "join code: %s  (valid 10 minutes, once)\non the other host: sunstack org join <this host's Tailscale name> --code %s\n", code, code)
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
		fmt.Fprintf(stdout, "sunstack: %s revoked; its token, snapshot and mail are gone\n", a.pos[0])
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
			send += ", key " + h.Keys.Fingerprint()
			contact := h.Contact
			if contact == "" {
				contact = "never"
			}
			ver := h.Version
			if ver == "" {
				ver = "version unknown"
			}
			if h.Machine != "" && !strings.EqualFold(h.Machine, h.Name) {
				role += " (machine " + h.Machine + ")"
			}
			fmt.Fprintf(stdout, "  %s%s  %s, %s, last contact %s, %d waiting\n", h.Name, role, send, ver, contact, h.Waiting)
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

	}
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "unknown hub command " + sub + "\n" + hubUsage}
}

// orgMembership is org join, leave, keys and trust.
func orgMembership(rest []string, stdin io.Reader, stdout io.Writer) error {
	switch rest[0] {
	case "leave":
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
	case "keys":
		a, err := parse(rest[1:], "", "")
		if err == nil {
			err = a.atMost(0, "org keys")
		}
		if err != nil {
			return err
		}
		t, err := hub.KeysText()
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, t)
		return nil
	case "update":
		a, err := parse(rest[1:], "", "all yes")
		if err == nil {
			err = a.atMost(1, "org update")
		}
		if err != nil {
			return err
		}
		if err := core.UserOnly("sunstack org update"); err != nil {
			return err
		}
		setup.RefreshLatest()
		target := setup.CachedLatest()
		if target == "" || setup.Older(target, version) {
			target = version
		}
		var names []string
		switch {
		case a.has("all") && len(a.pos) == 0:
			v := hub.LoadView(time.Now())
			if v == nil {
				return &core.Error{Code: core.ExitFail, Reason: "no_org", Msg: "this host is in no org"}
			}
			for _, h := range v.Hosts {
				switch {
				case h.You || (h.Version != "" && !setup.Older(h.Version, target)):
				case h.State == "not synced yet" || h.State == "offline":
					fmt.Fprintf(stdout, "sunstack: skipping %s (%s)\n", h.Name, h.State)
				default:
					names = append(names, h.Name)
				}
			}
			if len(names) == 0 {
				fmt.Fprintf(stdout, "sunstack: every host runs %s or newer\n", target)
				return nil
			}
		case len(a.pos) == 1 && !a.has("all"):
			names = a.pos
		default:
			return missing("a host name, or --all")
		}
		if !a.has("yes") {
			what := "update sunstack to " + target + " on " + strings.Join(names, ", ") + " and restart its connector"
			if !setup.IsTerminal(os.Stdin) {
				return &core.Error{Code: core.ExitUsage, Reason: "confirm", Msg: "this will " + what + "; rerun with --yes once the user has confirmed"}
			}
			if !setup.Confirm(stdin, stdout, "This will "+what+". Continue?", false) {
				fmt.Fprintln(stdout, "sunstack: nothing updated")
				return nil
			}
		}
		failed := 0
		for _, n := range names {
			r, err := hub.Update(n, target)
			if err != nil {
				failed++
				fmt.Fprintf(stdout, "sunstack: %s: %v\n", n, err)
				continue
			}
			fmt.Fprintf(stdout, "sunstack: %s updated %s -> %s; its connector restarts\n", n, r.From, r.To)
		}
		if failed > 0 {
			return &core.Error{Code: core.ExitFail, Reason: "update_failed", Msg: fmt.Sprintf("%d host(s) not updated", failed)}
		}
		return nil
	case "allow", "deny":
		a, err := parse(rest[1:], "", "")
		if err != nil {
			return err
		}
		if err := core.UserOnly("sunstack org " + rest[0]); err != nil {
			return err
		}
		if rest[0] == "allow" && len(a.pos) == 0 {
			fmt.Fprint(stdout, hub.GrantsText())
			return nil
		}
		if len(a.pos) < 2 {
			return missing("host name, then peek, spawn or update")
		}
		if rest[0] == "deny" {
			if err := hub.Deny(a.pos[0], a.pos[1:]); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "sunstack: %s may no longer %s here\n", a.pos[0], strings.Join(a.pos[1:], " or "))
			return nil
		}
		g, err := hub.Allow(a.pos[0], a.pos[1:])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s (key %s) may now %s here. Check the key on %s with sunstack org keys. This allows:\n%s",
			g.Name, g.Fingerprint, strings.Join(g.Kinds, ", "), g.Name, hub.GrantWarning(g.Kinds))
		return nil
	case "trust":
		a, err := parse(rest[1:], "", "")
		if err == nil {
			err = a.atMost(1, "org trust")
		}
		if err != nil {
			return err
		}
		if len(a.pos) < 1 {
			return missing("host name")
		}
		fp, err := hub.Trust(a.pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "sunstack: %s's new key is trusted (%s)\n", a.pos[0], fp)
		return nil
	}
	a, err := parse(rest[1:], "code", "")
	if err == nil {
		err = a.atMost(1, "org join")
	}
	if err != nil {
		return err
	}
	if len(a.pos) < 1 || a.flags["code"] == "" {
		return missing("the hub's Tailscale name and --code (sunstack hub invite on the hub)")
	}
	c, err := hub.Join(a.pos[0], a.flags["code"])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "sunstack: joined org %s through %s\nnext: sunstack hub connect --install\n", c.OrgName, c.HubURL)
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
	fmt.Fprintf(stdout, "sunstack: sent %s to %s:%s (%s)\n", s.Mail.ID, s.ToName, s.Letter.To, where)
	return nil
}

// sentText lists messages sent to other hosts with their place.
func sentText(all bool) string {
	var b strings.Builder
	for _, s := range hub.SentMail() {
		if !all && s.Status == "delivered" && s.Letter.Type != "task" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("\nSent to other hosts\n")
		}
		line := fmt.Sprintf("  %s  %s -> %s:%s  %s", s.Mail.ID, s.Letter.Type, s.ToName, s.Letter.To, s.Status)
		if s.Reason != "" {
			line += " (" + s.Reason + ")"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

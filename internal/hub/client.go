package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// sshProgram is the ssh client, or $SUNSTACK_SSH (tests use a stand-in).
func sshProgram() string {
	if p := os.Getenv("SUNSTACK_SSH"); p != "" {
		return p
	}
	return "ssh"
}

// client reaches the hub: over SSH with the dedicated key, or, on the hub
// itself, by running hub serve as a child, so the hub's own traffic goes
// through the same checks as every other host's (§18.5).
type client struct{ target string }

func (c *client) command(verb string) (*exec.Cmd, error) {
	if c.target == LocalHub {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(exe, "hub", "serve", "--host", core.ThisHost().ID)
		cmd.Env = append(os.Environ(), "SSH_ORIGINAL_COMMAND="+verb)
		return cmd, nil
	}
	args := []string{"-i", KeyPath(), "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-T"}
	if verb == "watch" {
		args = append(args, "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3")
	}
	return exec.Command(sshProgram(), append(args, c.target, verb)...), nil
}

// call runs a one-shot verb and decodes its reply.
func (c *client) call(verb string, stdin []byte) (*frame, error) {
	cmd, err := c.command(verb)
	if err != nil {
		return nil, err
	}
	var out, errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(stdin), &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	var f frame
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &f); err != nil {
		return nil, fmt.Errorf("unexpected reply from the hub: %.200s", out.String())
	}
	return &f, nil
}

func (c *client) hello() (*Hello, error) {
	f, err := c.call("hello", nil)
	if err != nil {
		return nil, err
	}
	if f.Hello == nil {
		return nil, errors.New("the hub did not answer hello")
	}
	return f.Hello, nil
}

func joined() (*Config, *client, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, nil, failErr("org", "%v", err)
	}
	if c == nil {
		return nil, nil, failErr("no_org", "this host is in no org (sunstack org join <hub>)")
	}
	return c, &client{target: c.Hub}, nil
}

// Snapshot is this host's org snapshot as it crosses hosts: every session's
// title, last prompt and last reply removed, agent sessions included (§18.1).
func Snapshot() ([]byte, error) {
	o := core.BuildOrg()
	stripActivity(o)
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFrame {
		return nil, errors.New("this host's snapshot is larger than 256 KB")
	}
	return b, nil
}

// stripActivity removes every session's title, last prompt and last reply.
func stripActivity(o *core.Org) {
	strip := func(ss []*core.HostSession) {
		for _, s := range ss {
			s.Activity = core.Activity{LastActive: s.Activity.LastActive}
		}
	}
	for _, t := range o.Teams {
		strip(t.Free)
		for _, a := range t.Agents {
			strip(a.Sessions)
		}
	}
	for _, g := range o.Free {
		strip(g.Sessions)
	}
}

// PushOnce sends this host's snapshot in one call.
func PushOnce() error {
	_, cl, err := joined()
	if err != nil {
		return err
	}
	b, err := Snapshot()
	if err != nil {
		return err
	}
	_, err = cl.call("push", b)
	return err
}

// PullOnce fetches the roster and the other hosts' snapshots in one call and
// stores them in ~/.sunstack/remote/.
func PullOnce() (*Roster, error) {
	_, cl, err := joined()
	if err != nil {
		return nil, err
	}
	f, err := cl.call("pull", nil)
	if err != nil {
		return nil, err
	}
	if f.Roster == nil {
		return nil, errors.New("the hub sent no roster")
	}
	if err := saveRoster(f.Roster, f.HubTime); err != nil {
		return nil, err
	}
	for _, sn := range f.Snaps {
		_ = saveSnap(sn, f.HubTime)
	}
	return f.Roster, nil
}

// Cached is what this host knows of the org, from ~/.sunstack/remote/.
type cachedRoster struct {
	Roster    *Roster `json:"roster"`
	HubTime   string  `json:"hub_time"`   // the hub's clock when sent
	LocalTime string  `json:"local_time"` // this host's clock when received
}

type cachedSnap struct {
	Snap
	HubTime   string `json:"hub_time"`
	LocalTime string `json:"local_time"`
}

func saveRoster(r *Roster, hubTime string) error {
	return writeJSON(filepath.Join(remoteDir(), "roster.json"), &cachedRoster{Roster: r, HubTime: hubTime, LocalTime: stamp(time.Now())}, 0o600)
}

func saveSnap(sn *Snap, hubTime string) error {
	if !isFileName(sn.Host) {
		return errors.New("bad host id")
	}
	return writeJSON(filepath.Join(remoteDir(), sn.Host+".json"), &cachedSnap{Snap: *sn, HubTime: hubTime, LocalTime: stamp(time.Now())}, 0o600)
}

// Sent is a message this host sent to another host, with its place.
type Sent struct {
	Mail   Mail   `json:"mail"`
	ToName string `json:"to_name"`
	Status string `json:"status"` // queued, at hub, delivered, refused
	Reason string `json:"reason,omitempty"`
	At     string `json:"at"`
}

// Queue puts a message for another host in the outbox (§18.8). to is
// "<host>:<address>"; from is "user" or "<team>/<agent>".
func Queue(to, from, typ, body, replyTo string) (*Sent, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, failErr("org", "%v", err)
	}
	if c == nil {
		return nil, failErr("no_org", "this host is in no org, so it cannot send to another host (sunstack org join <hub>)")
	}
	hostName, addr, ok := SplitAddress(to)
	if !ok {
		return nil, usageErr("address another host as <host>:<team>/<agent>[_task] or <host>:<session ID>")
	}
	if !ValidAddress(addr) {
		return nil, usageErr("on another host, address <team>/<agent>[_task] or a session ID, not %q: a bare agent is ambiguous there and pane IDs are reused", addr)
	}
	h := knownHost(hostName)
	if h == nil {
		return nil, failErr("not_found", "no host %s in the org (sunstack org --refresh, then sunstack org --by host)", hostName)
	}
	if h.ID == core.ThisHost().ID {
		return nil, usageErr("%s is this host; send to %s", hostName, addr)
	}
	if typ == "" {
		typ = "fyi"
	}
	if !contains(RemoteTypes, typ) {
		return nil, usageErr("type %s cannot be sent to another host (task, question, handoff, fyi or done)", typ)
	}
	if typ == "task" {
		if err := core.CheckBrief(body); err != nil {
			return nil, err
		}
	}
	m := Mail{ID: core.NewMessageID(from), ToHost: h.ID, From: from, To: addr, Type: typ, ReplyTo: replyTo, At: stamp(time.Now()), Body: body}
	if err := checkMail(&m); err != nil {
		return nil, usageErr("%v", err)
	}
	if b, _ := json.Marshal(m); len(b) > MaxFrame {
		return nil, usageErr("the message is larger than 256 KB")
	}
	if err := writeJSON(filepath.Join(outboxDir(), m.ID+".json"), &m, 0o600); err != nil {
		return nil, err
	}
	return &Sent{Mail: m, ToName: h.Name, Status: "queued", At: m.At}, nil
}

// knownHost finds a host by name in the cached roster (or, on the hub, its
// own roster).
func knownHost(name string) *Host {
	var r *Roster
	if c, _ := LoadConfig(); c != nil && c.IsHub() {
		r, _ = store{hubDir(c.OrgID)}.roster()
	}
	if r == nil {
		var cr cachedRoster
		if readJSON(filepath.Join(remoteDir(), "roster.json"), &cr) == nil {
			r = cr.Roster
		}
	}
	if r == nil {
		return nil
	}
	return r.byName(name)
}

// FlushOnce sends the outbox in one call per message. It is what send does
// when no connector runs. A failed connection leaves the mail queued.
func FlushOnce() error {
	_, cl, err := joined()
	if err != nil {
		return err
	}
	for _, p := range jsonFiles(outboxDir()) {
		var m Mail
		if readJSON(p, &m) != nil {
			continue
		}
		b, _ := json.Marshal(m)
		f, err := cl.call("mail", b)
		if err != nil {
			return fmt.Errorf("hub unreachable: %v", err)
		}
		settle(&m, f.Op, f.Reason)
	}
	return nil
}

// settle moves a message from the outbox to sent with the hub's answer.
func settle(m *Mail, op, reason string) {
	status := "at hub"
	if op == "refused" {
		status = "refused"
	} else if op != "stored" {
		return
	}
	src := filepath.Join(outboxDir(), m.ID+".json")
	var s Sent
	dst := filepath.Join(sentDir(), m.ID+".json")
	if readJSON(dst, &s) == nil && (s.Status == "delivered" || s.Status == "refused") {
		os.Remove(src)
		return
	}
	name := m.ToHost
	if h := knownHostByID(m.ToHost); h != nil {
		name = h.Name
	}
	if writeJSON(dst, &Sent{Mail: *m, ToName: name, Status: status, Reason: reason, At: stamp(time.Now())}, 0o600) == nil {
		os.Remove(src)
	}
}

// record applies a receipt to a sent message.
func record(rc *Receipt) {
	if !isFileName(rc.ID) {
		return
	}
	dst := filepath.Join(sentDir(), rc.ID+".json")
	var s Sent
	if readJSON(dst, &s) != nil {
		// The receipt came before the hub's answer was recorded.
		if readJSON(filepath.Join(outboxDir(), rc.ID+".json"), &s.Mail) != nil {
			return
		}
		os.Remove(filepath.Join(outboxDir(), rc.ID+".json"))
		s.ToName = rc.ToHost
		if h := knownHostByID(rc.ToHost); h != nil {
			s.ToName = h.Name
		}
	}
	if s.Status == rc.Status && s.Reason == rc.Reason {
		return
	}
	s.Status, s.Reason, s.At = rc.Status, rc.Reason, rc.At
	_ = writeJSON(dst, &s, 0o600)
}

func knownHostByID(id string) *Host {
	var cr cachedRoster
	if readJSON(filepath.Join(remoteDir(), "roster.json"), &cr) == nil && cr.Roster != nil {
		return cr.Roster.byID(id)
	}
	return nil
}

// SentMail lists messages this host sent to other hosts, queued first.
func SentMail() []*Sent {
	var out []*Sent
	for _, p := range jsonFiles(outboxDir()) {
		var m Mail
		if readJSON(p, &m) == nil {
			name := m.ToHost
			if h := knownHostByID(m.ToHost); h != nil {
				name = h.Name
			}
			out = append(out, &Sent{Mail: m, ToName: name, Status: "queued", At: m.At})
		}
	}
	for _, p := range jsonFiles(sentDir()) {
		var s Sent
		if readJSON(p, &s) == nil {
			out = append(out, &s)
		}
	}
	return out
}

// Deliver puts mail from another host into the local inbox it names,
// keeping its ID, so a second copy is impossible (§18.5). final is false
// for a failure worth retrying later (a busy lock, the file system).
func Deliver(m *Mail) (status, reason string, final bool) {
	if err := checkMail(m); err != nil {
		return "refused", err.Error(), true
	}
	sum := sha256.Sum256([]byte(m.ID))
	opts := core.SendOptions{
		Body: m.Body, Type: m.Type, ReplyTo: m.ReplyTo,
		FromLabel: m.FromName + ":" + m.From, FromHost: m.FromName,
		ID: m.ID, Op: "x" + hex.EncodeToString(sum[:16]),
	}
	var err error
	if core.IsSessionID(m.To) {
		var s *core.HostSession
		if s, err = core.FindSession(m.To, ""); err == nil {
			if s.Agent != "" {
				var dst *core.Project
				if dst, err = core.FindProject(s.TeamRoot); err == nil {
					opts.To = strings.SplitN(s.Label, "@", 2)[0]
					_, err = dst.Send(opts)
				}
			} else {
				_, err = core.SendToSession(s, opts)
			}
		}
	} else {
		team, agent, _ := strings.Cut(m.To, "/")
		var dst *core.Project
		if dst, err = core.ResolveTeam(team); err == nil {
			opts.To = agent
			_, err = dst.Send(opts)
		}
	}
	if err == nil {
		return "delivered", "", true
	}
	var ce *core.Error
	if errors.As(err, &ce) && (ce.Reason == "busy" || ce.Reason == "fs") {
		return "", ce.Msg, false
	}
	if errors.As(err, &ce) {
		return "refused", ce.Msg, true
	}
	return "", err.Error(), false
}

package hub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lucklyric/sunstack/internal/core"
)

func httpClient(timeout time.Duration) *http.Client { return &http.Client{Timeout: timeout} }

// client calls the hub over HTTP with this host's token. On the hub, it
// calls its own listener, so the hub's own traffic takes the same checks.
type client struct{ c *Config }

func (cl *client) url() (string, error) {
	if !cl.c.IsHub() {
		return cl.c.HubURL, nil
	}
	b, err := os.ReadFile(store{hubDir(cl.c.OrgID)}.addrFile())
	if err != nil {
		return "", errors.New("the hub's listener is not running (sunstack hub connect)")
	}
	return "http://" + strings.TrimSpace(string(b)), nil
}

func (cl *client) request(method, path string, body []byte, timeout time.Duration) (*http.Response, error) {
	base, err := cl.url()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cl.c.Token)
	req.Header.Set("X-Sunstack-Version", Version)
	req.Header.Set("X-Sunstack-Machine", core.MachineName())
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient(timeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("hub unreachable: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg := readError(resp)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, &revoked{msg}
		}
		return nil, errors.New(msg)
	}
	return resp, nil
}

type revoked struct{ msg string }

func (r *revoked) Error() string { return r.msg }

// call makes one request and decodes its answer.
func (cl *client) call(method, path string, body []byte) (*frame, error) {
	resp, err := cl.request(method, path, body, 20*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var f frame
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&f); err != nil {
		return nil, fmt.Errorf("unexpected answer from the hub: %v", err)
	}
	return &f, nil
}

func joined() (*Config, *client, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, nil, failErr("org", "%v", err)
	}
	if c == nil {
		return nil, nil, failErr("no_org", "this host is in no org (sunstack org join <hub> --code <code>)")
	}
	if c.IsManaged() {
		// Every call to the hub goes through here; a managed host makes none.
		return nil, nil, managedErr("never connects to the hub")
	}
	return c, &client{c: c}, nil
}

// Snapshot is this host's org snapshot as it crosses hosts: every session's
// title, last prompt and last reply removed, agent sessions included (§18.1).
func Snapshot() ([]byte, error) {
	o := core.BuildOrg()
	stripActivity(o)
	o.SSH = core.SSHPeers()
	return snapshotOf(o)
}

// lineCut is the longest board line another host gets (§29.2).
const lineCut = 400

// cutLine shortens s to lineCut characters, the last one "…".
func cutLine(s string) string {
	if utf8.RuneCountInString(s) <= lineCut {
		return s
	}
	return string([]rune(s)[:lineCut-1]) + "…"
}

// snapshotOf encodes o. Over MaxFrame it cuts long board lines, then drops
// the agents' done entries, then the objectives' key results, and says so in
// Trimmed; sessions always stay.
func snapshotOf(o *core.Org) ([]byte, error) {
	b, err := json.Marshal(o)
	if err != nil || len(b) <= MaxFrame {
		return b, err
	}
	cut := func(xs []string) {
		for i := range xs {
			xs[i] = cutLine(xs[i])
		}
	}
	for _, t := range o.Teams {
		for _, a := range t.Agents {
			cut(a.Now)
			cut(a.Done)
		}
		for i := range t.Objectives {
			cut(t.Objectives[i].KRs)
		}
	}
	if b, err = json.Marshal(o); err != nil || len(b) <= MaxFrame {
		return b, err
	}
	done, krs := 0, 0
	for _, t := range o.Teams {
		for _, a := range t.Agents {
			done += len(a.Done)
			a.Done = []string{}
		}
	}
	o.Trimmed = fmt.Sprintf("snapshot cut to fit 256 KB: %d done entries not shown", done)
	if b, err = json.Marshal(o); err != nil || len(b) <= MaxFrame {
		connIssue("snapshot_trimmed", o.Trimmed)
		return b, err
	}
	for _, t := range o.Teams {
		for i := range t.Objectives {
			krs += len(t.Objectives[i].KRs)
			t.Objectives[i].KRs = []string{}
		}
	}
	o.Trimmed = fmt.Sprintf("snapshot cut to fit 256 KB: %d done entries and %d key results not shown", done, krs)
	if b, err = json.Marshal(o); err != nil || len(b) <= MaxFrame {
		connIssue("snapshot_trimmed", o.Trimmed)
		return b, err
	}
	return nil, errors.New("this host's snapshot is larger than 256 KB, even without done entries and key results")
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
	_, err = cl.call("POST", "/v1/push", b)
	return err
}

// PullOnce fetches the roster and the other hosts' snapshots in one call and
// stores them in ~/.sunstack/remote/.
func PullOnce() (*Roster, error) { return pull("/v1/pull") }

// RefreshOnce is PullOnce after the hub pulled its managed hosts (§25.6).
func RefreshOnce() (*Roster, error) { return pull("/v1/pull?refresh=1") }

func pull(path string) (*Roster, error) {
	_, cl, err := joined()
	if err != nil {
		return nil, err
	}
	f, err := cl.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if f.Roster == nil {
		return nil, errors.New("the hub sent no roster")
	}
	if err := takeRoster(f.Roster, f.HubTime); err != nil {
		return nil, err
	}
	for _, sn := range f.Snaps {
		_ = saveSnap(sn, f.HubTime)
	}
	return f.Roster, nil
}

// takeRoster pins new keys and stores the roster.
func takeRoster(r *Roster, hubTime string) error {
	if err := pinRoster(r); err != nil {
		return err
	}
	return saveRoster(r, hubTime)
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
	// The hub names the host; only an ID may become a file name here, next
	// to pins.json and allow.json.
	if !idRe.MatchString(sn.Host) {
		return errors.New("bad host id")
	}
	return writeJSON(filepath.Join(remoteDir(), sn.Host+".json"), &cachedSnap{Snap: *sn, HubTime: hubTime, LocalTime: stamp(time.Now())}, 0o600)
}

func cachedHosts() *Roster {
	var cr cachedRoster
	if readJSON(filepath.Join(remoteDir(), "roster.json"), &cr) == nil {
		return cr.Roster
	}
	return nil
}

// Sent is a message this host sent to another host, with its place. The
// outbox keeps the letter in the clear on this host; only the sealed mail
// leaves it.
type Sent struct {
	Mail   Mail   `json:"mail"`
	Letter Letter `json:"letter"`
	ToName string `json:"to_name"`
	Status string `json:"status"` // queued, at hub, delivered, refused
	Reason string `json:"reason,omitempty"`
	At     string `json:"at"`
}

// Queue seals a message for another host and puts it in the outbox
// (§18.8, §19.4). to is "<host>:<address>"; from is "user" or
// "<team>/<agent>". fromSession, the sending CLI session, is kept only for
// the user, so a reply can reach a session that works as no agent.
func Queue(to, from, typ, body, replyTo, fromSession string) (*Sent, error) {
	if replyTo == "" {
		if err := RefuseManaged("sends to another host only a reply to a message it got from that host"); err != nil {
			return nil, err
		}
	}
	hostName, addr, ok := SplitAddress(to)
	if !ok {
		return nil, usageErr("address another host as <host>:<team>/<agent>[_task] or <host>:<session ID>")
	}
	if !ValidAddress(addr) {
		return nil, usageErr("on another host, address <team>/<agent>[_task] or a session ID, not %q: a bare agent is ambiguous there and pane IDs are reused", addr)
	}
	if typ == "" {
		typ = "fyi"
	}
	l := &Letter{From: from, To: addr, Type: typ, ReplyTo: replyTo, Body: body}
	if from == "user" && core.IsSessionID(fromSession) {
		l.FromSession = fromSession
	}
	if err := checkLetter(l); err != nil {
		return nil, usageErr("%v", err)
	}
	if typ == "task" {
		if err := core.CheckBrief(body); err != nil {
			return nil, err
		}
	}
	return queueLetter(hostName, l, true)
}

// queueLetter seals a letter for the host called hostName and puts it in
// the outbox. keep says whether the outbox keeps the letter in the clear; a
// control reply is kept only sealed (§20.5).
func queueLetter(hostName string, l *Letter, keep bool) (*Sent, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, failErr("org", "%v", err)
	}
	if c == nil {
		return nil, failErr("no_org", "this host is in no org, so it cannot send to another host (sunstack org join <hub> --code <code>)")
	}
	r := cachedHosts()
	var h *Host
	if r != nil {
		h = r.byName(hostName)
	}
	if h == nil {
		return nil, failErr("not_found", "no host %s in the org (sunstack org --refresh, then sunstack org --by host)", hostName)
	}
	if h.ID == core.ThisHost().ID {
		return nil, usageErr("%s is this host", hostName)
	}
	if p := loadPins()[h.ID]; p != nil && !strings.EqualFold(p.Name, h.Name) {
		return nil, failErr("name_changed", "the hub now lists another host as %s (pinned as %s); ask the hub's user, and check sunstack org keys", h.Name, p.Name)
	}
	return queueTo(h.ID, h.Name, l, keep)
}

// queueTo seals a letter for the host with ID id, called name, with the
// key pinned for that ID.
func queueTo(id, name string, l *Letter, keep bool) (*Sent, error) {
	me := core.ThisHost()
	pin := loadPins()[id]
	switch {
	case pin == nil:
		return nil, failErr("no_key", "no key pinned for %s yet (sunstack org --refresh)", name)
	case pin.Changed != nil:
		return nil, failErr("key_changed", "%s's key changed; compare fingerprints (sunstack org keys) and run sunstack org trust %s", name, name)
	}
	l.At = stamp(time.Now())
	keys, err := LoadKeys()
	if err != nil {
		return nil, err
	}
	m := Mail{ID: core.NewMessageID(l.From), FromHost: me.ID, ToHost: id}
	if c, _ := LoadConfig(); c.IsManaged() {
		// §25.5: a managed host only answers what it got from that host.
		if !importedFrom(l.ReplyTo, id) {
			return nil, managedErr("sends to another host only a reply to a message it got from that host")
		}
		m.InReply = l.ReplyTo
	}
	if err := seal(keys, pin.Keys, &m, l); err != nil {
		return nil, err
	}
	if b, _ := json.Marshal(m); len(b) > MaxFrame {
		return nil, usageErr("the message is larger than 256 KB")
	}
	s := &Sent{Mail: m, ToName: name, Status: "queued", At: l.At}
	if keep {
		s.Letter = *l
	} else {
		s.Letter = Letter{From: l.From, To: l.To, Type: l.Type, ReplyTo: l.ReplyTo, FromSession: l.FromSession, At: l.At}
	}
	if err := writeJSON(filepath.Join(outboxDir(), m.ID+".json"), s, 0o600); err != nil {
		return nil, err
	}
	return s, nil
}

// FlushOnce sends the outbox in one call per message. It is what send does
// when no connector runs. A failed connection leaves the mail queued.
func FlushOnce() error {
	_, cl, err := joined()
	if err != nil {
		return err
	}
	for _, p := range jsonFiles(outboxDir()) {
		var s Sent
		if readJSON(p, &s) != nil {
			continue
		}
		b, _ := json.Marshal(s.Mail)
		f, err := cl.call("POST", "/v1/mail", b)
		if err != nil {
			return err
		}
		settle(s.Mail.ID, f.Op, f.Reason)
	}
	return nil
}

// sentMu orders the hub's answer (settle) and the receiver's receipt
// (record) for a message, so one never overwrites the other.
var sentMu sync.Mutex

// settle moves a message from the outbox to sent with the hub's answer.
func settle(id, op, reason string) {
	if !isFileName(id) {
		return
	}
	sentMu.Lock()
	defer sentMu.Unlock()
	src := filepath.Join(outboxDir(), id+".json")
	status := "at hub"
	switch op {
	case "refused":
		status = "refused"
	case "retry":
		// It stays queued; the reason says why it waits.
		var s Sent
		if readJSON(src, &s) == nil && s.Reason != reason {
			s.Reason = reason
			_ = writeJSON(src, &s, 0o600)
		}
		return
	case "stored":
	default:
		return
	}
	dst := filepath.Join(sentDir(), id+".json")
	var done Sent
	if readJSON(dst, &done) == nil && (done.Status == "delivered" || done.Status == "refused") {
		os.Remove(src)
		return
	}
	var s Sent
	if readJSON(src, &s) != nil {
		return
	}
	s.Status, s.Reason, s.At = status, reason, stamp(time.Now())
	if writeJSON(dst, &s, 0o600) == nil {
		os.Remove(src)
	}
}

// record applies a receipt to a sent message. A receipt that comes before
// the hub's answer moves the mail from the outbox, removing the queued copy
// only once the sent one is written.
func record(rc *Receipt) error {
	if !isFileName(rc.ID) {
		return nil
	}
	sentMu.Lock()
	defer sentMu.Unlock()
	dst := filepath.Join(sentDir(), rc.ID+".json")
	src := ""
	var s Sent
	if readJSON(dst, &s) != nil {
		src = filepath.Join(outboxDir(), rc.ID+".json")
		if readJSON(src, &s) != nil {
			return nil
		}
	} else if s.Status == rc.Status && s.Reason == rc.Reason {
		return nil
	}
	s.Status, s.Reason, s.At = rc.Status, rc.Reason, rc.At
	if err := writeJSON(dst, &s, 0o600); err != nil {
		return err
	}
	if src != "" {
		os.Remove(src)
	}
	return nil
}

// SentMail lists messages this host sent to other hosts, queued first.
func SentMail() []*Sent {
	var out []*Sent
	for _, dir := range []string{outboxDir(), sentDir()} {
		for _, p := range jsonFiles(dir) {
			var s Sent
			if readJSON(p, &s) == nil {
				out = append(out, &s)
			}
		}
	}
	return out
}

// Deliver opens mail from another host and puts it in the local inbox it
// names, keeping its ID, so a second copy is impossible (§18.5, §19.4).
// final is false for a failure worth retrying later.
func Deliver(m *Mail) (status, reason string, final bool) {
	if !core.IsMessageID(m.ID) {
		return "refused", "invalid message id", true
	}
	pin := loadPins()[m.FromHost]
	if pin == nil {
		// A host that joined after this one's last roster: pin it first.
		if _, err := PullOnce(); err == nil {
			pin = loadPins()[m.FromHost]
		}
	}
	switch {
	case pin == nil:
		return "", "no key for the sending host yet", false
	case pin.Changed != nil:
		return "refused", "the sending host's key changed; on the receiving host, compare fingerprints and run sunstack org trust " + pin.Name, true
	}
	keys, err := LoadKeys()
	if err != nil {
		return "", err.Error(), false
	}
	l, err := open(keys, pin.Keys, m)
	if err != nil {
		return "refused", err.Error(), true
	}
	if m.InReply != "" && l.ReplyTo != m.InReply {
		return "refused", "the letter answers another message than its envelope says", true
	}
	if isControl(l.Type) {
		// §20.3: never put in an inbox.
		if err := checkControl(l); err != nil {
			return "refused", err.Error(), true
		}
		if isReply(l.Type) {
			return takeReply(m, l)
		}
		if err := noteImported(m); err != nil {
			return "", err.Error(), false
		}
		return takeRequest(m, pin, l)
	}
	if err := checkLetter(l); err != nil {
		return "refused", err.Error(), true
	}
	if err := noteImported(m); err != nil {
		return "", err.Error(), false
	}
	sum := sha256.Sum256([]byte(m.ID))
	opts := core.SendOptions{
		Body: l.Body, Type: l.Type, ReplyTo: l.ReplyTo,
		FromLabel: pin.Name + ":" + l.From, FromHost: pin.Name,
		ID: m.ID, Op: "x" + hex.EncodeToString(sum[:16]),
	}
	if l.From == "user" && l.FromSession != "" {
		opts.FromSession = pin.Name + ":" + l.FromSession
	}
	if core.IsSessionID(l.To) {
		var s *core.HostSession
		if s, err = core.FindSession(l.To, ""); err == nil {
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
		team, agent, _ := strings.Cut(l.To, "/")
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

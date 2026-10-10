package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Control requests (§20.3): spawn, peek and update travel sealed and
// signed like mail, but are never put in an inbox. The receiving host
// checks its grants, records the request before acting, runs one request at
// a time, and answers with a sealed reply that only the asking host, and
// only for a request it still has open, accepts.

// ControlKinds are the requests; each is answered by <kind>-reply.
var ControlKinds = []string{"spawn", "peek", "update"}

// Clock skew allowed on a request's expiry.
const skew = 2 * time.Minute

// Control is the body of a control letter.
type Control struct {
	Expires string          `json:"expires,omitempty"` // requests
	Args    json.RawMessage `json:"args,omitempty"`    // requests
	Error   string          `json:"error,omitempty"`   // replies: the refusal or failure
	Result  json.RawMessage `json:"result,omitempty"`  // replies
}

func isControl(typ string) bool { return slices.Contains(ControlKinds, typ) || isReply(typ) }
func isReply(typ string) bool {
	for _, k := range ControlKinds {
		if typ == k+"-reply" {
			return true
		}
	}
	return false
}

func checkControl(l *Letter) error {
	var c Control
	switch {
	case l.From != "user" || l.To != "host":
		return errors.New("a control request is from the user to a host")
	case json.Unmarshal([]byte(l.Body), &c) != nil:
		return errors.New("bad control body")
	case isReply(l.Type) && !core.IsMessageID(l.ReplyTo):
		return errors.New("a reply names the request it answers")
	case !isReply(l.Type) && c.Expires == "":
		return errors.New("a request carries its expiry")
	}
	return nil
}

func askedDir() string   { return filepath.Join(remoteDir(), "asked") }
func repliesDir() string { return filepath.Join(remoteDir(), "replies") }
func journalPath() string {
	return filepath.Join(remoteDir(), "requests.json")
}

// asked is a request this host sent and still waits on.
type asked struct {
	ID, HostID, Host, Kind, Expires string
}

// Request seals a control request for the host called hostName and queues
// it. The caller waits for the reply with Await.
func Request(hostName, kind string, args any, ttl time.Duration) (string, error) {
	if !slices.Contains(ControlKinds, kind) {
		return "", usageErr("unknown request %s", kind)
	}
	// Its reply goes through the hub's can_send check (§20.3).
	if r := cachedHosts(); r != nil {
		if h := r.byName(hostName); h != nil && !h.CanSend {
			return "", failErr("read_only", "%s is read-only in the org, so it cannot answer requests; on the hub, revoke it and invite it again without --read-only", h.Name)
		}
	}
	cleanAsked()
	b, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	exp := stamp(time.Now().Add(ttl))
	body, _ := json.Marshal(Control{Expires: exp, Args: b})
	s, err := queueLetter(hostName, &Letter{From: "user", To: "host", Type: kind, Body: string(body)}, true)
	if err != nil {
		return "", err
	}
	a := asked{ID: s.Mail.ID, HostID: s.Mail.ToHost, Host: s.ToName, Kind: kind, Expires: exp}
	return a.ID, writeJSON(filepath.Join(askedDir(), a.ID+".json"), a, 0o600)
}

// Await waits up to timeout for the reply to request id, opens it in
// memory and deletes the parked file. A refusal at the hub, or by the
// other host, comes back as an error.
func Await(id string, timeout time.Duration) (*Control, error) {
	if !isFileName(id) {
		return nil, usageErr("invalid request id")
	}
	var a asked
	if readJSON(filepath.Join(askedDir(), id+".json"), &a) != nil {
		return nil, failErr("not_found", "no open request %s", id)
	}
	deadline := time.Now().Add(timeout)
	for {
		var m Mail
		path := filepath.Join(repliesDir(), id+".json")
		if readJSON(path, &m) == nil {
			os.Remove(path)
			os.Remove(filepath.Join(askedDir(), id+".json"))
			pin := loadPins()[a.HostID]
			keys, err := LoadKeys()
			if err != nil || pin == nil {
				return nil, failErr("no_key", "cannot open the reply from %s", a.Host)
			}
			l, err := open(keys, pin.Keys, &m)
			if err != nil || l.Type != a.Kind+"-reply" || l.ReplyTo != id || m.FromHost != a.HostID {
				return nil, failErr("bad_reply", "the reply from %s does not match the request", a.Host)
			}
			var c Control
			if json.Unmarshal([]byte(l.Body), &c) != nil {
				return nil, failErr("bad_reply", "the reply from %s cannot be read", a.Host)
			}
			if c.Error != "" {
				return &c, failErr("refused", "%s: %s", a.Host, c.Error)
			}
			return &c, nil
		}
		var s Sent
		if readJSON(filepath.Join(sentDir(), id+".json"), &s) == nil && s.Status == "refused" {
			os.Remove(filepath.Join(askedDir(), id+".json"))
			hint := ""
			if strings.Contains(s.Reason, "type") {
				hint = " (a host before v0.10 cannot take requests; update it by hand once)"
			}
			return nil, failErr("refused", "%s refused it: %s%s", a.Host, s.Reason, hint)
		}
		if time.Now().After(deadline) {
			return nil, failErr("timeout", "no reply from %s yet (request %s); its connector may be down", a.Host, id)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// takeReply parks a reply, still sealed, for the command waiting on it.
// Anything else is dropped.
func takeReply(m *Mail, l *Letter) (string, string, bool) {
	var a asked
	if !isFileName(l.ReplyTo) || readJSON(filepath.Join(askedDir(), l.ReplyTo+".json"), &a) != nil ||
		a.HostID != m.FromHost || l.Type != a.Kind+"-reply" {
		return "refused", "no open request matches this reply", true
	}
	if err := writeJSON(filepath.Join(repliesDir(), l.ReplyTo+".json"), m, 0o600); err != nil {
		return "", err.Error(), false
	}
	return "delivered", "", true
}

// journal entry: a request this host received.
type received struct {
	From     string `json:"from"`                // the asking host's name
	FromHost string `json:"from_host,omitempty"` // and its ID, where the reply goes
	Kind     string `json:"kind"`
	Digest   string `json:"digest"`
	Status   string `json:"status"` // queued, running, done, uncertain
	Reply    *Mail  `json:"reply,omitempty"`
	Expires  string `json:"expires"`
}

var journalMu sync.Mutex

func loadJournal() map[string]*received {
	j := map[string]*received{}
	_ = readJSON(journalPath(), &j)
	return j
}

// saveJournal keeps entries until a day after they expire.
func saveJournal(j map[string]*received) error {
	for id, e := range j {
		if t, ok := parseStamp(e.Expires); ok && time.Since(t) > 24*time.Hour {
			delete(j, id)
		}
	}
	return writeJSON(journalPath(), j, 0o600)
}

type job struct {
	mail *Mail
	from *Pin
	kind string
	args json.RawMessage
}

var (
	jobsOnce sync.Once
	jobs     chan job
)

// Handlers run requests on this host; they return the reply's result.
var handlers = map[string]func(from, id string, args json.RawMessage) (any, error){
	"peek":   peekHandler,
	"spawn":  spawnHandler,
	"update": updateHandler,
}

// takeRequest records a request, then queues it for the worker. A request
// seen before gets its recorded reply again; it is never run twice.
func takeRequest(m *Mail, pin *Pin, l *Letter) (string, string, bool) {
	var c Control
	_ = json.Unmarshal([]byte(l.Body), &c)
	exp, ok := parseStamp(c.Expires)
	if !ok || time.Now().After(exp.Add(skew)) {
		return "refused", "the request expired", true
	}
	sum := sha256.Sum256([]byte(l.Type + "\n" + string(c.Args)))
	digest := hex.EncodeToString(sum[:])
	journalMu.Lock()
	j := loadJournal()
	if e := j[m.ID]; e != nil {
		journalMu.Unlock()
		if e.Digest == digest && e.Reply != nil {
			requeue(e.Reply, pin.Name)
		}
		return "delivered", "", true
	}
	j[m.ID] = &received{From: pin.Name, FromHost: m.FromHost, Kind: l.Type, Digest: digest, Status: "queued", Expires: c.Expires}
	err := saveJournal(j)
	journalMu.Unlock()
	if err != nil {
		return "", err.Error(), false
	}
	jobsOnce.Do(func() {
		jobs = make(chan job, 8)
		go worker()
	})
	select {
	case jobs <- job{mail: m, from: pin, kind: l.Type, args: c.Args}:
	default:
		reply(m, pin, l.Type, nil, errors.New("this host is busy with other requests; try again"))
	}
	return "delivered", "", true
}

// worker runs control requests one at a time, checking the grant again
// just before acting.
func worker() {
	for jb := range jobs {
		setStatus(jb.mail.ID, "running", nil)
		var res any
		var err error
		switch h := handlers[jb.kind]; {
		case !Allowed(jb.mail.FromHost, jb.kind):
			err = errors.New("not allowed: the user on this host has not run sunstack org allow " + jb.from.Name + " " + jb.kind)
		case h == nil:
			err = errors.New(jb.kind + " is not available on this host")
		default:
			res, err = h(jb.from.Name, jb.mail.ID, jb.args)
		}
		reply(jb.mail, jb.from, jb.kind, res, err)
		afterReply()
	}
}

func setStatus(id, status string, r *Mail) {
	journalMu.Lock()
	defer journalMu.Unlock()
	j := loadJournal()
	if e := j[id]; e != nil {
		e.Status = status
		if r != nil {
			e.Reply = r
		}
		_ = saveJournal(j)
	}
}

// reply seals the outcome to the asking host and queues it; the outbox and
// the journal hold it only sealed.
func reply(req *Mail, to *Pin, kind string, res any, err error) {
	c := Control{}
	if err != nil {
		c.Error = err.Error()
	} else if res != nil {
		c.Result, _ = json.Marshal(res)
	}
	body, _ := json.Marshal(c)
	// By the asking host's ID, never its name, which the hub controls.
	s, qerr := queueTo(req.FromHost, to.Name, &Letter{From: "user", To: "host", Type: kind + "-reply", ReplyTo: req.ID, Body: string(body)}, false)
	if qerr != nil {
		return
	}
	setStatus(req.ID, "done", &s.Mail)
}

// requeue sends a recorded reply again.
func requeue(m *Mail, toName string) {
	p := filepath.Join(outboxDir(), m.ID+".json")
	if _, err := os.Stat(p); err == nil {
		return
	}
	os.Remove(filepath.Join(sentDir(), m.ID+".json"))
	_ = writeJSON(p, &Sent{Mail: *m, ToName: toName, Status: "queued", At: stamp(time.Now())}, 0o600)
}

// peekArgs and peekResult are a peek's request and answer.
type peekArgs struct {
	Target string `json:"target"`
	Lines  int    `json:"lines"`
}

// PeekResult is what a peek returns.
type PeekResult struct {
	Label string `json:"label"`
	Text  string `json:"text"`
	At    string `json:"at"`
}

func peekHandler(_, _ string, raw json.RawMessage) (any, error) {
	var a peekArgs
	if json.Unmarshal(raw, &a) != nil {
		return nil, errors.New("bad peek request")
	}
	if a.Lines < 1 || a.Lines > core.PeekLimit {
		return nil, errors.New("a peek is 1 to 50 lines")
	}
	if !ValidAddress(a.Target) {
		return nil, errors.New("peek <team>/<agent>[_task] or a session ID; pane IDs are refused across hosts")
	}
	label, text, err := core.PeekFor(a.Target, a.Lines)
	if err != nil {
		return nil, err
	}
	return PeekResult{Label: label, Text: text, At: stamp(time.Now())}, nil
}

// Peek asks another host for the tail of a session's pane.
func Peek(hostName, target string, lines int) (*PeekResult, error) {
	id, err := Request(hostName, "peek", peekArgs{Target: target, Lines: lines}, time.Minute)
	if err != nil {
		return nil, err
	}
	if err := FlushOnce(); err != nil {
		// The connector moves it on when one runs.
		_ = err
	}
	c, err := Await(id, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var r PeekResult
	if json.Unmarshal(c.Result, &r) != nil {
		return nil, failErr("bad_reply", "the peek reply cannot be read")
	}
	// Another host's pane text: its control sequences are shown, not run.
	r.Label, r.Text = core.VisibleControls(r.Label), core.VisibleControls(r.Text)
	return &r, nil
}

// RecoverRequests answers the requests a restart cut short: one still
// queued never ran; one that was running may have started, so it is
// reported as uncertain and never run again (§20.3). The connector calls
// it when it starts.
func RecoverRequests() {
	journalMu.Lock()
	j := loadJournal()
	var open []string
	for id, e := range j {
		if e.Status == "queued" || e.Status == "running" {
			open = append(open, id)
		}
	}
	journalMu.Unlock()
	pins := loadPins()
	for _, id := range open {
		e := j[id]
		host := e.FromHost
		if host == "" { // recorded by an older version: by name, when only one pin has it
			for pid, p := range pins {
				if p.Name == e.From {
					if host != "" {
						host = ""
						break
					}
					host = pid
				}
			}
		}
		pin := pins[host]
		if pin == nil {
			continue
		}
		msg := "this host restarted before running it; ask again"
		if e.Status == "running" {
			msg = "this host restarted while running it; the result is uncertain (a session it started carries @sunstack_request " + id + "); it is not run again"
		}
		reply(&Mail{ID: id, FromHost: host}, pin, e.Kind, nil, errors.New(msg))
	}
}

// cleanAsked forgets requests past their expiry and deletes, unopened,
// replies no open request waits for (§20.5).
func cleanAsked() {
	open := map[string]bool{}
	for _, p := range jsonFiles(askedDir()) {
		var a asked
		if readJSON(p, &a) != nil {
			os.Remove(p)
			continue
		}
		if t, ok := parseStamp(a.Expires); ok && time.Now().After(t.Add(skew)) {
			os.Remove(p)
			continue
		}
		open[a.ID] = true
	}
	for _, p := range jsonFiles(repliesDir()) {
		if !open[strings.TrimSuffix(filepath.Base(p), ".json")] {
			os.Remove(p)
		}
	}
}

package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Messages (design §5, §8): one file per message in _local/inbox/<id>/.
// A session takes a message before working on it (moved to
// .taken/<token>/, so only one session handles it) and acks it when done
// (moved to _local/log/messages/). Released or vanished sessions' taken
// messages go back to the inbox.

// MessageTypes are the kinds of message.
var MessageTypes = []string{"task", "question", "handoff", "fyi", "done", "answer", "shutdown"}

// Message is one inbox entry.
type Message struct {
	ID, From, Via, To, Session, At, Type, ReplyTo string
	Op                                            string // sender's operation ID, so a repeated send is not delivered twice
	FromSession                                   string // the sender's Claude session or Codex thread ID, for a reply to a session without an agent
	Follows                                       string // the earlier task this one follows up (§17.4)
	FromHost                                      string // the host a message from another host came from (§18.8)
	Returned                                      string // "<session label> <time>": put back unfinished by that session (§31)
	Replaces                                      string // the earlier task this one replaces, named by its sender (§31)
	ReplacedBy                                    string // set by check: a newer task that replaces this one
	Body                                          string
	State                                         string // pending, taken (by this session), taken by <label>
	path                                          string
	mod                                           time.Time // the file's time, to order messages sent in the same second
	takenBy                                       string    // the claim token whose taken folder holds it
}

var msgIDRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-z0-9._-]+-[0-9a-f]{6}$`)

// IsMessageID reports whether s has the form of a message ID.
func IsMessageID(s string) bool { return len(s) <= 128 && msgIDRe.MatchString(s) }

func (p *Project) inboxDir(id string) string        { return p.local("inbox", id) }
func (p *Project) takenDir(id, token string) string { return p.local("inbox", id, ".taken", token) }
func (p *Project) messageArchive() string           { return p.local("log", "messages") }
func (p *Project) msgPath(dir, msgID string) string { return filepath.Join(dir, msgID+".md") }
func hasType(t string) bool {
	for _, m := range MessageTypes {
		if m == t {
			return true
		}
	}
	return false
}

func parseMessage(path string) (*Message, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	m := &Message{path: path}
	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
			for _, l := range strings.Split(s[4:4+end], "\n") {
				k, v, ok := strings.Cut(l, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(strings.SplitN(v, "#", 2)[0])
				switch strings.TrimSpace(k) {
				case "id":
					m.ID = v
				case "from":
					m.From = v
				case "via":
					m.Via = v
				case "to":
					m.To = v
				case "session":
					m.Session = v
				case "at":
					m.At = v
				case "type":
					m.Type = v
				case "reply_to":
					m.ReplyTo = v
				case "op":
					m.Op = v
				case "from_session":
					m.FromSession = v
				case "follows":
					m.Follows = v
				case "from_host":
					m.FromHost = v
				case "returned":
					m.Returned = v
				case "replaces":
					m.Replaces = v
				}
			}
			m.Body = strings.TrimLeft(s[4+end+5:], "\n")
		}
	}
	if m.ID == "" {
		m.ID = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return m, nil
}

func (m *Message) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nid: %s\nfrom: %s\n", m.ID, m.From)
	if m.Via != "" {
		fmt.Fprintf(&b, "via: %s\n", m.Via)
	}
	fmt.Fprintf(&b, "to: %s\n", m.To)
	if m.Session != "" {
		fmt.Fprintf(&b, "session: %s\n", m.Session)
	}
	fmt.Fprintf(&b, "at: %s\ntype: %s\n", m.At, m.Type)
	if m.ReplyTo != "" {
		fmt.Fprintf(&b, "reply_to: %s\n", m.ReplyTo)
	}
	if m.Op != "" {
		fmt.Fprintf(&b, "op: %s\n", m.Op)
	}
	if m.FromSession != "" {
		fmt.Fprintf(&b, "from_session: %s\n", m.FromSession)
	}
	if m.Follows != "" {
		fmt.Fprintf(&b, "follows: %s\n", m.Follows)
	}
	if m.FromHost != "" {
		fmt.Fprintf(&b, "from_host: %s\n", m.FromHost)
	}
	if m.Replaces != "" {
		fmt.Fprintf(&b, "replaces: %s\n", m.Replaces)
	}
	if m.Returned != "" {
		fmt.Fprintf(&b, "returned: %s\n", m.Returned)
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimRight(m.Body, "\n") + "\n")
	return b.String()
}

// SendOptions are the inputs of `sunstack send`.
type SendOptions struct {
	To      string // agent ID, a title with one agent, or a session name <id>_<task>
	Body    string
	Type    string
	ReplyTo string
	From    string // sender agent ID; empty means the user
	Token   string // the sender's token, required with From
	Via     string // the agent session that sent it in the user's name, if any
	NoNudge bool
	Nudge   bool   // nudge for an fyi from an agent too (§32)
	Op      string // optional operation ID: the same ID and text return the earlier message
	// FromLabel is a sender already checked in another team (<team>/<id>);
	// the CLI sets it for messages between teams.
	FromLabel   string
	FromSession string // the sending CLI session's ID, if known
	Follows     string // an earlier task this task follows up; its brief and replies are quoted
	Replaces    string // an earlier task of the same sender to the same recipient that this one replaces
	// FromAnswer marks the answer command (approval-gated): only it may send
	// an answer, which speaks for the user on an ask.
	FromAnswer bool
	// ID presets the message ID, for mail from another host, which keeps
	// its ID on every host; FromHost names that host.
	ID, FromHost string
}

// SendResult says where a message went.
type SendResult struct {
	ID, To, Session string
	Nudged          string // session name that was nudged, or ""
	Note            string // why nobody was nudged
}

// resolveRecipient accepts an ID, a title with one agent, or a session name.
func (p *Project) resolveRecipient(to string) (id, session string, err error) {
	// The org view labels sessions <id>_<task>@<host>; this host's suffix is
	// accepted, so a label can be pasted as is.
	if i := strings.LastIndex(to, "@"); i > 0 {
		if to[i+1:] != ThisHost().Name {
			return "", "", fail(ExitFail, "not_found", "%s is on another host; send to <host>:<team>/<agent> through the org hub", to)
		}
		to = to[:i]
	}
	if i := strings.LastIndex(to, "_"); i > 0 {
		id, task := to[:i], to[i+1:]
		if p.HasAgent(id) && ValidTask(task) {
			claims, err := p.Claims(id)
			if err != nil {
				return "", "", err
			}
			for _, c := range claims {
				if c.Task == task {
					return id, to, nil
				}
			}
			return "", "", fail(ExitFail, "not_found", "no live session %s (sunstack team --sessions lists them)", to)
		}
	}
	id, err = p.resolve(to)
	return id, "", err
}

// MaxMessage is the largest message body, quoted context included. Sealed
// for another host it still fits the hub's 256 KB frame.
const MaxMessage = 128 << 10

// NewMessageID makes a message ID for a sender.
func NewMessageID(from string) string { return newMessageID(from) }

func newMessageID(from string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	f := regexp.MustCompile(`[^a-z0-9.-]+`).ReplaceAllString(strings.ToLower(from), "-")
	return time.Now().UTC().Format("20060102T150405Z") + "-" + f + "-" + hex.EncodeToString(b)
}

// Send writes a message into the recipient's inbox, then, unless told not
// to, types a one-line nudge into the pane of a live session of that agent.
func (p *Project) Send(o SendOptions) (*SendResult, error) {
	if strings.TrimSpace(o.Body) == "" {
		return nil, fail(ExitUsage, "missing_arguments", "the message text is empty")
	}
	if o.Type == "" {
		o.Type = "fyi"
	}
	if !hasType(o.Type) {
		return nil, fail(ExitUsage, "usage", "type must be one of %s", strings.Join(MessageTypes, ", "))
	}
	if o.Type == "task" {
		if err := CheckBrief(o.Body); err != nil {
			return nil, err
		}
	}
	if o.Type == "answer" && !o.FromAnswer {
		return nil, fail(ExitUsage, "usage", "an answer speaks for the user; only sunstack answer sends one")
	}
	if o.Follows != "" {
		if o.Type != "task" {
			return nil, fail(ExitUsage, "usage", "--follows is for a task")
		}
		ctx, err := p.followContext(o.Follows)
		if err != nil {
			return nil, err
		}
		o.Body = strings.TrimRight(o.Body, "\n") + "\n" + ctx
	}
	if o.ReplyTo != "" && !msgIDRe.MatchString(o.ReplyTo) {
		return nil, fail(ExitUsage, "usage", "invalid --reply-to message id: %s", o.ReplyTo)
	}
	if len(o.Body) > MaxMessage {
		return nil, fail(ExitUsage, "usage", "the message is larger than %d KB; put long text in a file and send its path", MaxMessage>>10)
	}
	if o.From != "" && !safeID(o.From) {
		return nil, fail(ExitUsage, "usage", "invalid --from agent id: %q", o.From)
	}
	// A line break in a header value would add a header line.
	if strings.ContainsAny(o.FromLabel+o.Via+o.FromHost+o.ID, "\r\n") {
		return nil, fail(ExitUsage, "usage", "a line break in the sender")
	}
	from := "user"
	if o.FromLabel != "" {
		from = o.FromLabel
	} else if o.From != "" {
		claims, err := p.Claims(o.From)
		if err != nil {
			return nil, err
		}
		if findToken(claims, o.Token) == nil {
			return nil, fail(ExitClaim, "token", "sending as %s needs this session's --token", o.From)
		}
		from = o.From
	}
	id, session, err := p.resolveRecipient(o.To)
	if err != nil {
		return nil, err
	}
	if o.FromSession != "" && !IsReplySession(o.FromSession) {
		o.FromSession = ""
	}
	if o.Op != "" && !opRe.MatchString(o.Op) {
		return nil, fail(ExitUsage, "usage", "invalid --op: letters, digits, . _ - only, at most 64")
	}
	// Publish under the recipient's lock, which fire and rename also take,
	// so a message never lands in a removed agent's inbox. Two senders with
	// the same --op deliver once. The nudge runs after the lock is freed.
	unlock, err := p.lock(id)
	if err != nil {
		return nil, err
	}
	release := unlock
	defer func() { release() }()
	if again, s2, err := p.resolveRecipient(o.To); err != nil || again != id || s2 != session {
		return nil, fail(ExitFail, "not_found", "%s was removed while waiting", o.To)
	}
	dir := p.inboxDir(id)
	if err := p.noSymlink(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	m := &Message{From: from, To: id, Session: session, At: now(), Type: o.Type, ReplyTo: o.ReplyTo, Body: o.Body, Op: o.Op, FromSession: o.FromSession, Follows: o.Follows, Replaces: o.Replaces, FromHost: o.FromHost}
	if from == "user" {
		m.Via = o.Via
	}
	if o.Op != "" {
		if prev := findOp(m, p.inboxDir(id), p.messageArchive(), p.local("inbox", id, ".taken")); prev != nil {
			return &SendResult{ID: prev.ID, To: id, Session: prev.Session, Note: "already sent with this --op; not sent again"}, nil
		}
	}
	// With or without --op: a preset id never replaces a message already here.
	if o.ID != "" && idUsed(o.ID, p.inboxDir(id), p.messageArchive(), p.local("inbox", id, ".taken")) {
		return nil, fail(ExitFail, "conflict", "a different message with id %s is already here", o.ID)
	}
	// After the --op lookup, so a retry of a delivered replacement returns it
	// even once the older task is closed.
	if o.Replaces != "" {
		if err := p.checkReplaces(o, from, id); err != nil {
			return nil, err
		}
	}
	// Create the temporary file exclusively, so two senders never share a
	// name; readers only see the .md after the rename.
	var tmp string
	for i := 0; ; i++ {
		m.ID = newMessageID(from)
		if o.ID != "" {
			if !msgIDRe.MatchString(o.ID) || i > 0 {
				return nil, fail(ExitUsage, "usage", "invalid or duplicate message id %s", o.ID)
			}
			m.ID = o.ID
		}
		tmp = filepath.Join(dir, "."+m.ID+".tmp")
		if o.ID != "" {
			dropStaleTmp(tmp, true)
		}
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && i < 5 {
			continue
		}
		if err != nil {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		_, werr := f.WriteString(m.text())
		if werr == nil {
			werr = f.Sync() // on disk before anyone acks it as delivered
		}
		cerr := f.Close()
		if werr != nil || cerr != nil {
			os.Remove(tmp)
			return nil, fail(ExitFail, "fs", "could not write the message")
		}
		break
	}
	if err := os.Rename(tmp, p.msgPath(dir, m.ID)); err != nil {
		os.Remove(tmp)
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	SyncDir(filepath.Dir(p.msgPath(dir, m.ID)))
	release()
	release = func() {}
	fields := []string{m.Type, m.ID}
	if session != "" {
		fields = append(fields, "session="+session)
	}
	if m.Via != "" {
		fields = append(fields, "via="+strings.ReplaceAll(m.Via, " ", "_"))
	}
	p.LogEvent("send", from+" -> "+id, fields...)
	res := &SendResult{ID: m.ID, To: id, Session: session}
	if o.NoNudge {
		res.Note = "not nudged (--no-nudge)"
		return res, nil
	}
	if quietFyi(o, from) {
		res.Note = fyiNote
		return res, nil
	}
	res.Nudged, res.Note = p.nudge(id, session, m)
	return res, nil
}

// An fyi needs no action, so one from an agent waits for the recipient's next
// check instead of interrupting it (§32). One from the user still nudges.
const fyiNote = "not nudged (an fyi waits for the next check)"

func quietFyi(o SendOptions, from string) bool {
	return o.Type == "fyi" && !o.Nudge && from != "user" && !strings.HasSuffix(from, ":user")
}

// nudge types a one-line prompt into the pane of the best live session:
// the named one, or the most recently active one whose pane runs its tool.
func (p *Project) nudge(id, session string, m *Message) (string, string) {
	// One nudge per agent at a time, so two senders never type into the
	// same pane at once.
	unlock, lerr := p.lock("_nudge_" + id)
	if lerr != nil {
		return "", "not nudged (another nudge is in progress); the message waits in the inbox and shows at the next prompt"
	}
	defer unlock()
	claims, err := p.Claims(id)
	if err != nil || len(claims) == 0 {
		return "", "no live session; the message waits in the inbox (sunstack spawn starts one)"
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].LastContact > claims[j].LastContact })
	// A check is already coming: from that session for a message to it, from
	// any session of the agent for one to the agent.
	for _, c := range claims {
		if (session == "" || c.Label(id) == session) && nudgeWaiting(p.nudgedPath(id, c.Token)) {
			p.LogEvent("nudge-held", c.Label(id), m.ID)
			return "", "not nudged (a nudge is already waiting); the message waits in the inbox"
		}
	}
	busy := ""
	sc := &scanCache{}
	for _, c := range claims {
		if session != "" && c.Label(id) != session {
			continue
		}
		if c.TmuxPane == "" || c.TmuxSocket == "" || !sameServer(c) || !paneRunsTool(c.TmuxSocket, c.TmuxPane, c.Tool) {
			continue
		}
		if why := claimPaneHeld(c, sc); why != "" {
			busy = c.Label(id) + ": " + why // not typed into a pane that may not be this session (§34.1)
			continue
		}
		prompt := NudgePrompt(c.Tool, m)
		if prompt == "" {
			continue
		}
		// The marker goes first, so a check the prompt sets off always
		// finds and clears it.
		markNudged(p.nudgedPath(id, c.Token))
		if err := typeInto(c.TmuxSocket, c.TmuxPane, c.Tool, prompt); err != nil {
			os.Remove(p.nudgedPath(id, c.Token))
			busy = c.Label(id) + ": " + err.Error()
			continue
		}
		p.LogEvent("nudge", c.Label(id), m.ID)
		return c.Label(id), ""
	}
	if busy != "" {
		return "", "not nudged (" + busy + "); the message waits in the inbox and shows at that session's next prompt"
	}
	return "", "no session with a live Claude Code or Codex pane; the message waits in the inbox and shows at that session's next prompt"
}

// NudgePrompt is what a nudge types: the check skill, named the way each CLI
// invokes skills, followed by a sentence (a bare Codex skill mention can be
// swallowed by its picker). It carries no digits, names or IDs, so even if it
// ever reached a menu it could not pick a numbered option.
func NudgePrompt(tool string, m *Message) string {
	msg := " a new " + m.Type + " message is waiting"
	switch tool {
	case "claude":
		return "/sunstack:check" + msg
	case "codex":
		return "$sunstack:check" + msg
	}
	return ""
}

// paneRunsTool reports whether the foreground of the pane is the named CLI,
// so a nudge is never typed into a plain shell.
func paneRunsTool(socket, pane, tool string) bool {
	runs, _ := paneTool(socket, pane, tool)
	return runs
}

// paneTool is paneRunsTool, and whether tmux and ps answered in time: a
// check that timed out says nothing about the pane.
func paneTool(socket, pane, tool string) (runs, known bool) {
	if tool != "claude" && tool != "codex" {
		return false, true
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdWait)
	defer cancel()
	tc := exec.CommandContext(ctx, "tmux", TmuxArgs(socket, "display-message", "-p", "-t", pane, "#{pane_tty}")...)
	tc.WaitDelay = time.Second
	out, err := tc.Output()
	tty := strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/")
	if err != nil || tty == "" {
		return false, ctx.Err() == nil && !errors.Is(err, exec.ErrWaitDelay)
	}
	pc := exec.CommandContext(ctx, "ps", "-o", "stat=,comm=", "-t", tty)
	pc.WaitDelay = time.Second
	ps, err := pc.Output()
	if err != nil {
		return false, ctx.Err() == nil && !errors.Is(err, exec.ErrWaitDelay)
	}
	for _, l := range strings.Split(string(ps), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && strings.Contains(f[0], "+") && filepath.Base(f[len(f)-1]) == tool {
			return true, true
		}
	}
	return false, true
}

// dialogMarkers are texts of menus, approval prompts and questions in Claude
// Code and Codex. While one is on screen, typed keys could answer it: Codex
// menus even react to single letters before Enter.
var dialogMarkers = []string{
	"enter to confirm", "enter to continue", "esc to cancel", "esc to go back", "do you want to",
	"would you like to", "allow command", "yes, proceed", "yes, and", "(y/n)", "press enter",
	"trust this folder", "trust the contents", "-- normal --",
}

// menuLine is the highlighted option of a numbered menu, such as
// "› 2. Trust all"; a numbered list in ordinary output has no cursor.
var menuLine = regexp.MustCompile(`^\s*[›❯]\s*[0-9]+\.\s`)

// screenTail returns the last non-blank lines of a pane capture.
func screenTail(screen string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append([]string{lines[i]}, tail...)
		}
	}
	return tail
}

// inputLine finds the CLI's input line and returns what is typed in it.
func inputLine(tool string, tail []string) (string, bool) {
	rule := func(l string) bool {
		t := strings.TrimSpace(l)
		return strings.HasPrefix(t, "───") || strings.HasSuffix(t, "───")
	}
	for i := len(tail) - 1; i >= 0; i-- {
		t := strings.TrimSpace(tail[i])
		switch tool {
		case "claude":
			// The input line sits between the two borders of the input box.
			if strings.HasPrefix(t, "❯") && i > 0 && i+1 < len(tail) && rule(tail[i-1]) && rule(tail[i+1]) {
				return strings.TrimSpace(strings.TrimPrefix(t, "❯")), true
			}
		case "codex":
			if strings.HasPrefix(t, "›") {
				return strings.TrimSpace(strings.TrimPrefix(t, "›")), true
			}
		}
	}
	return "", false
}

// ReadyForInput reports whether the CLI in the pane shows its normal input
// box with no menu or approval prompt, so typing lands in the composer.
func ReadyForInput(tool, screen string) bool {
	tail := screenTail(screen, 15)
	low := strings.ToLower(strings.Join(tail, "\n"))
	for _, m := range dialogMarkers {
		if strings.Contains(low, m) {
			return false
		}
	}
	for _, l := range tail {
		if menuLine.MatchString(l) {
			return false
		}
	}
	_, ok := inputLine(tool, tail)
	return ok
}

// boxEmpty reports whether the CLI's input box holds nothing typed. Both
// CLIs show a hint there when it is empty ("Ask Codex to do anything", "Try
// ..."), drawn dim; typed text is not. known is false when the screen could
// not be read with its colors, or has no input box.
func boxEmpty(socket, pane, tool string) (empty, known bool) {
	cmd, done := quick("tmux", TmuxArgs(socket, "capture-pane", "-e", "-p", "-t", pane)...)
	defer done()
	out, err := cmd.Output()
	if err != nil {
		return false, false
	}
	mark := "›"
	if tool == "claude" {
		mark = "❯"
	}
	// Colors carry over from row to row, so the whole screen is read in
	// order, and the box's rows are cut from it after.
	raw := strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n")
	plain := make([]string, len(raw))
	visible := make([]string, len(raw))
	dim := false
	for i, l := range raw {
		plain[i] = sgrRe.ReplaceAllString(l, "")
		visible[i], dim = undimmedFrom(l, dim)
	}
	start, end, ok := composerRows(tool, plain)
	if !ok {
		return false, false
	}
	typed := strings.TrimPrefix(strings.TrimSpace(visible[start]), mark)
	for _, v := range visible[start+1 : end] {
		typed += v
	}
	return strings.TrimFunc(typed, func(r rune) bool { return unicode.IsSpace(r) || r == '\u00a0' }) == "", true
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// undimmed is a line's text that is not drawn dim (SGR 2).
func undimmed(line string) string {
	s, _ := undimmedFrom(line, false)
	return s
}

// undimmedFrom is undimmed for a row that starts dim or not, with the state
// it ends in.
func undimmedFrom(line string, dim bool) (string, bool) {
	var b strings.Builder
	for line != "" {
		loc := sgrRe.FindStringIndex(line)
		if loc == nil {
			if !dim {
				b.WriteString(line)
			}
			return b.String(), dim
		}
		if !dim {
			b.WriteString(line[:loc[0]])
		}
		params := strings.TrimSuffix(strings.TrimPrefix(line[loc[0]:loc[1]], "\x1b["), "m")
		ps := strings.Split(strings.ReplaceAll(params, ":", ";"), ";")
		for i := 0; i < len(ps); i++ {
			switch ps[i] {
			case "", "0", "22":
				dim = false
			case "2":
				dim = true
			case "38", "48", "58":
				// A color's arguments: 5;n or 2;r;g;b, not attributes.
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else if i+1 < len(ps) && ps[i+1] == "2" {
					i += 4
				}
			}
		}
		line = line[loc[1]:]
	}
	return b.String(), dim
}

// paneTokenRe finds a claim token on a pane: a spawned session's first prompt
// holds "--token <hex>".
var paneTokenRe = regexp.MustCompile(`(--token[ =])[0-9A-Za-z]+`)

// maskTokens hides claim tokens in pane text before peek shows it.
func maskTokens(s string) string { return paneTokenRe.ReplaceAllString(s, "${1}****") }

func paneScreen(socket, pane string) string {
	cmd, done := quick("tmux", TmuxArgs(socket, "capture-pane", "-p", "-J", "-t", pane)...)
	defer done()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// typeInto types text into a pane and presses Enter, only while the CLI shows
// its normal input box. Before Enter it checks the screen again and that the
// input line holds exactly the text: if the user had a draft there, the text
// is erased and Enter is not pressed, so their draft is never sent. The pause
// keeps a TUI from reading the Enter as part of a paste.
func typeInto(socket, pane, tool, text string) error {
	if !ReadyForInput(tool, paneScreen(socket, pane)) {
		return fmt.Errorf("the session is showing a prompt or menu")
	}
	// Typing into a draft is never done: what is there is the user's.
	if empty, known := boxEmpty(socket, pane, tool); !empty || !known {
		return fmt.Errorf("the input box holds a draft, or could not be read; it was left as it was")
	}
	if err := exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "-l", text)...).Run(); err != nil {
		return err
	}
	time.Sleep(400 * time.Millisecond)
	screen := paneScreen(socket, pane)
	got, ok := inputLine(tool, screenTail(screen, 15))
	if !ReadyForInput(tool, screen) || !ok {
		return fmt.Errorf("a prompt or menu appeared while typing; Enter was not pressed")
	}
	if got != text {
		// The box was empty: what is there now is a part of this text, and
		// only that is taken back.
		if strings.HasPrefix(text, got) {
			n := fmt.Sprint(len([]rune(got)))
			exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "-N", n, "BSpace")...).Run()
			return fmt.Errorf("not all of the text arrived; it was taken back")
		}
		return fmt.Errorf("the input box holds other text; it was left as it is")
	}
	return exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "Enter")...).Run()
}

// requeue returns messages taken by sessions that no longer hold a claim.
// Caller holds id's lock.
// stuck lists the messages that could not go back (§34.5); they stay taken
// in a folder with no claim, and the next requeue tries them again.
func (p *Project) requeue(id string, claims []*Live, gone map[string]string) (out []*Message, stuck []string) {
	live := map[string]bool{}
	for _, c := range claims {
		live[c.Token] = true
	}
	dirs, err := os.ReadDir(p.local("inbox", id, ".taken"))
	if err != nil && !os.IsNotExist(err) {
		stuck = append(stuck, "the unreadable taken folder of "+id)
	}
	for _, d := range dirs {
		if !d.IsDir() || live[d.Name()] {
			continue
		}
		label := gone[d.Name()]
		if label == "" {
			label = "a session that ended"
		}
		names, err := readNames(p.takenDir(id, d.Name()), ".md")
		if err != nil {
			// Unreadable: what it holds cannot be put back or named (§34.5).
			stuck = append(stuck, "the unreadable folder of session "+label)
			continue
		}
		for _, f := range names {
			src, dst := filepath.Join(p.takenDir(id, d.Name()), f), filepath.Join(p.inboxDir(id), f)
			// Mark it returned before it moves, so whoever reads it next
			// knows it was started and left (§31). One header: the latest.
			// Keep the file's time: the ledger orders same-second replies by
			// it, and the header holds the return time. Only the one header
			// line changes; a file without a header moves as it is.
			m, err := parseMessage(src)
			marked := err == nil && p.setHeader(src, "returned", label+" "+now()) == nil
			if marked {
				m.Returned = label + " " + now()
			}
			if os.Rename(src, dst) != nil {
				stuck = append(stuck, strings.TrimSuffix(f, ".md"))
				continue
			}
			if m == nil {
				continue
			}
			if !marked {
				m.Returned = label + " " + now()
				p.LogEvent("requeue", label, m.ID, "unmarked")
			} else {
				p.LogEvent("requeue", label, m.ID)
			}
			out = append(out, m)
		}
		os.Remove(p.takenDir(id, d.Name()))
	}
	return out, stuck
}

// ReturnedBy splits the returned header into the session label and the time.
func (m *Message) ReturnedBy() (label, at string) {
	i := strings.LastIndex(m.Returned, " ")
	if i < 0 {
		return m.Returned, ""
	}
	return m.Returned[:i], m.Returned[i+1:]
}

// setHeader sets one header line of a message file and leaves every other
// byte as it was, keeping the file's time. A file without a header is an
// error and stays untouched.
func (p *Project) setHeader(path, key, value string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	nl := "\n"
	if strings.HasPrefix(s, "---\r\n") {
		nl = "\r\n"
	}
	open := "---" + nl
	end := -1
	if strings.HasPrefix(s, open) {
		// As parseMessage reads it: the closing line comes after the opening
		// one, never overlapping it (an empty block is no header).
		if i := strings.Index(s[len(open):], nl+"---"+nl); i >= 0 {
			end = len(open) + i
		}
	}
	if end < 0 {
		return fmt.Errorf("%s has no header", filepath.Base(path))
	}
	lines := strings.Split(s[len(open):end], nl)
	found := false
	for i, l := range lines {
		if k, _, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == key {
			if found {
				lines[i] = ""
				continue
			}
			lines[i], found = key+": "+value, true
		}
	}
	if !found {
		lines = append(lines, key+": "+value)
	}
	kept := lines[:0]
	for _, l := range lines {
		if l != "" {
			kept = append(kept, l)
		}
	}
	st, serr := os.Stat(path)
	if err := writeAtomic(path, []byte("---"+nl+strings.Join(kept, nl)+s[end:])); err != nil {
		return err
	}
	if serr == nil {
		os.Chtimes(path, st.ModTime(), st.ModTime())
	}
	return nil
}

// notifyReturned tells the agent that sent each returned task or handoff,
// once, after the caller's locks are freed (§31): the sender judges whether
// it still holds. Tasks from the user or beyond the team show in tasks,
// next and org instead.
func (p *Project) notifyReturned(id string, ms []*Message) (failed []string) {
	for _, m := range ms {
		if (m.Type != "task" && m.Type != "handoff") || m.From == id || !p.HasAgent(m.From) {
			continue
		}
		label, _ := m.ReturnedBy()
		body := fmt.Sprintf("%s returned unfinished by %s. Still valid? Leave it for %s, re-send it with --follows or --replaces, or tell %s it is no longer needed (--reply-to %s).", m.ID, label, id, id, m.ID)
		if _, err := p.Send(SendOptions{To: m.From, FromLabel: id, Type: "fyi", Body: body, NoNudge: true}); err != nil {
			failed = append(failed, fmt.Sprintf("could not tell %s that %s was returned unfinished (%v); send that note yourself", m.From, m.ID, err))
		}
	}
	return failed
}

// Check lists the messages this session may handle: pending ones addressed
// to the agent (or to this session), and the ones it has taken.
func (p *Project) Check(id, token string) ([]*Message, error) {
	if !ValidID(id) {
		return nil, fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	me, err := p.requireToken(id, token)
	if err != nil {
		return nil, err
	}
	claims, _ := p.Claims(id)
	p.requeue(id, claims, nil) // a stuck one is tried again at the next check
	p.clearNudged(id, token)
	label := me.Label(id)
	orphan := orphanSession(id, claims)
	var out []*Message
	for _, n := range listNames(p.takenDir(id, token), ".md") {
		if m, err := parseMessage(filepath.Join(p.takenDir(id, token), n)); err == nil {
			m.State = "taken (by this session)"
			out = append(out, m)
		}
	}
	for _, n := range listNames(p.inboxDir(id), ".md") {
		m, err := parseMessage(filepath.Join(p.inboxDir(id), n))
		if err != nil || (m.Session != "" && m.Session != label && !orphan(m.Session)) {
			continue
		}
		m.State = "pending"
		out = append(out, m)
	}
	// A replacement counts wherever the newer task is: pending, taken by
	// another session, or acked.
	// The newest replacement wins, by its send time, then its ID.
	replacedBy := map[string]*Message{}
	for _, m := range p.allMessages() {
		if m.Replaces == "" || m.To != id {
			continue
		}
		if prev := replacedBy[m.Replaces]; prev == nil || m.At > prev.At || (m.At == prev.At && m.ID > prev.ID) {
			replacedBy[m.Replaces] = m
		}
	}
	for _, m := range out {
		if r := replacedBy[m.ID]; r != nil {
			m.ReplacedBy = r.ID
		}
	}
	return out, nil
}

// orphanSession reports, for a message addressed to one session, whether
// that session has ended; such a message is open to any session of the agent.
func orphanSession(id string, claims []*Live) func(string) bool {
	live := map[string]bool{}
	for _, c := range claims {
		live[c.Label(id)] = true
	}
	return func(session string) bool { return !live[session] }
}

// Take moves a pending message to this session, so no other session works on it.
func (p *Project) Take(id, token, msgID string) error {
	return p.moveMessage(id, token, msgID, false)
}

// Ack archives a message this session has handled (taken, or still pending).
func (p *Project) Ack(id, token, msgID string) error {
	return p.moveMessage(id, token, msgID, true)
}

func (p *Project) moveMessage(id, token, msgID string, ack bool) error {
	if !ValidID(id) || !msgIDRe.MatchString(msgID) {
		return fail(ExitUsage, "usage", "invalid id or message id")
	}
	unlock, err := p.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	me, err := p.requireToken(id, token)
	if err != nil {
		return err
	}
	p.clearNudged(id, token)
	pending := p.msgPath(p.inboxDir(id), msgID)
	mine := p.msgPath(p.takenDir(id, token), msgID)
	// Every folder a move reads or writes stays inside the project (§34.3).
	for _, path := range []string{pending, mine, p.msgPath(p.messageArchive(), msgID)} {
		if err := p.noSymlink(path); err != nil {
			return err
		}
	}
	src := ""
	switch {
	case exists(mine):
		src = mine
	case exists(pending):
		m, err := parseMessage(pending)
		claims, _ := p.Claims(id)
		if err == nil && m.Session != "" && m.Session != me.Label(id) && !orphanSession(id, claims)(m.Session) {
			return fail(ExitClaim, "taken", "message %s is for session %s", msgID, m.Session)
		}
		src = pending
	default:
		if matches, _ := filepath.Glob(filepath.Join(p.local("inbox", id, ".taken"), "*", msgID+".md")); len(matches) > 0 {
			return fail(ExitClaim, "taken", "another session of %s has taken message %s", id, msgID)
		}
		if exists(p.msgPath(p.messageArchive(), msgID)) {
			return fail(ExitFail, "done", "message %s was already acked", msgID)
		}
		return fail(ExitFail, "not_found", "no message %s for %s", msgID, id)
	}
	dst := mine
	if ack {
		dst = p.msgPath(p.messageArchive(), msgID)
	}
	if src == dst {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	stampTaken(dst, ack)
	if ack {
		os.Remove(p.takenDir(id, token)) // only when empty
		p.LogEvent("ack", me.Label(id), msgID)
	} else {
		p.LogEvent("take", me.Label(id), msgID)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// SessionInfo describes one live session for `sunstack team --sessions`.
type SessionInfo struct {
	Name, ID, Tool, Host, Where, Pane, Session, LastContact, Resume string
	Spawned                                                         bool
}

// Sessions lists every live session of the team.
func (p *Project) Sessions() []SessionInfo {
	var out []SessionInfo
	for _, s := range p.Status() {
		for i, c := range s.Claims {
			out = append(out, SessionInfo{
				Name: c.Label(s.ID), ID: s.ID, Tool: c.Tool, Host: c.Host, Where: s.Where[i],
				Pane: c.TmuxPane, Session: c.Session, LastContact: c.LastContact,
				Resume: ResumeCommand(c), Spawned: c.Spawned,
			})
		}
	}
	return out
}

// ResumeCommand reopens a recorded CLI session.
func ResumeCommand(l *Live) string {
	if l == nil || l.Session == "" {
		return ""
	}
	switch l.Tool {
	case "claude":
		return "claude --resume " + l.Session
	case "codex":
		return "codex resume " + l.Session
	}
	return ""
}

// PendingForSession finds the claims held by a CLI session (by its session
// ID, or else by its tmux pane) and counts the messages waiting for each.
func (p *Project) PendingForSession(session, pane, socket string) []string {
	var out []string
	for _, id := range p.Agents() {
		claims, err := p.Claims(id)
		if err != nil {
			continue
		}
		for _, c := range claims {
			mine := (session != "" && c.Session == session) || (session == "" || c.Session == "") && samePane(c, pane, socket)
			if !mine {
				continue
			}
			n := 0
			for _, f := range listNames(p.inboxDir(id), ".md") {
				if m, err := parseMessage(filepath.Join(p.inboxDir(id), f)); err == nil && (m.Session == "" || m.Session == c.Label(id) || orphanSession(id, claims)(m.Session)) {
					n++
				}
			}
			if n > 0 {
				out = append(out, fmt.Sprintf("%s has %d new message(s) waiting", c.Label(id), n))
			}
			if k, oldest := takenIn(p.takenDir(id, c.Token)); k > 0 {
				out = append(out, fmt.Sprintf("%s has %d taken message(s) not finished%s", c.Label(id), k, TakenAge(oldest)))
			}
		}
	}
	return out
}

// CallerLabel names the agent CLI session running a command: its session
// name if it holds a Sunstack claim, otherwise the CLI's name.
func (p *Project) CallerLabel(tool, session, pane, socket string) string {
	if tool == "" {
		return ""
	}
	for _, id := range p.Agents() {
		claims, _ := p.Claims(id)
		for _, c := range claims {
			if (session != "" && c.Session == session) || (c.Session == "" && samePane(c, pane, socket)) {
				return tool + " session " + c.Label(id)
			}
		}
	}
	return tool + " session"
}

var opRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// findOp returns an earlier message with the same operation ID and the same
// recipient, session, sender, type, reply and text: pending, taken (in any
// folder under taken) or already handled.
// idUsed reports whether a message file with this ID exists in the pending,
// archive or taken folders (taken has one folder per session).
// dropStaleTmp removes the temporary file a killed delivery of a preset id
// left behind, so the retry is not refused for good. Under the inbox's lock
// no other writer can own it; without the lock only an old one is dropped.
func dropStaleTmp(tmp string, locked bool) {
	if st, err := os.Lstat(tmp); err == nil && st.Mode().IsRegular() && (locked || time.Since(st.ModTime()) > time.Minute) {
		os.Remove(tmp)
	}
}

func idUsed(id, pending, archive, taken string) bool {
	dirs := []string{pending, archive}
	if taken != "" {
		t, _ := filepath.Glob(filepath.Join(taken, "*"))
		dirs = append(dirs, t...)
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(d, id+".md")); err == nil {
			return true
		}
	}
	return false
}

func findOp(m *Message, pending, archive, taken string) *Message {
	dirs := []string{pending, archive}
	if taken != "" {
		t, _ := filepath.Glob(filepath.Join(taken, "*"))
		dirs = append(dirs, taken)
		dirs = append(dirs, t...)
	}
	for _, d := range dirs {
		for _, n := range listNames(d, ".md") {
			x, err := parseMessage(filepath.Join(d, n))
			if err == nil && x.Op == m.Op && x.To == m.To && x.Session == m.Session && x.From == m.From && x.FromSession == m.FromSession &&
				x.Type == m.Type && x.ReplyTo == m.ReplyTo && x.Replaces == m.Replaces && strings.TrimSpace(x.Body) == strings.TrimSpace(m.Body) {
				return x
			}
		}
	}
	return nil
}

// paneToolProc is the pane's foreground CLI process: its PID and when it
// started (§34.10).
func paneToolProc(socket, pane, tool string) (int, time.Time, bool) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return 0, time.Time{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmdWait)
	defer cancel()
	tc := exec.CommandContext(ctx, "tmux", TmuxArgs(socket, "display-message", "-p", "-t", pane, "#{pane_tty}")...)
	tc.WaitDelay = time.Second
	out, err := tc.Output()
	tty := strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/")
	if err != nil || tty == "" {
		return 0, time.Time{}, false
	}
	pc := exec.CommandContext(ctx, "ps", "-o", "stat=,pid=,lstart=,comm=", "-t", tty)
	pc.Env = append(os.Environ(), "TZ=UTC", "LC_ALL=C") // one format to parse, as ProcStart does
	pc.WaitDelay = time.Second
	ps, err := pc.Output()
	if err != nil {
		return 0, time.Time{}, false
	}
	for _, l := range strings.Split(string(ps), "\n") {
		f := strings.Fields(l)
		// stat, pid, lstart as five fields (Sat Oct 24 14:58:22 2026), comm.
		if len(f) >= 8 && strings.Contains(f[0], "+") && filepath.Base(f[len(f)-1]) == tool {
			pid, perr := strconv.Atoi(f[1])
			t, terr := time.ParseInLocation("Mon Jan 2 15:04:05 2006", strings.Join(f[2:7], " "), time.UTC)
			if perr == nil && terr == nil {
				return pid, t, true
			}
		}
	}
	return 0, time.Time{}, false
}

// readNames is listNames that reports a folder it cannot read; a missing
// folder holds nothing.
func readNames(dir, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, suffix) && !strings.HasPrefix(n, ".") {
			out = append(out, n)
		}
	}
	return out, nil
}

// scanCache holds one session scan for several checks.
type scanCache struct {
	done  bool
	all   []*HostSession
	notes []string
	dup   map[string][]string
}

func (sc *scanCache) get() *scanCache {
	if !sc.done {
		sc.all, sc.notes = ScanSessions()
		sc.dup, sc.done = DuplicateSessions(sc.all), true
	}
	return sc
}

// claimPaneHeld says why a claim's pane cannot be taken to be that session,
// or "" when it can: the scan must list the tool, the session must run in
// one pane only, and the pane's CLI must be the claimed process, or for a
// claim from before v0.17.2 run the claimed session (§34.1, §34.10).
func claimPaneHeld(c *Live, sc *scanCache) string {
	s := sc.get()
	if scanMissed(s.notes, c.Tool) {
		return "could not list " + c.Tool + " sessions to confirm its pane"
	}
	if c.Session != "" && s.dup[c.Session] != nil {
		return dupNote(c.Session, s.dup[c.Session])
	}
	if c.CLIPID != 0 {
		if pid, _, ok := paneToolProc(c.TmuxSocket, c.TmuxPane, c.Tool); ok && pid != c.CLIPID {
			return "its pane now runs another " + c.Tool + " process, not the one that claimed it"
		}
		return ""
	}
	for _, x := range s.all {
		if x.Pane == c.TmuxPane && x.SessionID != "" && c.Session != "" && x.SessionID != c.Session && sameTmuxServer(c, serverOfSocket(x.socket), serverOfSocket) {
			return "its pane now runs another session (" + x.SessionID + ")"
		}
	}
	return ""
}

func serverOfSocket(socket string) string { pid, _ := serverPID(socket); return pid }

package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Messages (design §5, §8): one file per message in _local/inbox/<id>/.
// A session takes a message before working on it (moved to
// .taken/<token>/, so only one session handles it) and acks it when done
// (moved to _local/log/messages/). Released or vanished sessions' taken
// messages go back to the inbox.

// MessageTypes are the kinds of message.
var MessageTypes = []string{"question", "handoff", "fyi", "done", "shutdown"}

// Message is one inbox entry.
type Message struct {
	ID, From, Via, To, Session, At, Type, ReplyTo string
	Body                                          string
	State                                         string // pending, taken (by this session), taken by <label>
	path                                          string
}

var msgIDRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-z0-9._-]+-[0-9a-f]{6}$`)

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
}

// SendResult says where a message went.
type SendResult struct {
	ID, To, Session string
	Nudged          string // session name that was nudged, or ""
	Note            string // why nobody was nudged
}

// resolveRecipient accepts an ID, a title with one agent, or a session name.
func (p *Project) resolveRecipient(to string) (id, session string, err error) {
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
			return "", "", fail(ExitFail, "not_found", "no live session %s (sunstack sessions lists them)", to)
		}
	}
	id, err = p.resolve(to)
	return id, "", err
}

func newMessageID(from string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	f := strings.NewReplacer("_", "-", "/", "-").Replace(strings.ToLower(from))
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
	if o.ReplyTo != "" && !msgIDRe.MatchString(o.ReplyTo) {
		return nil, fail(ExitUsage, "usage", "invalid --reply-to message id: %s", o.ReplyTo)
	}
	from := "user"
	if o.From != "" {
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
	dir := p.inboxDir(id)
	if err := p.noSymlink(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	m := &Message{From: from, To: id, Session: session, At: now(), Type: o.Type, ReplyTo: o.ReplyTo, Body: o.Body}
	if from == "user" {
		m.Via = o.Via
	}
	// Create the temporary file exclusively, so two senders never share a
	// name; readers only see the .md after the rename.
	var tmp string
	for i := 0; ; i++ {
		m.ID = newMessageID(from)
		tmp = filepath.Join(dir, "."+m.ID+".tmp")
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && i < 5 {
			continue
		}
		if err != nil {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		_, werr := f.WriteString(m.text())
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
	res.Nudged, res.Note = p.nudge(id, session, m)
	return res, nil
}

// nudge types a one-line prompt into the pane of the best live session:
// the named one, or the most recently active one whose pane runs its tool.
func (p *Project) nudge(id, session string, m *Message) (string, string) {
	claims, err := p.Claims(id)
	if err != nil || len(claims) == 0 {
		return "", "no live session; the message waits in the inbox (sunstack spawn starts one)"
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].LastContact > claims[j].LastContact })
	busy := ""
	for _, c := range claims {
		if session != "" && c.Label(id) != session {
			continue
		}
		if c.TmuxPane == "" || c.TmuxSocket == "" || !paneRunsTool(c.TmuxSocket, c.TmuxPane, c.Tool) {
			continue
		}
		prompt := NudgePrompt(c.Tool, m)
		if prompt == "" {
			continue
		}
		if err := typeInto(c.TmuxSocket, c.TmuxPane, c.Tool, prompt); err != nil {
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
	if tool != "claude" && tool != "codex" {
		return false
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return false
	}
	out, err := exec.Command("tmux", TmuxArgs(socket, "display-message", "-p", "-t", pane, "#{pane_tty}")...).Output()
	tty := strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/")
	if err != nil || tty == "" {
		return false
	}
	ps, err := exec.Command("ps", "-o", "stat=,comm=", "-t", tty).Output()
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(ps), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && strings.Contains(f[0], "+") && filepath.Base(f[len(f)-1]) == tool {
			return true
		}
	}
	return false
}

// dialogMarkers are texts of menus, approval prompts and questions in Claude
// Code and Codex. While one is on screen, typed keys could answer it.
var dialogMarkers = []string{
	"enter to confirm", "esc to cancel", "do you want to", "would you like to", "allow command",
	"yes, proceed", "yes, and", "(y/n)", "press enter", "trust this folder", "trust the contents",
	"❯ 1.", "› 1.", "-- normal --",
}

// ReadyForInput reports whether the CLI in the pane shows its normal input
// box with no menu or approval prompt, so typing lands in the composer.
func ReadyForInput(tool, screen string) bool {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < 15; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append([]string{lines[i]}, tail...)
		}
	}
	low := strings.ToLower(strings.Join(tail, "\n"))
	for _, m := range dialogMarkers {
		if strings.Contains(low, m) {
			return false
		}
	}
	rule := func(l string) bool {
		t := strings.TrimSpace(l)
		return strings.HasPrefix(t, "───") || strings.HasSuffix(t, "───")
	}
	for i, l := range tail {
		t := strings.TrimSpace(l)
		switch tool {
		case "claude":
			// The input line sits between the two borders of the input box.
			if strings.HasPrefix(t, "❯") && i > 0 && i+1 < len(tail) && rule(tail[i-1]) && rule(tail[i+1]) {
				return true
			}
		case "codex":
			if strings.HasPrefix(t, "›") {
				return true
			}
		}
	}
	return false
}

func paneScreen(socket, pane string) string {
	out, err := exec.Command("tmux", TmuxArgs(socket, "capture-pane", "-p", "-J", "-t", pane)...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// typeInto types text into a pane and presses Enter, only while the CLI shows
// its normal input box; it checks again right before Enter. The pause keeps a
// TUI from reading the Enter as part of a paste.
func typeInto(socket, pane, tool, text string) error {
	if !ReadyForInput(tool, paneScreen(socket, pane)) {
		return fmt.Errorf("the session is showing a prompt or menu")
	}
	if err := exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "-l", text)...).Run(); err != nil {
		return err
	}
	time.Sleep(400 * time.Millisecond)
	if !ReadyForInput(tool, paneScreen(socket, pane)) {
		return fmt.Errorf("a prompt or menu appeared while typing; Enter was not pressed")
	}
	return exec.Command("tmux", TmuxArgs(socket, "send-keys", "-t", pane, "Enter")...).Run()
}

// requeue returns messages taken by sessions that no longer hold a claim.
// Caller holds id's lock.
func (p *Project) requeue(id string, claims []*Live) int {
	live := map[string]bool{}
	for _, c := range claims {
		live[c.Token] = true
	}
	n := 0
	dirs, _ := os.ReadDir(p.local("inbox", id, ".taken"))
	for _, d := range dirs {
		if !d.IsDir() || live[d.Name()] {
			continue
		}
		for _, f := range listNames(p.takenDir(id, d.Name()), ".md") {
			if os.Rename(filepath.Join(p.takenDir(id, d.Name()), f), filepath.Join(p.inboxDir(id), f)) == nil {
				n++
			}
		}
		os.Remove(p.takenDir(id, d.Name()))
	}
	return n
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
	p.requeue(id, claims)
	label := me.Label(id)
	var out []*Message
	for _, n := range listNames(p.takenDir(id, token), ".md") {
		if m, err := parseMessage(filepath.Join(p.takenDir(id, token), n)); err == nil {
			m.State = "taken (by this session)"
			out = append(out, m)
		}
	}
	for _, n := range listNames(p.inboxDir(id), ".md") {
		m, err := parseMessage(filepath.Join(p.inboxDir(id), n))
		if err != nil || (m.Session != "" && m.Session != label) {
			continue
		}
		m.State = "pending"
		out = append(out, m)
	}
	return out, nil
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
	pending := p.msgPath(p.inboxDir(id), msgID)
	mine := p.msgPath(p.takenDir(id, token), msgID)
	src := ""
	switch {
	case exists(mine):
		src = mine
	case exists(pending):
		m, err := parseMessage(pending)
		if err == nil && m.Session != "" && m.Session != me.Label(id) {
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

// SessionInfo describes one live session for `sunstack sessions`.
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
				if m, err := parseMessage(filepath.Join(p.inboxDir(id), f)); err == nil && (m.Session == "" || m.Session == c.Label(id)) {
					n++
				}
			}
			n += len(listNames(p.takenDir(id, c.Token), ".md"))
			if n > 0 {
				out = append(out, fmt.Sprintf("%s has %d message(s) waiting", c.Label(id), n))
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

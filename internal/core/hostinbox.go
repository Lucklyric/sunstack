package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The host inbox (v0.8.1): messages to a Claude Code or Codex session that
// works as no agent. They live in ~/.sunstack/inbox/<session ID>/, keyed by
// the Claude session ID or Codex thread ID, so the session finds them through
// its own environment. Delivery and nudges follow the team inbox's rules.

var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsSessionID reports whether s looks like a Claude session or Codex thread ID.
func IsSessionID(s string) bool { return sessionIDRe.MatchString(s) }

func hostInboxDir(sid string) string { return filepath.Join(Home(), "inbox", sid) }
func hostTakenDir(sid string) string { return filepath.Join(hostInboxDir(sid), ".taken") }
func hostArchive() string            { return filepath.Join(Home(), "log", "messages") }

// FindSession finds a running session on this host by tmux pane (%N) or by
// session ID, with its team and agent if it has one.
func FindSession(ref string) (*HostSession, error) {
	all, _ := ScanSessions()
	classify(all, teamsFor(all))
	for _, s := range all {
		if (strings.HasPrefix(ref, "%") && s.Pane == ref) || (ref != "" && s.SessionID == ref) {
			return s, nil
		}
	}
	return nil, fail(ExitFail, "not_found", "no running Claude Code or Codex session %s on this host (sunstack org --by host lists them)", ref)
}

// SendToSession writes a message into a free session's host inbox and nudges
// it. A session that works as an agent should be sent to by its agent label.
func SendToSession(s *HostSession, o SendOptions) (*SendResult, error) {
	if strings.TrimSpace(o.Body) == "" {
		return nil, fail(ExitUsage, "missing_arguments", "the message text is empty")
	}
	if o.Type == "" {
		o.Type = "fyi"
	}
	if !hasType(o.Type) || o.Type == "shutdown" {
		return nil, fail(ExitUsage, "usage", "type must be question, handoff, fyi or done for a session without an agent")
	}
	if !IsSessionID(s.SessionID) {
		return nil, fail(ExitFail, "not_found", "that session has no session ID sunstack can address")
	}
	if o.Op != "" && !opRe.MatchString(o.Op) {
		return nil, fail(ExitUsage, "usage", "invalid --op: letters, digits, . _ - only, at most 64")
	}
	if o.ReplyTo != "" && !msgIDRe.MatchString(o.ReplyTo) {
		return nil, fail(ExitUsage, "usage", "invalid --reply-to message id: %s", o.ReplyTo)
	}
	if o.FromSession != "" && !IsSessionID(o.FromSession) {
		o.FromSession = ""
	}
	from := "user"
	if o.FromLabel != "" {
		from = o.FromLabel
	}
	to := "session " + s.SessionID
	dir := hostInboxDir(s.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	if o.Op != "" {
		for _, d := range []string{dir, hostTakenDir(s.SessionID), hostArchive()} {
			for _, n := range listNames(d, ".md") {
				if m, err := parseMessage(filepath.Join(d, n)); err == nil && m.Op == o.Op && m.To == to && strings.TrimSpace(m.Body) == strings.TrimSpace(o.Body) {
					return &SendResult{ID: m.ID, To: to, Note: "already sent with this --op; not sent again"}, nil
				}
			}
		}
	}
	m := &Message{From: from, To: to, At: now(), Type: o.Type, ReplyTo: o.ReplyTo, Body: o.Body, Op: o.Op, FromSession: o.FromSession}
	if from == "user" {
		m.Via = o.Via
	}
	var tmp string
	for i := 0; ; i++ {
		m.ID = newMessageID(from)
		tmp = filepath.Join(dir, "."+m.ID+".tmp")
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
	if err := os.Rename(tmp, filepath.Join(dir, m.ID+".md")); err != nil {
		os.Remove(tmp)
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	res := &SendResult{ID: m.ID, To: to}
	if o.NoNudge {
		res.Note = "not nudged (--no-nudge)"
		return res, nil
	}
	res.Nudged, res.Note = nudgeSession(s, m)
	return res, nil
}

// nudgeSession types the check prompt into a free session's pane, under the
// same rules as an agent nudge: only into an idle input box.
func nudgeSession(s *HostSession, m *Message) (string, string) {
	wait := "; the message waits and shows at that session's next prompt"
	if s.Pane == "" || (s.Tool != "claude" && s.Tool != "codex") {
		return "", "not nudged (the session is not in a tmux pane)" + wait
	}
	locks := filepath.Join(Home(), "locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		return "", "not nudged" + wait
	}
	unlock, err := lockDir(filepath.Join(locks, "nudge-"+s.SessionID), "a nudge")
	if err != nil {
		return "", "not nudged (another nudge is in progress)" + wait
	}
	defer unlock()
	if !paneRunsTool(s.socket, s.Pane, s.Tool) {
		return "", "not nudged (the pane is not running " + s.Tool + " in the foreground)" + wait
	}
	if err := typeInto(s.socket, s.Pane, s.Tool, NudgePrompt(s.Tool, m)); err != nil {
		return "", "not nudged (" + err.Error() + ")" + wait
	}
	return s.Tool + " " + s.Pane, ""
}

// HostCheck lists the messages for this free session: pending and taken.
func HostCheck(sid string) ([]*Message, error) {
	if !IsSessionID(sid) {
		return nil, fail(ExitUsage, "no_session", "this CLI did not give its session ID (CLAUDE_CODE_SESSION_ID or CODEX_THREAD_ID)")
	}
	var out []*Message
	for _, d := range []struct{ dir, state string }{{hostTakenDir(sid), "taken (by this session)"}, {hostInboxDir(sid), "pending"}} {
		for _, n := range listNames(d.dir, ".md") {
			if m, err := parseMessage(filepath.Join(d.dir, n)); err == nil {
				m.State = d.state
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// HostPending counts what waits for a session in the host inbox.
func HostPending(sid string) int {
	if !IsSessionID(sid) {
		return 0
	}
	return len(listNames(hostInboxDir(sid), ".md")) + len(listNames(hostTakenDir(sid), ".md"))
}

// HostMove takes (ack false) or archives (ack true) one of this session's
// messages.
func HostMove(sid, msgID string, ack bool) error {
	if !IsSessionID(sid) {
		return fail(ExitUsage, "no_session", "this CLI did not give its session ID (CLAUDE_CODE_SESSION_ID or CODEX_THREAD_ID)")
	}
	if !msgIDRe.MatchString(msgID) {
		return fail(ExitUsage, "usage", "invalid message id")
	}
	pending := filepath.Join(hostInboxDir(sid), msgID+".md")
	taken := filepath.Join(hostTakenDir(sid), msgID+".md")
	src := ""
	switch {
	case exists(taken):
		src = taken
	case exists(pending):
		src = pending
	case exists(filepath.Join(hostArchive(), msgID+".md")):
		return fail(ExitFail, "done", "message %s was already acked", msgID)
	default:
		return fail(ExitFail, "not_found", "no message %s for this session", msgID)
	}
	dst := taken
	if ack {
		dst = filepath.Join(hostArchive(), msgID+".md")
	}
	if src == dst {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	return nil
}

// ResolveTeam finds an indexed team by name or ID.
func ResolveTeam(ref string) (*Project, error) {
	if filepath.IsAbs(ref) {
		return FindProject(ref)
	}
	var hits []*Project
	for _, p := range IndexedProjects() {
		t, _ := p.Team()
		if t.ID == ref || strings.EqualFold(t.Name, ref) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fail(ExitFail, "not_found", "no team %q on this host (sunstack teams lists them)", ref)
	case 1:
		return hits[0], nil
	}
	var roots []string
	for _, h := range hits {
		roots = append(roots, h.Root)
	}
	return nil, fail(ExitUsage, "ambiguous", "%s matches several teams on this host (%s), for example two checkouts of one repo; address the session by its tmux pane or session ID instead (sunstack org --by host)", ref, strings.Join(roots, ", "))
}

// TeamLabel is how a sender in this team is named to another team.
func (p *Project) TeamLabel(id string) string {
	t, _ := p.Team()
	// A message header treats # as a comment, so keep it out of the name.
	return fmt.Sprintf("%s/%s", strings.ReplaceAll(t.Name, "#", "-"), id)
}

// VerifySender checks that token is a live claim on id, for a message sent
// as that agent to another team or a session.
func (p *Project) VerifySender(id, token string) error {
	claims, err := p.Claims(id)
	if err != nil {
		return err
	}
	if findToken(claims, token) == nil {
		return fail(ExitClaim, "token", "sending as %s needs this session's --token", id)
	}
	return nil
}

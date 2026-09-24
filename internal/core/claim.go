package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Live is one session's claim on an agent ID, in _local/live/<id>/<token>.json.
// Several sessions may work as the same agent, each with its own claim.
type Live struct {
	Tool        string `json:"tool"`
	Host        string `json:"host"`
	Token       string `json:"token"`
	Claimed     string `json:"claimed"`
	LastContact string `json:"last_contact"`
	Session     string `json:"session,omitempty"`
	TmuxPane    string `json:"tmux_pane,omitempty"`
	TmuxSocket  string `json:"tmux_socket,omitempty"`
	Task        string `json:"task,omitempty"`       // what this session works on, up to 10 characters
	PaneTitle   string `json:"pane_title,omitempty"` // the tmux pane title before as, restored on release

	path string // where it was read from
}

// Label names a session: <id>_<task>, or just <id> when it has no task.
func (l *Live) Label(id string) string {
	if l.Task == "" {
		return id
	}
	return id + "_" + l.Task
}

var taskRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,9}$`)

// ValidTask checks a session task label: lowercase, digits and -, 1 to 10 characters.
func ValidTask(s string) bool { return taskRe.MatchString(s) }

func (p *Project) claimDir(id string) string { return p.local("live", id) }

// legacyClaim is the single-claim file of protocol 1, still read so claims
// made by an older version keep working until they are released.
func (p *Project) legacyClaim(id string) string { return p.local("live", id+".json") }

func (p *Project) readClaim(id, path string) (*Live, error) {
	if err := p.noSymlink(path); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fail(ExitFail, "bad_claim", "cannot read a claim on %s: %v", id, err)
	}
	var l Live
	if json.Unmarshal(b, &l) != nil || !tokenRe.MatchString(l.Token) {
		return nil, fail(ExitFail, "bad_claim", "the claim file %s is damaged; check it, then delete it to free %s", path, id)
	}
	l.path = path
	return &l, nil
}

var tokenRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Claims lists the sessions working as id, oldest first. A claim file that
// exists but cannot be read or parsed is an error, never "free": state
// changes stop until it is repaired.
func (p *Project) Claims(id string) ([]*Live, error) {
	var out []*Live
	if l, err := p.readClaim(id, p.legacyClaim(id)); err != nil {
		return nil, err
	} else if l != nil {
		out = append(out, l)
	}
	if err := p.noSymlink(p.claimDir(id)); err != nil {
		return nil, err
	}
	for _, n := range listNames(p.claimDir(id), ".json") {
		l, err := p.readClaim(id, filepath.Join(p.claimDir(id), n))
		if err != nil {
			return nil, err
		}
		if l != nil {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Claimed < out[j].Claimed })
	return out, nil
}

func findToken(claims []*Live, token string) *Live {
	for _, c := range claims {
		if token != "" && c.Token == token {
			return c
		}
	}
	return nil
}

// writeLive stores l under its token, moving a legacy claim file if needed.
func (p *Project) writeLive(id string, l *Live) error {
	b, _ := json.MarshalIndent(l, "", "  ")
	path := filepath.Join(p.claimDir(id), l.Token+".json")
	if err := writeAtomic(path, append(b, '\n')); err != nil {
		return err
	}
	if l.path != "" && l.path != path {
		os.Remove(l.path)
	}
	l.path = path
	return nil
}

func (p *Project) removeLive(l *Live) error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func claimLines(claims []*Live) string {
	var b strings.Builder
	for _, c := range claims {
		b.WriteString(c.ClaimLine() + "\n")
	}
	return b.String()
}

// ClaimLine describes an existing claim for the user and for --expect.
func (l *Live) ClaimLine() string {
	pane := l.TmuxPane
	if pane == "" {
		pane = "-"
	}
	task := l.Task
	if task == "" {
		task = "-"
	}
	return fmt.Sprintf("claim=%s task=%s tool=%s host=%s pane=%s last_contact=%s", l.Token, task, l.Tool, l.Host, pane, l.LastContact)
}

// AsOptions are the inputs of `sunstack as`.
type AsOptions struct {
	Arg      string // id or title
	Tool     string
	Token    string // resume with this token
	Takeover bool
	Expect   string // token shown in the refusal
	Join     bool   // work as this agent alongside the sessions already on it
	Task     string // short label for what this session works on
	Pane     string // $TMUX_PANE
	Socket   string // tmux server socket, from $TMUX
	Session  string // CLI session id, when known
}

// AsResult is a successful claim.
type AsResult struct {
	ID     string
	Mode   string // claimed | joined | resumed | taken_over
	Token  string
	Task   string
	Others []*Live // other sessions working as this agent
}

// Roster lists "id<TAB>free|active:<n>|unknown<TAB>role" lines.
func (p *Project) Roster(prefix string) string {
	var b strings.Builder
	for _, id := range p.Agents() {
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			continue
		}
		state := "free"
		if cs, err := p.Claims(id); err != nil {
			state = "unknown"
		} else if len(cs) > 0 {
			state = fmt.Sprintf("active:%d", len(cs))
		}
		agent, _ := os.ReadFile(filepath.Join(p.AgentDir(id), "AGENT.md"))
		fmt.Fprintf(&b, "%s\t%s\t%s\n", id, state, Duty(agent))
	}
	return b.String()
}

// resolve maps an id or a title with exactly one instance to an id.
func (p *Project) resolve(arg string) (string, error) {
	if arg == "" {
		r := p.Roster("")
		if r == "" {
			return "", fail(ExitFail, "empty_team", "no agents hired yet; use the recruit skill, or sunstack hire <title> <name>")
		}
		return "", &Error{Code: ExitUsage, Reason: "missing_arguments", Msg: "choose an id", Stdout: "missing_arguments: id\n" + r}
	}
	if p.HasAgent(arg) {
		if !ValidID(arg) {
			return "", fail(ExitUsage, "usage", "invalid id: %s", arg)
		}
		return arg, nil
	}
	if !ValidPart(arg) {
		return "", fail(ExitUsage, "usage", "invalid id or title: %s", arg)
	}
	var matches []string
	for _, id := range p.Agents() {
		if strings.HasPrefix(id, arg+".") {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", fail(ExitFail, "not_found", "no agent %s; hire it first", arg)
	case 1:
		return matches[0], nil
	}
	return "", &Error{Code: ExitUsage, Reason: "missing_arguments", Msg: "several instances of " + arg,
		Stdout: "missing_arguments: id\n" + p.Roster(arg+".")}
}

// checkIdentityFiles makes sure the files an identity is built from are real,
// readable and conflict-free before anything is claimed.
func (p *Project) checkIdentityFiles(id string) error {
	for _, f := range []struct {
		path     string
		required bool
	}{
		{filepath.Join(p.Dir, "PROTOCOL.md"), true},
		{filepath.Join(p.AgentDir(id), "AGENT.md"), true},
		{filepath.Join(p.Dir, "PILLARS.md"), false},
		{filepath.Join(p.AgentDir(id), "pillars.md"), false},
		{filepath.Join(p.AgentDir(id), "context.md"), false},
	} {
		rel, _ := filepath.Rel(p.Root, f.path)
		if err := p.noSymlink(f.path); err != nil {
			return err
		}
		b, ok, err := readMaybe(f.path)
		switch {
		case err != nil:
			return fail(ExitFail, "fs", "cannot read %s: %v", rel, err)
		case !ok && f.required:
			return fail(ExitFail, "missing_file", "%s is missing; run sunstack init or sunstack health", rel)
		case HasConflictMarkers(b):
			return fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", rel)
		}
	}
	return nil
}

func samePane(l *Live, pane, socket string) bool {
	return pane != "" && l.TmuxPane == pane && (l.TmuxSocket == "" || socket == "" || l.TmuxSocket == socket)
}

// As claims, resumes or takes over an agent ID (design §6, §7).
func (p *Project) As(o AsOptions) (*AsResult, error) {
	if o.Task != "" && !ValidTask(o.Task) {
		return nil, fail(ExitUsage, "usage", "invalid task %q: lowercase letters, digits and -, at most 10 characters", o.Task)
	}
	id, err := p.resolve(o.Arg)
	if err != nil {
		return nil, err
	}
	if err := p.checkIdentityFiles(id); err != nil {
		return nil, err
	}

	unlock, err := p.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !p.HasAgent(id) {
		return nil, fail(ExitFail, "not_found", "%s was removed while waiting", id)
	}

	claims, err := p.Claims(id)
	if err != nil {
		return nil, err
	}
	res := &AsResult{ID: id}
	claimed := now()
	var replace *Live // a claim this one replaces
	var pane *Live
	for _, c := range claims {
		if samePane(c, o.Pane, o.Socket) {
			pane = c
		}
	}
	switch {
	case o.Token != "":
		mine := findToken(claims, o.Token)
		if mine == nil {
			return nil, fail(ExitClaim, "token", "token does not match any claim on %s (taken over or released?)", id)
		}
		res.Mode, res.Token, claimed, replace = "resumed", mine.Token, mine.Claimed, mine
		if o.Task == "" {
			o.Task = mine.Task
		}
	case o.Takeover:
		if o.Expect == "" {
			return nil, fail(ExitUsage, "usage", "--takeover needs --expect <claim shown in the refusal>")
		}
		replace = findToken(claims, o.Expect)
		if replace == nil {
			e := fail(ExitClaim, "occupied", "the claim %s on %s is gone or changed; confirm again with a current claim line", o.Expect, id)
			e.Stdout = claimLines(claims)
			return nil, e
		}
		res.Mode, res.Token = "taken_over", NewToken()
	case len(claims) == 0:
		res.Mode, res.Token = "claimed", NewToken()
	case pane != nil:
		e := fail(ExitClaim, "occupied_same_pane", "%s is claimed from this same tmux pane; confirm with the user, then rerun with --takeover --expect %s", id, pane.Token)
		e.Stdout = pane.ClaimLine() + "\n"
		return nil, e
	case o.Join:
		res.Mode, res.Token = "joined", NewToken()
	default:
		e := fail(ExitClaim, "active", "%d other session(s) are working as %s; with the user's OK, join them (--join) or pick another agent", len(claims), id)
		e.Stdout = claimLines(claims)
		return nil, e
	}

	host, _ := os.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	l := &Live{Tool: o.Tool, Host: host, Token: res.Token, Claimed: claimed, LastContact: now(),
		Session: o.Session, TmuxPane: o.Pane, TmuxSocket: o.Socket, Task: o.Task}
	res.Task = o.Task
	for _, c := range claims {
		if c != replace {
			res.Others = append(res.Others, c)
		}
	}
	if l.Tool == "" {
		l.Tool = "unknown"
	}
	if replace != nil {
		l.path = replace.path // resumed: rewritten in place; taken over: replaced
		if res.Mode == "taken_over" {
			p.removeLive(replace)
			os.RemoveAll(p.sessionTmp(id, replace.Token))
			l.path = ""
		}
	}
	if l.TmuxPane != "" && l.TmuxSocket != "" {
		// Remember the pane's title so release can put it back.
		if replace != nil && replace.PaneTitle != "" && replace.TmuxPane == l.TmuxPane {
			l.PaneTitle = replace.PaneTitle
		} else if out, err := exec.Command("tmux", TmuxArgs(l.TmuxSocket, "display-message", "-p", "-t", l.TmuxPane, "#{pane_title}")...).Output(); err == nil {
			l.PaneTitle = strings.TrimSpace(string(out))
		}
	}
	if err := p.writeLive(id, l); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	fields := []string{"tool=" + l.Tool}
	if l.Task != "" {
		fields = append(fields, "task="+l.Task)
	}
	if l.TmuxPane != "" {
		fields = append(fields, "pane="+l.TmuxPane)
		// Name the pane after the session so tmux shows who works there; only
		// when we know which tmux server the pane lives on.
		if l.TmuxSocket != "" {
			exec.Command("tmux", TmuxArgs(l.TmuxSocket, "select-pane", "-t", l.TmuxPane, "-T", l.Label(id))...).Run()
		}
	}
	p.LogEvent(map[string]string{"claimed": "claim", "joined": "join", "resumed": "resume", "taken_over": "takeover"}[res.Mode], id, fields...)
	return res, nil
}

// Bundle is the identity text printed after a successful as.
func (p *Project) Bundle(r *AsResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "sunstack: %s %s\n", r.Mode, r.ID)
	section := func(title, path string) {
		if c, err := os.ReadFile(path); err == nil {
			fmt.Fprintf(&b, "\n===== %s =====\n%s", title, c)
			if len(c) > 0 && c[len(c)-1] != '\n' {
				b.WriteByte('\n')
			}
		}
	}
	section("PROTOCOL.md", filepath.Join(p.Dir, "PROTOCOL.md"))
	section("PILLARS.md (team)", filepath.Join(p.Dir, "PILLARS.md"))
	section("BOARD.md (team: objectives, the user's key results, directives)", p.teamBoardPath())
	section(r.ID+"/AGENT.md", filepath.Join(p.AgentDir(r.ID), "AGENT.md"))
	section(r.ID+"/pillars.md", filepath.Join(p.AgentDir(r.ID), "pillars.md"))
	section(r.ID+"/board.md", p.boardPath(r.ID))
	section(r.ID+"/context.md", filepath.Join(p.AgentDir(r.ID), "context.md"))
	boards := p.LoadBoards()
	if un := boards.Unaligned(r.ID); len(un) > 0 {
		b.WriteString("\n===== directives not aligned yet (check your board against each, then set aligned: to the last) =====\n")
		for _, d := range un {
			fmt.Fprintf(&b, "%s %s %s\n", d.Key, d.Date, d.Text)
		}
	}
	if len(r.Others) > 0 {
		b.WriteString("\n===== other sessions working as this agent (work on a different key result; merge on mismatch) =====\n")
		for _, c := range r.Others {
			fmt.Fprintf(&b, "%s: %s on %s, last contact %s\n", c.Label(r.ID), c.Tool, c.Host, c.LastContact)
		}
	}
	if months := listNames(p.archiveDir(r.ID), ".md"); len(months) > 0 {
		fmt.Fprintf(&b, "\n===== archive (not loaded; read %s/archive/<month>.md only when you need history) =====\n%s\n", r.ID, strings.Join(months, ", "))
	}
	b.WriteString("\n===== threads =====\n")
	for _, n := range listNames(filepath.Join(p.AgentDir(r.ID), "threads"), ".md") {
		b.WriteString(strings.TrimSuffix(n, ".md") + "\n")
	}
	b.WriteString("\n===== inbox (listed only; act on it only if spawned for it or the user asks) =====\n")
	for _, n := range listNames(p.local("inbox", r.ID), ".md") {
		b.WriteString(n + "\n")
	}
	task := r.Task
	if task == "" {
		task = "-"
	}
	fmt.Fprintf(&b, `
===== session state =====
root: %s
id: %s
token: %s
task: %s
session_name: %s
protocol: %d
Pass --root, the id and --token explicitly on every later snapshot, commit and release.
Before ending or switching identity, run the Sunstack save skill, then sunstack release.
`, p.Root, r.ID, r.Token, task, (&Live{Task: r.Task}).Label(r.ID), ProtocolVersion)
	return b.String()
}

func listNames(dir, suffix string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, suffix) && !strings.HasPrefix(n, ".") {
			out = append(out, n)
		}
	}
	return out
}

// sessionTmp is one session's scratch space for candidates.
func (p *Project) sessionTmp(id, token string) string { return p.local("tmp", id, token) }

// requireToken must be called with the ID lock held.
func (p *Project) requireToken(id, token string) (*Live, error) {
	if token == "" {
		return nil, fail(ExitClaim, "token", "missing --token; run as again (design §7)")
	}
	claims, err := p.Claims(id)
	if err != nil {
		return nil, err
	}
	cur := findToken(claims, token)
	if cur == nil {
		return nil, fail(ExitClaim, "token", "token does not match any claim on %s (taken over or released?)", id)
	}
	return cur, nil
}

// Release drops this session's claim; other sessions on the same ID keep theirs.
func (p *Project) Release(id, token string) error {
	if !ValidID(id) {
		return fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := p.requireToken(id, token)
	if err != nil {
		return err
	}
	if err := p.removeLive(cur); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if cur.TmuxPane != "" && cur.TmuxSocket != "" && cur.PaneTitle != "" {
		exec.Command("tmux", TmuxArgs(cur.TmuxSocket, "select-pane", "-t", cur.TmuxPane, "-T", cur.PaneTitle)...).Run()
	}
	os.RemoveAll(p.sessionTmp(id, token))
	if rest, _ := p.Claims(id); len(rest) == 0 {
		os.RemoveAll(p.local("tmp", id))
		os.Remove(p.claimDir(id))
	}
	p.LogEvent("release", id)
	return nil
}

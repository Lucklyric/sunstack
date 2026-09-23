package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Live is a claim record in _local/live/<id>.json (design §5).
type Live struct {
	Tool        string `json:"tool"`
	Host        string `json:"host"`
	Token       string `json:"token"`
	Claimed     string `json:"claimed"`
	LastContact string `json:"last_contact"`
	Session     string `json:"session,omitempty"`
	TmuxPane    string `json:"tmux_pane,omitempty"`
}

func (p *Project) livePath(id string) string { return p.local("live", id+".json") }

// ReadLive returns the claim on id, or nil when it is free.
func (p *Project) ReadLive(id string) *Live {
	b, err := os.ReadFile(p.livePath(id))
	if err != nil {
		return nil
	}
	var l Live
	if json.Unmarshal(b, &l) != nil || l.Token == "" {
		return nil
	}
	return &l
}

func (p *Project) writeLive(id string, l *Live) error {
	b, _ := json.MarshalIndent(l, "", "  ")
	return writeAtomic(p.livePath(id), append(b, '\n'))
}

// ClaimLine describes an existing claim for the user and for --expect.
func (l *Live) ClaimLine() string {
	pane := l.TmuxPane
	if pane == "" {
		pane = "-"
	}
	return fmt.Sprintf("claim=%s tool=%s host=%s pane=%s last_contact=%s", l.Token, l.Tool, l.Host, pane, l.LastContact)
}

// AsOptions are the inputs of `sunstack as`.
type AsOptions struct {
	Arg      string // id or title
	Tool     string
	Token    string // resume with this token
	Takeover bool
	Expect   string // token shown in the refusal
	Pane     string // $TMUX_PANE
	Session  string // CLI session id, when known
}

// AsResult is a successful claim.
type AsResult struct {
	ID    string
	Mode  string // claimed | resumed | taken_over
	Token string
}

// Roster lists "id<TAB>free|claimed" lines.
func (p *Project) Roster(prefix string) string {
	var b strings.Builder
	for _, id := range p.Agents() {
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			continue
		}
		state := "free"
		if p.ReadLive(id) != nil {
			state = "claimed"
		}
		fmt.Fprintf(&b, "%s\t%s\n", id, state)
	}
	return b.String()
}

// resolve maps an id or a title with exactly one instance to an id.
func (p *Project) resolve(arg string) (string, error) {
	if arg == "" {
		r := p.Roster("")
		if r == "" {
			return "", fail(ExitFail, "empty_team", "no agents hired yet; run sunstack hire first")
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

// As claims, resumes or takes over an agent ID (design §6, §7).
func (p *Project) As(o AsOptions) (*AsResult, error) {
	id, err := p.resolve(o.Arg)
	if err != nil {
		return nil, err
	}
	for _, f := range []string{
		filepath.Join(p.Dir, "PILLARS.md"),
		filepath.Join(p.AgentDir(id), "AGENT.md"),
		filepath.Join(p.AgentDir(id), "pillars.md"),
		filepath.Join(p.AgentDir(id), "context.md"),
	} {
		if fileHasConflict(f) {
			rel, _ := filepath.Rel(p.Root, f)
			return nil, fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", rel)
		}
	}

	unlock, err := p.lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()

	cur := p.ReadLive(id)
	res := &AsResult{ID: id}
	claimed := now()
	switch {
	case cur == nil:
		res.Mode, res.Token = "claimed", NewToken()
	case o.Token != "":
		if o.Token != cur.Token {
			return nil, fail(ExitClaim, "token", "token does not match the current claim on %s (taken over?)", id)
		}
		res.Mode, res.Token, claimed = "resumed", cur.Token, cur.Claimed
	case o.Takeover:
		if o.Expect == "" {
			return nil, fail(ExitUsage, "usage", "--takeover needs --expect <claim shown in the refusal>")
		}
		if o.Expect != cur.Token {
			e := fail(ExitClaim, "occupied", "the claim on %s changed since it was shown; confirm again with the new claim line", id)
			e.Stdout = cur.ClaimLine() + "\n"
			return nil, e
		}
		res.Mode, res.Token = "taken_over", NewToken()
	case o.Pane != "" && cur.TmuxPane == o.Pane:
		e := fail(ExitClaim, "occupied_same_pane", "%s is claimed from this same tmux pane; confirm with the user, then rerun with --takeover --expect %s", id, cur.Token)
		e.Stdout = cur.ClaimLine() + "\n"
		return nil, e
	default:
		e := fail(ExitClaim, "occupied", "%s is claimed by another session; only take over (--takeover --expect %s) if the user confirms no other session should continue", id, cur.Token)
		e.Stdout = cur.ClaimLine() + "\n"
		return nil, e
	}

	host, _ := os.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	l := &Live{Tool: o.Tool, Host: host, Token: res.Token, Claimed: claimed, LastContact: now(), Session: o.Session, TmuxPane: o.Pane}
	if l.Tool == "" {
		l.Tool = "unknown"
	}
	if err := p.writeLive(id, l); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	fields := []string{"tool=" + l.Tool}
	if l.TmuxPane != "" {
		fields = append(fields, "pane="+l.TmuxPane)
	}
	p.LogEvent(map[string]string{"claimed": "claim", "resumed": "resume", "taken_over": "takeover"}[res.Mode], id, fields...)
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
	section(r.ID+"/AGENT.md", filepath.Join(p.AgentDir(r.ID), "AGENT.md"))
	section(r.ID+"/pillars.md", filepath.Join(p.AgentDir(r.ID), "pillars.md"))
	section(r.ID+"/context.md", filepath.Join(p.AgentDir(r.ID), "context.md"))
	b.WriteString("\n===== threads =====\n")
	for _, n := range listNames(filepath.Join(p.AgentDir(r.ID), "threads"), ".md") {
		b.WriteString(strings.TrimSuffix(n, ".md") + "\n")
	}
	b.WriteString("\n===== inbox (listed only; act on it only if spawned for it or the user asks) =====\n")
	for _, n := range listNames(p.local("inbox", r.ID), ".md") {
		b.WriteString(n + "\n")
	}
	fmt.Fprintf(&b, `
===== session state =====
root: %s
id: %s
token: %s
protocol: %d
Pass --root, the id and --token explicitly on every later snapshot, commit and release.
Run save and release before ending or switching identity.
`, p.Root, r.ID, r.Token, ProtocolVersion)
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

// requireToken must be called with the ID lock held.
func (p *Project) requireToken(id, token string) (*Live, error) {
	if token == "" {
		return nil, fail(ExitClaim, "token", "missing --token; run as again (design §7)")
	}
	cur := p.ReadLive(id)
	if cur == nil {
		return nil, fail(ExitClaim, "token", "%s is not claimed; run as first", id)
	}
	if cur.Token != token {
		return nil, fail(ExitClaim, "token", "token does not match the current claim on %s (taken over?)", id)
	}
	return cur, nil
}

// Release drops the claim held with token (design §6).
func (p *Project) Release(id, token string) error {
	if !ValidID(id) {
		return fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := p.requireToken(id, token); err != nil {
		return err
	}
	if err := os.Remove(p.livePath(id)); err != nil && !os.IsNotExist(err) {
		return fail(ExitFail, "fs", "%v", err)
	}
	os.RemoveAll(p.local("tmp", id))
	p.LogEvent("release", id)
	return nil
}

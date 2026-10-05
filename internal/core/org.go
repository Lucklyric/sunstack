package core

import (
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// The org snapshot (org design, v0.8): one collector for this host's teams,
// agents, boards, claims and running sessions. Every view renders it, and in
// v0.9 the same JSON is what a peer host returns.

// OrgSchema versions the snapshot's JSON.
const OrgSchema = 1

// Org is this host's snapshot.
type Org struct {
	Schema    int          `json:"schema"`
	Host      HostInfo     `json:"host"`
	At        string       `json:"at"`
	Teams     []*OrgTeam   `json:"teams"`
	Free      []*FreeGroup `json:"free"` // sessions outside any team
	Attention []string     `json:"attention"`
	Notes     []string     `json:"notes,omitempty"`
}

// OrgTeam is one team on this host.
type OrgTeam struct {
	ID         string         `json:"id"` // team ID, or the root for a team without a TEAM file
	Name       string         `json:"name"`
	Root       string         `json:"root"`
	Objectives []OrgObjective `json:"objectives"`
	Issues     []string       `json:"issues"`
	Agents     []*OrgAgent    `json:"agents"`
	Free       []*HostSession `json:"free"` // sessions in this project that hold no claim
	project    *Project
}

// OrgObjective is a team objective with the open key results serving it.
type OrgObjective struct {
	Key  string   `json:"key"`
	Text string   `json:"text"`
	Due  string   `json:"due,omitempty"`
	KRs  []string `json:"krs"`
}

// OrgAgent is one agent with its current work and sessions.
type OrgAgent struct {
	ID       string         `json:"id"`
	Duty     string         `json:"duty"`
	Now      []string       `json:"now"`
	Done     []string       `json:"done"` // Done entries still on the board, with how each was checked
	Pending  int            `json:"pending"`
	Sessions []*HostSession `json:"sessions"`
}

// FreeGroup is free sessions outside teams, grouped by git remote or folder.
type FreeGroup struct {
	Group    string         `json:"group"`
	Sessions []*HostSession `json:"sessions"`
}

// BuildOrg collects the snapshot. Teams found in a session's folder that are
// not indexed yet are registered on the way.
func BuildOrg() *Org {
	o := &Org{Schema: OrgSchema, Host: ThisHost(), At: now()}
	sessions, notes := ScanSessions()
	o.Notes = notes
	teams := teamsFor(sessions)
	byAgent := classify(sessions, teams)
	today := time.Now()
	for _, p := range teams {
		t := p.orgTeam(sessions, byAgent[p.Root], today)
		o.Teams = append(o.Teams, t)
	}
	groups := map[string]*FreeGroup{}
	remotes := map[string]string{}
	for _, s := range sessions {
		if s.Agent != "" || s.Team != "" {
			continue
		}
		g := realPath(s.Cwd)
		if r, ok := remotes[s.Cwd]; ok {
			g = r
		} else if r := gitRemote(s.Cwd); r != "" {
			remotes[s.Cwd], g = r, r
		}
		if g == "" {
			g = "(unknown folder)"
		}
		s.Group = g
		if groups[g] == nil {
			groups[g] = &FreeGroup{Group: g}
			o.Free = append(o.Free, groups[g])
		}
		groups[g].Sessions = append(groups[g].Sessions, s)
	}
	sort.Slice(o.Free, func(i, j int) bool { return o.Free[i].Group < o.Free[j].Group })
	o.Attention = o.attention()
	o.fillEmpty()
	return o
}

// fillEmpty turns nil lists into empty ones, so the JSON never has nulls.
func (o *Org) fillEmpty() {
	if o.Teams == nil {
		o.Teams = []*OrgTeam{}
	}
	if o.Free == nil {
		o.Free = []*FreeGroup{}
	}
	if o.Attention == nil {
		o.Attention = []string{}
	}
	for _, t := range o.Teams {
		if t.Objectives == nil {
			t.Objectives = []OrgObjective{}
		}
		if t.Issues == nil {
			t.Issues = []string{}
		}
		if t.Agents == nil {
			t.Agents = []*OrgAgent{}
		}
		if t.Free == nil {
			t.Free = []*HostSession{}
		}
		for i := range t.Objectives {
			if t.Objectives[i].KRs == nil {
				t.Objectives[i].KRs = []string{}
			}
		}
		for _, a := range t.Agents {
			if a.Now == nil {
				a.Now = []string{}
			}
			if a.Done == nil {
				a.Done = []string{}
			}
			if a.Sessions == nil {
				a.Sessions = []*HostSession{}
			}
		}
	}
}

// teamsFor is the indexed teams plus any team a session runs in, which is
// registered on the way.
func teamsFor(sessions []*HostSession) []*Project {
	teams := IndexedProjects()
	checked := map[string]bool{}
	for _, s := range sessions {
		if s.Cwd == "" || checked[s.Cwd] {
			continue
		}
		checked[s.Cwd] = true
		if p, err := FindProject(realPath(s.Cwd)); err == nil && !hasRoot(teams, p.Root) {
			_ = p.Register()
			teams = append(teams, p)
		}
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i].Root < teams[j].Root })
	return teams
}

// classify marks each session with the team and agent it works as, and
// returns, per team root and agent, its sessions: the matched ones, plus a
// "not seen" entry for a claim no running process matches. Sessions holding
// no claim are placed in the deepest team whose project holds their folder.
func classify(sessions []*HostSession, teams []*Project) map[string]map[string][]*HostSession {
	host := ThisHost().Name
	servers := map[string]string{}
	serverOf := func(socket string) string {
		if v, ok := servers[socket]; ok {
			return v
		}
		pid, _ := serverPID(socket)
		servers[socket] = pid
		return pid
	}
	out := map[string]map[string][]*HostSession{}
	for _, p := range teams {
		tf, ok := p.Team()
		tid := tf.ID
		if !ok {
			tid = p.Root
		}
		out[p.Root] = map[string][]*HostSession{}
		for _, id := range p.Agents() {
			claims, _ := p.Claims(id)
			for _, c := range claims {
				var s *HostSession
				for _, x := range sessions {
					if x.Agent != "" {
						continue
					}
					bySession := c.Session != "" && x.SessionID == c.Session
					// A pane can be reused by a later session: when both sides
					// know their session ID, they must agree.
					byPane := c.TmuxPane != "" && x.Pane == c.TmuxPane && sameServer(c) &&
						sameTmuxServer(c, serverOf(x.socket), serverOf) &&
						(c.Session == "" || x.SessionID == "" || c.Session == x.SessionID)
					if bySession || byPane {
						s = x
						break
					}
				}
				if s == nil {
					// No running process matches the claim: outside tmux with
					// no session ID recorded, or the session has ended.
					s = &HostSession{Tool: c.Tool, Status: "not seen", Reach: "none", SessionID: c.Session, Pane: c.TmuxPane, Where: TmuxWhere(c.TmuxSocket, c.TmuxPane)}
				}
				s.Team, s.TeamName, s.TeamRoot, s.Agent, s.Label = tid, tf.Name, p.Root, id, c.Label(id)+"@"+host
				s.Doing, s.DoingAt = c.Doing, c.DoingAt
				out[p.Root][id] = append(out[p.Root][id], s)
			}
		}
	}
	for _, s := range sessions {
		if s.Agent != "" || s.Cwd == "" {
			continue
		}
		if p := TeamOf(s.Cwd, teams); p != nil {
			tf, ok := p.Team()
			s.Team, s.TeamName, s.TeamRoot = tf.ID, tf.Name, p.Root
			if !ok {
				s.Team = p.Root
			}
		}
	}
	return out
}

func (p *Project) orgTeam(sessions []*HostSession, byAgent map[string][]*HostSession, today time.Time) *OrgTeam {
	tf, ok := p.Team()
	t := &OrgTeam{ID: tf.ID, Name: tf.Name, Root: p.Root, project: p}
	if !ok {
		t.ID = p.Root
	}
	boards := p.LoadBoards()
	t.Issues = boards.Issues(today)
	for _, it := range boards.Team {
		if it.Section != "Objectives" || it.Done {
			continue
		}
		obj := OrgObjective{Key: it.Key, Text: it.Text, Due: it.Due}
		for _, id := range sortedKeys(boards.Agents) {
			for _, kr := range boards.Agents[id] {
				if kr.Obj == it.Key && !kr.Done {
					obj.KRs = append(obj.KRs, fmt.Sprintf("%s %s (%s)", kr.Ref(), kr.Text, strings.ToLower(kr.Section)))
				}
			}
		}
		t.Objectives = append(t.Objectives, obj)
	}
	for _, st := range p.Status() {
		a := &OrgAgent{ID: st.ID, Duty: st.Duty, Pending: st.Inbox}
		for _, it := range boards.Agents[st.ID] {
			if it.Section == "Now" && !it.Done {
				line := it.Key + " " + it.Text
				if it.By != "" {
					line += " (by " + it.By + ")"
				}
				a.Now = append(a.Now, line)
			}
			if it.Section == "Done" {
				v := it.Verified
				if v == "" {
					v = "missing"
				}
				a.Done = append(a.Done, fmt.Sprintf("%s %s (verified: %s)", it.Key, it.Text, v))
			}
		}
		a.Sessions = append(a.Sessions, byAgent[st.ID]...)
		t.Agents = append(t.Agents, a)
	}
	for _, s := range sessions {
		if s.Agent == "" && s.TeamRoot == p.Root {
			t.Free = append(t.Free, s)
		}
	}
	return t
}

func sortedKeys(m map[string][]Item) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// attention lists what waits on the user.
func (o *Org) attention() []string {
	var out []string
	seen := map[*HostSession]bool{}
	waiting := func(s *HostSession, team string) {
		if seen[s] || (s.Status != "waiting" && s.Status != "blocked") {
			return
		}
		seen[s] = true
		out = append(out, fmt.Sprintf("%s: %s is %s for you (%s)", team, sessionName(s), s.Status, placeOf(s)))
	}
	outside := func(s *HostSession) string {
		if s.SessionID != "" && s.Reach == "next prompt" {
			return fmt.Sprintf("; or move it into tmux: exit it, then sunstack reopen %s", s.SessionID)
		}
		return ""
	}
	for _, t := range o.Teams {
		for _, a := range t.Agents {
			for _, s := range a.Sessions {
				waiting(s, t.Name)
			}
			if a.Pending == 0 {
				continue
			}
			reachable := false
			for _, s := range a.Sessions {
				reachable = reachable || s.Reach == "nudge"
			}
			switch {
			case len(a.Sessions) == 0:
				out = append(out, fmt.Sprintf("%s: %d message(s) wait for %s, which has no live session (sunstack spawn %s starts one)", t.Name, a.Pending, a.ID, a.ID))
			case !reachable:
				out = append(out, fmt.Sprintf("%s: %d message(s) wait for %s; its sessions are outside tmux, so they see them at their next prompt%s", t.Name, a.Pending, a.ID, outside(a.Sessions[0])))
			}
		}
		for _, s := range t.Free {
			if s.Pending > 0 && s.Reach != "nudge" {
				out = append(out, fmt.Sprintf("%s: %d message(s) wait for %s; it is outside tmux, so it sees them at its next prompt%s", t.Name, s.Pending, sessionName(s), outside(s)))
			}
		}
		for _, s := range t.Free {
			waiting(s, t.Name)
		}
		for _, is := range t.Issues {
			if strings.Contains(is, "user") || strings.Contains(is, "decided by default") || strings.Contains(is, " was due ") || strings.Contains(is, "not aligned") ||
				strings.Contains(is, "does not exist") || strings.Contains(is, "is not an objective") {
				out = append(out, t.Name+": "+is)
			}
		}
	}
	for _, g := range o.Free {
		for _, s := range g.Sessions {
			waiting(s, g.Group)
			if s.Pending > 0 && s.Reach != "nudge" {
				out = append(out, fmt.Sprintf("%s: %d message(s) wait for %s; it is outside tmux, so it sees them at its next prompt%s", g.Group, s.Pending, sessionName(s), outside(s)))
			}
		}
	}
	return out
}

func sessionName(s *HostSession) string {
	switch {
	case s.Label != "":
		return s.Label
	case s.Name != "":
		return s.Tool + " " + s.Name
	}
	return s.Tool + " session"
}

func placeOf(s *HostSession) string {
	switch {
	case s.Where != "":
		return "tmux " + s.Where + " " + s.Pane
	case s.Reach == "background":
		return "background"
	case s.SessionID != "":
		return "outside tmux (messages wait for its next prompt), id " + s.SessionID
	case s.PID > 0:
		return fmt.Sprintf("outside tmux, pid %d", s.PID)
	}
	return "no tmux pane"
}

// gitRemote is the folder's origin URL without any credentials in it.
func gitRemote(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	r := strings.TrimSpace(string(out))
	if u, err := url.Parse(r); err == nil && u.User != nil {
		u.User = nil
		r = u.String()
	}
	return r
}

// OrgView renders the snapshot: "attention", "team", "agent" or "host".
func (o *Org) Text(view string) string {
	var b strings.Builder
	switch view {
	case "attention":
		o.writeAttention(&b)
	case "agent":
		o.writePeople(&b)
	case "host":
		o.writeHosts(&b)
	default:
		o.writeAttention(&b)
		b.WriteString("\n")
		o.writeWork(&b)
	}
	for _, n := range o.Notes {
		fmt.Fprintf(&b, "\nnote: %s\n", n)
	}
	return b.String()
}

func (o *Org) writeAttention(b *strings.Builder) {
	fmt.Fprintf(b, "Needs you (%s)\n", o.Host.Name)
	if len(o.Attention) == 0 {
		b.WriteString("  nothing\n")
	}
	for _, a := range o.Attention {
		fmt.Fprintf(b, "  %s\n", a)
	}
}

func sessionLine(s *HostSession) string {
	line := fmt.Sprintf("%s  %s, %s, %s", sessionName(s), s.Tool, s.Status, placeOf(s))
	if s.Doing != "" {
		line += "\n      doing: " + s.Doing
		if len(s.DoingAt) >= 10 {
			line += " (" + s.DoingAt[:10] + ")"
		}
	} else if s.Activity.LastPrompt != "" {
		line += "\n      last prompt: " + s.Activity.LastPrompt
	}
	if s.Activity.LastActive != "" {
		line += "\n      last active " + s.Activity.LastActive
	}
	return line
}

func freeLine(s *HostSession) string {
	line := fmt.Sprintf("%s  %s, %s", sessionName(s), s.Status, placeOf(s))
	if s.Pending > 0 {
		line += fmt.Sprintf(", %d message(s) waiting", s.Pending)
	}
	if s.Activity.Title != "" {
		line += "\n      title: " + s.Activity.Title
	}
	if s.Activity.LastPrompt != "" {
		line += "\n      last prompt: " + s.Activity.LastPrompt
	}
	if s.Activity.LastReply != "" {
		line += "\n      last reply: " + s.Activity.LastReply
	}
	if s.Activity.LastActive != "" {
		line += "\n      last active " + s.Activity.LastActive
	}
	return line
}

func (o *Org) writeWork(b *strings.Builder) {
	if len(o.Teams) == 0 {
		b.WriteString("No teams on this host yet (sunstack teams --scan <dir> finds them).\n")
	}
	for _, t := range o.Teams {
		fmt.Fprintf(b, "Team %s  %s\n", t.Name, t.Root)
		if len(t.Objectives) == 0 {
			b.WriteString("  no objectives yet\n")
		}
		for _, ob := range t.Objectives {
			due := ""
			if ob.Due != "" {
				due = " (due " + ob.Due + ")"
			}
			fmt.Fprintf(b, "  %s %s%s\n", ob.Key, ob.Text, due)
			if len(ob.KRs) == 0 {
				b.WriteString("    no open key results\n")
			}
			for _, kr := range ob.KRs {
				fmt.Fprintf(b, "    %s\n", kr)
			}
		}
		for _, a := range t.Agents {
			for _, s := range a.Sessions {
				fmt.Fprintf(b, "  session %s\n", sessionLine(s))
			}
		}
		for _, s := range t.Free {
			fmt.Fprintf(b, "  free %s\n", freeLine(s))
		}
		if n := len(t.Issues); n > 0 {
			fmt.Fprintf(b, "  %d board item(s) need attention (sunstack board --root %s)\n", n, t.Root)
		}
		b.WriteString("\n")
	}
	if len(o.Free) > 0 {
		b.WriteString("Sessions outside teams\n")
		for _, g := range o.Free {
			fmt.Fprintf(b, "  %s\n", g.Group)
			for _, s := range g.Sessions {
				fmt.Fprintf(b, "    %s\n", freeLine(s))
			}
		}
	}
}

func (o *Org) writePeople(b *strings.Builder) {
	for _, t := range o.Teams {
		fmt.Fprintf(b, "Team %s\n", t.Name)
		for _, a := range t.Agents {
			fmt.Fprintf(b, "  %s  %s\n", a.ID, a.Duty)
			for _, n := range a.Now {
				fmt.Fprintf(b, "    now: %s\n", n)
			}
			for _, d := range a.Done {
				fmt.Fprintf(b, "    done: %s\n", d)
			}
			if a.Pending > 0 {
				fmt.Fprintf(b, "    %d message(s) waiting\n", a.Pending)
			}
			if len(a.Sessions) == 0 {
				b.WriteString("    no live session\n")
			}
			for _, s := range a.Sessions {
				fmt.Fprintf(b, "    %s\n", sessionLine(s))
			}
		}
		b.WriteString("\n")
	}
}

func (o *Org) writeHosts(b *strings.Builder) {
	fmt.Fprintf(b, "Host %s\n", o.Host.Name)
	n := 0
	for _, t := range o.Teams {
		for _, a := range t.Agents {
			for _, s := range a.Sessions {
				fmt.Fprintf(b, "  %s\n", tagFirst(sessionLine(s), "["+t.Name+"]"))
				n++
			}
		}
		for _, s := range t.Free {
			fmt.Fprintf(b, "  %s\n", tagFirst(freeLine(s), "[free in "+t.Name+"]"))
			n++
		}
	}
	for _, g := range o.Free {
		for _, s := range g.Sessions {
			fmt.Fprintf(b, "  %s\n", tagFirst(freeLine(s), "[free: "+g.Group+"]"))
			n++
		}
	}
	if n == 0 {
		b.WriteString("  no Claude Code or Codex sessions running\n")
	}
}

func hasRoot(teams []*Project, root string) bool {
	for _, t := range teams {
		if t.Root == root {
			return true
		}
	}
	return false
}

// tagFirst puts a tag at the end of the first line of a multi-line entry.
func tagFirst(entry, tag string) string {
	first, rest, ok := strings.Cut(entry, "\n")
	if !ok {
		return entry + "  " + tag
	}
	return first + "  " + tag + "\n" + rest
}

// sameTmuxServer reports whether a pane found on the server with PID pid is
// on the claim's tmux server. Older claims have no server PID; their socket
// is asked instead.
func sameTmuxServer(c *Live, pid string, serverOf func(string) string) bool {
	if pid == "" {
		return false
	}
	if c.TmuxServer != "" {
		return pid == c.TmuxServer
	}
	return serverOf(c.TmuxSocket) == pid
}

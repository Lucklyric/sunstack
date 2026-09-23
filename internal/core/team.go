package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/assets"
)

// Init creates sunstack/ in dir and maintains the AGENTS.md routing block and
// the .gitignore line. Re-running only fills in what is missing (design §6).
func Init(dir string) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	var done []string
	agentsPath := filepath.Join(root, "AGENTS.md")
	agents, hasAgents, err := readMaybe(agentsPath)
	if err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	hadBlock := strings.Contains(string(agents), assets.RouteBegin)
	newAgents, changed, err := routeBlock(agents)
	if err != nil {
		return nil, err
	}
	ss := filepath.Join(root, "sunstack")
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"README.md", assets.Readme()},
		{"PROTOCOL.md", assets.Protocol()},
		{"PILLARS.md", []byte(assets.PillarsTemplate)},
	} {
		path := filepath.Join(ss, f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := writeAtomic(path, f.data); err != nil {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		done = append(done, "created sunstack/"+f.name)
	}
	if changed {
		if err := writeAtomic(agentsPath, newAgents); err != nil {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		switch {
		case hadBlock:
			done = append(done, "refreshed the sunstack block in AGENTS.md")
		case hasAgents:
			done = append(done, "added the sunstack block to AGENTS.md")
		default:
			done = append(done, "created AGENTS.md with the sunstack block")
		}
	}
	gi := filepath.Join(root, ".gitignore")
	b, _, _ := readMaybe(gi)
	if !hasLine(b, "sunstack/_local/") {
		if len(b) > 0 && b[len(b)-1] != '\n' {
			b = append(b, '\n')
		}
		b = append(b, "sunstack/_local/\n"...)
		if err := writeAtomic(gi, b); err != nil {
			return nil, fail(ExitFail, "fs", "%v", err)
		}
		done = append(done, "added sunstack/_local/ to .gitignore")
	}
	return done, nil
}

func hasLine(b []byte, line string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// routeBlock inserts or refreshes the routing block; a malformed block is an
// error and nothing is written.
func routeBlock(src []byte) ([]byte, bool, error) {
	s := string(src)
	nb, ne := strings.Count(s, assets.RouteBegin), strings.Count(s, assets.RouteEnd)
	switch {
	case nb == 0 && ne == 0:
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		if s != "" {
			s += "\n"
		}
		return []byte(s + assets.RoutingBlock), true, nil
	case nb == 1 && ne == 1 && strings.Index(s, assets.RouteBegin) < strings.Index(s, assets.RouteEnd):
		i := strings.Index(s, assets.RouteBegin)
		j := strings.Index(s, assets.RouteEnd) + len(assets.RouteEnd)
		if j < len(s) && s[j] == '\n' {
			j++
		}
		out := s[:i] + assets.RoutingBlock + s[j:]
		return []byte(out), out != s, nil
	}
	return nil, false, fail(ExitFail, "malformed_block", "AGENTS.md has an incomplete or repeated sunstack block (%d begin, %d end markers); fix it by hand, nothing was written", nb, ne)
}

// PersonalLibrary is ~/.sunstack/library, or $SUNSTACK_HOME/library.
func PersonalLibrary() string {
	home := os.Getenv("SUNSTACK_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".sunstack")
	}
	return filepath.Join(home, "library")
}

// Template finds a role template: personal library first, then built-in.
func Template(title string) ([]byte, string, bool) {
	if b, err := os.ReadFile(filepath.Join(PersonalLibrary(), title, "AGENT.md")); err == nil {
		return b, "personal", true
	}
	if b, ok := assets.Template(title); ok {
		return b, "built-in", true
	}
	return nil, "", false
}

// LibraryEntry is one available template.
type LibraryEntry struct{ Title, Source, Summary string }

// Library lists personal and built-in templates; personal ones shadow built-in.
func Library() []LibraryEntry {
	seen := map[string]bool{}
	var out []LibraryEntry
	entries, _ := os.ReadDir(PersonalLibrary())
	for _, e := range entries {
		if b, err := os.ReadFile(filepath.Join(PersonalLibrary(), e.Name(), "AGENT.md")); err == nil && e.IsDir() {
			out = append(out, LibraryEntry{e.Name(), "personal", Duty(b)})
			seen[e.Name()] = true
		}
	}
	for _, t := range assets.Titles() {
		if !seen[t] {
			b, _ := assets.Template(t)
			out = append(out, LibraryEntry{t, "built-in", Duty(b)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// Duty is the first line under "## 职责", for one-line summaries.
func Duty(agentMD []byte) string {
	lines := strings.Split(string(agentMD), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "## 职责" {
			for _, m := range lines[i+1:] {
				m = strings.TrimSpace(m)
				if strings.HasPrefix(m, "## ") {
					return ""
				}
				if m != "" {
					return strings.TrimPrefix(m, "- ")
				}
			}
		}
	}
	return ""
}

// setFrontmatter sets (or with val "" removes) keys in the leading --- block.
func setFrontmatter(doc []byte, kv [][2]string) ([]byte, error) {
	s := strings.ReplaceAll(string(doc), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, fail(ExitFail, "invalid_agent", "AGENT.md must start with a --- frontmatter block")
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return nil, fail(ExitFail, "invalid_agent", "AGENT.md frontmatter is not closed with ---")
	}
	head, body := s[4:4+end], s[4+end:]
	lines := strings.Split(head, "\n")
	for _, p := range kv {
		idx := -1
		for i, l := range lines {
			if k, _, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == p[0] {
				idx = i
			}
		}
		switch {
		case p[1] == "" && idx >= 0:
			lines = append(lines[:idx], lines[idx+1:]...)
		case p[1] != "" && idx >= 0:
			lines[idx] = p[0] + ": " + p[1]
		case p[1] != "":
			lines = append(lines, p[0]+": "+p[1])
		}
	}
	return []byte("---\n" + strings.Join(lines, "\n") + body), nil
}

// instances lists hired IDs of a title.
func (p *Project) instances(title string) []string {
	var out []string
	for _, id := range p.Agents() {
		if id == title || strings.HasPrefix(id, title+".") {
			out = append(out, id)
		}
	}
	return out
}

// HireOptions are the inputs of `sunstack hire`.
type HireOptions struct {
	Title, Name string
	File        string // an approved AGENT.md draft (recruit)
}

// Hire creates sunstack/<id>/ from a template or an approved draft.
func (p *Project) Hire(o HireOptions) (string, error) {
	if o.Title == "" {
		var b strings.Builder
		b.WriteString("missing_arguments: title\n")
		for _, e := range Library() {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", e.Title, e.Source, e.Summary)
		}
		return "", &Error{Code: ExitUsage, Reason: "missing_arguments", Msg: "choose a template, or use recruit for a new role", Stdout: b.String()}
	}
	if !ValidPart(o.Title) || o.Title == "review" {
		return "", fail(ExitUsage, "usage", "invalid title: %s", o.Title)
	}
	if o.Name != "" && !ValidPart(o.Name) {
		return "", fail(ExitUsage, "usage", "invalid name: %s", o.Name)
	}
	var doc []byte
	from := ""
	if o.File != "" {
		b, err := os.ReadFile(o.File)
		if err != nil {
			return "", fail(ExitFail, "no_candidate", "draft not found: %s", o.File)
		}
		if m := titleRe.FindSubmatch(b); m == nil || string(m[1]) != o.Title {
			return "", fail(ExitFail, "invalid_agent", "the draft's frontmatter must have title: %s", o.Title)
		}
		doc, from = b, "custom"
	} else {
		b, _, ok := Template(o.Title)
		if !ok {
			return "", fail(ExitFail, "not_found", "no template %s; see sunstack library, or create a new role with the recruit skill", o.Title)
		}
		doc = b
	}
	id := o.Title
	if o.Name != "" {
		id += "." + o.Name
	} else if len(p.instances(o.Title)) > 0 {
		return "", &Error{Code: ExitUsage, Reason: "missing_arguments", Msg: o.Title + " already has an instance; give this one a name",
			Stdout: "missing_arguments: name\n" + strings.Join(p.instances(o.Title), "\n") + "\n"}
	}
	if _, err := os.Stat(p.AgentDir(id)); err == nil {
		return "", fail(ExitFail, "exists", "%s already exists", id)
	}
	kv := [][2]string{{"name", o.Name}, {"hired", time.Now().Format("2006-01-02")}}
	if from != "" {
		kv = append(kv, [2]string{"from", from})
	}
	doc, err := setFrontmatter(doc, kv)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(filepath.Join(p.AgentDir(id), "AGENT.md"), doc); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if err := writeAtomic(filepath.Join(p.AgentDir(id), "context.md"), []byte(assets.ContextTemplate)); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	src := "template=" + o.Title
	if from == "custom" {
		src = "custom"
	}
	p.LogEvent("hire", id, src)
	return id, nil
}

// uncommitted lists git changes under a path, or nil outside git.
func (p *Project) uncommitted(rel string) []string {
	out, err := exec.Command("git", "-C", p.Root, "status", "--porcelain", "--", rel).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// Fire deletes an agent that nobody holds, after checking for uncommitted
// content and pending messages (design §6).
func (p *Project) Fire(id string, discard bool) error {
	if !ValidID(id) {
		return fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	if !p.HasAgent(id) {
		return fail(ExitFail, "not_found", "no agent %s", id)
	}
	unlock, err := p.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if cur := p.ReadLive(id); cur != nil {
		e := fail(ExitClaim, "occupied", "%s is claimed; release it (or take it over) before firing", id)
		e.Stdout = cur.ClaimLine() + "\n"
		return e
	}
	if !discard {
		var problems []string
		if u := p.uncommitted(filepath.Join("sunstack", id)); len(u) > 0 {
			problems = append(problems, fmt.Sprintf("%d uncommitted change(s) under sunstack/%s", len(u), id))
		}
		if n := len(listNames(p.local("inbox", id), ".md")); n > 0 {
			problems = append(problems, fmt.Sprintf("%d pending message(s)", n))
		}
		if len(problems) > 0 {
			return fail(ExitFail, "would_lose_work", "%s: %s; rerun with --discard to delete anyway", id, strings.Join(problems, ", "))
		}
	}
	if err := os.RemoveAll(p.AgentDir(id)); err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	os.RemoveAll(p.local("inbox", id))
	os.RemoveAll(p.local("tmp", id))
	p.LogEvent("fire", id)
	return nil
}

// LibrarySave stores an agent's AGENT.md as a personal template.
func (p *Project) LibrarySave(id, as string, force bool) (string, error) {
	if !p.HasAgent(id) {
		return "", fail(ExitFail, "not_found", "no agent %s", id)
	}
	title := strings.SplitN(id, ".", 2)[0]
	if as != "" {
		title = as
	}
	if !ValidPart(title) {
		return "", fail(ExitUsage, "usage", "invalid title: %s", title)
	}
	b, err := os.ReadFile(filepath.Join(p.AgentDir(id), "AGENT.md"))
	if err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if b, err = setFrontmatter(b, [][2]string{{"title", title}, {"name", ""}, {"hired", ""}, {"from", title + "@personal"}}); err != nil {
		return "", err
	}
	dest := filepath.Join(PersonalLibrary(), title, "AGENT.md")
	if _, err := os.Stat(dest); err == nil && !force {
		return "", fail(ExitFail, "exists", "personal template %s exists; rerun with --force to replace it", title)
	}
	if err := writeAtomic(dest, b); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	return dest, nil
}

// section returns the "- " lines under a "## name" heading.
func section(doc []byte, name string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(string(doc), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			in = t == "## "+name
			continue
		}
		if in && strings.HasPrefix(t, "- ") {
			out = append(out, t)
		}
	}
	return out
}

// pillarLines returns the "- " lines of a pillars file, skipping comments.
func pillarLines(doc []byte) []string {
	var out []string
	inComment := false
	for _, l := range strings.Split(string(doc), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "<!--") {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if inComment {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if strings.HasPrefix(t, "- ") {
			out = append(out, t)
		}
	}
	return out
}

// Pillars shows the effective pillars of id (team + its own), or the team's
// when id is TeamID, each labeled with its source.
func (p *Project) Pillars(id string) (string, error) {
	team, _, _ := readMaybe(filepath.Join(p.Dir, "PILLARS.md"))
	var b strings.Builder
	for _, l := range pillarLines(team) {
		fmt.Fprintf(&b, "[team] %s\n", strings.TrimPrefix(l, "- "))
	}
	if id != TeamID {
		if !p.HasAgent(id) {
			return "", fail(ExitFail, "not_found", "no agent %s (pillar matches exact IDs only)", id)
		}
		own, _, _ := readMaybe(filepath.Join(p.AgentDir(id), "pillars.md"))
		for _, l := range pillarLines(own) {
			fmt.Fprintf(&b, "[%s] %s\n", id, strings.TrimPrefix(l, "- "))
		}
	}
	if b.Len() == 0 {
		b.WriteString("(no pillars)\n")
	}
	return b.String(), nil
}

// AgentStatus is one row of the team view.
type AgentStatus struct {
	ID, Title, Duty string
	Claim           *Live
	Where           string // tmux session:window.pane, "pane closed", or ""
	Inbox           int
	Proposals       []string
	Threads         []string
	ContextLines    int
	Conflict        bool
}

// TmuxWhere resolves a pane ID to session:window.pane via tmux.
func TmuxWhere(pane string) string {
	if pane == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{session_name}:#{window_index}.#{pane_index}").Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return "pane closed"
	}
	return strings.TrimSpace(string(out))
}

// Status collects the team view (design §6 team).
func (p *Project) Status() []AgentStatus {
	var out []AgentStatus
	for _, id := range p.Agents() {
		agent, _ := os.ReadFile(filepath.Join(p.AgentDir(id), "AGENT.md"))
		ctx, _ := os.ReadFile(filepath.Join(p.AgentDir(id), "context.md"))
		s := AgentStatus{
			ID: id, Title: strings.SplitN(id, ".", 2)[0], Duty: Duty(agent),
			Claim:        p.ReadLive(id),
			Inbox:        len(listNames(p.local("inbox", id), ".md")),
			Proposals:    section(ctx, "提议"),
			ContextLines: bytes.Count(ctx, []byte("\n")),
			Conflict:     HasConflictMarkers(ctx) || HasConflictMarkers(agent),
		}
		for _, t := range listNames(filepath.Join(p.AgentDir(id), "threads"), ".md") {
			s.Threads = append(s.Threads, strings.TrimSuffix(t, ".md"))
		}
		if s.Claim != nil {
			s.Where = TmuxWhere(s.Claim.TmuxPane)
		}
		out = append(out, s)
	}
	return out
}

// TeamText renders Status for `sunstack team`.
func (p *Project) TeamText() string {
	st := p.Status()
	if len(st) == 0 {
		return "no agents hired yet; run sunstack hire <title> [name], or use the recruit skill\n"
	}
	var b strings.Builder
	title := ""
	for _, s := range st {
		if s.Title != title {
			title = s.Title
			fmt.Fprintf(&b, "%s\n", title)
		}
		state := "free"
		if c := s.Claim; c != nil {
			state = fmt.Sprintf("claimed by %s on %s", c.Tool, c.Host)
			if s.Where != "" {
				state += " at " + s.Where
			}
			state += ", last contact " + c.LastContact
		}
		fmt.Fprintf(&b, "  %-22s %s\n", s.ID, state)
		fmt.Fprintf(&b, "  %-22s %s\n", "", s.Duty)
		fmt.Fprintf(&b, "  %-22s inbox %d, proposals %d, threads %d\n", "", s.Inbox, len(s.Proposals), len(s.Threads))
	}
	return b.String()
}

// Events returns event log lines, optionally for one agent.
func (p *Project) Events(id string) []string {
	b, _ := os.ReadFile(p.local("log", "events.log"))
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l == "" {
			continue
		}
		if id != "" && !strings.Contains(" "+l+" ", " "+id+" ") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// EventsPath is the event log.
func (p *Project) EventsPath() string { return p.local("log", "events.log") }

// InboxEntry is one pending message.
type InboxEntry struct{ File, From, Type, At string }

// Inbox lists pending messages of id without changing anything.
func (p *Project) Inbox(id string) []InboxEntry {
	var out []InboxEntry
	for _, n := range listNames(p.local("inbox", id), ".md") {
		b, _ := os.ReadFile(p.local("inbox", id, n))
		e := InboxEntry{File: n}
		for _, l := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(strings.SplitN(v, "#", 2)[0])
			switch strings.TrimSpace(k) {
			case "from":
				e.From = v
			case "type":
				e.Type = v
			case "at":
				e.At = v
			}
		}
		out = append(out, e)
	}
	return out
}

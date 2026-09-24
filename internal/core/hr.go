package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HR gathers the facts a staffing review needs: who is on the team and how
// loaded they are, which objectives and needs nobody covers, who has been
// idle, which templates are unused, and what the project is made of. The hr
// skill turns these facts into hire, rename or fire suggestions.

const (
	busyNow  = 5  // more Now entries than this: overloaded
	idleDays = 14 // no board entry and no event for this long: idle
)

// HRReport is the output of `sunstack hr`.
type HRReport struct {
	Agents    []HRAgent
	Uncovered []string // objectives no agent works toward
	Gaps      []string // needs and directives that point at nobody
	Unused    []string // library templates with no agent
	Project   []string // project signals: languages, notable folders and files
}

// HRAgent is one agent's load.
type HRAgent struct {
	ID, Duty, LastActive string
	Now, Next, Sessions  int
	Pending              int
	Idle, Busy           bool
}

// HR builds the report.
func (p *Project) HR(today time.Time) *HRReport {
	r := &HRReport{}
	b := p.LoadBoards()
	status := p.Status()
	events := p.Events("")
	objWork := map[string]int{}
	titles := map[string]bool{}
	for _, s := range status {
		titles[s.Title] = true
		a := HRAgent{ID: s.ID, Duty: s.Duty, Sessions: len(s.Claims), Pending: s.Inbox}
		newest := ""
		for _, it := range b.Agents[s.ID] {
			switch it.Section {
			case "Now":
				a.Now++
			case "Next":
				a.Next++
			}
			if !it.Done && it.Obj != "" {
				objWork[it.Obj]++
			}
			if it.Date > newest {
				newest = it.Date
			}
		}
		for i := len(events) - 1; i >= 0; i-- {
			l := " " + events[i] + " "
			if strings.Contains(l, " "+s.ID+" ") || strings.Contains(l, " "+s.ID+"_") || strings.Contains(l, " to="+s.ID+" ") {
				if f := strings.Fields(events[i]); len(f) > 0 && len(f[0]) >= 10 && f[0][:10] > newest {
					newest = f[0][:10]
				}
				break
			}
		}
		a.LastActive = newest
		a.Busy = a.Now > busyNow
		if d, err := time.Parse("2006-01-02", newest); a.Now == 0 && a.Next == 0 && a.Sessions == 0 &&
			(err != nil || today.Sub(d) > idleDays*24*time.Hour) {
			a.Idle = true
		}
		r.Agents = append(r.Agents, a)
	}
	for _, it := range b.Team {
		switch {
		case it.Section == "Objectives" && objWork[it.Key] == 0:
			r.Uncovered = append(r.Uncovered, fmt.Sprintf("%s %s (no agent has a key result for it)", it.Key, it.Text))
		case it.Section == "Directives":
			for _, to := range it.To {
				if to != "all" && !p.HasAgent(to) && !titles[to] {
					r.Gaps = append(r.Gaps, fmt.Sprintf("directive %s is addressed to %s, who is not on the team", it.Key, to))
				}
			}
		}
	}
	for id, items := range b.Agents {
		for _, it := range items {
			if it.Done {
				continue
			}
			for _, n := range it.Needs {
				owner := strings.SplitN(n, "#", 2)[0]
				if owner != "user" && !p.HasAgent(owner) {
					r.Gaps = append(r.Gaps, fmt.Sprintf("%s#%s needs %s, who is not on the team", id, it.Key, owner))
				}
			}
		}
	}
	sort.Strings(r.Gaps)
	for _, e := range Library() {
		if !titles[e.Title] {
			r.Unused = append(r.Unused, fmt.Sprintf("%s (%s): %s", e.Title, e.Source, e.Summary))
		}
	}
	r.Project = projectSignals(p.Root)
	return r
}

// projectSignals summarizes what the project is made of: the most common
// file types and notable folders and files. It looks at most a few thousand
// files and skips dependency, build and hidden folders.
func projectSignals(root string) []string {
	skip := map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
		"sunstack": true, "__pycache__": true, ".venv": true, "venv": true}
	notable := map[string]string{"Dockerfile": "Dockerfile", "go.mod": "Go module", "package.json": "Node package",
		"pyproject.toml": "Python project", "Cargo.toml": "Rust crate", "Makefile": "Makefile",
		".github": "GitHub workflows", "docs": "docs/", "tests": "tests/", "test": "test/", "notebooks": "notebooks/",
		"data": "data/", "infra": "infra/", "terraform": "Terraform", "k8s": "Kubernetes", "migrations": "migrations/",
		"paper": "paper/", "slides": "slides/"}
	exts := map[string]int{}
	var found []string
	seen := map[string]bool{}
	files := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || files > 5000 {
			return filepath.SkipDir
		}
		name := d.Name()
		rel, _ := filepath.Rel(root, path)
		depth := strings.Count(rel, string(filepath.Separator))
		if label, ok := notable[name]; ok && depth <= 1 && !seen[label] {
			seen[label] = true
			found = append(found, label)
		}
		if d.IsDir() {
			if path != root && (skip[name] || (strings.HasPrefix(name, ".") && name != ".github") || depth > 4) {
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" && len(ext) <= 6 {
			exts[ext]++
		}
		return nil
	})
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range exts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v || list[i].v == list[j].v && list[i].k < list[j].k })
	var top []string
	for i := 0; i < len(list) && i < 8; i++ {
		top = append(top, fmt.Sprintf("%s %d", list[i].k, list[i].v))
	}
	out := []string{fmt.Sprintf("%d files looked at; most common: %s", files, strings.Join(top, ", "))}
	if len(found) > 0 {
		sort.Strings(found)
		out = append(out, "notable: "+strings.Join(found, ", "))
	}
	if b, err := os.ReadFile(filepath.Join(root, "README.md")); err == nil {
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		i := 0
		if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" { // skip frontmatter
			for i = 1; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
			}
			i++
		}
		for ; i < len(lines); i++ {
			if t := strings.TrimSpace(strings.TrimLeft(lines[i], "# ")); t != "" && !strings.HasPrefix(t, "<!--") {
				out = append(out, "README starts: "+t)
				break
			}
		}
	}
	return out
}

// Text renders the report.
func (r *HRReport) Text() string {
	var b strings.Builder
	b.WriteString("Team\n")
	if len(r.Agents) == 0 {
		b.WriteString("  (nobody hired yet)\n")
	}
	for _, a := range r.Agents {
		flag := ""
		switch {
		case a.Busy:
			flag = "  OVERLOADED"
		case a.Idle:
			flag = "  IDLE"
		}
		last := a.LastActive
		if last == "" {
			last = "never"
		}
		fmt.Fprintf(&b, "  %-24s now %d, next %d, sessions %d, messages %d, last active %s%s\n", a.ID, a.Now, a.Next, a.Sessions, a.Pending, last, flag)
		if a.Duty != "" {
			fmt.Fprintf(&b, "  %-24s %s\n", "", a.Duty)
		}
	}
	section := func(title, empty string, lines []string) {
		fmt.Fprintf(&b, "\n%s\n", title)
		if len(lines) == 0 {
			fmt.Fprintf(&b, "  %s\n", empty)
		}
		for _, l := range lines {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}
	section("Objectives nobody works toward", "none (or no objectives set yet; see sunstack board)", r.Uncovered)
	section("Needs and directives that point at nobody", "none", r.Gaps)
	section("Templates not on the team", "none", r.Unused)
	section("Project", "", r.Project)
	return b.String()
}

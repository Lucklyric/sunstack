package tui

// The Org tree by folder (§40.6): each host, then a tree of the folders its
// sessions run in, then the sessions. A chain of folders with one child each
// shows as one row.

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
	"github.com/Lucklyric/sunstack/internal/hub"
)

// homeRe finds a home folder on macOS, Linux or Windows.
var homeRe = regexp.MustCompile(`^(/Users/[^/]+|/home/[^/]+|/root|[A-Za-z]:/Users/[^/]+)`)

// tildePath is a folder from the home folder, with forward slashes.
func tildePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if loc := homeRe.FindStringIndex(p); loc != nil {
		return "~" + p[loc[1]:]
	}
	return p
}

// sessionFolder is the folder a session runs in, as far as the scan knows.
func sessionFolder(s *core.HostSession, g *core.FreeGroup) string {
	switch {
	case s.Cwd != "":
		return s.Cwd
	case s.TeamRoot != "":
		return s.TeamRoot
	case g != nil && g.Kind == "folder":
		return g.Group
	}
	return "(unknown folder)"
}

type folderNode struct {
	kids  map[string]*folderNode
	sess  []*core.HostSession
	teams []string // the teams rooted here
}

func (f *folderNode) child(name string) *folderNode {
	if f.kids == nil {
		f.kids = map[string]*folderNode{}
	}
	c := f.kids[name]
	if c == nil {
		c = &folderNode{}
		f.kids[name] = c
	}
	return c
}

// at is the node for p. An absolute path keeps its leading / on its first
// part, so /opt/work and opt/work stay apart.
func (f *folderNode) at(p string) *folderNode {
	n := f
	abs := strings.HasPrefix(p, "/")
	for _, part := range strings.Split(strings.Trim(p, "/"), "/") {
		if part != "" {
			if abs {
				part, abs = "/"+part, false
			}
			n = n.child(part)
		}
	}
	return n
}

// count is how many sessions sit under f.
func (f *folderNode) count() int {
	n := len(f.sess)
	for _, c := range f.kids {
		n += c.count()
	}
	return n
}

// folderRows are the Org tree in folder order.
func (m *model) folderRows() []treeNode {
	if m.hosts == nil {
		return m.folderTree(m.org, nil, "folder:", 0)
	}
	var out []treeNode
	for _, h := range m.hostOrder() {
		o, hv := h.Org, h
		if h.You {
			o, hv = m.org, nil
		}
		var rows []treeNode
		if o != nil {
			rows = m.folderTree(o, hv, "host:"+h.ID+"/folder:", 1)
		}
		out = m.hostHeading(out, h, rows)
	}
	return out
}

// folderTree is one snapshot's sessions by folder, from depth d.
func (m *model) folderTree(o *core.Org, h *hub.HostView, prefix string, d int) []treeNode {
	root := &folderNode{}
	teamRoots := map[string]string{}
	for _, t := range o.Teams {
		teamRoots[t.Name] = t.Root
	}
	add := func(s *core.HostSession, g *core.FreeGroup, team, agent string) {
		if m.keep(s, team, agent) {
			dir := sessionFolder(s, g)
			if dir == "(unknown folder)" && team != "" {
				dir = teamRoots[team]
			}
			n := root.at(tildePath(dir))
			n.sess = append(n.sess, s)
		}
	}
	for _, t := range o.Teams {
		r := root.at(tildePath(t.Root))
		r.teams = append(r.teams, t.Name)
		for _, a := range t.Agents {
			for _, s := range a.Sessions {
				add(s, nil, t.Name, a.ID)
			}
		}
		for _, s := range t.Free {
			add(s, nil, t.Name, "")
		}
	}
	for _, g := range o.Free {
		for _, s := range g.Sessions {
			add(s, g, "", "")
		}
	}
	var out []treeNode
	var walk func(f *folderNode, name, full string, d int)
	walk = func(f *folderNode, name, full string, d int) {
		// One child and nothing here: show the chain as one row, with the
		// teams rooted anywhere along it.
		teams := append([]string{}, f.teams...)
		for len(f.sess) == 0 && len(f.kids) == 1 {
			for k, c := range f.kids {
				name, full, f = path.Join(name, k), path.Join(full, k), c
			}
			teams = append(teams, f.teams...)
		}
		if f.count() == 0 {
			return
		}
		key := prefix + full
		n := treeNode{kind: "folder", key: key, depth: d, label: name, fold: true, org: o, host: h, need: f.count()}
		if len(teams) > 0 {
			n.label += "  (" + strings.Join(teams, ", ") + ")"
		}
		out = append(out, n)
		if m.folded[key] {
			return
		}
		for _, s := range f.sess {
			out = append(out, treeNode{kind: "session", key: key + "/" + sessionName(s), depth: d + 1, label: m.sessLabel(s, h), sess: s, org: o, host: h})
		}
		names := make([]string, 0, len(f.kids))
		for k := range f.kids {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			walk(f.kids[k], k, path.Join(full, k), d+1)
		}
	}
	top := make([]string, 0, len(root.kids))
	for k := range root.kids {
		top = append(top, k)
	}
	sort.Strings(top)
	for _, k := range top {
		walk(root.kids[k], k, k, d)
	}
	return out
}

// folderDetails is the right pane for a folder row.
func (m *model) folderDetails(n treeNode, w int) []string {
	full := n.key[strings.LastIndex(n.key, "folder:")+len("folder:"):]
	return []string{wrap(cHeader.Render(full), w), "", fmt.Sprintf("%s under this folder", plural(n.need, "session", "sessions"))}
}

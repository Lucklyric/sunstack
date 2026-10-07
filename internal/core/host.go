package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The host layer (org design, v0.8): ~/.sunstack/ holds this host's ID and
// an index of the teams on it. The index only says where teams are; their
// files are always read live from each project.

// Home is ~/.sunstack, or $SUNSTACK_HOME.
func Home() string {
	if h := os.Getenv("SUNSTACK_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".sunstack")
}

// HostInfo is this machine's identity.
type HostInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ThisHost reads ~/.sunstack/host.json, creating it on first use. The name
// follows the current short host name.
func ThisHost() HostInfo {
	name, _ := os.Hostname()
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	// SUNSTACK_HOST_NAME names this host in the org when its host name is
	// not the name to show (or several test hosts share one machine).
	if n := os.Getenv("SUNSTACK_HOST_NAME"); n != "" {
		name = n
	}
	path := filepath.Join(Home(), "host.json")
	var h HostInfo
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &h) == nil && h.ID != "" {
		h.Name = name
		return h
	}
	h = HostInfo{ID: NewToken(), Name: name}
	if b, err := json.MarshalIndent(h, "", "  "); err == nil {
		_ = writeAtomic(path, append(b, '\n'))
	}
	return h
}

// TeamFile is sunstack/TEAM: the team's ID and name, committed with the team
// so every host recognizes it, whatever the path.
type TeamFile struct {
	ID, Name    string
	MaxSessions int // live sessions per team on a host before spawn refuses (§16.7)
}

// DefaultMaxSessions is the session cap when TEAM sets none.
const DefaultMaxSessions = 6

func (p *Project) teamFilePath() string { return filepath.Join(p.Dir, "TEAM") }

// Team reads sunstack/TEAM. ok is false for a team made before v0.8.
func (p *Project) Team() (TeamFile, bool) {
	t := TeamFile{Name: filepath.Base(p.Root), MaxSessions: DefaultMaxSessions}
	b, err := os.ReadFile(p.teamFilePath())
	if err != nil {
		return t, false
	}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "id":
			t.ID = strings.TrimSpace(v)
		case "name":
			if v = strings.TrimSpace(v); v != "" {
				t.Name = v
			}
		case "max_sessions":
			t.MaxSessions, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return t, t.ID != ""
}

// ensureTeamFile writes sunstack/TEAM if it is missing. An existing file is
// never changed.
func ensureTeamFile(dir string) (bool, error) {
	path := filepath.Join(dir, "TEAM")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	root := filepath.Dir(dir)
	body := fmt.Sprintf("# Sunstack team identity: the same on every host. Keep it in git; do not edit the id.\nid: %s\nname: %s\n", teamIDFor(root), filepath.Base(root))
	return true, writeAtomic(path, []byte(body))
}

// teamIDFor derives a team's ID from its repository's origin URL and the
// team's folder inside the repository, so two hosts that add TEAM before
// syncing still agree. Without a git remote it is random.
func teamIDFor(root string) string {
	top, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	remote := gitRemote(root)
	if err != nil || remote == "" {
		return NewToken()
	}
	rel, err := filepath.Rel(mustEval(strings.TrimSpace(string(top))), mustEval(root))
	if err != nil {
		return NewToken()
	}
	sum := sha256.Sum256([]byte(normalizeRemote(remote) + "\n" + filepath.ToSlash(rel)))
	return hex.EncodeToString(sum[:8])
}

// normalizeRemote makes the SSH and HTTPS forms of one repository equal:
// git@github.com:a/b.git and https://github.com/a/b both become github.com/a/b.
func normalizeRemote(r string) string {
	r = strings.TrimSpace(r)
	if i := strings.Index(r, "://"); i >= 0 {
		r = r[i+3:]
		if j := strings.IndexByte(r, '@'); j >= 0 && j < strings.IndexByte(r+"/", '/') {
			r = r[j+1:]
		}
	} else if i := strings.IndexByte(r, '@'); i >= 0 {
		r = strings.Replace(r[i+1:], ":", "/", 1)
	}
	r = strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
	if i := strings.IndexByte(r, '/'); i > 0 {
		host := strings.ToLower(r[:i])
		r = host + r[i:]
		// These hosts ignore case in owner and repository names.
		if host == "github.com" || host == "gitlab.com" || host == "bitbucket.org" {
			r = strings.ToLower(r)
		}
	}
	return r
}

// TeamEntry is one team in this host's index.
type TeamEntry struct {
	ID        string `json:"id,omitempty"` // empty for a team without sunstack/TEAM
	Name      string `json:"name"`
	Root      string `json:"root"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

func indexPath() string { return filepath.Join(Home(), "teams.json") }

func readIndex() []TeamEntry {
	var list []TeamEntry
	if b, err := os.ReadFile(indexPath()); err == nil {
		_ = json.Unmarshal(b, &list)
	}
	return list
}

// updateIndex changes the index under ~/.sunstack/locks/index.
func updateIndex(f func([]TeamEntry) []TeamEntry) error {
	locks := filepath.Join(Home(), "locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		return err
	}
	unlock, err := lockDir(filepath.Join(locks, "index"), "the team index")
	if err != nil {
		return err
	}
	defer unlock()
	list := f(readIndex())
	sort.Slice(list, func(i, j int) bool { return list[i].Root < list[j].Root })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(indexPath(), append(b, '\n'))
}

// Register adds or refreshes this team in the host index. Best effort:
// callers ignore the error, so a read-only home never fails a command.
func (p *Project) Register() error {
	t, _ := p.Team()
	return updateIndex(func(list []TeamEntry) []TeamEntry {
		for i := range list {
			if list[i].Root == p.Root {
				list[i].ID, list[i].Name, list[i].LastSeen = t.ID, t.Name, now()
				return list
			}
		}
		return append(list, TeamEntry{ID: t.ID, Name: t.Name, Root: p.Root, FirstSeen: now(), LastSeen: now()})
	})
}

// Teams lists the indexed teams.
func Teams() []TeamEntry { return readIndex() }

// IndexedProjects opens every indexed team that still exists.
func IndexedProjects() []*Project {
	var out []*Project
	for _, e := range readIndex() {
		// Through FindProject, so a symlinked sunstack/ is refused here too,
		// and only an entry that is itself the team root counts.
		if p, err := FindProject(e.Root); err == nil && p.Root == mustEval(e.Root) {
			out = append(out, p)
		}
	}
	return out
}

// PruneTeams drops index entries whose team is gone.
func PruneTeams() ([]string, error) {
	var gone []string
	err := updateIndex(func(list []TeamEntry) []TeamEntry {
		var keep []TeamEntry
		for _, e := range list {
			if _, err := os.Stat(filepath.Join(e.Root, "sunstack", "PROTOCOL.md")); err != nil {
				gone = append(gone, e.Root)
				continue
			}
			keep = append(keep, e)
		}
		return keep
	})
	return gone, err
}

// ScanTeams finds teams under dir (skipping hidden, dependency and build
// folders, at most 8 levels deep) and registers them.
func ScanTeams(dir string) ([]string, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true, "__pycache__": true, "venv": true}
	var found []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		rel, _ := filepath.Rel(root, path)
		if path != root && (strings.HasPrefix(name, ".") || skip[name] || strings.Count(rel, string(filepath.Separator)) >= 8) {
			return filepath.SkipDir
		}
		if name == "sunstack" {
			if _, err := os.Stat(filepath.Join(path, "PROTOCOL.md")); err == nil {
				found = append(found, filepath.Dir(path))
			}
			return filepath.SkipDir
		}
		return nil
	})
	for _, r := range found {
		if p, err := FindProject(r); err == nil && p.Root == mustEval(r) {
			_ = p.Register()
		}
	}
	return found, nil
}

func mustEval(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// realPath resolves symlinks in the longest existing part of path, so a
// session folder compares equal to a team root (on macOS /var is /private/var).
func realPath(path string) string {
	if path == "" {
		return ""
	}
	rest := ""
	for d := filepath.Clean(path); ; d = filepath.Dir(d) {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			return filepath.Join(r, rest)
		}
		if filepath.Dir(d) == d {
			return path
		}
		rest = filepath.Join(filepath.Base(d), rest)
	}
}

// TeamOf finds the indexed team whose root contains path (the deepest one).
func TeamOf(path string, teams []*Project) *Project {
	path = realPath(path)
	var best *Project
	for _, p := range teams {
		if path == p.Root || strings.HasPrefix(path, p.Root+string(filepath.Separator)) {
			if best == nil || len(p.Root) > len(best.Root) {
				best = p
			}
		}
	}
	return best
}

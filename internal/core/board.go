package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/assets"
)

// Boards: the team's sunstack/BOARD.md (objectives, the user's own key
// results, directives) and each agent's <id>/board.md (its key results in
// Now / Next / Done). Every entry starts with the date it was last updated,
// so staleness is visible and old entries can be archived by date.

const (
	staleDays    = 7  // a Now entry not updated for this long is stale
	tidyDays     = 7  // Done entries older than this move to the archive
	directiveAge = 30 // aligned directives older than this move to the archive
	boardLines   = 40 // a board longer than this should be tidied
)

// Item is one dated board entry.
type Item struct {
	Owner   string // agent ID, or "user" for the team board
	Key     string // O1, KR1, D1
	Date    string // YYYY-MM-DD: last updated, or done
	Section string // Objectives, User, Directives, Now, Next, Done
	Obj     string // the objective a key result serves
	Text    string
	Due     string
	Needs   []string // <id>#KR<n> or user#KR<n>
	To      []string // directive addressees, or "all"
	Done    bool
}

// Ref names a key result across the team: <owner>#<key>.
func (it Item) Ref() string { return it.Owner + "#" + it.Key }

var (
	itemRe    = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2}) (O\d+|KR\d+|D\d+)\b\s*(?:\[(O\d+)\])?\s*(.*)$`)
	attrRe    = regexp.MustCompile(`\((due|needs|to):\s*([^)]*)\)`)
	alignedRe = regexp.MustCompile(`(?m)^aligned:\s*D(\d+)\s*$`)
)

// parseBoard reads the dated entries of a board. Lines that start with "- "
// but carry no date and key are returned as undated.
func parseBoard(owner string, doc []byte) (items []Item, undated []string) {
	sec := ""
	inComment := false
	for _, l := range strings.Split(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "<!--") {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if inComment {
			inComment = !strings.Contains(t, "-->")
			continue
		}
		if strings.HasPrefix(t, "## ") {
			sec = strings.TrimPrefix(t, "## ")
			continue
		}
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		m := itemRe.FindStringSubmatch(t)
		if m == nil {
			undated = append(undated, t)
			continue
		}
		it := Item{Owner: owner, Date: m[1], Key: m[2], Obj: m[3], Section: sec}
		rest := m[4]
		for _, a := range attrRe.FindAllStringSubmatch(rest, -1) {
			vals := splitList(a[2])
			switch a[1] {
			case "due":
				it.Due = strings.TrimSpace(a[2])
			case "needs":
				it.Needs = vals
			case "to":
				it.To = vals
			}
		}
		if strings.Contains(rest, "(done)") {
			it.Done = true
		}
		it.Text = strings.TrimSpace(attrRe.ReplaceAllString(strings.ReplaceAll(rest, "(done)", ""), ""))
		if sec == "Done" {
			it.Done = true
		}
		items = append(items, it)
	}
	return items, undated
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// aligned is the number of the last directive an agent board has aligned with.
func aligned(doc []byte) int {
	if m := alignedRe.FindSubmatch(doc); m != nil {
		n, _ := strconv.Atoi(string(m[1]))
		return n
	}
	return 0
}

func keyNum(key string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(key, "OKRD"))
	return n
}

func (p *Project) teamBoardPath() string      { return filepath.Join(p.Dir, "BOARD.md") }
func (p *Project) boardPath(id string) string { return filepath.Join(p.AgentDir(id), "board.md") }
func (p *Project) archiveDir(owner string) string {
	if owner == TeamID {
		return filepath.Join(p.Dir, "archive")
	}
	return filepath.Join(p.AgentDir(owner), "archive")
}

// Boards is the whole team's board state.
type Boards struct {
	Team    []Item
	Agents  map[string][]Item
	Aligned map[string]int
	Lines   map[string]int // board.md line counts
	Missing []string       // agents without a board.md
	Undated map[string][]string
}

// LoadBoards reads the team board and every agent's board.
func (p *Project) LoadBoards() *Boards {
	b := &Boards{Agents: map[string][]Item{}, Aligned: map[string]int{}, Lines: map[string]int{}, Undated: map[string][]string{}}
	team, _, _ := readMaybe(p.teamBoardPath())
	var und []string
	b.Team, und = parseBoard("user", team)
	if len(und) > 0 {
		b.Undated["BOARD.md"] = und
	}
	for _, id := range p.Agents() {
		doc, ok, _ := readMaybe(p.boardPath(id))
		if !ok {
			b.Missing = append(b.Missing, id)
			continue
		}
		b.Agents[id], und = parseBoard(id, doc)
		if len(und) > 0 {
			b.Undated[id+"/board.md"] = und
		}
		b.Aligned[id] = aligned(doc)
		b.Lines[id] = strings.Count(string(doc), "\n")
	}
	return b
}

// Unaligned lists directives addressed to id that its board has not aligned with.
func (b *Boards) Unaligned(id string) []Item {
	var out []Item
	for _, it := range b.Team {
		if it.Section != "Directives" || keyNum(it.Key) <= b.Aligned[id] {
			continue
		}
		for _, to := range it.To {
			if to == "all" || to == id || to == strings.SplitN(id, ".", 2)[0] {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// Issues finds what needs attention: stale, overdue, undated or unaligned
// entries, key results without a known objective, and broken dependencies.
func (b *Boards) Issues(today time.Time) []string {
	var out []string
	objs := map[string]bool{}
	byRef := map[string]Item{}
	for _, it := range b.Team {
		switch it.Section {
		case "Objectives":
			objs[it.Key] = true
		case "User":
			byRef[it.Ref()] = it
		}
	}
	ids := make([]string, 0, len(b.Agents))
	for id, items := range b.Agents {
		ids = append(ids, id)
		for _, it := range items {
			byRef[it.Ref()] = it
		}
	}
	sort.Strings(ids)
	check := func(it Item) {
		if it.Done {
			return
		}
		switch {
		case it.Obj == "":
			out = append(out, fmt.Sprintf("%s serves no objective; tag it [O<n>]", it.Ref()))
		case !objs[it.Obj]:
			out = append(out, fmt.Sprintf("%s serves %s, which is not an objective in BOARD.md", it.Ref(), it.Obj))
		}
		for _, n := range it.Needs {
			if n == "user" {
				continue
			}
			dep, ok := byRef[n]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("%s needs %s, which does not exist", it.Ref(), n))
			case !dep.Done:
				out = append(out, fmt.Sprintf("%s is waiting on %s (%s)", it.Ref(), n, strings.ToLower(dep.Section)))
			}
		}
		if _, err := time.Parse("2006-01-02", it.Due); err == nil && it.Due < today.Format("2006-01-02") {
			out = append(out, fmt.Sprintf("%s was due %s", it.Ref(), it.Due))
		}
		if d, err := time.Parse("2006-01-02", it.Date); err == nil && it.Section == "Now" && today.Sub(d) > staleDays*24*time.Hour {
			out = append(out, fmt.Sprintf("%s has not been updated since %s", it.Ref(), it.Date))
		}
	}
	for _, it := range b.Team {
		if it.Section == "User" {
			check(it)
		}
	}
	for _, id := range ids {
		for _, it := range b.Agents[id] {
			check(it)
		}
		for _, d := range b.Unaligned(id) {
			out = append(out, fmt.Sprintf("%s has not aligned with %s", id, d.Key))
		}
		if b.Lines[id] > boardLines {
			out = append(out, fmt.Sprintf("%s/board.md has %d lines; tidy it", id, b.Lines[id]))
		}
	}
	files := make([]string, 0, len(b.Undated))
	for f := range b.Undated {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		out = append(out, fmt.Sprintf("%s has %d undated entr(ies); start each with - <date> <key>", f, len(b.Undated[f])))
	}
	return out
}

// BoardText renders the team view, or one agent's board with its issues.
func (p *Project) BoardText(id string) (string, error) {
	b := p.LoadBoards()
	today := time.Now()
	var s strings.Builder
	if id != "" {
		if !p.HasAgent(id) {
			return "", fail(ExitFail, "not_found", "no agent %s", id)
		}
		doc, ok, _ := readMaybe(p.boardPath(id))
		if !ok {
			return "", fail(ExitFail, "not_found", "%s has no board.md yet; run sunstack tidy %s to create it", id, id)
		}
		s.Write(doc)
		for _, d := range b.Unaligned(id) {
			fmt.Fprintf(&s, "\nnot aligned: %s %s %s", d.Key, d.Date, d.Text)
		}
		if len(b.Unaligned(id)) > 0 {
			s.WriteString("\n")
		}
		return s.String(), nil
	}
	byObj := map[string][]Item{}
	var order []string
	for _, it := range b.Team {
		if it.Section == "Objectives" {
			order = append(order, it.Key)
		}
	}
	add := func(it Item) {
		if it.Done || it.Section == "Objectives" || it.Section == "Directives" {
			return
		}
		byObj[it.Obj] = append(byObj[it.Obj], it)
	}
	for _, it := range b.Team {
		add(it)
	}
	ids := make([]string, 0, len(b.Agents))
	for id := range b.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, it := range b.Agents[id] {
			add(it)
		}
	}
	line := func(it Item) {
		fmt.Fprintf(&s, "    %-26s %-4s %s %s", it.Ref(), strings.ToLower(it.Section), it.Date, it.Text)
		if it.Due != "" {
			fmt.Fprintf(&s, " (due %s)", it.Due)
		}
		if len(it.Needs) > 0 {
			fmt.Fprintf(&s, " (needs %s)", strings.Join(it.Needs, ", "))
		}
		s.WriteString("\n")
	}
	s.WriteString("Objectives\n")
	if len(order) == 0 {
		s.WriteString("  (none yet; the user sets them in BOARD.md)\n")
	}
	for _, it := range b.Team {
		if it.Section != "Objectives" {
			continue
		}
		fmt.Fprintf(&s, "  %s %s %s", it.Key, it.Date, it.Text)
		if it.Due != "" {
			fmt.Fprintf(&s, " (due %s)", it.Due)
		}
		s.WriteString("\n")
		for _, kr := range byObj[it.Key] {
			line(kr)
		}
		delete(byObj, it.Key)
	}
	var rest []string
	for k := range byObj {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		if k == "" {
			s.WriteString("  (no objective)\n")
		} else {
			fmt.Fprintf(&s, "  %s (not in BOARD.md)\n", k)
		}
		for _, kr := range byObj[k] {
			line(kr)
		}
	}
	s.WriteString("\nDirectives\n")
	n := 0
	for _, it := range b.Team {
		if it.Section != "Directives" {
			continue
		}
		n++
		var pending []string
		for _, id := range ids {
			for _, d := range b.Unaligned(id) {
				if d.Key == it.Key {
					pending = append(pending, id)
				}
			}
		}
		fmt.Fprintf(&s, "  %s %s %s (to %s)", it.Key, it.Date, it.Text, strings.Join(it.To, ", "))
		if len(pending) > 0 {
			fmt.Fprintf(&s, " not aligned yet: %s", strings.Join(pending, ", "))
		}
		s.WriteString("\n")
	}
	if n == 0 {
		s.WriteString("  (none)\n")
	}
	if len(b.Missing) > 0 {
		fmt.Fprintf(&s, "\nNo board yet: %s (sunstack tidy <id> creates one)\n", strings.Join(b.Missing, ", "))
	}
	if is := b.Issues(today); len(is) > 0 {
		s.WriteString("\nNeeds attention\n")
		for _, i := range is {
			s.WriteString("  " + i + "\n")
		}
	}
	return s.String(), nil
}

// Direct appends a dated directive from the user to BOARD.md. to lists agent
// IDs or titles; empty means all.
func (p *Project) Direct(text string, to []string) (string, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", fail(ExitUsage, "missing_arguments", "the directive text is empty")
	}
	if strings.ContainsAny(text, "()") {
		return "", fail(ExitUsage, "usage", "keep parentheses out of the directive text; they mark fields")
	}
	if len(to) == 0 {
		to = []string{"all"}
	}
	for _, t := range to {
		if t != "all" && !p.HasAgent(t) && len(p.instances(t)) == 0 {
			return "", fail(ExitFail, "not_found", "no agent or title %s", t)
		}
	}
	unlock, err := p.lock(TeamID)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := p.noSymlink(p.teamBoardPath()); err != nil {
		return "", err
	}
	doc, ok, err := readMaybe(p.teamBoardPath())
	if err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if !ok {
		doc = []byte(assets.TeamBoardTemplate)
	}
	if HasConflictMarkers(doc) {
		return "", fail(ExitFail, "conflict", "merge conflict markers in BOARD.md; resolve them first")
	}
	items, _ := parseBoard("user", doc)
	next := 1
	for _, it := range items {
		if it.Section == "Directives" && keyNum(it.Key) >= next {
			next = keyNum(it.Key) + 1
		}
	}
	for _, n := range archivedDirectives(p.archiveDir(TeamID)) {
		if n >= next {
			next = n + 1
		}
	}
	key := fmt.Sprintf("D%d", next)
	entry := fmt.Sprintf("- %s %s %s (to: %s)", today(), key, text, strings.Join(to, ", "))
	doc = appendToSection(doc, "Directives", entry)
	if err := writeAtomic(p.teamBoardPath(), doc); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	p.LogEvent("direct", "team", key, "to="+strings.Join(to, ","))
	return key, nil
}

// archivedDirectives returns directive numbers already moved to the archive,
// so numbering never repeats.
func archivedDirectives(dir string) []int {
	var out []int
	for _, n := range listNames(dir, ".md") {
		doc, _ := os.ReadFile(filepath.Join(dir, n))
		items, _ := parseBoard("user", doc)
		for _, it := range items {
			if strings.HasPrefix(it.Key, "D") {
				out = append(out, keyNum(it.Key))
			}
		}
	}
	return out
}

func today() string { return time.Now().Format("2006-01-02") }

// appendToSection adds line at the end of "## name", creating the section at
// the end of doc if it is missing.
func appendToSection(doc []byte, name, line string) []byte {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## "+name {
			start = i
			break
		}
	}
	if start < 0 {
		lines = append(lines, "## "+name, line)
		return []byte(strings.Join(lines, "\n") + "\n")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	// Insert after the section's last non-blank line.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	return []byte(strings.Join(lines, "\n") + "\n")
}

// Tidy moves old finished entries to the archive (agent: Done entries older
// than tidyDays; team: done user key results older than tidyDays, and
// directives older than directiveAge that every addressee has aligned with).
// It creates a missing board. The archive is never loaded by as; it is kept
// for reference, one file per month.
func (p *Project) Tidy(owner string) (int, error) {
	path := p.teamBoardPath()
	tmpl := assets.TeamBoardTemplate
	if owner != TeamID {
		if !ValidID(owner) || !p.HasAgent(owner) {
			return 0, fail(ExitFail, "not_found", "no agent %s", owner)
		}
		path, tmpl = p.boardPath(owner), assets.BoardTemplate
	}
	var boards *Boards
	if owner == TeamID {
		boards = p.LoadBoards() // read before locking the team; agent boards change independently
	}
	unlock, err := p.lock(owner)
	if err != nil {
		return 0, err
	}
	defer unlock()
	if err := p.noSymlink(path); err != nil {
		return 0, err
	}
	doc, ok, err := readMaybe(path)
	if err != nil {
		return 0, fail(ExitFail, "fs", "%v", err)
	}
	if !ok {
		if err := writeAtomic(path, []byte(tmpl)); err != nil {
			return 0, fail(ExitFail, "fs", "%v", err)
		}
		p.LogEvent("tidy", owner, "created board")
		return 0, nil
	}
	if HasConflictMarkers(doc) {
		return 0, fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", filepath.Base(path))
	}
	now := time.Now()
	old := func(date string, days int) bool {
		d, err := time.Parse("2006-01-02", date)
		return err == nil && now.Sub(d) > time.Duration(days)*24*time.Hour
	}
	moved := map[string][]string{} // archive month -> lines
	var keep []string
	sec := ""
	n := 0
	for _, l := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n"), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			sec = strings.TrimPrefix(t, "## ")
		}
		items, _ := parseBoard(owner, []byte("## "+sec+"\n"+t))
		if len(items) == 1 {
			it := items[0]
			archive := false
			switch {
			case owner != TeamID && sec == "Done":
				archive = old(it.Date, tidyDays)
			case owner == TeamID && sec == "User" && it.Done:
				archive = old(it.Date, tidyDays)
			case owner == TeamID && sec == "Directives" && old(it.Date, directiveAge):
				archive = true
				for id := range boards.Agents {
					for _, d := range boards.Unaligned(id) {
						if d.Key == it.Key {
							archive = false
						}
					}
				}
			}
			if archive {
				month := it.Date[:7]
				moved[month] = append(moved[month], t)
				n++
				continue
			}
		}
		keep = append(keep, l)
	}
	if n == 0 {
		return 0, nil
	}
	// Archive first: a crash in between leaves an entry in both, never in neither.
	for month, lines := range moved {
		ap := filepath.Join(p.archiveDir(owner), month+".md")
		if err := p.noSymlink(ap); err != nil {
			return 0, err
		}
		cur, _, _ := readMaybe(ap)
		for _, l := range lines {
			cur = appendToSection(cur, "Board", l)
		}
		if err := writeAtomic(ap, cur); err != nil {
			return 0, fail(ExitFail, "fs", "%v", err)
		}
	}
	if err := writeAtomic(path, []byte(strings.Join(keep, "\n")+"\n")); err != nil {
		return 0, fail(ExitFail, "fs", "%v", err)
	}
	p.LogEvent("tidy", map[bool]string{true: "team", false: owner}[owner == TeamID], fmt.Sprintf("archived=%d", n))
	return n, nil
}

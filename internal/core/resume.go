package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// sinceLastSave is the resume block of as (§16.5): what changed in the
// project since the agent last wrote its board or context, so a resumed
// session continues from the board instead of redoing work. Empty for an
// agent that has never saved.
func (p *Project) sinceLastSave(id string) string {
	board, err := os.Stat(p.boardPath(id))
	if err != nil {
		return ""
	}
	last := board.ModTime()
	if st, err := os.Stat(filepath.Join(p.AgentDir(id), "context.md")); err == nil && st.ModTime().After(last) {
		last = st.ModTime()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n===== Since your last save (%s): read this first, continue from your board, redo nothing it marks done =====\n", last.Local().Format("2006-01-02 15:04"))
	news := false
	out, err := exec.Command("git", "-C", p.Root, "log", "--since=@"+strconv.FormatInt(last.Unix(), 10), "-n", "10", "--format=%h %s", "--", ".").Output()
	if err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l != "" {
				fmt.Fprintf(&b, "commit %s\n", l)
				news = true
			}
		}
	}
	day := last.Format("2006-01-02")
	boards := p.LoadBoards()
	for _, other := range sortedKeys(boards.Agents) {
		if other == id {
			continue
		}
		for _, it := range boards.Agents[other] {
			if it.Done || it.Date < day {
				continue
			}
			for _, n := range it.Needs {
				if strings.HasPrefix(n, id+"#") {
					fmt.Fprintf(&b, "%s %s (needs %s)\n", it.Ref(), it.Text, n)
					news = true
				}
			}
		}
	}
	stamp := last.UTC().Format("20060102T150405Z")
	entries, _ := os.ReadDir(p.inboxDir(id))
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && e.Name() > stamp {
			count++
		}
	}
	if count > 0 {
		fmt.Fprintf(&b, "%d new message(s) in the inbox\n", count)
		news = true
	}
	if !news {
		b.WriteString("nothing new\n")
	}
	return b.String()
}

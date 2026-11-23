package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// checkpoint (§52) prints where this folder's work stands: the last wrap
// note, git in the folder and its submodules, the team's open work, and
// this session's messages. --save <file> stores a wrap note. Outside a
// team, git or a session it prints what it can and still exits 0.
func checkpointCommand(a *args, stdout io.Writer) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	_, sid := detectTool()
	p, perr := core.FindProject(a.flags["root"])
	if perr != nil && a.flags["root"] != "" {
		return perr
	}
	repos := cpRepos(dir)

	if f := a.flags["save"]; f != "" {
		body, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var h strings.Builder
		fmt.Fprintf(&h, "saved: %s\nfolder: %s\nsession: %s\n", time.Now().UTC().Format(time.RFC3339), dir, sid)
		for _, r := range repos {
			if rev := cpGit(r.dir, "rev-parse", "--short", "HEAD"); rev != "" {
				fmt.Fprintf(&h, "commit %s: %s\n", r.label, rev)
			}
		}
		h.WriteString("---\n")
		h.Write(bytes.TrimLeft(body, "\n"))
		p, err := core.SaveWrapNote(dir, sid, []byte(h.String()))
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "saved the wrap note %s\n", p)
		return nil
	}

	// The last wrap note: its body, how old it is, and whose.
	note, hasNote := core.FindWrapNote(dir, sid)
	commits := map[string]string{}
	var since time.Time
	fmt.Fprintln(stdout, "== wrap note")
	if hasNote {
		b, _ := os.ReadFile(note.Path)
		commits = core.WrapNoteCommits(string(b))
		since = note.Saved
		whose := "this session"
		if note.Other {
			whose = "another session"
		}
		fmt.Fprintf(stdout, "saved %s ago by %s: %s\n", cpAgo(note.Saved), whose, note.Path)
		fmt.Fprintln(stdout, strings.TrimRight(string(b), "\n"))
	} else {
		fmt.Fprintln(stdout, "none for this folder (the wrap skill leaves one)")
	}

	// Git, per repo: branch state, commits since the note, uncommitted files.
	for _, r := range repos {
		fmt.Fprintf(stdout, "\n== git %s\n", r.label)
		if st := cpGit(r.dir, "status", "-sb", "--", r.scope); st != "" {
			fmt.Fprintln(stdout, strings.SplitN(st, "\n", 2)[0])
		}
		var log string
		base := commits[r.label]
		switch {
		case base != "" && cpGitOK(r.dir, "merge-base", "--is-ancestor", base, "HEAD"):
			fmt.Fprintf(stdout, "commits since the note (%s):\n", base)
			log = cpGit(r.dir, "log", "--oneline", "-n", "30", base+"..HEAD", "--", r.scope)
		case hasNote:
			if base != "" {
				fmt.Fprintf(stdout, "the note's commit %s is not in this branch: it may be out of date\n", base)
			}
			fmt.Fprintf(stdout, "commits since %s:\n", since.Local().Format("2006-01-02 15:04"))
			log = cpGit(r.dir, "log", "--oneline", "-n", "30", "--since="+since.Format(time.RFC3339), "--", r.scope)
		default:
			fmt.Fprintln(stdout, "last commits:")
			log = cpGit(r.dir, "log", "--oneline", "-n", "10", "--", r.scope)
		}
		if log == "" {
			log = "(none)"
		}
		fmt.Fprintln(stdout, cpIndent(log))
		if st := cpGit(r.dir, "status", "--short", "--", r.scope); st != "" {
			lines := strings.Split(st, "\n")
			fmt.Fprintf(stdout, "uncommitted (%d):\n", len(lines))
			if len(lines) > 20 {
				lines = append(lines[:20], fmt.Sprintf("... and %d more", len(lines)-20))
			}
			fmt.Fprintln(stdout, cpIndent(strings.Join(lines, "\n")))
		} else {
			fmt.Fprintln(stdout, "uncommitted: none")
		}
	}
	if len(repos) == 0 {
		fmt.Fprintln(stdout, "\n== git\nnot a git repository")
	}

	// The team's open work and what to do next, scoped as next scopes it.
	if perr == nil {
		fmt.Fprintf(stdout, "\n== team %s: in progress\n", filepath.Base(p.Root))
		n := 0
		boards := p.LoadBoards()
		ids := make([]string, 0, len(boards.Agents))
		for id := range boards.Agents {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			for _, it := range boards.Agents[id] {
				if it.Section == "Now" && !it.Done {
					n++
					if n <= 15 {
						fmt.Fprintf(stdout, "  %s %s %s\n", id, it.Key, it.Text)
					}
				}
			}
		}
		if n == 0 {
			fmt.Fprintln(stdout, "  (no Now entries)")
		} else if n > 15 {
			fmt.Fprintf(stdout, "  ... and %d more (sunstack board)\n", n-15)
		}
		// Done entries since the note's day, else the latest few.
		var done []core.Item
		for _, id := range ids {
			for _, it := range boards.Agents[id] {
				if it.Section == "Done" && (since.IsZero() || it.Date >= since.Local().Format("2006-01-02")) {
					done = append(done, it)
				}
			}
		}
		sort.SliceStable(done, func(i, j int) bool { return done[i].Date > done[j].Date })
		title := "done since the note"
		if since.IsZero() {
			title = "done, latest"
		}
		if len(done) > 10 {
			done = done[:10]
		}
		fmt.Fprintf(stdout, "== team %s: %s\n", filepath.Base(p.Root), title)
		if len(done) == 0 {
			fmt.Fprintln(stdout, "  (none)")
		}
		for _, it := range done {
			v := it.Verified
			if v == "" {
				v = "missing"
			}
			fmt.Fprintf(stdout, "  %s %s %s %s (verified: %s)\n", it.Date, it.Owner, it.Key, it.Text, v)
		}
	}
	na, _ := parse(nil, "root", "all json team")
	if r := a.flags["root"]; r != "" {
		na.flags["root"] = r
	}
	var nb bytes.Buffer
	if err := nextCommand(na, false, &nb); err == nil && strings.TrimSpace(nb.String()) != "" {
		fmt.Fprintf(stdout, "\n== next\n%s", nb.String())
	}

	if lines := hostWaitingLines(sid); len(lines) > 0 {
		fmt.Fprintf(stdout, "\n== this session\n  %s (sunstack check --session)\n", strings.Join(lines, "; "))
	}
	return nil
}

type cpRepo struct{ label, dir, scope string }

// cpRepos are the repos checkpoint reports on: the folder's own repo,
// limited to the folder, then each checked-out submodule under it, whole.
func cpRepos(dir string) []cpRepo {
	if !cpGitOK(dir, "rev-parse", "--show-toplevel") {
		return nil
	}
	out := []cpRepo{{label: ".", dir: dir, scope: "."}}
	for _, l := range strings.Split(cpGit(dir, "submodule", "status", "--", "."), "\n") {
		if l == "" || l[0] == '-' {
			continue // not checked out on this host
		}
		f := strings.Fields(l[1:])
		if len(f) < 2 {
			continue
		}
		path := filepath.ToSlash(f[1])
		out = append(out, cpRepo{label: path, dir: filepath.Join(dir, filepath.FromSlash(path)), scope: "."})
	}
	return out
}

// cpGit runs one read-only git command with a bound and returns its
// trimmed output, "" on any failure.
func cpGit(dir string, args ...string) string {
	s, _ := cpGitRun(dir, args...)
	return s
}

// cpGitOK says whether a git command succeeded.
func cpGitOK(dir string, args ...string) bool {
	_, ok := cpGitRun(dir, args...)
	return ok
}

func cpGitRun(dir string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimRight(string(b), "\n"), err == nil
}

func cpIndent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

func cpAgo(t time.Time) string {
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// wrapHookLine is the SessionStart hook's line about a wrap note for this
// folder: it only stats files.
func wrapHookLine(cwd, sid string) string {
	if cwd == "" {
		return ""
	}
	n, ok := core.FindWrapNote(cwd, sid)
	if !ok || n.Other && time.Since(n.Saved) > 7*24*time.Hour {
		return "" // another session's old note is not news
	}
	whose := ""
	if n.Other {
		whose = " by another session"
	}
	return fmt.Sprintf("[sunstack] a wrap note for this folder was saved %s ago%s: run `sunstack checkpoint` to read it and see where the work stands. ", cpAgo(n.Saved), whose)
}

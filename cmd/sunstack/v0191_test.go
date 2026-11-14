package main

import (
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// wantTitle is the default title (§48) of a session in the team at p.
func wantTitle(t *testing.T, p, who, task string) string {
	t.Helper()
	pr, err := core.FindProject(p)
	if err != nil {
		t.Fatalf("project at %s: %v", p, err)
	}
	return core.Title(pr.Slug(), who, task)
}

// A free session is titled <project>/<label>, whether Sunstack or the caller
// picked the label; a title the user gives is kept as given.
func TestSessionTitles(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	opt := func(pane, name string) string {
		return s.tmux(t, "display-message", "-p", "-t", pane, "#{"+name+"}")
	}
	r := sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex")
	expect(t, r, 0, "free codex")
	want := wantTitle(t, p, "codex-1", "")
	requireContains(t, r.out, "titled "+want)
	pane := field(paneRe, r.out)
	if got := opt(pane, "@sunstack_label") + " " + opt(pane, "@sunstack_title") + " " + opt(pane, "pane_title"); got != "codex-1 "+want+" "+want {
		t.Errorf("default marks %q", got)
	}

	// A label the caller gives is the who part of the title.
	r = sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "review")
	expect(t, r, 0, "labeled free codex")
	pane = field(paneRe, r.out)
	if got := opt(pane, "@sunstack_title") + " " + opt(pane, "pane_title"); got != wantTitle(t, p, "review", "")+" "+wantTitle(t, p, "review", "") {
		t.Errorf("labeled marks %q", got)
	}
	expect(t, sh(t, p, s.env, "name", "review", "audit"), 0, "rename")
	if got := opt(pane, "@sunstack_title"); got != wantTitle(t, p, "audit", "") {
		t.Errorf("after sunstack name %q", got)
	}

	// The user's own title is kept as given.
	r = sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--name", "mine", "--title", "My own: notes")
	expect(t, r, 0, "titled free codex")
	pane = field(paneRe, r.out)
	if got := opt(pane, "@sunstack_title") + "|" + opt(pane, "pane_title"); got != "My own: notes|My own: notes" {
		t.Errorf("user-titled marks %q", got)
	}
	expect(t, sh(t, p, s.env, "name", "mine", "mine2", "--title", "Second"), 0, "name with a title")
	if got := opt(pane, "@sunstack_title"); got != "Second" {
		t.Errorf("after name --title %q", got)
	}
	expect(t, sh(t, p, s.env, "spawn", "--place", "here", "--free", "--tool", "codex", "--title", "it's"), 2, "a quote in a title")
	expect(t, sh(t, p, s.env, "spawn", "reviewer", "--place", "here", "--title", "x"), 2, "--title on an agent session")
}

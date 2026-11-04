package main

// v0.17.6 (§38): next suggests the session's name and the skill that fits.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func claudeTitle176(t *testing.T, env []string, sid string, lines ...string) {
	t.Helper()
	path := filepath.Join(envValue(env, "HOME"), ".claude", "projects", "fixture", sid+".jsonl")
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
}

func titleLine176(sid, title string) string {
	b, _ := json.Marshal(map[string]string{"type": "custom-title", "customTitle": title, "sessionId": sid})
	return string(b)
}

func TestNextSuggestsRename(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	sid := "00000000-0000-4000-8000-000000000381"
	cenv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	next := func() string {
		r := sh(t, p, cenv, "next", "--all")
		expect(t, r, 0, "next")
		return r.out
	}
	// No Sunstack name yet: no item, even with a transcript.
	claudeTitle176(t, env, sid, `{"type":"user","message":{"role":"user","content":"hello"}}`)
	if out := next(); strings.Contains(out, "/rename") {
		t.Fatalf("a session with no Sunstack name got a rename item: %s", out)
	}
	expect(t, sh(t, p, cenv, "as", "reviewer", "--task", "web"), 0, "claim")

	// Never renamed: the item.
	requireContains(t, next(), "has no title", "/rename reviewer_web")

	// A title record only inside a tool result line still counts as none.
	quoted, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"content": titleLine176(sid, "reviewer_web")}})
	big := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 9<<20) + `"}}`
	claudeTitle176(t, env, sid, string(quoted), big)
	requireContains(t, next(), "/rename reviewer_web")

	// Another title, then the matching one after a 9 MB line.
	claudeTitle176(t, env, sid, titleLine176(sid, "old-name"))
	requireContains(t, next(), `titled "old-name"`, "/rename reviewer_web")
	claudeTitle176(t, env, sid, titleLine176(sid, "old-name"), big, titleLine176(sid, "reviewer_web"))
	if out := next(); strings.Contains(out, "/rename") {
		t.Errorf("a session with the right title got a rename item: %s", out)
	}

	// No transcript: the title is unknown, so no item.
	must(t, os.RemoveAll(filepath.Join(envValue(env, "HOME"), ".claude", "projects")))
	if out := next(); strings.Contains(out, "/rename") {
		t.Errorf("no transcript, yet a rename item: %s", out)
	}
}

func TestNextSuggestsRenameCodex(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	sid := "019a0000-0000-7000-8000-000000000382"
	xenv := append(append([]string{}, env...), "CODEX_THREAD_ID="+sid)
	expect(t, sh(t, p, xenv, "as", "reviewer", "--task", "web", "--tool", "codex"), 0, "claim")
	index := filepath.Join(envValue(env, "HOME"), ".codex", "session_index.jsonl")
	must(t, os.MkdirAll(filepath.Dir(index), 0o755))
	write := func(names ...string) {
		var lines []string
		for _, n := range names {
			b, _ := json.Marshal(map[string]string{"id": sid, "thread_name": n})
			lines = append(lines, string(b))
		}
		other, _ := json.Marshal(map[string]string{"id": "019a0000-0000-7000-8000-000000000999", "thread_name": "reviewer_web"})
		must(t, os.WriteFile(index, []byte(strings.Join(append(lines, string(other)), "\n")+"\n"), 0o600))
	}
	// An index with no entry for this thread: its title is unknown, no item.
	write()
	if r := sh(t, p, xenv, "next", "--all"); strings.Contains(r.out, "rename it") {
		t.Errorf("no index entry for the thread, yet a rename item: %s", r.out)
	}
	write("Read the protocol")
	r := sh(t, p, xenv, "next", "--all")
	expect(t, r, 0, "next")
	requireContains(t, r.out, `titled "Read the protocol"`, "rename it to reviewer_web in Codex's agents list")
	write("Read the protocol", "reviewer_web")
	if r := sh(t, p, xenv, "next", "--all"); strings.Contains(r.out, "rename it") {
		t.Errorf("a Codex thread with the right name got a rename item: %s", r.out)
	}
}

func TestNextSkillHint(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/next/SKILL.md")
	must(t, err)
	s := strings.Join(strings.Fields(string(b)), " ")
	requireContains(t, s, "the Sunstack skill that fits it", "/rename")
}

func TestNextSuggestsRenameFreeSession(t *testing.T) {
	p := fixture(t)
	s := privateTmux(t, p, isolatedEnv(t))
	sid := "00000000-0000-4000-8000-000000000383"
	cenv := append(append([]string{}, s.env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	claudeTitle176(t, s.env, sid, titleLine176(sid, "scratch"))
	s.tmux(t, "set-option", "-p", "-t", s.pane, "@sunstack_label", "claude-2")
	for _, dir := range []string{p, t.TempDir()} {
		r := sh(t, dir, cenv, "next", "--all")
		expect(t, r, 0, "next")
		requireContains(t, r.out, `titled "scratch"`, "/rename claude-2")
	}
}

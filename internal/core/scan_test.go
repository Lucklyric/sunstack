package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexRollout(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "rollout-a.jsonl")
	writeLines(t, main,
		`{"type":"session_meta","payload":{"id":"thread-1","cwd":"/work/app","source":"cli"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>x</environment_context>"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug\nplease"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Fixed it.\nDetails follow."}]}}`,
	)
	id, status, a := codexRollout(main)
	if id != "thread-1" || status != "busy" || a.LastPrompt != "fix the login bug" || a.LastReply != "Fixed it." {
		t.Errorf("got %q %q %+v", id, status, a)
	}
	writeLines(t, main+"2",
		`{"type":"session_meta","payload":{"id":"thread-2","source":"cli"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"hello"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
	)
	if _, status, a := codexRollout(main + "2"); status != "idle" || a.LastPrompt != "hello" {
		t.Errorf("event_msg form: %q %+v", status, a)
	}
	if rolloutCwd(main) != "/work/app" {
		t.Error("rollout cwd")
	}

	// The reviewer subthread is skipped in favor of the session's own thread.
	sub := filepath.Join(dir, "rollout-b.jsonl")
	writeLines(t, sub, `{"type":"session_meta","payload":{"id":"g","source":{"subagent":{"other":"guardian"}}}}`)
	if got := mainRollout([]string{"/dev/null", sub, main}); got != main {
		t.Errorf("main rollout: %s", got)
	}
}

func TestClaudeActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeLines(t, path,
		`{"type":"user","message":{"role":"user","content":"add a dark mode"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"Added it.\nMore."}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"ai-title","aiTitle":"Dark mode"}`,
		`{"type":"last-prompt","lastPrompt":"now test it"}`,
	)
	a := claudeActivity(path)
	if a.Title != "Dark mode" || a.LastPrompt != "now test it" || a.LastReply != "Added it." || a.LastActive == "" {
		t.Errorf("%+v", a)
	}
}

func TestOneLine(t *testing.T) {
	if oneLine("\n  first \nsecond") != "first" {
		t.Error("first non-empty line")
	}
	if r := []rune(oneLine(strings.Repeat("é", 300))); len(r) != 160 {
		t.Errorf("cut to 160, got %d", len(r))
	}
}

func TestOwnerDead(t *testing.T) {
	if ownerDead([]byte("2026-10-01T00:00:00Z pid 1 start Mon_Jan_1_00:00:00_1990\n")) != true {
		t.Error("a live pid with another start time is a reused pid")
	}
	if ownerDead([]byte("2026-10-01T00:00:00Z pid 1\n")) {
		t.Error("an old-style owner line with a live pid is not dead")
	}
}

func TestNormalizeRemote(t *testing.T) {
	for _, r := range []string{"git@github.com:Me/Repo.git", "https://github.com/Me/Repo", "https://tok@GitHub.com/Me/Repo.git/", "ssh://git@github.com/Me/Repo.git"} {
		if got := normalizeRemote(r); got != "github.com/Me/Repo" {
			t.Errorf("%s -> %s", r, got)
		}
	}
}

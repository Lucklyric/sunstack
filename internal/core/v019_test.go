package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v0.19.0 review (§47): the transcript reader ties a turn to its prompt and
// follows only that turn.

func rec(t *testing.T, tr *transcript, at time.Time, m map[string]any) {
	t.Helper()
	m["timestamp"] = at.UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	tr.record(b)
}

func codexEv(typ, turn string, extra map[string]any) map[string]any {
	p := map[string]any{"type": typ, "turn_id": turn}
	for k, v := range extra {
		p[k] = v
	}
	return map[string]any{"type": "event_msg", "payload": p}
}

func codexUser(text string) map[string]any {
	return map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": text}}}}
}

func claudeUser(content any) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}
}

func claudeSays(id, text string) map[string]any {
	return map[string]any{"type": "assistant", "message": map[string]any{"id": id, "role": "assistant", "content": []map[string]any{{"type": "text", "text": text}}}}
}

func TestTranscriptTurns(t *testing.T) {
	now := time.Now()
	t.Run("a turn before Enter does not count", func(t *testing.T) {
		tr := &transcript{tool: "codex", since: now, expect: "hello", pin: true}
		rec(t, tr, now.Add(-time.Second), codexEv("task_started", "a", nil))
		rec(t, tr, now.Add(-time.Second), codexUser("hello"))
		if tr.started {
			t.Fatal("a start before Enter was taken")
		}
	})
	t.Run("another prompt's turn does not count", func(t *testing.T) {
		tr := &transcript{tool: "codex", since: now, expect: "hello", pin: true}
		rec(t, tr, now.Add(time.Second), codexEv("task_started", "a", nil))
		rec(t, tr, now.Add(time.Second), codexUser("something queued earlier"))
		if tr.started && tr.matched {
			t.Fatal("a turn with another prompt was taken as ours")
		}
		rec(t, tr, now.Add(2*time.Second), codexEv("task_started", "b", nil))
		rec(t, tr, now.Add(2*time.Second), codexUser("# AGENTS.md instructions"))
		rec(t, tr, now.Add(2*time.Second), codexUser("hello"))
		if !tr.started || !tr.matched || tr.turnID != "b" {
			t.Fatalf("our turn not found: %+v", tr.turn)
		}
	})
	t.Run("the next turn does not replace the answer", func(t *testing.T) {
		tr := &transcript{tool: "codex", since: now, expect: "hello", pin: true}
		rec(t, tr, now.Add(time.Second), codexEv("task_started", "a", nil))
		rec(t, tr, now.Add(time.Second), codexUser("hello"))
		rec(t, tr, now.Add(time.Second), codexEv("task_complete", "a", map[string]any{"last_agent_message": "requested answer"}))
		rec(t, tr, now.Add(2*time.Second), codexEv("task_started", "b", nil))
		if !tr.ended || strings.Join(tr.reply, "") != "requested answer" {
			t.Fatalf("lost the answer: %+v", tr.turn)
		}
	})
	t.Run("another turn's end does not end ours", func(t *testing.T) {
		tr := &transcript{tool: "codex", since: now, expect: "hello", pin: true}
		rec(t, tr, now.Add(time.Second), codexEv("task_started", "a", nil))
		rec(t, tr, now.Add(time.Second), codexUser("hello"))
		rec(t, tr, now.Add(time.Second), codexEv("task_complete", "z", nil))
		if tr.ended {
			t.Fatal("ended by another turn's record")
		}
	})
	t.Run("an XML-like prompt is a prompt", func(t *testing.T) {
		tr := &transcript{tool: "claude", since: now, expect: "<request>Review this</request>", pin: true}
		rec(t, tr, now.Add(time.Second), claudeUser("<request>Review this</request>"))
		if !tr.started || !tr.matched {
			t.Fatal("the prompt was not taken as one")
		}
		rec(t, tr, now.Add(time.Second), claudeUser("<local-command-stdout>x</local-command-stdout>"))
		rec(t, tr, now.Add(time.Second), claudeSays("m1", "done"))
		rec(t, tr, now.Add(time.Second), map[string]any{"type": "system", "subtype": "turn_duration"})
		if !tr.ended || strings.Join(tr.reply, "") != "done" {
			t.Fatalf("%+v", tr.turn)
		}
	})
	t.Run("the reply is the last message", func(t *testing.T) {
		tr := &transcript{tool: "claude"}
		rec(t, tr, now, claudeUser("q"))
		rec(t, tr, now, claudeSays("m1", "Thinking aloud"))
		rec(t, tr, now, claudeSays("m2", "Final answer"))
		rec(t, tr, now, claudeSays("m2", "second part"))
		rec(t, tr, now, map[string]any{"type": "system", "subtype": "turn_duration"})
		if got := strings.Join(tr.reply, "|"); got != "Final answer|second part" {
			t.Fatalf("reply %q", got)
		}
	})
}

func TestLeadingSpace(t *testing.T) {
	if !leadingSpace("codex", "Test\n›    hello\n") || leadingSpace("codex", "Test\n› hello\n") {
		t.Error("codex")
	}
	if !leadingSpace("claude", "───\n❯   hi\n───\n") || leadingSpace("claude", "───\n❯ hi\n───\n") {
		t.Error("claude")
	}
}

func TestTranscriptExactPrompt(t *testing.T) {
	now := time.Now()
	for _, tool := range []string{"codex", "claude"} {
		tr := &transcript{tool: tool, since: now, expect: "delete foo", pin: true}
		if tool == "codex" {
			rec(t, tr, now.Add(time.Second), codexEv("task_started", "a", nil))
			rec(t, tr, now.Add(time.Second), codexUser("do not delete foo"))
			rec(t, tr, now.Add(time.Second), codexUser("deletefoo"))
		} else {
			rec(t, tr, now.Add(time.Second), claudeUser("do not delete foo"))
			rec(t, tr, now.Add(time.Second), claudeUser("deletefoo"))
		}
		if tr.matched {
			t.Errorf("%s: another prompt that holds it was taken as ours", tool)
		}
	}
}

// A turn that is not ours ends; ours comes after it and is still found.
func TestTranscriptPastOtherTurn(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	var lines []string
	for _, m := range []map[string]any{
		codexEv("task_started", "other", nil), codexUser("queued earlier"), codexEv("task_complete", "other", map[string]any{"last_agent_message": "not this"}),
		codexEv("task_started", "ours", nil), codexUser("hello"), codexEv("task_complete", "ours", map[string]any{"last_agent_message": "the answer"}),
	} {
		m["timestamp"] = now.Add(time.Second).UTC().Format(time.RFC3339Nano)
		b, _ := json.Marshal(m)
		lines = append(lines, string(b))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := &transcript{tool: "codex", path: path, since: now, expect: "hello", pin: true}
	tr.poll()
	if !tr.matched || !tr.ended || strings.Join(tr.reply, "") != "the answer" {
		t.Fatalf("%+v", tr.turn)
	}
}

func TestUndimmed(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[1m›\x1b[0m\x1b[48;2;30;30;30m \x1b[2mAsk Codex to do anything\x1b[0m\x1b[48;2;30;30;30m": "› ",
		"\x1b[39m❯ \x1b[2mTry \"fix typecheck errors\"\x1b[0m":                                         "❯ ",
		"\x1b[1m›\x1b[0m\x1b[48;2;30;30;30m xy":                                                        "› xy",
		"\x1b[2mdim\x1b[22m plain":                                                                     " plain",
	} {
		if got := undimmed(in); got != want {
			t.Errorf("undimmed(%q) = %q, want %q", in, got, want)
		}
	}
}

// The input box's edges come from what the CLI draws, not from a blank row a
// draft may hold, and a dim hint stays dim on the rows it wraps onto.
func TestComposerRows(t *testing.T) {
	codexDraft := []string{"Test Codex", "› ", "", "secret draft", "", "  test-model high · ~/x", "  ← for agents · ? for shortcuts"}
	if s, e, ok := composerRows("codex", codexDraft); !ok || s != 1 || e != 4 {
		t.Errorf("codex box %d..%d %v", s, e, ok)
	}
	claude := []string{"───", "❯ a", "", "b", "───", "  -- INSERT --"}
	if s, e, ok := composerRows("claude", claude); !ok || s != 1 || e != 4 {
		t.Errorf("claude box %d..%d %v", s, e, ok)
	}
	if v, dim := undimmedFrom("› \x1b[2mThis placeholder wraps", false); v != "› " || !dim {
		t.Errorf("first row %q %v", v, dim)
	}
	if v, _ := undimmedFrom(" onto a second row\x1b[0m", true); v != "" {
		t.Errorf("second row %q", v)
	}
}

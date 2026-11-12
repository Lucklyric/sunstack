package core

import (
	"encoding/json"
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
		tr := &transcript{tool: "claude", since: now, expect: squash("<request>Review this</request>"), pin: true}
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

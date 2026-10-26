package main

// v0.17.1: fixes from the v0.17.0 release review.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// A question closes on any fyi or done reply from its recipient, and its
// sender can withdraw it with an fyi or done reply of its own.
func TestQuestionCloses(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	tb := field(tokenRe, sh(t, p, env, "as", "builder.bob").out)
	tr := field(tokenRe, sh(t, p, env, "as", "reviewer.rita").out)
	ask := func() string {
		r := sh(t, p, env, "send", "builder.bob", "which one?", "--type", "question", "--from", "builder.alice", "--token", ta)
		expect(t, r, 0, "question")
		return field(msgRe, r.out)
	}
	open := func(q string) bool { return strings.Contains(sh(t, p, env, "tasks", "--type", "question").out, q) }

	q1 := ask()
	expect(t, sh(t, p, env, "send", "builder.alice", "CSV, as said in chat", "--reply-to", q1, "--from", "builder.bob", "--token", tb), 0, "recipient fyi")
	if open(q1) {
		t.Error("an fyi answer from the recipient left the question open")
	}
	q2 := ask()
	expect(t, sh(t, p, env, "send", "builder.bob", "closed: answered in chat", "--reply-to", q2, "--from", "builder.alice", "--token", ta), 0, "sender withdraws")
	if open(q2) {
		t.Error("the sender's withdrawal left the question open")
	}
	q3 := ask()
	expect(t, sh(t, p, env, "send", "builder.bob", "still need this", "--type", "question", "--reply-to", q3, "--from", "builder.alice", "--token", ta), 0, "sender re-asks")
	expect(t, sh(t, p, env, "send", "builder.alice", "not mine to answer", "--reply-to", q3, "--from", "reviewer.rita", "--token", tr), 0, "unrelated fyi")
	if !open(q3) {
		t.Error("a re-ask or an unrelated agent's reply closed the question")
	}
}

// --upto keeps issues that arrived after the reviewed list for the next triage.
func TestTriageUpto(t *testing.T) {
	p, env := team086(t)
	expect(t, sh(t, p, env, "issue", "first note"), 0, "first")
	var list []core.IssueEntry
	must(t, json.Unmarshal([]byte(sh(t, p, env, "issues", "--since", "triaged", "--json").out), &list))
	if len(list) != 1 {
		t.Fatalf("want 1 entry, got %d", len(list))
	}
	upto := list[0].At
	// A later note, a second after the reviewed one.
	home := envValue(env, "SUNSTACK_HOME")
	f, err := os.OpenFile(filepath.Join(home, "log", "issues.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	late, _ := json.Marshal(core.IssueEntry{At: "2999-01-01T00:00:00Z", Source: "note", Code: "note", Msg: "late note"})
	_, err = f.Write(append(late, '\n'))
	must(t, err)
	f.Close()

	expect(t, sh(t, p, env, "issues", "--triaged", "--upto", upto), 0, "mark up to the reviewed entry")
	r := sh(t, p, env, "issues", "--since", "triaged")
	requireContains(t, r.out, "late note")
	expect(t, sh(t, p, env, "issues", "--upto", upto), 2, "--upto needs --triaged")
	expect(t, sh(t, p, env, "issues", "--triaged", "--upto", "yesterday"), 2, "--upto takes a time")
}

// A task or question sent to another host leaves the default list once its
// recipient there replies done; an unrelated sender's reply does not count.
func TestRemoteAnswered(t *testing.T) {
	p, env := team086(t)
	t.Setenv("SUNSTACK_HOME", envValue(env, "SUNSTACK_HOME"))
	out := filepath.Join(envValue(env, "SUNSTACK_HOME"), "outbox")
	must(t, os.MkdirAll(out, 0o700))
	q := "20260101T000000Z-builder.alice-aaaaaa"
	writeFile(t, out, q+".json", `{"mail":{"id":"`+q+`"},"letter":{"from":"builder.alice","type":"question","to":"proj/builder.bob"},"to_name":"remote","status":"delivered"}`)
	pr, err := core.FindProject(p)
	must(t, err)
	if !strings.Contains(sentText(false, ""), q) {
		t.Fatal("an unanswered remote question is not listed")
	}
	_, err = pr.Send(core.SendOptions{To: "builder.alice", Type: "done", ReplyTo: q, Body: "no", FromLabel: "remote:proj/reviewer.rita", FromHost: "remote", NoNudge: true})
	must(t, err)
	if !strings.Contains(sentText(false, ""), q) {
		t.Error("an unrelated sender's reply answered the remote question")
	}
	_, err = pr.Send(core.SendOptions{To: "builder.alice", Type: "done", ReplyTo: q, Body: "CSV.", FromLabel: "remote:proj/builder.bob", FromHost: "remote", NoNudge: true})
	must(t, err)
	if strings.Contains(sentText(false, ""), q) {
		t.Error("an answered remote question is still listed")
	}
	if !strings.Contains(sentText(true, ""), q) {
		t.Error("--all drops the answered remote question")
	}
}

func TestCheckSkillWithdraw(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/check/SKILL.md")
	must(t, err)
	s := strings.Join(strings.Fields(string(b)), " ")
	if !strings.Contains(s, "withdraw it with an `fyi --reply-to <its id>`") {
		t.Error("check skill lacks the withdrawal rule")
	}
}

// Review r0171: the answer may be in another team on this host, in a host
// inbox, an fyi to a question, the sender's own withdrawal in the outbox, or
// from a session-ID recipient.
func TestRemoteAnsweredHostWide(t *testing.T) {
	_, env := team086(t)
	home := envValue(env, "SUNSTACK_HOME")
	t.Setenv("SUNSTACK_HOME", home)
	other := t.TempDir()
	expect(t, sh(t, other, env, "init"), 0, "init other")
	expect(t, sh(t, other, env, "hire", "builder", "carl"), 0, "hire carl")
	pr, err := core.FindProject(other)
	must(t, err)
	out := filepath.Join(home, "outbox")
	must(t, os.MkdirAll(out, 0o700))
	sent := func(id, from, typ, to, replyTo string) {
		rt := ""
		if replyTo != "" {
			rt = `,"reply_to":"` + replyTo + `"`
		}
		writeFile(t, out, id+".json", `{"mail":{"id":"`+id+`"},"letter":{"from":"`+from+`","type":"`+typ+`","to":"`+to+`"`+rt+`},"to_name":"remote","status":"delivered"}`)
	}
	listed := func(id string) bool { return strings.Contains(sentText(false, ""), id) }

	// A task answered in the other team's inbox.
	t1 := "20260101T000001Z-builder.carl-aaaaa1"
	sent(t1, "other/builder.carl", "task", "proj/builder.bob", "")
	_, err = pr.Send(core.SendOptions{To: "builder.carl", Type: "done", ReplyTo: t1, Body: "done", FromLabel: "remote:proj/builder.bob", FromHost: "remote", NoNudge: true})
	must(t, err)
	if listed(t1) {
		t.Error("a task answered in another team is still listed")
	}
	// A question answered with an fyi.
	q1 := "20260101T000002Z-builder.carl-aaaaa2"
	sent(q1, "other/builder.carl", "question", "proj/builder.bob", "")
	_, err = pr.Send(core.SendOptions{To: "builder.carl", Type: "fyi", ReplyTo: q1, Body: "CSV", FromLabel: "remote:proj/builder.bob", FromHost: "remote", NoNudge: true})
	must(t, err)
	if listed(q1) {
		t.Error("a remote question answered by fyi is still listed")
	}
	// A question the sender withdrew; a task cannot be withdrawn that way.
	q2 := "20260101T000003Z-builder.carl-aaaaa3"
	sent(q2, "other/builder.carl", "question", "proj/builder.bob", "")
	sent("20260101T000004Z-builder.carl-aaaaa4", "other/builder.carl", "fyi", "proj/builder.bob", q2)
	if listed(q2) {
		t.Error("a withdrawn remote question is still listed")
	}
	t2 := "20260101T000005Z-builder.carl-aaaaa5"
	sent(t2, "other/builder.carl", "task", "proj/builder.bob", "")
	sent("20260101T000006Z-builder.carl-aaaaa6", "other/builder.carl", "fyi", "proj/builder.bob", t2)
	if !listed(t2) {
		t.Error("an fyi from the sender closed a remote task")
	}
	// A task to a remote session, answered into a host inbox.
	sid := "00000000-0000-4000-8000-000000001712"
	t3 := "20260101T000007Z-user-aaaaa7"
	sent(t3, "user", "task", sid, "")
	dir := filepath.Join(home, "inbox", "00000000-0000-4000-8000-000000000001")
	must(t, os.MkdirAll(dir, 0o700))
	writeFile(t, dir, "20260101T000008Z-remote-user-aaaaa8.md", "---\nid: 20260101T000008Z-remote-user-aaaaa8\nfrom: remote:user\nto: 00000000-0000-4000-8000-000000000001\nat: 2026-01-01T00:00:08Z\ntype: done\nreply_to: "+t3+"\nfrom_session: remote:"+sid+"\nfrom_host: remote\n---\ndone\n")
	if listed(t3) {
		t.Error("a session recipient's reply in a host inbox did not close the task")
	}
}

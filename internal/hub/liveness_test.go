package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// §18.5: mail stays on the hub until the receiving host acks it. A watch
// stream that sent a mail frame once, and never again while the file is
// still there, strands mail the host could not take right away (a busy
// lock, a missing pin) until the stream drops.
func TestWatchResendsUnackedMail(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	s := store{dir: t.TempDir()}
	me := &Host{ID: "bbbbbbbbbbbbbbbb", Name: "server"}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(writeJSON(s.hostsFile(), &Roster{Schema: 1, OrgID: "0123456789abcdef", OrgName: "o",
		Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop"}, me}}, 0o600))
	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: me.ID, Eph: "e", Sealed: "s", Sig: "g"}
	mailFile := filepath.Join(s.mailDir(me.ID), m.ID+".json")
	must(writeJSON(mailFile, m, 0o600))

	poll, resend := pollEvery, resendAfter
	pollEvery, resendAfter = 20*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { pollEvery, resendAfter = poll, resend })

	// One stream for the whole test: it must see the file, send it, and
	// send it again while it is still there.
	run := func(d time.Duration) []frame {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			(&Server{s: s}).watch(rec, httptest.NewRequest("GET", "/v1/watch", nil).WithContext(ctx), me)
			close(done)
		}()
		time.Sleep(d)
		cancel()
		<-done
		var out []frame
		sc := bufio.NewScanner(rec.Body)
		for sc.Scan() {
			var f frame
			if json.Unmarshal(sc.Bytes(), &f) == nil {
				out = append(out, f)
			}
		}
		return out
	}
	mails := func(fs []frame) int {
		n := 0
		for _, f := range fs {
			if f.Op == "mail" && f.Mail != nil && f.Mail.ID == m.ID {
				n++
			}
		}
		return n
	}
	// Twenty polls, five resend windows: the unacked mail must come again.
	if n := mails(run(500 * time.Millisecond)); n < 2 {
		t.Fatalf("unacked mail sent %d time(s) in 500ms with resend after %s; it is never offered again", n, resendAfter)
	}
	// Acked mail (the file is gone) is not sent again: the stream sends it
	// once at the start, the ack removes it before the first resend window.
	if n := mails(run(0)); n != 1 {
		t.Fatalf("a fresh stream sent the mail %d time(s) at once, want 1", n)
	}
	must(os.Remove(mailFile))
	if n := mails(run(300 * time.Millisecond)); n != 0 {
		t.Errorf("acked mail (file removed) was sent %d time(s)", n)
	}
}

// The connector says why a delivery was not final, so a message the hub
// keeps re-sending is visible in hub-connect.log; and it sends no ack.
func TestHandleLogsNonFinalDelivery(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir()) // no org.json: no pin, and PullOnce fails without the network
	var log bytes.Buffer
	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb", Eph: "e", Sealed: "s", Sig: "g"}
	gone := handle(&frame{Op: "mail", Mail: m}, func(f frame) error {
		t.Errorf("a non-final delivery sent %+v", f)
		return nil
	}, &log)
	if gone {
		t.Fatal("handle said the host is gone")
	}
	if !strings.Contains(log.String(), m.ID) || !strings.Contains(log.String(), "no key for the sending host yet") {
		t.Fatalf("the connector did not log why the delivery was not final:\n%s", log.String())
	}
}

// §18.5: mail stays in the outbox until the hub says stored. A connector
// whose hub answered retry (roster unreadable, queue full, store failed)
// must offer the message again on the same connection, not only after a
// reconnect.
func TestRetryMailOfferedAgain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUNSTACK_HOME", filepath.Join(home, ".sunstack"))
	t.Setenv("SUNSTACK_HOST_NAME", "laptop")
	t.Setenv("SUNSTACK_MACHINE_NAME", "laptop")
	agents := filepath.Join(home, "agents.json")
	if err := os.WriteFile(agents, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUNSTACK_CLAUDE_AGENTS", agents)
	t.Setenv("SUNSTACK_CODEX_SCAN", "off")
	t.Setenv("SUNSTACK_TMUX_SOCKET", filepath.Join(home, "no-tmux"))

	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb", Eph: "e", Sealed: "s", Sig: "g"}
	src := filepath.Join(outboxDir(), m.ID+".json")
	if err := writeJSON(src, &Sent{Mail: *m, Status: "queued", At: stamp(time.Now())}, 0o600); err != nil {
		t.Fatal(err)
	}

	// A fake hub: the stream stays open, and the first mail post is
	// answered retry, every later one stored.
	offers := make(chan int32, 16)
	var posts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/v1/mail", func(w http.ResponseWriter, r *http.Request) {
		var got Mail
		json.NewDecoder(r.Body).Decode(&got)
		n := posts.Add(1)
		offers <- n
		if n == 1 {
			writeFrame(w, frame{Op: "retry", ID: got.ID, Reason: "hub could not store the message"})
			return
		}
		writeFrame(w, frame{Op: "stored", ID: got.ID})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // push, ping, ack
		writeFrame(w, frame{Op: "ok", HubTime: stamp(time.Now())})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	var log bytes.Buffer
	ended := make(chan error, 1)
	cl := &client{c: &Config{OrgID: "0123456789abcdef", OrgName: "o", Hub: "h", HubURL: ts.URL, Token: "t"}}
	go func() { ended <- session(cl, &log) }()
	defer func() {
		ts.CloseClientConnections()
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Error("the session did not end when the stream closed")
		}
	}()

	wait := func(n int32) {
		t.Helper()
		for {
			select {
			case got := <-offers:
				if got >= n {
					return
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("the outbox mail was offered %d time(s), not %d, within 5s of the hub's retry; it is never offered again on this connection\nlog: %s", posts.Load(), n, log.String())
			}
		}
	}
	wait(1)
	var kept Sent
	deadline := time.Now().Add(2 * time.Second)
	for readJSON(src, &kept) != nil || kept.Reason == "" {
		if time.Now().After(deadline) {
			t.Fatalf("after retry the outbox copy is %+v, want it queued with the hub's reason", kept)
		}
		time.Sleep(20 * time.Millisecond)
	}
	wait(2)
	deadline = time.Now().Add(2 * time.Second)
	for {
		var done Sent
		if _, err := os.Stat(src); err != nil && readJSON(filepath.Join(sentDir(), m.ID+".json"), &done) == nil && done.Status == "at hub" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after stored the mail did not move from the outbox to sent (%+v)", done)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

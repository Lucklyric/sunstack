package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The hub names the snapshot's host; a name that is not a host ID must not
// become a file name in remote/, where the pins and grants live.
func TestSnapHostIsAnID(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	os.MkdirAll(remoteDir(), 0o700)
	os.WriteFile(pinsPath(), []byte(`{}`), 0o600)
	for _, id := range []string{"pins", "allow", "requests", "roster", "AAAAAAAAAAAAAAAA"} {
		if err := saveSnap(&Snap{Host: id}, stamp(time.Now())); err == nil {
			t.Errorf("snapshot for host %q saved", id)
		}
	}
	if b, _ := os.ReadFile(pinsPath()); string(b) != `{}` {
		t.Errorf("pins overwritten: %s", b)
	}
	k, _ := LoadKeys()
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: "pins", Name: "x", Keys: k.Public()}}}); err != nil {
		t.Fatal(err)
	}
	if loadPins()["pins"] != nil {
		t.Error("a host that is not an ID was pinned")
	}
}

// A reply goes to the host that asked, by its ID and pinned key, even when
// the roster now shows another host under its name.
func TestReplySealedToAsker(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, _ := LoadKeys()
	other := &Keys{}
	{
		home := os.Getenv("SUNSTACK_HOME")
		t.Setenv("SUNSTACK_HOME", t.TempDir())
		other, _ = LoadKeys()
		t.Setenv("SUNSTACK_HOME", home)
	}
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop", Keys: k.Public()}}}); err != nil {
		t.Fatal(err)
	}
	// The request came in; then the hub renames the asker and lists itself
	// as laptop before the reply goes out.
	asker := loadPins()["aaaaaaaaaaaaaaaa"]
	r := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "old", Keys: k.Public()}, {ID: "cccccccccccccccc", Name: "laptop", Keys: other.Public()}}}
	if err := pinRoster(r); err != nil {
		t.Fatal(err)
	}
	if err := saveRoster(r, stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(&Config{OrgID: "o", Hub: "h", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	req := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa"}
	reply(req, asker, "peek", PeekResult{Text: "SECRET"}, nil)
	files, _ := filepath.Glob(filepath.Join(outboxDir(), "*.json"))
	if len(files) != 1 {
		t.Fatalf("outbox: %v", files)
	}
	var s Sent
	b, _ := os.ReadFile(files[0])
	json.Unmarshal(b, &s)
	if s.Mail.ToHost != "aaaaaaaaaaaaaaaa" {
		t.Errorf("the reply went to %s", s.Mail.ToHost)
	}
	// A new host under a name a pinned host had needs the user's trust.
	if p := loadPins()["cccccccccccccccc"]; p == nil || p.Changed == nil {
		t.Errorf("a new host taking a pinned name was trusted: %+v", p)
	}
	if s, _ := KeysText(); !strings.Contains(s, "laptop: NEW HOST") {
		t.Errorf("org keys:\n%s", s)
	}
	if _, err := queueLetter("laptop", &Letter{From: "user", To: "shop/builder.alice", Type: "fyi"}, true); err == nil || !strings.Contains(err.Error(), "trust") {
		t.Errorf("mail to a host that took a pinned name: %v", err)
	}
}

// Two hosts with one name: sending by that name is refused, never a guess.
func TestAmbiguousName(t *testing.T) {
	r := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop"}, {ID: "bbbbbbbbbbbbbbbb", Name: "Laptop"}}}
	if h := r.byName("laptop"); h != nil {
		t.Errorf("byName picked %s", h.ID)
	}
}

// A remote snapshot and a peek answer carry text from another host; terminal
// control sequences in it are made visible, never passed to the terminal.
func TestRemoteTextCleaned(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	org := core.Org{Notes: []string{"a\x1b]52;c;ZXZpbA==\x07b"}, Teams: []*core.OrgTeam{{Name: "t\x1b[2J", Issues: []string{"\x9b31m"}}}}
	raw, _ := json.Marshal(org)
	os.MkdirAll(remoteDir(), 0o700)
	if err := saveSnap(&Snap{Host: "aaaaaaaaaaaaaaaa", Snapshot: raw}, stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	hv := &HostView{}
	loadSnap(&Config{}, "aaaaaaaaaaaaaaaa", time.Now(), hv)
	if hv.Org == nil {
		t.Fatal("no snapshot loaded")
	}
	b, _ := json.Marshal(hv.Org)
	var back core.Org
	json.Unmarshal(b, &back)
	for _, s := range []string{back.Notes[0], back.Teams[0].Name, back.Teams[0].Issues[0]} {
		if strings.ContainsAny(s, "\x1b\x07\u009b") {
			t.Errorf("control sequence kept: %q", s)
		}
	}
}

// Pin changes wait for the pins lock, so a trust is never lost to a roster
// update writing at the same time.
func TestPinWritesLocked(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, _ := LoadKeys()
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "laptop", Keys: k.Public()}}}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockPins()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- pinRoster(&Roster{Hosts: []*Host{{ID: "bbbbbbbbbbbbbbbb", Name: "server", Keys: k.Public()}}})
	}()
	select {
	case err := <-done:
		t.Fatalf("pinRoster did not wait for the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if loadPins()["bbbbbbbbbbbbbbbb"] == nil {
		t.Error("the pin was not written")
	}
}

// A remote update that hangs ends at the limit with a refusal, and the
// connector is not restarted.
func TestUpdateTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in update runs /bin/sh")
	}
	t.Setenv("SUNSTACK_SERVICE", "1")
	t.Setenv("SUNSTACK_UPDATE_CMD", "sleep 30")
	old := updateLimit
	updateLimit = 300 * time.Millisecond
	defer func() { updateLimit = old }()
	start := time.Now()
	_, err := updateHandler("", "", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Errorf("hung update: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the update ran %s", time.Since(start))
	}
	if restartAfterReply.Load() {
		t.Error("the connector would restart after a failed update")
	}
}

// A hub that moves a pinned host onto another pinned host's name does not
// redirect mail sent to that name.
func TestPinnedNameSwap(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	a, _ := LoadKeys()
	var b *Keys
	{
		home := os.Getenv("SUNSTACK_HOME")
		t.Setenv("SUNSTACK_HOME", t.TempDir())
		b, _ = LoadKeys()
		t.Setenv("SUNSTACK_HOME", home)
	}
	before := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "assistant", Keys: a.Public()}, {ID: "bbbbbbbbbbbbbbbb", Name: "other", Keys: b.Public()}}}
	if err := pinRoster(before); err != nil {
		t.Fatal(err)
	}
	after := &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "renamed", Keys: a.Public()}, {ID: "bbbbbbbbbbbbbbbb", Name: "assistant", Keys: b.Public()}}}
	if err := pinRoster(after); err != nil {
		t.Fatal(err)
	}
	if err := saveRoster(after, stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(&Config{OrgID: "o", Hub: "h", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := queueLetter("assistant", &Letter{From: "user", To: "shop/builder.alice", Type: "fyi"}, true); err == nil {
		t.Error("mail to assistant went to the host that took its name")
	}
	if _, _, err := pinByName("assistant"); err == nil {
		t.Error("a grant to assistant would go to the host that took its name")
	}
}

// A request without a grant is answered at once and never journaled or
// queued; a request may not stay valid longer than the longest one Sunstack
// sends.
func TestRequestNeedsGrantFirst(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, _ := LoadKeys()
	const from = "aaaaaaaaaaaaaaaa"
	if err := saveConfig(&Config{OrgID: "o", Hub: "h", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: from, Name: "laptop", Keys: k.Public()}}}); err != nil {
		t.Fatal(err)
	}
	pin := loadPins()[from]
	ask := func(exp time.Time) (string, string) {
		body, _ := json.Marshal(Control{Expires: stamp(exp), Args: json.RawMessage(`{}`)})
		m := &Mail{ID: core.NewMessageID("user"), FromHost: from}
		status, msg, _ := takeRequest(m, pin, &Letter{From: "user", To: "host", Type: "peek", Body: string(body)})
		if loadJournal()[m.ID] != nil {
			t.Errorf("request %s (%s) was journaled", m.ID, status)
		}
		return status, msg
	}
	ask(time.Now().Add(time.Minute))
	if _, err := Allow("laptop", []string{"peek"}); err != nil {
		t.Fatal(err)
	}
	if status, _ := ask(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)); status != "refused" {
		t.Errorf("a request valid until 2100: %s", status)
	}
}

// A failure the hub expects to pass (its roster unreadable, a full queue)
// keeps the mail in the outbox for the next try; a final refusal moves it.
func TestTransientRefusalKeepsOutbox(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	s := store{dir: t.TempDir()}
	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb", Eph: "e", Sealed: "s", Sig: "g"}
	f := s.putMail("aaaaaaaaaaaaaaaa", m) // no hosts.json yet
	if f.Op != "retry" {
		t.Fatalf("unreadable roster: %+v", f)
	}
	os.MkdirAll(outboxDir(), 0o700)
	if err := writeJSON(filepath.Join(outboxDir(), m.ID+".json"), &Sent{Mail: *m, Status: "queued"}, 0o600); err != nil {
		t.Fatal(err)
	}
	settle(m.ID, f.Op, f.Reason)
	var kept Sent
	if readJSON(filepath.Join(outboxDir(), m.ID+".json"), &kept) != nil {
		t.Fatal("a transient refusal removed the mail from the outbox")
	}
	if kept.Reason != f.Reason {
		t.Errorf("outbox reason %q, want %q", kept.Reason, f.Reason)
	}
	settle(m.ID, "refused", "no such host in the org")
	if _, err := os.Stat(filepath.Join(outboxDir(), m.ID+".json")); err == nil {
		t.Error("a final refusal kept the mail in the outbox")
	}
}

// A timed-out update ends with everything it started, so nothing keeps
// changing the install after the reply says it did not finish.
func TestUpdateTimeoutEndsChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix; the stand-in update runs /bin/sh")
	}
	mark := filepath.Join(t.TempDir(), "late")
	t.Setenv("SUNSTACK_SERVICE", "1")
	t.Setenv("SUNSTACK_UPDATE_CMD", "(sleep 1; touch '"+mark+"') & sleep 30")
	old := updateLimit
	updateLimit = 300 * time.Millisecond
	defer func() { updateLimit = old }()
	if _, err := updateHandler("", "", json.RawMessage(`{}`)); err == nil {
		t.Fatal("a hung update succeeded")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(mark); err == nil {
		t.Error("a child of the timed-out update kept running")
	}
}

// A request that expired while it waited behind others is not run.
func TestQueuedRequestExpires(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	ran := false
	old := handlers["peek"]
	handlers["peek"] = func(_, _ string, _ json.RawMessage) (any, error) { ran = true; return nil, nil }
	defer func() { handlers["peek"] = old }()
	jb := job{mail: &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa"}, from: &Pin{Name: "laptop"}, kind: "peek", expires: time.Now().Add(-time.Hour)}
	if _, err := runJob(jb); err == nil || !strings.Contains(err.Error(), "expired") || ran {
		t.Errorf("an expired request: ran=%v, %v", ran, err)
	}
}

// A roster name with terminal controls is not pinned, so org keys and
// everything that names the host never prints it.
func TestRosterNameControls(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, _ := LoadKeys()
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "box\x1b]52;c;ZXZpbA==\x07", Keys: k.Public()}}}); err != nil {
		t.Fatal(err)
	}
	if s, _ := KeysText(); strings.ContainsAny(s, "\x1b\x07") {
		t.Errorf("org keys printed a control sequence: %q", s)
	}
}

// A peer's snapshot with null entries loads without them, so the views
// that walk it cannot crash.
func TestSnapshotNulls(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	os.MkdirAll(remoteDir(), 0o700)
	if err := saveSnap(&Snap{Host: "aaaaaaaaaaaaaaaa", Snapshot: json.RawMessage(`{"schema":1,"teams":[null,{"name":"t","agents":[null]}],"sessions":[null]}`)}, stamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	hv := &HostView{}
	loadSnap(&Config{}, "aaaaaaaaaaaaaaaa", time.Now(), hv)
	if hv.Org == nil {
		t.Fatal("no snapshot loaded")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("a null entry crashed the view: %v", r)
		}
	}()
	OrgCounts(hv.Org)
}

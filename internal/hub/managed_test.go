package hub

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// §25.5: mail from a managed host goes on only as a reply to mail the hub
// delivered to it from the host it now writes to; anything else is held,
// and released or dropped by the user.
func TestManagedReplyGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a hub on Windows is not supported")
	}
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	t.Setenv("SUNSTACK_HOST_NAME", "hub-host")
	c, err := Init("o")
	if err != nil {
		t.Fatal(err)
	}
	s := store{hubDir(c.OrgID)}
	const gc, lap, other = "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb", "cccccccccccccccc"
	if err := s.withRoster(func(r *Roster) error {
		r.Hosts = append(r.Hosts, &Host{ID: gc, Name: "gcloud", Managed: true, SSH: "gcloud"},
			&Host{ID: lap, Name: "laptop", CanSend: true}, &Host{ID: other, Name: "other", CanSend: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	asked := core.NewMessageID("user")
	if err := writeJSON(s.deliveredFile(gc, asked), map[string]string{"from_host": lap}, 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(to, inReply string) *Mail {
		return &Mail{ID: core.NewMessageID("user"), FromHost: gc, ToHost: to, Eph: "e", Sealed: "s", Sig: "g", InReply: inReply}
	}
	ok := env(lap, asked)
	if f := s.putManaged(gc, ok); f.Op != "stored" || len(jsonFiles(s.mailDir(lap))) != 1 {
		t.Fatalf("a reply: %+v", f)
	}
	if f := s.putManaged(gc, &Mail{ID: ok.ID, FromHost: lap, ToHost: lap}); f.Op != "refused" {
		t.Errorf("another sending host: %+v", f)
	}
	for _, m := range []*Mail{env(lap, ""), env(lap, core.NewMessageID("user")), env(other, asked)} {
		if f := s.putManaged(gc, m); f.Op != "stored" {
			t.Errorf("held mail answered %+v", f)
		}
	}
	if n := len(jsonFiles(s.mailDir(other))) + len(jsonFiles(s.mailDir(lap))); n != 1 {
		t.Errorf("held mail was delivered: %d", n)
	}
	list, err := Held()
	if err != nil || len(list) != 3 || list[0].From != "gcloud" {
		t.Fatalf("held: %+v %v", list, err)
	}
	// Dropped: a resend is refused, never held again.
	drop := list[0].ID
	if err := SettleHeld(drop, false); err != nil {
		t.Fatal(err)
	}
	if f := s.putManaged(gc, &Mail{ID: drop, FromHost: gc, ToHost: lap}); f.Op != "refused" {
		t.Errorf("a dropped resend: %+v", f)
	}
	// Released: delivered once, and a resend is answered stored.
	rel := list[1]
	if err := SettleHeld(rel.ID, true); err != nil {
		t.Fatal(err)
	}
	toDir := s.mailDir(lap)
	if rel.To == "other" {
		toDir = s.mailDir(other)
	}
	if _, err := os.Stat(filepath.Join(toDir, rel.ID+".json")); err != nil {
		t.Errorf("released mail not delivered: %v", err)
	}
	if f := s.putManaged(gc, &Mail{ID: rel.ID, FromHost: gc, ToHost: lap}); f.Op != "stored" {
		t.Errorf("a released resend: %+v", f)
	}
	if list, _ := Held(); len(list) != 1 {
		t.Errorf("held after settling two: %+v", list)
	}
	if err := SettleHeld(drop, true); err == nil {
		t.Error("settled the same mail twice")
	}
	// The cap: past it, mail waits on the managed host.
	for i := 0; i < heldMax; i++ {
		s.putManaged(gc, env(lap, ""))
	}
	if f := s.putManaged(gc, env(lap, "")); f.Op != "retry" {
		t.Errorf("past the cap: %+v", f)
	}
}

// An envelope's in_reply is signed, and the receiver refuses a letter that
// answers another message than its envelope says.
func TestInReplySigned(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	k, err := LoadKeys()
	if err != nil {
		t.Fatal(err)
	}
	m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb", InReply: core.NewMessageID("user")}
	if err := seal(k, k.Public(), m, &Letter{From: "user", To: "t/a", Type: "done", ReplyTo: m.InReply, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := open(k, k.Public(), m); err != nil {
		t.Fatalf("open: %v", err)
	}
	m.InReply = core.NewMessageID("user")
	if _, err := open(k, k.Public(), m); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("a changed in_reply opened: %v", err)
	}
}

// A managed host stays live for three pulls (§25.4).
func TestManagedLiveness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	t.Setenv("SUNSTACK_HOST_NAME", "laptop")
	if err := saveConfig(&Config{OrgID: "0123456789abcdef", OrgName: "o", Hub: "h", HubURL: "http://h:7731", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string { return stamp(now.Add(-d)) }
	r := &Roster{OrgName: "o", Hosts: []*Host{
		{ID: "aaaaaaaaaaaaaaaa", Name: "m89", Managed: true, Contact: ago(89 * time.Second)},
		{ID: "bbbbbbbbbbbbbbbb", Name: "m91", Managed: true, Contact: ago(91 * time.Second)},
		{ID: "cccccccccccccccc", Name: "c45", Contact: ago(45 * time.Second)},
	}}
	if err := writeJSON(filepath.Join(remoteDir(), "roster.json"), &cachedRoster{Roster: r, HubTime: stamp(now), LocalTime: stamp(now)}, 0o600); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, h := range LoadView(now).Hosts {
		got[h.Name] = h.Label()
	}
	if !strings.HasPrefix(got["m89"], "m89 (managed, live") || !strings.HasPrefix(got["m91"], "m91 (managed, stale") || !strings.HasPrefix(got["c45"], "c45 (stale") {
		t.Errorf("labels: %v", got)
	}
}

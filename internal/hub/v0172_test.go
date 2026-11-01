package hub

// v0.17.2 (§34), batch 2: name bindings, first keys, expired replies.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

func must172(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// §34.8: a trusted name stays with its key across staged roster renames.
func TestStagedNameSwap(t *testing.T) {
	local := t.TempDir()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	a, err := LoadKeys()
	must172(t, err)
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	b, err := LoadKeys()
	must172(t, err)
	t.Setenv("SUNSTACK_HOME", local)
	must172(t, saveConfig(&Config{OrgID: "review", Hub: "hub", Token: "token"}))
	roster := func(an, bn string) *Roster {
		return &Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: an, Keys: a.Public()}, {ID: "bbbbbbbbbbbbbbbb", Name: bn, Keys: b.Public()}}}
	}
	must172(t, takeRoster(roster("assistant", "other"), stamp(time.Now())))
	takeRoster(roster("retired", "other"), stamp(time.Now()))
	takeRoster(roster("retired", "assistant"), stamp(time.Now()))
	if sent, err := Queue("assistant:shop/builder.alice", "user", "fyi", "PRIVATE-MARKER", "", ""); err == nil && sent.Mail.ToHost != "aaaaaaaaaaaaaaaa" {
		t.Errorf("mail for assistant went to host %s after staged renames", sent.Mail.ToHost)
	}
	if g, err := Allow("assistant", []string{"spawn"}); err == nil && g.HostID != "aaaaaaaaaaaaaaaa" {
		t.Errorf("a grant for assistant went to host %s without org trust", g.HostID)
	}
}

// §34.11: concurrent first use returns one key pair, the one on disk.
func TestConcurrentFirstKeys(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	const n = 32
	start := make(chan struct{})
	results := make(chan *Keys, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if k, err := LoadKeys(); err == nil {
				results <- k
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	current, err := LoadKeys()
	must172(t, err)
	for k := range results {
		if !k.Public().equal(current.Public()) {
			t.Fatal("a caller got a key pair that is not the one on disk")
		}
	}
}

// §34.12: a reply past its request's expiry is dropped.
func TestExpiredControlReply(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	local, err := LoadKeys()
	must172(t, err)
	home := core.Home()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	sender, err := LoadKeys()
	must172(t, err)
	t.Setenv("SUNSTACK_HOME", home)
	id := core.NewMessageID("user")
	host := "aaaaaaaaaaaaaaaa"
	must172(t, pinRoster(&Roster{Hosts: []*Host{{ID: host, Name: "peer", Keys: sender.Public()}}}))
	must172(t, writeJSON(filepath.Join(askedDir(), id+".json"), asked{ID: id, HostID: host, Host: "peer", Kind: "peek", Expires: stamp(time.Now().Add(-time.Hour))}, 0o600))
	body, _ := json.Marshal(Control{Result: json.RawMessage(`{"Text":"old screen"}`)})
	reply := &Mail{ID: core.NewMessageID("user"), FromHost: host, ToHost: core.ThisHost().ID}
	must172(t, seal(sender, local.Public(), reply, &Letter{From: "user", To: "host", Type: "peek-reply", ReplyTo: id, Body: string(body)}))
	Deliver(reply)
	if _, err := os.Stat(filepath.Join(repliesDir(), id+".json")); err == nil {
		t.Error("a reply to a request that expired an hour ago was kept")
	}
}

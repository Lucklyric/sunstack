package hub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

func TestAgentGrants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	keys, err := LoadKeys()
	if err != nil {
		t.Fatal(err)
	}
	const host = "aaaaaaaaaaaaaaaa"
	roster := &Roster{Hosts: []*Host{{ID: host, Name: "sender", Keys: keys.Public()}}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(saveConfig(&Config{OrgID: "testorg", Hub: LocalHub, Token: "test-token"}))
	must(pinRoster(roster))
	team := func(name, id string) string {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		_, err := core.Init(root, false)
		must(err)
		must(os.WriteFile(filepath.Join(root, "sunstack", "TEAM"), []byte("id: "+id+"\nname: "+name+"\n"), 0o644))
		p, err := core.FindProject(root)
		must(err)
		must(p.Register())
		return root
	}
	alpha := team("alpha", "1111111111111111")
	team("beta", "2222222222222222")
	legacy := team("legacy", "3333333333333333")
	must(os.Remove(filepath.Join(legacy, "sunstack", "TEAM")))
	check := func(kind, id string, wantMax int, want bool) {
		t.Helper()
		if max, ok := AgentAllowed(host, kind, id); max != wantMax || ok != want {
			t.Errorf("AgentAllowed(%s, %s) = %d, %v; want %d, %v", kind, id, max, ok, wantMax, want)
		}
	}
	if _, err := AllowAgents("sender", "peek", nil, 2); err == nil {
		t.Error("peek inherited without a spawn agent grant")
	}
	_, err = Allow("sender", []string{"spawn", "peek"})
	must(err)
	check("spawn", "1111111111111111", 0, false)
	check("peek", "1111111111111111", 0, false)
	if _, err := AllowAgents("sender", "peek", nil, 2); err == nil {
		t.Error("peek inherited a user spawn grant")
	}
	must(Deny("sender", []string{"spawn", "peek"}))
	g, err := AllowAgents("sender", "spawn", []string{"alpha", "beta", "alpha"}, 3)
	must(err)
	if !slices.Equal(g.Teams, []string{"1111111111111111", "2222222222222222"}) {
		t.Fatalf("team IDs: %v", g.Teams)
	}
	check("spawn", "1111111111111111", 3, true)
	check("spawn", "alpha", 0, false)
	check("peek", "1111111111111111", 0, false)
	check("update", "1111111111111111", 0, false)
	if Allowed(host, "spawn") || Allowed(host, "peek") || Allowed(host, "spawn:agents") {
		t.Error("an agent grant authorized a user request")
	}
	if _, ok := AgentAllowed("bbbbbbbbbbbbbbbb", "spawn", "1111111111111111"); ok {
		t.Error("another host inherited the agent grant")
	}
	_, err = AllowAgents("sender", "peek", nil, 2)
	must(err)
	_, err = Allow("sender", []string{"peek", "spawn"})
	must(err)
	_, err = AllowAgents("sender", "spawn", []string{"beta"}, 1)
	must(err)
	check("spawn", "1111111111111111", 0, false)
	check("spawn", "2222222222222222", 1, true)
	check("peek", "1111111111111111", 2, true) // the copy does not follow regrants
	must(os.WriteFile(filepath.Join(alpha, "sunstack", "TEAM"), []byte("id: 1111111111111111\nname: renamed\n"), 0o644))
	check("peek", "1111111111111111", 2, true) // renaming a team preserves its ID grant
	if txt := GrantsText(); !strings.Contains(txt, "sender: peek, spawn, key ") ||
		!strings.Contains(txt, "sender: spawn:agents, teams beta, max 1, key ") ||
		!strings.Contains(txt, "sender: peek:agents, teams renamed, beta, max 2, key ") ||
		!strings.Contains(txt, "age <1m") {
		t.Errorf("grants listing: %s", txt)
	}
	for _, tc := range []struct {
		kind  string
		teams []string
		max   int
	}{
		{"spawn", nil, 2}, {"spawn", []string{}, 2}, {"spawn", []string{"legacy"}, 2},
		{"spawn", []string{"missing"}, 2}, {"spawn", []string{""}, 2},
		{"spawn", []string{"beta"}, 0}, {"spawn", []string{"beta"}, 11},
		{"update", []string{"beta"}, 2}, {"peek", []string{}, 2},
	} {
		if _, err := AllowAgents("sender", tc.kind, tc.teams, tc.max); err == nil {
			t.Errorf("invalid grant accepted: %+v", tc)
		}
	}
	check("spawn", "2222222222222222", 1, true) // failed grants leave the old grant
	_, err = AllowAgents("sender", "peek", []string{"beta"}, 2)
	must(err)
	check("peek", "1111111111111111", 0, false)
	must(DenyAgents("sender", "spawn"))
	check("spawn", "2222222222222222", 0, false)
	check("peek", "2222222222222222", 2, true)
	if !Allowed(host, "spawn") || !Allowed(host, "peek") {
		t.Error("denying an agent grant removed user grants")
	}
	must(Deny("sender", []string{"peek", "spawn"}))
	check("peek", "2222222222222222", 2, true)
	_, err = AllowAgents("sender", "spawn", []string{"beta"}, 10)
	must(err)
	check("spawn", "2222222222222222", 10, true)
	must(saveConfig(&Config{OrgID: "anotherorg", Hub: LocalHub, Token: "test-token"}))
	check("spawn", "2222222222222222", 0, false)
	must(saveConfig(&Config{OrgID: "testorg", Hub: LocalHub, Token: "test-token"}))
	// A key change voids both kinds, even after trusting or regranting one.
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	newKeys, err := LoadKeys()
	must(err)
	t.Setenv("SUNSTACK_HOME", home)
	roster.Hosts[0].Keys = newKeys.Public()
	must(pinRoster(roster))
	check("spawn", "2222222222222222", 0, false)
	check("peek", "2222222222222222", 0, false)
	if _, err := AllowAgents("sender", "spawn", []string{"beta"}, 2); err == nil {
		t.Error("a changed, untrusted key was granted")
	}
	_, err = Trust("sender")
	must(err)
	check("spawn", "2222222222222222", 0, false)
	check("peek", "2222222222222222", 0, false)
	if _, err := AllowAgents("sender", "peek", nil, 2); err == nil {
		t.Error("peek copied a void spawn grant")
	}
	_, err = AllowAgents("sender", "spawn", []string{"beta"}, 2)
	must(err)
	check("spawn", "2222222222222222", 2, true)
	check("peek", "2222222222222222", 0, false)
	must(DenyAgents("sender", "spawn"))
	must(DenyAgents("sender", "peek"))
	if len(loadGrants()) != 0 {
		t.Error("empty grants were kept")
	}
}

// All writers share a lock: none may read a stale grant set while another
// writer is holding it, and concurrent user/agent changes must all survive.
func TestGrantWritesLocked(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	keys, err := LoadKeys()
	if err != nil {
		t.Fatal(err)
	}
	if err := pinRoster(&Roster{Hosts: []*Host{{ID: "aaaaaaaaaaaaaaaa", Name: "sender", Keys: keys.Public()}}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := core.Init(root, false); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockGrants()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unlock() }()
	done := make(chan error, 4)
	go func() { _, err := Allow("sender", []string{"spawn"}); done <- err }()
	go func() { _, err := Allow("sender", []string{"peek"}); done <- err }()
	go func() { _, err := AllowAgents("sender", "spawn", []string{root}, 2); done <- err }()
	go func() { _, err := AllowAgents("sender", "peek", []string{root}, 2); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("grant write did not wait for the lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(loadGrants()) != 3 || !Allowed("aaaaaaaaaaaaaaaa", "spawn") || !Allowed("aaaaaaaaaaaaaaaa", "peek") {
		t.Fatalf("concurrent grant changes lost: %+v", loadGrants())
	}
	unlock, err = lockGrants()
	if err != nil {
		t.Fatal(err)
	}
	go func() { done <- Deny("sender", []string{"spawn"}) }()
	go func() { done <- DenyAgents("sender", "spawn") }()
	select {
	case err := <-done:
		t.Fatalf("deny did not wait for the lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if len(loadGrants()) != 2 || Allowed("aaaaaaaaaaaaaaaa", "spawn") || !Allowed("aaaaaaaaaaaaaaaa", "peek") {
		t.Fatalf("concurrent deny changes lost: %+v", loadGrants())
	}
}

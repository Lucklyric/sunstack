package main

import (
	"os"
	"strings"
	"testing"
)

// A host keeps its keys across org leave, so it joins again; another key
// under its host ID would take over its name and mail, so that is refused.
func TestRejoinNeedsSameKeys(t *testing.T) {
	hubHost, addr, hosts := newOrgEnv(t, nil, "laptop")
	laptop := hosts[0]
	laptop.join(t, hubHost, addr)
	expect(t, laptop.run(t, "org", "leave"), 0, "leave")
	laptop.join(t, hubHost, addr)

	expect(t, laptop.run(t, "org", "leave"), 0, "leave again")
	must(t, os.Remove(laptop.file("keys.json")))
	r := hubHost.run(t, "hub", "invite")
	expect(t, r, 0, "invite")
	r = laptop.run(t, "org", "join", addr, "--code", field(codeRe, r.out))
	expect(t, r, 1, "join with new keys")
	if !strings.Contains(r.stderr, "other keys") {
		t.Errorf("join with new keys: %s", r.stderr)
	}
}

// Org membership and the hub's roster are the user's: an agent session is
// refused, as for org allow.
func TestOrgAdminIsUserOnly(t *testing.T) {
	hubHost, _, _ := newOrgEnv(t, nil)
	agent := &orgHost{name: hubHost.name, home: hubHost.home, env: append(append([]string{}, hubHost.env...), "CLAUDECODE=1")}
	for _, args := range [][]string{
		{"hub", "init", "other"}, {"hub", "invite"}, {"hub", "revoke", "laptop"},
		{"org", "join", "127.0.0.1", "--code", "x"}, {"org", "leave"}, {"org", "trust", "laptop"},
	} {
		r := agent.run(t, args...)
		expect(t, r, 2, strings.Join(args, " ")+" from an agent session")
		if !strings.Contains(r.stderr, "for the user") {
			t.Errorf("%v: %s", args, r.stderr)
		}
	}
}

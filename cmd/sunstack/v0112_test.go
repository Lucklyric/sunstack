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

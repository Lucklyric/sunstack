package main

// §34.16: spawn opens beside this pane unless the user asked for another
// place. An agent session must confirm team or window placement.

import (
	"os"
	"strings"
	"testing"
)

func TestSpawnPlaceConfirm(t *testing.T) {
	p, env := team086(t)
	agent := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000161")
	for _, args := range [][]string{
		{"spawn", "builder.bob", "--place", "team"},
		{"spawn", "builder.bob", "--place", "window"},
		{"spawn", "builder.bob", "--window"},
		{"spawn", "--free", "--place", "team"},
	} {
		r := sh(t, p, agent, args...)
		expect(t, r, 2, strings.Join(args, " "))
		requireContains(t, r.stderr, "the user asked for")
		if r := sh(t, p, agent, append(args, "--yes")...); strings.Contains(r.stderr, "the user asked for") {
			t.Errorf("%v --yes is still refused: %s", args, r.stderr)
		}
	}
	// From a terminal (no agent session) the place is the user's own choice.
	if r := sh(t, p, env, "spawn", "builder.bob", "--place", "team"); strings.Contains(r.stderr, "the user asked for") {
		t.Errorf("the user's own spawn was refused: %s", r.stderr)
	}
	requireContains(t, strings.Join(strings.Fields(sh(t, p, env, "help", "all").out), " "), "beside this pane (the default)")
	b, err := os.ReadFile("../../plugins/sunstack/skills/spawn/SKILL.md")
	must(t, err)
	requireContains(t, strings.Join(strings.Fields(string(b)), " "), "add `--yes`, which says the user asked for that place")
}

package main

// v0.17.7 (§39): in a team, plain next is this session's own list.

import (
	"os"
	"strings"
	"testing"
)

func TestNextScopedToSession(t *testing.T) {
	p, env := team086(t)
	ta := field(tokenRe, sh(t, p, env, "as", "builder.alice").out)
	commitBoard(t, p, env, "builder.alice", ta, "## Now\n- 2026-10-05 KR1 [O1] Export to CSV\n"+
		"## Asks\n- 2026-10-05 Q1 CSV or Parquet first? (options: csv | parquet) (default: csv after 2999-01-01)\n## Done\n")
	sid := "00000000-0000-4000-8000-000000000177"
	bobEnv := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID="+sid)
	tb := field(tokenRe, sh(t, p, bobEnv, "as", "builder.bob").out)
	commitBoard(t, p, bobEnv, "builder.bob", tb, "## Now\n- 2026-10-05 KR1 [O1] Data model\n## Next\n## Done\n"+
		"- 2026-10-04 KR7 [O1] Old work A\n")

	// The agent's session: its own next step first, then its own items; the
	// rest of the team is one count line.
	r := sh(t, p, bobEnv, "next")
	expect(t, r, 0, "next")
	lines := nextLineRe.FindAllStringSubmatch(r.out, -1)
	if len(lines) == 0 || lines[0][1] != "yours" || !strings.HasPrefix(lines[0][2], "builder.bob: KR1 Data model") {
		t.Errorf("bob's own step should lead:\n%s", r.out)
	}
	requireContains(t, r.out, "more item(s) in this team: sunstack next --team")
	r = sh(t, p, bobEnv, "next", "--all")
	requireContains(t, r.out, "builder.bob#KR7")
	if strings.Contains(r.out, "builder.alice") {
		t.Errorf("another agent's item in the session's next:\n%s", r.out)
	}

	// --team, --root and a plain terminal keep the whole team's list.
	for _, c := range []struct {
		env  []string
		args []string
	}{{bobEnv, []string{"next", "--team"}}, {bobEnv, []string{"next", "--root", p}}, {env, []string{"next"}}} {
		r := sh(t, p, c.env, c.args...)
		requireContains(t, r.out, "builder.alice#Q1")
		if strings.Contains(r.out, "in this team: sunstack next --team") {
			t.Errorf("%v should list the whole team:\n%s", c.args, r.out)
		}
	}

	// A free session in the team: only its own items, the team counted.
	free := append(append([]string{}, env...), "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=00000000-0000-4000-8000-000000000178")
	r = sh(t, p, free, "next")
	requireContains(t, r.out, "more item(s) in this team")
	if strings.Contains(r.out, "builder.alice") || strings.Contains(r.out, "builder.bob") {
		t.Errorf("a free session got agents' items:\n%s", r.out)
	}
}

func TestNextSkillLeadsWithSession(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/next/SKILL.md")
	must(t, err)
	s := strings.Join(strings.Fields(string(b)), " ")
	requireContains(t, s, "Lead with this conversation", "`--team`: the whole team's list", "Offer this conversation's next step first")
}

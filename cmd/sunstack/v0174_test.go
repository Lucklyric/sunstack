package main

// v0.17.4 (§37): spawn answers the Codex hook review and Claude's folder
// trust screen after the user's one-time consent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func choices174(t *testing.T, env []string) []string {
	t.Helper()
	var out []string
	files, _ := filepath.Glob(filepath.Join(envValue(env, "SUNSTACK_TEST_EVENTS"), "input-*.jsonl"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, l := range strings.Split(string(b), "\n") {
			if i := strings.Index(l, "choice:"); i >= 0 {
				out = append(out, strings.TrimSuffix(l[i+len("choice:"):], `"}`))
			}
		}
	}
	return out
}

func TestPromptsCommand(t *testing.T) {
	p, env := team086(t)
	requireContains(t, sh(t, p, env, "prompts").out, "codex hooks: ask", "claude folders: ask")
	agent := append(append([]string{}, env...), "CLAUDECODE=1")
	r := sh(t, p, agent, "prompts", "--codex-hooks", "on")
	expect(t, r, 2, "an agent sets prompts")
	requireContains(t, r.stderr, "user_only")
	expect(t, sh(t, p, env, "prompts", "--codex-hooks", "on", "--claude-folders", "off"), 0, "the user sets prompts")
	requireContains(t, sh(t, p, env, "prompts").out, "codex hooks: on", "claude folders: off")
	expect(t, sh(t, p, env, "prompts", "--codex-hooks", "maybe"), 2, "a bad value")
}

func TestSpawnAnswersTrustScreens(t *testing.T) {
	for _, c := range []struct {
		tool, mode, flag, value, want string
		answered                      bool
	}{
		{"codex", "codexhooks", "--codex-hooks", "on", "Trust all and continue", true},
		{"codex", "codexhooks", "--codex-hooks", "ask", "", false},
		{"claude", "claudetrust", "--claude-folders", "on", "Yes, I trust this folder", true},
		{"claude", "claudetrust", "--claude-folders", "off", "", false},
	} {
		t.Run(c.tool+"-"+c.value, func(t *testing.T) {
			p := fixture(t)
			s := privateTmux(t, p, isolatedEnv(t))
			expect(t, sh(t, p, s.env, "prompts", c.flag, c.value), 0, "prompts")
			s.tmux(t, "set-environment", "-g", "SUNSTACK_TEST_MODE", c.mode)
			start := time.Now()
			r := sh(t, p, s.env, "spawn", "reviewer", "--tool", c.tool, "--place", "here")
			expect(t, r, 0, "spawn")
			if d := time.Since(start); d > 20*time.Second {
				t.Errorf("spawn took %s", d)
			}
			got := choices174(t, s.env)
			if c.answered {
				if len(got) != 1 || got[0] != c.want {
					t.Errorf("choices %v, want [%s]", got, c.want)
				}
				requireContains(t, r.out, "answered")
				return
			}
			if len(got) != 0 {
				t.Errorf("answered without consent: %v", got)
			}
			requireContains(t, r.out, "waiting at")
			if hint := strings.Contains(r.out, "sunstack prompts "+c.flag+" on"); hint != (c.value == "ask") {
				t.Errorf("hint to turn it on shown=%v for %s: %s", hint, c.value, r.out)
			}
		})
	}
}

func TestSpawnSkillPrompts(t *testing.T) {
	b, err := os.ReadFile("../../plugins/sunstack/skills/spawn/SKILL.md")
	must(t, err)
	s := strings.Join(strings.Fields(string(b)), " ")
	requireContains(t, s, "trusts every current hook of every plugin, not only Sunstack's", "`sunstack prompts --codex-hooks <on|off>`", "in their own terminal")
	if strings.Contains(s, "it asks for approval") {
		t.Error("spawn skill has the agent run user-only sunstack prompts")
	}
}

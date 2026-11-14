package core

import "testing"

func TestSlugAndTitle(t *testing.T) {
	for in, want := range map[string]string{
		"WebShop":                         "webshop",
		"Web Shop!":                       "web-shop",
		"--a__b--":                        "a-b",
		"":                                "project",
		"a-very-long-project-name-indeed": "a-very-long-project",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Title("webshop", "builder.alice", "ci-flake"); got != "webshop/builder.alice:ci-flake" {
		t.Errorf("agent title %q", got)
	}
	if got := Title("webshop", "codex-1", ""); got != "webshop/codex-1" {
		t.Errorf("free title %q", got)
	}
}

func TestCLIName(t *testing.T) {
	for id, want := range map[string]string{
		"builder.codex": "codex", "builder.codex-2": "codex", "reviewer.claude": "claude",
		"builder.core": "", "builder.codexlike": "", "codex": "", "builder.alice": "",
	} {
		if got := CLIName(id); got != want {
			t.Errorf("CLIName(%q) = %q, want %q", id, got, want)
		}
	}
}

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

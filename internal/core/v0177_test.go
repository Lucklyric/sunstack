package core

import (
	"strings"
	"testing"
)

// The folded drift row names the list's own expansion command (§39).
func TestNextTextDriftKeepsScope(t *testing.T) {
	items := []Issue{{"drift", "a", "x", "fix"}, {"drift", "b", "y", "fix"}}
	if out := NextText(items, 5, "sunstack next --team --all"); !strings.Contains(out, "do: sunstack next --team --all lists them") {
		t.Errorf("drift row lost the scope:\n%s", out)
	}
}

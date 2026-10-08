package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex parses skill frontmatter as YAML and skips a skill it cannot read:
// an unquoted value must not hold ": " or " #".
func TestSkillFrontmatterIsYAML(t *testing.T) {
	files, _ := filepath.Glob("../../plugins/*/skills/*/SKILL.md")
	if len(files) == 0 {
		t.Fatal("no skills found")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(string(b), "---", 3)
		if len(parts) < 3 {
			t.Errorf("%s: no frontmatter", f)
			continue
		}
		for _, line := range strings.Split(parts[1], "\n") {
			_, v, ok := strings.Cut(line, ": ")
			if !ok || strings.HasPrefix(v, "'") || strings.HasPrefix(v, "\"") || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
				continue
			}
			if strings.Contains(v, ": ") || strings.Contains(v, " #") {
				t.Errorf("%s: quote this value: %.60s", f, line)
			}
		}
	}
}

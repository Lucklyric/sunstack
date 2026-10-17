package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// The verify skill hires from the verifier template when no agent fits.
func TestVerifierTemplate(t *testing.T) {
	b, ok := Template("verifier")
	if !ok {
		t.Fatal("no verifier template")
	}
	for _, want := range []string{"title: verifier", "from: verifier@" + LibraryVersion, "## Role", "## How I work", "## Context policy"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("verifier template lacks %q", want)
		}
	}
	if _, err := os.Stat("../../plugins/sunstack/skills/verify/SKILL.md"); err != nil {
		t.Error("no verify skill")
	}
}

// Every skill named in the skills, the READMEs and the CLI's messages exists,
// and each skill's name matches its folder.
func TestSkillReferencesExist(t *testing.T) {
	dirs, _ := filepath.Glob("../../plugins/sunstack/skills/*/SKILL.md")
	have := map[string]bool{}
	for _, f := range dirs {
		name := filepath.Base(filepath.Dir(f))
		have[name] = true
		b, _ := os.ReadFile(f)
		if !strings.Contains(string(b), "\nname: "+name+"\n") {
			t.Errorf("%s: name is not %q", f, name)
		}
	}
	// A skill is named after the CLI command it runs.
	for _, n := range []string{"health", "hire", "send"} {
		if !have[n] {
			t.Errorf("no %s skill", n)
		}
	}
	files := append(dirs, "../../README.md", "project/README.md", "project/PROTOCOL.md")
	for _, root := range []string{"../../cmd", "../../internal"} {
		filepath.WalkDir(root, func(f string, d fs.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") && !strings.Contains(f, "testdata") {
				files = append(files, f)
			}
			return nil
		})
	}
	ref := regexp.MustCompile("(?:(?:the |use |\\()([a-z]+) skill\\b|skills? `([a-z]+)`|[/$]sunstack:([a-z]+))")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			name := m[1] + m[2] + m[3]
			if !have[name] {
				t.Errorf("%s names a skill that does not exist: %q", f, m[0])
			}
		}
	}
}

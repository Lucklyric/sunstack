package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func vgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeSkill(t *testing.T, root, name, body string) {
	t.Helper()
	d := filepath.Join(root, ".claude", "skills", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A check is flagged once a covered file is committed after its sync
// marker, edited, or gone; untouched or uncovered changes are not (§44).
func TestVerifyDrift(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	root := filepath.Join(repo, "proj") // a project inside its repository
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src"), 0o755))
	must(os.WriteFile(filepath.Join(root, "src", "lease.go"), []byte("package src\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "other.go"), []byte("package x\n"), 0o644))
	vgit(t, repo, "init", "-q")
	vgit(t, repo, "add", "-A")
	vgit(t, repo, "commit", "-qm", "one")
	sync := vgit(t, repo, "rev-parse", "HEAD")
	writeSkill(t, root, "verify-lease", "---\nname: verify-lease\n---\n# Lease\n\n## Coverage\n\n- sync: `"+sync+"`\n- covers: `src/lease.go`\n")
	writeSkill(t, root, "verify-app", "---\nname: verify-app\n---\n# Driving the app\n\n## Launch\nsync: none\n") // a driving skill, no record
	if got := VerifyDrift(root); len(got) != 0 {
		t.Fatalf("fresh check flagged: %+v", got)
	}

	must(os.WriteFile(filepath.Join(root, "other.go"), []byte("package x // changed\n"), 0o644))
	vgit(t, repo, "commit", "-qam", "uncovered")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Errorf("an uncovered change flagged: %+v", got)
	}

	must(os.WriteFile(filepath.Join(root, "src", "lease.go"), []byte("package src // edited\n"), 0o644))
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "verify-lease") || !strings.Contains(got[0].Text, "src/lease.go") || got[0].Kind != "drift" {
		t.Errorf("uncommitted edit: %+v", got)
	}
	vgit(t, repo, "commit", "-qam", "covered")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Do, "maintain") {
		t.Errorf("committed change: %+v", got)
	}

	// A content-hash marker, for files outside git.
	sum := sha256.Sum256([]byte("package src // edited\n"))
	writeSkill(t, root, "verify-lease", "# Lease\n## Coverage\nsync: sha256:"+hex.EncodeToString(sum[:])+"\ncovers: src/lease.go\n")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Errorf("matching hash flagged: %+v", got)
	}
	must(os.Remove(filepath.Join(root, "src", "lease.go")))
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "missing") {
		t.Errorf("missing covered file: %+v", got)
	}

	// A record that cannot be checked says so.
	writeSkill(t, root, "verify-lease", "# Lease\n## Coverage\nsync: 0123456789abcdef\ncovers: other.go\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "not found") {
		t.Errorf("unknown marker: %+v", got)
	}
	writeSkill(t, root, "verify-lease", "# Lease\n## Coverage\nnothing yet\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "no coverage record") {
		t.Errorf("empty record: %+v", got)
	}
}

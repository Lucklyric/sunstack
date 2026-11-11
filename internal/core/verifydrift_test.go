package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func writeCheck(t *testing.T, root, name, body string) {
	t.Helper()
	d := filepath.Join(root, "sunstack", "formal-verify", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "check.md"), []byte(body), 0o644); err != nil {
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
	writeCheck(t, root, "lease", "# lease: one holder at a time\n\n## Coverage\n\n- sync: `"+sync+"`\n- covers: `src/lease.go`\n")
	// An app-driving verify skill is not a formal check and is never read.
	d := filepath.Join(root, ".claude", "skills", "verify-app")
	must(os.MkdirAll(d, 0o755))
	must(os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("# Driving the app\n## Coverage\nnothing\n"), 0o644))
	if got := VerifyDrift(root); len(got) != 0 {
		t.Fatalf("fresh check flagged: %+v", got)
	}

	must(os.WriteFile(filepath.Join(root, "other.go"), []byte("package x // changed\n"), 0o644))
	vgit(t, repo, "commit", "-qam", "uncovered")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Errorf("an uncovered change flagged: %+v", got)
	}

	must(os.WriteFile(filepath.Join(root, "src", "lease.go"), []byte("package src // edited\n"), 0o644))
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "lease") || !strings.Contains(got[0].Text, "src/lease.go") || got[0].Kind != "drift" {
		t.Errorf("uncommitted edit: %+v", got)
	}
	vgit(t, repo, "commit", "-qam", "covered")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Do, "maintain") {
		t.Errorf("committed change: %+v", got)
	}

	// A content-hash marker, for files outside git.
	sum, err := coverHash(root, []string{"src/lease.go"})
	must(err)
	writeCheck(t, root, "lease", "# Lease\n## Coverage\nsync: sha256:"+sum+"\ncovers: src/lease.go\n")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Errorf("matching hash flagged: %+v", got)
	}
	must(os.Remove(filepath.Join(root, "src", "lease.go")))
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "missing") {
		t.Errorf("missing covered file: %+v", got)
	}

	// A record that cannot be checked says so.
	writeCheck(t, root, "lease", "# Lease\n## Coverage\nsync: 0123456789abcdef\ncovers: other.go\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "not found") {
		t.Errorf("unknown marker: %+v", got)
	}
	writeCheck(t, root, "lease", "# Lease\n## Coverage\nnothing yet\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "no coverage record") {
		t.Errorf("empty record: %+v", got)
	}
}

// A checkout older than the marker, and bytes moved between two covered
// files, both count as drift (review of a9e49a7).
func TestVerifyDriftOlderCheckoutAndMovedBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	w := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("a.go", "one\n")
	vgit(t, root, "init", "-q")
	vgit(t, root, "add", "-A")
	vgit(t, root, "commit", "-qm", "one")
	old := vgit(t, root, "rev-parse", "HEAD")
	w("a.go", "two\n")
	vgit(t, root, "commit", "-qam", "two")
	sync := vgit(t, root, "rev-parse", "HEAD")
	vgit(t, root, "checkout", "-q", old)
	writeCheck(t, root, "a", "# a\n## Coverage\nsync: "+sync+"\ncovers: a.go\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "a.go") {
		t.Errorf("older checkout: %+v", got)
	}

	w("x", "ab")
	w("y", "c")
	sum, err := coverHash(root, []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	// The skill's recipe: shasum -a 256 x y | shasum -a 256
	if sum != "4fbc8dfd67807af31f631407b246522d9b44e2c050689f6867b06669d2b32405" {
		t.Errorf("coverHash = %s, not the shasum recipe", sum)
	}
	writeCheck(t, root, "a", "# a\n## Coverage\nsync: sha256:"+sum+"\ncovers: x y\n")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Fatalf("matching hash flagged: %+v", got)
	}
	w("x", "a")
	w("y", "bc")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "content hash") {
		t.Errorf("bytes moved between files: %+v", got)
	}
}

// A covered folder counts every file under it: one edited, deleted, added
// and committed after the marker, or new and not yet tracked; an ignored
// file does not count, and a content hash changes with the file list.
func TestVerifyDriftFolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	root := filepath.Join(repo, "proj")
	w := func(name, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rm := func(name string) {
		t.Helper()
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	w("src/a.go", "a\n")
	w("src/sub/b.go", "b\n")
	w("other.go", "x\n")
	w(".gitignore", "*.log\nsunstack/\n")
	vgit(t, repo, "init", "-q")
	vgit(t, repo, "add", "-A")
	vgit(t, repo, "commit", "-qm", "one")
	sync := vgit(t, repo, "rev-parse", "HEAD")
	flagged := func(covers string, want ...string) {
		t.Helper()
		writeCheck(t, root, "src", "# src\n## Coverage\nsync: "+sync+"\ncovers: "+covers+"\n")
		got := VerifyDrift(root)
		if len(want) == 0 {
			if len(got) != 0 {
				t.Errorf("covers %s flagged: %+v", covers, got)
			}
			return
		}
		if len(got) != 1 {
			t.Fatalf("covers %s: %+v, want %v", covers, got, want)
		}
		for _, s := range want {
			if !strings.Contains(got[0].Text, s) {
				t.Errorf("covers %s: %q lacks %q", covers, got[0].Text, s)
			}
		}
	}
	flagged("src/")
	flagged("src")
	w("other.go", "y\n")
	vgit(t, repo, "commit", "-qam", "uncovered")
	flagged("src/")

	w("src/debug.log", "ignored\n")
	flagged("src/")
	w("src/sub/new file.go", "n\n")
	flagged("src/", "src/sub/new file.go (new)")
	flagged("src", "src/sub/new file.go (new)")
	vgit(t, repo, "add", "-A")
	vgit(t, repo, "commit", "-qm", "added")
	flagged("src/", "src/sub/new file.go")
	vgit(t, root, "rm", "-q", "src/sub/new file.go")
	vgit(t, repo, "commit", "-qm", "back")
	flagged("src/")

	w("src/sub/b.go", "b edited\n")
	flagged("src/", "src/sub/b.go")
	w("src/sub/b.go", "b\n")
	rm("src/a.go")
	flagged("src/", "src/a.go")
	w("src/a.go", "a\n")
	flagged("src/ other.go", "other.go")

	// The content hash: every regular file under the folder in byte order.
	rm("src/debug.log")
	sum, err := coverHash(root, []string{"src/", "other.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("shasum"); err == nil && runtime.GOOS != "windows" {
		// The skill's recipe for a folder (a POSIX shell pipeline).
		cmd := exec.Command("sh", "-c", "{ find src -type f | LC_ALL=C sort; echo other.go; } | tr '\\n' '\\0' | xargs -0 shasum -a 256 | shasum -a 256")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if f := strings.Fields(string(out)); len(f) == 0 || f[0] != sum {
			t.Errorf("coverHash = %s, recipe printed %s", sum, out)
		}
	}
	writeCheck(t, root, "src", "# src\n## Coverage\nsync: sha256:"+sum+"\ncovers: src/ other.go\n")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Fatalf("matching hash flagged: %+v", got)
	}
	w("src/sub/c.go", "c\n")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "content hash") {
		t.Errorf("file added under a hashed folder: %+v", got)
	}
	rm("src/sub/c.go")
	rm("src/sub/b.go")
	if got := VerifyDrift(root); len(got) != 1 || !strings.Contains(got[0].Text, "content hash") {
		t.Errorf("file removed under a hashed folder: %+v", got)
	}
}

// sunstack.json moves the skill data folder; a path leaving the project is
// ignored.
func TestSkillDataDir(t *testing.T) {
	root := t.TempDir()
	if got := SkillDataDir(root); got != filepath.Join(root, "sunstack") {
		t.Errorf("default = %s", got)
	}
	for body, want := range map[string]string{
		`{"data": "docs/agents"}`: filepath.Join(root, "docs", "agents"),
		`{"data": "../outside"}`:  filepath.Join(root, "sunstack"),
		`not json`:                filepath.Join(root, "sunstack"),
	} {
		if err := os.WriteFile(filepath.Join(root, "sunstack.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := SkillDataDir(root); got != want {
			t.Errorf("%s: %s, want %s", body, got, want)
		}
	}
	os.WriteFile(filepath.Join(root, "sunstack.json"), []byte(`{"data": "docs/agents"}`), 0o644)
	writeDataCheck := filepath.Join(root, "docs", "agents", "formal-verify", "x")
	os.MkdirAll(writeDataCheck, 0o755)
	os.WriteFile(filepath.Join(writeDataCheck, "check.md"), []byte("# x\n## Coverage\nnothing\n"), 0o644)
	if got := VerifyDrift(root); len(got) != 1 {
		t.Errorf("check in the picked folder not read: %+v", got)
	}
}

// init turns a sunstack/ that holds only skill data into a team, and still
// refuses one that holds anything else.
func TestInitOverSkillData(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sunstack", "formal-verify", "lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root, false); err != nil {
		t.Fatalf("init over skill data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sunstack", "PROTOCOL.md")); err != nil {
		t.Error("no PROTOCOL.md after init")
	}
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "sunstack", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(other, false); err == nil {
		t.Error("init took over a sunstack/ folder that is not skill data")
	}
}

// Links never carry skill data or team files outside the project, and an
// absolute data path is not used (review of abfdfb4).
func TestSkillDataLinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "verify", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skip("no symlinks here")
	}
	for body, want := range map[string]string{
		`{"data": "linked"}`:                            "",
		`{"data": "` + filepath.ToSlash(outside) + `"}`: "",
	} {
		if err := os.WriteFile(filepath.Join(root, "sunstack.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := SkillDataDir(root); got != want && got != filepath.Join(root, "sunstack") {
			t.Errorf("%s: %q", body, got)
		}
	}
	os.Remove(filepath.Join(root, "sunstack.json"))
	if err := os.Symlink(outside, filepath.Join(root, "sunstack")); err != nil {
		t.Fatal(err)
	}
	if got := SkillDataDir(root); got != "" {
		t.Errorf("linked sunstack/ used: %q", got)
	}
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	if _, err := Init(root, false); err == nil {
		t.Error("init wrote team files through a linked sunstack/")
	}
	if _, err := os.Stat(filepath.Join(outside, "PROTOCOL.md")); err == nil {
		t.Error("PROTOCOL.md written outside the project")
	}
}

// Recheck of 31f53a1: any linked sunstack/ is refused by init, and a broken
// or circular link in the data path is not taken as a folder yet to come.
func TestSkillDataLinkEdges(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	for name, fill := range map[string]func(dir string){
		"empty":    func(string) {},
		"protocol": func(dir string) { os.WriteFile(filepath.Join(dir, "PROTOCOL.md"), []byte("x"), 0o644) },
	} {
		root, outside := t.TempDir(), t.TempDir()
		fill(outside)
		if err := os.Symlink(outside, filepath.Join(root, "sunstack")); err != nil {
			t.Skip("no symlinks here")
		}
		if _, err := Init(root, false); err == nil {
			t.Errorf("%s: init through a linked sunstack/", name)
		}
		if _, err := os.Stat(filepath.Join(outside, "README.md")); err == nil {
			t.Errorf("%s: team files written outside the project", name)
		}
	}
	root, outside := t.TempDir(), t.TempDir()
	os.Symlink(filepath.Join(outside, "not-created"), filepath.Join(root, "dangling"))
	os.Symlink(filepath.Join(root, "loop"), filepath.Join(root, "loop"))
	for _, data := range []string{"dangling/child", "loop", "loop/child"} {
		os.WriteFile(filepath.Join(root, "sunstack.json"), []byte(`{"data": "`+data+`"}`), 0o644)
		if got := SkillDataDir(root); got != filepath.Join(root, "sunstack") {
			t.Errorf("%s: %q", data, got)
		}
	}
}

// A covers path is one spelling, with or without a leading ./, and a name
// shasum would print escaped cannot be covered by a content hash.
func TestCoverSpelling(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("a\n"), 0o644)
	sum, err := coverHash(root, []string{"src/"})
	if err != nil {
		t.Fatal(err)
	}
	writeCheck(t, root, "s", "# s\n## Coverage\nsync: sha256:"+sum+"\ncovers: ./src/\n")
	if got := VerifyDrift(root); len(got) != 0 {
		t.Errorf("./src/ flagged: %+v", got)
	}
	if runtime.GOOS == "windows" {
		return // no backslash in a file name there
	}
	os.WriteFile(filepath.Join(root, "src", `b\c`), []byte("b\n"), 0o644)
	if _, err := coverHash(root, []string{"src/"}); err == nil || !strings.Contains(err.Error(), "commit marker") {
		t.Errorf("a backslash name was hashed: %v", err)
	}
}

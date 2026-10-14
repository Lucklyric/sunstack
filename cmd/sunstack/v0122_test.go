package main

// v0.12.2: fixes from the code review.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tidy never rebuilds an archive it could not read: the old entries would be
// lost and the board would still drop the new ones.
func TestTidyUnreadableArchive(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs file modes that deny reading")
	}
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, home, "init"), 0, "init")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire")
	tok := field(tokenRe, sh(t, p, home, "as", "builder.alice").out)
	r := sh(t, p, home, "snapshot", "builder.alice", "board.md", "--token", tok)
	cand := filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", tok, "board.md")
	must(t, os.WriteFile(cand, []byte("## Now\n## Next\n## Done\n- 2026-01-02 KR3 [O1] Newer work\n"), 0o644))
	expect(t, sh(t, p, home, "commit", "builder.alice", "board.md", cand, field(sumRe, r.out), "--token", tok), 0, "commit board")

	dir := filepath.Join(p, "sunstack", "builder.alice", "archive")
	must(t, os.MkdirAll(dir, 0o755))
	archive := filepath.Join(dir, "2026-01.md")
	must(t, os.WriteFile(archive, []byte("## Board\n- 2026-01-01 KR0 [O1] Older work\n"), 0o000))
	t.Cleanup(func() { os.Chmod(archive, 0o644) })

	r = sh(t, p, home, "tidy", "builder.alice")
	if r.code == 0 {
		t.Errorf("tidy should fail on an unreadable archive: %s", r.out)
	}
	must(t, os.Chmod(archive, 0o644))
	if a := readText(t, archive); !strings.Contains(a, "Older work") || strings.Contains(a, "Newer work") {
		t.Errorf("archive changed:\n%s", a)
	}
	if b := readText(t, filepath.Join(p, "sunstack", "builder.alice", "board.md")); !strings.Contains(b, "Newer work") {
		t.Errorf("board lost its entry:\n%s", b)
	}
}

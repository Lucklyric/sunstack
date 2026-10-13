package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Three sessions of one agent add a key result each from the same board and
// merge: the one that picked a number already taken, or archived, is refused.
func TestBoardKeyClash(t *testing.T) {
	p := fixture(t)
	toks := []string{field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--task", "a").out)}
	for _, task := range []string{"b", "c"} {
		toks = append(toks, field(tokenRe, sh(t, p, nil, "as", "builder.alice", "--join", "--task", task).out))
	}
	tmp := func(i int) string { return filepath.Join(p, "sunstack", "_local", "tmp", "builder.alice", toks[i]) }
	board := "## Now\n- 2026-10-12 KR1 [O1] old work\n"
	arch := filepath.Join(p, "sunstack", "builder.alice", "archive")
	must(t, os.MkdirAll(arch, 0o755))
	must(t, os.WriteFile(filepath.Join(arch, "2026-09.md"), []byte("## Done\n- 2026-09-01 KR2 [O1] archived work\n"), 0o644))
	commit := func(i int, body string) result {
		sum := field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", toks[i]).out)
		f := filepath.Join(tmp(i), "board.md")
		must(t, os.WriteFile(f, []byte(body), 0o644))
		return sh(t, p, nil, "commit", "builder.alice", "board.md", f, sum, "--token", toks[i])
	}
	expect(t, commit(0, board), 0, "seed")
	expect(t, commit(0, board+"- 2026-10-12 KR3 [O1] task a\n"), 0, "session a adds KR3")
	r := commit(1, board+"- 2026-10-12 KR3 [O1] task a\n- 2026-10-12 KR3 [O1] task b\n")
	expect(t, r, 1, "session b merges a second KR3")
	if !strings.Contains(r.stderr, "KR4") {
		t.Errorf("refusal should name the next free key: %s", r.stderr)
	}
	expect(t, commit(2, board+"- 2026-10-12 KR3 [O1] task a\n- 2026-10-12 KR2 [O1] task c\n"), 1, "session c reuses archived KR2")
	expect(t, commit(2, board+"- 2026-10-12 KR3 [O1] task a\n- 2026-10-12 KR4 [O1] task c\n- 2026-10-12 Q1 ask c\n"), 0, "KR4 and Q1 are new")

	// A stale snapshot is a mismatch first, never a key clash: KR1 was
	// archived by another session after this one read the board.
	cur := "- 2026-10-12 KR3 [O1] task a\n- 2026-10-12 KR4 [O1] task c\n- 2026-10-12 Q1 ask c\n"
	stale := field(sumRe, sh(t, p, nil, "snapshot", "builder.alice", "board.md", "--token", toks[1]).out)
	must(t, os.WriteFile(filepath.Join(arch, "2026-10.md"), []byte("## Done\n- 2026-10-12 KR1 [O1] old work\n"), 0o644))
	expect(t, commit(2, "## Now\n"+cur), 0, "session c archives KR1")
	f := filepath.Join(tmp(1), "stale.md")
	must(t, os.WriteFile(f, []byte(board+cur+"- 2026-10-12 KR5 [O1] task b\n"), 0o644))
	expect(t, sh(t, p, nil, "commit", "builder.alice", "board.md", f, stale, "--token", toks[1]), 3, "stale snapshot is a mismatch")
}

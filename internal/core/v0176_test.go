package core

// v0.17.6 (§38) review: the title reader's unknown cases and memory bound.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSessionTitleUnknownAndBounded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sid := "00000000-0000-4000-8000-000000000384"

	// A Codex index path that cannot be read: unknown.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(home, ".codex", "session_index.jsonl"), 0o755))
	if _, known := SessionTitle("codex", sid); known {
		t.Error("an unreadable Codex index counted as known")
	}

	// A huge non-title line is skipped without holding it in memory.
	path := filepath.Join(home, ".claude", "projects", "x", sid+".jsonl")
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	big := `{"type":"assistant","message":{"content":"` + strings.Repeat("x", 64<<20) + `"}}`
	must(os.WriteFile(path, []byte(big+"\n"+`{"type":"custom-title","customTitle":"t1","sessionId":"`+sid+`"}`+"\n"), 0o600))
	big = ""
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	title, known := SessionTitle("claude", sid)
	runtime.ReadMemStats(&after)
	if title != "t1" || !known {
		t.Errorf("title %q known %v, want t1 true", title, known)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 16<<20 {
		t.Errorf("the lookup allocated %d MiB for a 64 MiB line", got>>20)
	}
}

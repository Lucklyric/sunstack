package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// A wrap note is the resume note the wrap skill leaves before a compaction
// (§52): host-local, one per folder and session, under ~/.sunstack/wrap/.

// WrapNoteDir is this host's folder of wrap notes for one working folder,
// named after the folder with a hash of its resolved path.
func WrapNoteDir(folder string) string {
	abs, err := filepath.Abs(folder)
	if err != nil {
		abs = folder
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	key := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	name := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, filepath.Base(key))
	if len(name) > 40 {
		name = name[:40]
	}
	return filepath.Join(Home(), "wrap", name+"-"+hex.EncodeToString(sum[:4]))
}

// WrapNote is a found note: its path, when it was written, and whether
// another session wrote it.
type WrapNote struct {
	Path  string
	Saved time.Time
	Other bool
}

// FindWrapNote returns this session's note for the folder, else the newest
// note any session left there. It only stats files, so the prompt hook can
// call it.
func FindWrapNote(folder, sid string) (WrapNote, bool) {
	dir := WrapNoteDir(folder)
	if IsSessionID(sid) {
		p := filepath.Join(dir, sid+".md")
		if fi, err := os.Stat(p); err == nil {
			return WrapNote{Path: p, Saved: fi.ModTime()}, true
		}
	}
	var best WrapNote
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if best.Path == "" || fi.ModTime().After(best.Saved) {
			best = WrapNote{Path: filepath.Join(dir, e.Name()), Saved: fi.ModTime(), Other: true}
		}
	}
	return best, best.Path != ""
}

// SaveWrapNote writes this session's note for the folder, replacing the last
// one, and returns its path.
func SaveWrapNote(folder, sid string, data []byte) (string, error) {
	if !IsSessionID(sid) {
		return "", fail(ExitUsage, "no_session", "this CLI did not give its session ID (CLAUDE_CODE_SESSION_ID or CODEX_THREAD_ID)")
	}
	dir := WrapNoteDir(folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, sid+".md")
	return p, writeAtomicMode(p, data, 0o600)
}

// WrapNoteCommits reads the "commit <repo>: <rev>" header lines of a note.
func WrapNoteCommits(body string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(body, "\n") {
		if l == "" || l == "---" {
			break
		}
		if rest, ok := strings.CutPrefix(l, "commit "); ok {
			if repo, rev, ok := strings.Cut(rest, ": "); ok {
				out[repo] = strings.TrimSpace(rev)
			}
		}
	}
	return out
}

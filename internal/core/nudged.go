package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A nudge typed into a pane leaves a marker with its time; while it is
// younger than nudgeHold, later messages are not typed into that pane, so a
// burst of messages costs one check (design §30.2).
const nudgeHold = 5 * time.Minute

func (p *Project) nudgedPath(id, token string) string { return p.local("nudged", id, token) }

func hostNudgedPath(sid string) string { return filepath.Join(hostInboxDir(sid), "nudged") }

func markNudged(path string) {
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
	}
}

// nudgeWaiting reports whether the marker at path holds a fresh nudge.
func nudgeWaiting(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	return err == nil && time.Since(at) < nudgeHold
}

// clearNudged removes a session's marker and its agent folder once empty.
func (p *Project) clearNudged(id, token string) {
	os.Remove(p.nudgedPath(id, token))
	os.Remove(filepath.Dir(p.nudgedPath(id, token)))
}

// takenIn counts the messages in a taken folder and finds the oldest one's
// time (a take stamps the file).
func takenIn(dir string) (int, time.Time) {
	var oldest time.Time
	names := listNames(dir, ".md")
	for _, n := range names {
		if fi, err := os.Stat(filepath.Join(dir, n)); err == nil && (oldest.IsZero() || fi.ModTime().Before(oldest)) {
			oldest = fi.ModTime()
		}
	}
	return len(names), oldest
}

// TakenAge names the age of the oldest taken message once it is over a day.
func TakenAge(oldest time.Time) string {
	d := int(time.Since(oldest).Hours() / 24)
	switch {
	case oldest.IsZero() || d < 1:
		return ""
	case d == 1:
		return " (oldest taken 1 day ago)"
	}
	return fmt.Sprintf(" (oldest taken %d days ago)", d)
}

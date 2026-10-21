package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The issue log (§26): what went wrong on this host, one JSON line each, in
// ~/.sunstack/log/issues.jsonl. Never arguments, bodies, prompts or tokens.

const (
	issueMax     = 512 << 10 // past this the file moves to issues.1.jsonl
	issueMsgMax  = 300
	issueNoteMax = 500
	issueCodeMax = 40
	issueRepeat  = 10 * time.Minute // a repeating failure writes once per window
)

// IssueVersion is this binary's version, set by main.
var IssueVersion string

// IssueEntry is one entry. N counts the occurrences it stands for when over one.
type IssueEntry struct {
	At      string `json:"at"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source"` // cli, connector or note
	Cmd     string `json:"cmd,omitempty"`
	Code    string `json:"code"`
	Exit    int    `json:"exit,omitempty"`
	Msg     string `json:"msg"`
	N       int    `json:"n,omitempty"`
	Team    string `json:"team,omitempty"`
	Session string `json:"session,omitempty"`
}

var issueMu sync.Mutex

func issuePath() string { return filepath.Join(Home(), "log", "issues.jsonl") }

// RecordIssue appends e. Best effort: a failure here never fails a command.
func RecordIssue(e IssueEntry) {
	if e.At == "" {
		e.At = now()
	}
	e.Version = IssueVersion
	max := issueMsgMax
	if e.Source == "note" {
		max = issueNoteMax
	}
	if r := []rune(e.Msg); len(r) > max {
		e.Msg = string(r[:max])
	}
	if len(e.Code) > issueCodeMax {
		e.Code = e.Code[:issueCodeMax]
	}
	if e.Session == "" {
		e.Session = os.Getenv("CLAUDE_CODE_SESSION_ID")
		if e.Session == "" {
			e.Session = os.Getenv("CODEX_THREAD_ID")
		}
	}
	if len(e.Session) > 64 {
		e.Session = "" // not a session ID
	}
	line, err := json.Marshal(e)
	if err != nil || len(line) > 8<<10 { // every field is bounded; never past the reader
		return
	}
	issueMu.Lock()
	defer issueMu.Unlock()
	path := issuePath()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	// Other sunstack processes write here too: rotate and append under one lock.
	unlock, err := LockDir(filepath.Join(filepath.Dir(path), "issues.lock"), "the issue log")
	if err != nil {
		return
	}
	defer unlock()
	if fi, err := os.Stat(path); err == nil && fi.Size()+int64(len(line)) > issueMax {
		if os.Rename(path, filepath.Join(filepath.Dir(path), "issues.1.jsonl")) != nil {
			return // never past the bound
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// ReadIssues returns the entries at or after since, oldest first.
func ReadIssues(since time.Time) []IssueEntry {
	var out []IssueEntry
	dir := filepath.Dir(issuePath())
	for _, name := range []string{"issues.1.jsonl", "issues.jsonl"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), issueMax) // any entry the writer accepts
		for sc.Scan() {
			var e IssueEntry
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			if t, err := time.Parse(time.RFC3339, e.At); err == nil && !t.Before(since) {
				out = append(out, e)
			}
		}
		f.Close()
	}
	return out
}

// IssueCounts counts the occurrences per code since a time.
func IssueCounts(since time.Time) map[string]int {
	c := map[string]int{}
	for _, e := range ReadIssues(since) {
		c[e.Code] += max(e.N, 1)
	}
	return c
}

// IssueLimiter writes a repeating issue once per window per code, with the
// occurrences since the last entry in N. For long-running loops.
type IssueLimiter struct {
	mu      sync.Mutex
	last    map[string]time.Time
	n       map[string]int
	pending map[string]IssueEntry // the latest one held back, per code
}

// Record notes e at time at.
func (l *IssueLimiter) Record(e IssueEntry, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last, l.n, l.pending = map[string]time.Time{}, map[string]int{}, map[string]IssueEntry{}
	}
	l.n[e.Code]++
	if t, ok := l.last[e.Code]; ok && at.Sub(t) < issueRepeat {
		l.pending[e.Code] = e
		return
	}
	l.write(e, at)
}

// Flush writes what was held back for a code whose window has passed, so the
// end of a burst is counted without waiting for the next failure.
func (l *IssueLimiter) Flush(at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for code, e := range l.pending {
		if at.Sub(l.last[code]) >= issueRepeat {
			l.write(e, at)
		}
	}
}

func (l *IssueLimiter) write(e IssueEntry, at time.Time) {
	if l.n[e.Code] > 1 {
		e.N = l.n[e.Code]
	}
	e.At = at.UTC().Format("2006-01-02T15:04:05Z")
	l.last[e.Code], l.n[e.Code] = at, 0
	delete(l.pending, e.Code)
	RecordIssue(e)
}

// IssueLine is a host's issue counts as one line, most frequent first (§26).
func IssueLine(c map[string]int) string {
	if len(c) == 0 {
		return ""
	}
	var codes []string
	for k := range c {
		codes = append(codes, k)
	}
	sort.Slice(codes, func(i, j int) bool {
		if c[codes[i]] != c[codes[j]] {
			return c[codes[i]] > c[codes[j]]
		}
		return codes[i] < codes[j]
	})
	for i, k := range codes {
		codes[i] = fmt.Sprintf("%s %d", k, c[k])
	}
	return "issues (7 days): " + strings.Join(codes, ", ")
}

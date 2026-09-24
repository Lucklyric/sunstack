// Package core implements Sunstack's file protocol: project discovery, names,
// the per-ID lock, claims, snapshot/commit and the event log (design §4-§8).
package core

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ProtocolVersion changes only when file formats or exit codes change.
const ProtocolVersion = 2

// Exit codes (design §8).
const (
	ExitOK       = 0
	ExitFail     = 1 // filesystem, permission or content error
	ExitUsage    = 2 // usage error, or missing_arguments
	ExitMismatch = 3 // snapshot mismatch
	ExitClaim    = 4 // busy | token | occupied | occupied_same_pane
)

// Error carries an exit code, a machine-readable reason and a message.
// Stdout is optional extra output (choices, a claim line, a fresh snapshot)
// that must be printed before the error.
type Error struct {
	Code   int
	Reason string
	Msg    string
	Stdout string
}

func (e *Error) Error() string { return e.Reason + ": " + e.Msg }

func fail(code int, reason, format string, a ...any) *Error {
	return &Error{Code: code, Reason: reason, Msg: fmt.Sprintf(format, a...)}
}

var partRe = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// ValidPart checks a title, name or thread topic.
func ValidPart(s string) bool { return partRe.MatchString(s) }

// ValidID checks <title> or <title>.<name>.
func ValidID(id string) bool {
	parts := strings.Split(id, ".")
	switch len(parts) {
	case 1:
		return ValidPart(parts[0])
	case 2:
		return ValidPart(parts[0]) && ValidPart(parts[1])
	}
	return false
}

// Project is a directory holding sunstack/.
type Project struct {
	Root string // project root (absolute, symlinks resolved)
	Dir  string // <root>/sunstack
}

// FindProject walks up from start to the nearest directory holding a
// sunstack/ folder with PROTOCOL.md.
func FindProject(start string) (*Project, error) {
	if start == "" {
		var err error
		if start, err = os.Getwd(); err != nil {
			return nil, fail(ExitFail, "no_root", "%v", err)
		}
	}
	d, err := filepath.Abs(start)
	if err == nil {
		d, err = filepath.EvalSymlinks(d)
	}
	if err != nil {
		return nil, fail(ExitFail, "no_root", "cannot enter %s", start)
	}
	for {
		if st, err := os.Lstat(filepath.Join(d, "sunstack")); err == nil {
			if st.Mode()&os.ModeSymlink != 0 {
				return nil, fail(ExitFail, "symlink", "%s is a symlink; sunstack/ must be a real directory", filepath.Join(d, "sunstack"))
			}
			// A folder named sunstack is a team only once init has put
			// PROTOCOL.md in it; any other folder of that name is skipped.
			if _, err := os.Stat(filepath.Join(d, "sunstack", "PROTOCOL.md")); err == nil && st.IsDir() {
				return &Project{Root: d, Dir: filepath.Join(d, "sunstack")}, nil
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return nil, fail(ExitFail, "no_root", "no sunstack/ team (with PROTOCOL.md) found from %s upward; run sunstack init first", start)
		}
		d = parent
	}
}

// noSymlink rejects path if it, or any directory between the project root and
// it, is a symlink, so writes can never leave the project.
func (p *Project) noSymlink(path string) error {
	rel, err := filepath.Rel(p.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fail(ExitFail, "outside", "%s is outside the project", path)
	}
	cur := p.Root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		st, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil // the rest does not exist yet
			}
			return fail(ExitFail, "fs", "%v", err)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fail(ExitFail, "symlink", "refusing symlinked path: %s", cur)
		}
	}
	return nil
}

func (p *Project) local(parts ...string) string {
	return filepath.Join(append([]string{p.Dir, "_local"}, parts...)...)
}

// AgentDir returns sunstack/<id>.
func (p *Project) AgentDir(id string) string { return filepath.Join(p.Dir, id) }

// HasAgent reports whether sunstack/<id>/AGENT.md exists.
func (p *Project) HasAgent(id string) bool {
	_, err := os.Stat(filepath.Join(p.AgentDir(id), "AGENT.md"))
	return err == nil
}

// Agents lists hired IDs in directory order.
func (p *Project) Agents() []string {
	entries, _ := os.ReadDir(p.Dir)
	var ids []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), "_") && p.HasAgent(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	return ids
}

// Checksum of content; "absent" stands for a missing file.
func Checksum(b []byte, exists bool) string {
	if !exists {
		return "absent"
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:16])
}

// readMaybe returns the file content and whether it exists.
func readMaybe(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

var conflictRe = regexp.MustCompile(`(?m)^(<<<<<<<|>>>>>>>)( |$)|^=======$`)

// HasConflictMarkers reports git merge conflict markers in content.
func HasConflictMarkers(b []byte) bool { return conflictRe.Match(b) }

func fileHasConflict(path string) bool {
	b, ok, _ := readMaybe(path)
	return ok && HasConflictMarkers(b)
}

// NewToken returns 16 hex characters of randomness.
func NewToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// writeAtomic writes via a temporary file in the same directory, then renames.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// LogEvent appends one line to _local/log/events.log. Best effort: a logging
// failure never fails the command.
func (p *Project) LogEvent(event, subject string, fields ...string) {
	dir := p.local("log")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %-8s %s", now(), event, subject)
	for _, x := range fields {
		b.WriteString("  " + x)
	}
	b.WriteByte('\n')
	_, _ = f.Write(b.Bytes())
}

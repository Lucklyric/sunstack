package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// targetPath allows only context.md and threads/<topic>.md (design §8.4).
func (p *Project) targetPath(id, rel string) (string, error) {
	switch {
	case rel == "context.md":
	case strings.HasPrefix(rel, "threads/") && strings.HasSuffix(rel, ".md"):
		topic := strings.TrimSuffix(strings.TrimPrefix(rel, "threads/"), ".md")
		if !ValidPart(topic) {
			return "", fail(ExitUsage, "usage", "invalid thread topic: %s", topic)
		}
	default:
		return "", fail(ExitUsage, "usage", "target must be context.md or threads/<topic>.md, got: %s", rel)
	}
	path := filepath.Join(p.AgentDir(id), filepath.FromSlash(rel))
	for _, x := range []string{path, filepath.Join(p.AgentDir(id), "threads")} {
		if st, err := os.Lstat(x); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fail(ExitFail, "symlink", "refusing symlinked path: %s", x)
		}
	}
	return path, nil
}

// TmpDir is where snapshots are described from and candidates must live.
func (p *Project) TmpDir(id string) string { return p.local("tmp", id) }

func snapshotText(sum, candDir string, content []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "checksum: %s\ncandidate_dir: %s\n----- content -----\n", sum, candDir)
	b.Write(content)
	return b.String()
}

// Snapshot reads the target once and returns its content with the checksum of
// exactly those bytes (design §8.1).
func (p *Project) Snapshot(id, rel, token string) (string, error) {
	if !ValidID(id) {
		return "", fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	path, err := p.targetPath(id, rel)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fail(ExitClaim, "token", "missing --token")
	}
	if cur := p.ReadLive(id); cur == nil || cur.Token != token {
		return "", fail(ExitClaim, "token", "token does not match the current claim on %s", id)
	}
	b, ok, err := readMaybe(path)
	if err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if HasConflictMarkers(b) {
		return "", fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", rel)
	}
	if err := os.MkdirAll(p.TmpDir(id), 0o755); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	return snapshotText(Checksum(b, ok), p.TmpDir(id), b), nil
}

// CommitOptions are the inputs of `sunstack commit`.
type CommitOptions struct {
	ID, Rel, Candidate, Sum, Token string
	Delete                         bool
}

// Commit replaces (or deletes) the target if it still matches the snapshot
// checksum, all under the per-ID lock (design §8.3-§8.6).
func (p *Project) Commit(o CommitOptions) error {
	if !ValidID(o.ID) {
		return fail(ExitUsage, "usage", "invalid id: %s", o.ID)
	}
	path, err := p.targetPath(o.ID, o.Rel)
	if err != nil {
		return err
	}
	var cand []byte
	if !o.Delete {
		cdir, err1 := filepath.EvalSymlinks(filepath.Dir(o.Candidate))
		tdir, err2 := filepath.EvalSymlinks(p.TmpDir(o.ID))
		if err1 != nil || err2 != nil || cdir != tdir {
			return fail(ExitUsage, "usage", "candidate must sit directly in %s (no subfolder)", p.TmpDir(o.ID))
		}
		if cand, err = os.ReadFile(o.Candidate); err != nil {
			return fail(ExitFail, "no_candidate", "candidate not found: %s", o.Candidate)
		}
		if HasConflictMarkers(cand) {
			return fail(ExitFail, "conflict", "candidate contains merge conflict markers")
		}
	}

	unlock, err := p.lock(o.ID)
	if err != nil {
		return err
	}
	defer unlock()
	cur, err := p.requireToken(o.ID, o.Token)
	if err != nil {
		return err
	}
	b, ok, err := readMaybe(path)
	if err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if HasConflictMarkers(b) {
		return fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", o.Rel)
	}
	if sum := Checksum(b, ok); sum != o.Sum {
		e := fail(ExitMismatch, "mismatch", "%s changed since the snapshot; merge onto the content above and commit again", o.Rel)
		e.Stdout = snapshotText(sum, p.TmpDir(o.ID), b)
		return e
	}
	if o.Delete {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fail(ExitFail, "write_failed", "could not delete %s: %v", o.Rel, err)
		}
	} else {
		if err := writeAtomic(path, cand); err != nil {
			return fail(ExitFail, "write_failed", "could not write %s: %v", o.Rel, err)
		}
		os.Remove(o.Candidate)
	}
	cur.LastContact = now()
	_ = p.writeLive(o.ID, cur)
	verb := "commit"
	if o.Delete {
		verb = "delete"
	}
	p.LogEvent(verb, o.ID, o.Rel)
	return nil
}

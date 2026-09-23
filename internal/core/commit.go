package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TeamID names the team-level target (sunstack/PILLARS.md) for locks and
// scratch space; it cannot collide with an agent ID, which never starts with _.
const TeamID = "_team"

// target is a file Sunstack writes through a snapshot checksum.
type target struct {
	path    string // absolute file path
	rel     string // how it is named in output and events
	owner   string // agent ID, or TeamID
	amended bool   // pillars or AGENT.md: user-approved, via amend
}

// resolveTarget accepts:
//
//	<id> context.md | threads/<topic>.md     agent experience (commit)
//	<id> pillars.md | AGENT.md               agent rules (amend)
//	TeamID PILLARS.md                        team rules (amend)
func (p *Project) resolveTarget(id, rel string) (*target, error) {
	if id == TeamID {
		if rel != "PILLARS.md" {
			return nil, fail(ExitUsage, "usage", "the team target is PILLARS.md, got: %s", rel)
		}
		t := &target{path: filepath.Join(p.Dir, "PILLARS.md"), rel: "PILLARS.md", owner: TeamID, amended: true}
		return t, p.noSymlink(t.path)
	}
	if !ValidID(id) {
		return nil, fail(ExitUsage, "usage", "invalid id: %s", id)
	}
	t := &target{rel: rel, owner: id}
	switch {
	case rel == "context.md":
	case rel == "pillars.md" || rel == "AGENT.md":
		t.amended = true
	case strings.HasPrefix(rel, "threads/") && strings.HasSuffix(rel, ".md"):
		topic := strings.TrimSuffix(strings.TrimPrefix(rel, "threads/"), ".md")
		if !ValidPart(topic) {
			return nil, fail(ExitUsage, "usage", "invalid thread topic: %s", topic)
		}
	default:
		return nil, fail(ExitUsage, "usage", "target must be context.md, threads/<topic>.md, pillars.md or AGENT.md, got: %s", rel)
	}
	if !p.HasAgent(id) {
		return nil, fail(ExitFail, "not_found", "no agent %s", id)
	}
	t.path = filepath.Join(p.AgentDir(id), filepath.FromSlash(rel))
	return t, p.noSymlink(t.path)
}

// TmpDir is where snapshots point candidates to; they must sit directly in it.
func (p *Project) TmpDir(owner string) string { return p.local("tmp", owner) }

func snapshotText(sum, candDir string, content []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "checksum: %s\ncandidate_dir: %s\n----- content -----\n", sum, candDir)
	b.Write(content)
	return b.String()
}

// Snapshot reads the target once and returns its content with the checksum of
// exactly those bytes (design §8.1). Experience targets need the claim token;
// rule targets (pillars, AGENT.md, team pillars) are readable by anyone.
func (p *Project) Snapshot(id, rel, token string) (string, error) {
	t, err := p.resolveTarget(id, rel)
	if err != nil {
		return "", err
	}
	if !t.amended {
		if token == "" {
			return "", fail(ExitClaim, "token", "missing --token")
		}
		cur, err := p.ReadLive(id)
		if err != nil {
			return "", err
		}
		if cur == nil || cur.Token != token {
			return "", fail(ExitClaim, "token", "token does not match the current claim on %s", id)
		}
	}
	b, ok, err := readMaybe(t.path)
	if err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if HasConflictMarkers(b) {
		return "", fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", t.rel)
	}
	if err := os.MkdirAll(p.TmpDir(t.owner), 0o755); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	return snapshotText(Checksum(b, ok), p.TmpDir(t.owner), b), nil
}

// readCandidate checks the candidate sits directly in the owner's tmp dir and
// carries no conflict markers.
func (p *Project) readCandidate(owner, path string) ([]byte, error) {
	// Resolve the directory: a symlinked tmp dir resolves elsewhere and fails
	// the comparison; the file itself is checked by noSymlink below.
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || dir != p.TmpDir(owner) {
		return nil, fail(ExitUsage, "usage", "candidate must sit directly in %s (no subfolder)", p.TmpDir(owner))
	}
	abs := filepath.Join(dir, filepath.Base(path))
	if err := p.noSymlink(abs); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(abs); err != nil || !st.Mode().IsRegular() {
		return nil, fail(ExitFail, "no_candidate", "candidate not found or not a regular file: %s", path)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, fail(ExitFail, "no_candidate", "candidate not readable: %s", path)
	}
	if HasConflictMarkers(b) {
		return nil, fail(ExitFail, "conflict", "candidate contains merge conflict markers")
	}
	return b, nil
}

// swap compares the target with the snapshot checksum and, if unchanged,
// replaces it (or deletes it when cand is nil and del is set). Caller holds
// the owner's lock.
func swap(t *target, sum string, cand []byte, del bool, candDir string) error {
	b, ok, err := readMaybe(t.path)
	if err != nil {
		return fail(ExitFail, "fs", "%v", err)
	}
	if HasConflictMarkers(b) {
		return fail(ExitFail, "conflict", "merge conflict markers in %s; resolve them first", t.rel)
	}
	if cur := Checksum(b, ok); cur != sum {
		e := fail(ExitMismatch, "mismatch", "%s changed since the snapshot; merge onto the content above and try again", t.rel)
		e.Stdout = snapshotText(cur, candDir, b)
		return e
	}
	if del {
		if err := os.Remove(t.path); err != nil && !os.IsNotExist(err) {
			return fail(ExitFail, "write_failed", "could not delete %s: %v", t.rel, err)
		}
		return nil
	}
	if err := writeAtomic(t.path, cand); err != nil {
		return fail(ExitFail, "write_failed", "could not write %s: %v", t.rel, err)
	}
	return nil
}

// CommitOptions are the inputs of `sunstack commit`.
type CommitOptions struct {
	ID, Rel, Candidate, Sum, Token string
	Delete                         bool
}

// Commit writes the agent's own experience (context.md, threads) if it still
// matches the snapshot, under the per-ID lock and token (design §8.3-§8.6).
func (p *Project) Commit(o CommitOptions) error {
	t, err := p.resolveTarget(o.ID, o.Rel)
	if err != nil {
		return err
	}
	if t.amended {
		return fail(ExitUsage, "usage", "%s is a rule file; changes need user approval through sunstack amend", o.Rel)
	}
	var cand []byte
	if !o.Delete {
		if cand, err = p.readCandidate(o.ID, o.Candidate); err != nil {
			return err
		}
	}
	unlock, err := p.lock(o.ID)
	if err != nil {
		return err
	}
	defer unlock()
	if !p.HasAgent(o.ID) {
		return fail(ExitFail, "not_found", "%s was removed", o.ID)
	}
	cur, err := p.requireToken(o.ID, o.Token)
	if err != nil {
		return err
	}
	if err := swap(t, o.Sum, cand, o.Delete, p.TmpDir(o.ID)); err != nil {
		return err
	}
	if !o.Delete {
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

// AmendOptions are the inputs of `sunstack amend`.
type AmendOptions struct {
	ID, Rel, Candidate, Sum, Summary string
}

var titleRe = regexp.MustCompile(`(?m)^title:\s*(\S+)\s*$`)

// Amend writes a user-approved change to pillars or AGENT.md (design §6, the
// self-improvement loop). Approval happens before the call: the skill asks, and the
// CLI's own permission rules make both Claude Code and Codex prompt for it.
func (p *Project) Amend(o AmendOptions) error {
	t, err := p.resolveTarget(o.ID, o.Rel)
	if err != nil {
		return err
	}
	if !t.amended {
		return fail(ExitUsage, "usage", "%s is agent experience; use sunstack commit", o.Rel)
	}
	if strings.TrimSpace(o.Summary) == "" {
		return fail(ExitUsage, "usage", "--summary is required: one line saying what changes, for the event log")
	}
	cand, err := p.readCandidate(t.owner, o.Candidate)
	if err != nil {
		return err
	}
	if o.Rel == "AGENT.md" {
		m := titleRe.FindSubmatch(cand)
		if !strings.HasPrefix(string(cand), "---") || m == nil || string(m[1]) != strings.SplitN(o.ID, ".", 2)[0] {
			return fail(ExitFail, "invalid_agent", "AGENT.md must keep its frontmatter with title: %s", strings.SplitN(o.ID, ".", 2)[0])
		}
	}
	unlock, err := p.lock(t.owner)
	if err != nil {
		return err
	}
	defer unlock()
	if t.owner != TeamID && !p.HasAgent(o.ID) {
		return fail(ExitFail, "not_found", "%s was removed; the approved change was not applied", o.ID)
	}
	if err := swap(t, o.Sum, cand, false, p.TmpDir(t.owner)); err != nil {
		return err
	}
	os.Remove(o.Candidate)
	subject := o.ID
	if o.ID == TeamID {
		subject = "team"
	}
	p.LogEvent("amend", subject, t.rel, fmt.Sprintf("summary=%q", strings.TrimSpace(o.Summary)))
	return nil
}

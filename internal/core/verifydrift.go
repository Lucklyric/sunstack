package core

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// A project's verify-<area> skills (§44) each keep a coverage record:
//
//	## Coverage
//	sync: <commit>            (or sha256:<hex> of the covered files, in order)
//	covers: path/a.go path/b.go
//
// Paths are relative to the project folder. VerifyDrift reports each check
// whose covered files changed since its sync marker, so next can say that
// the check needs a maintain pass before it is trusted again.

// verifyRecord is one verify-<area> skill's coverage record.
type verifyRecord struct {
	name   string
	sync   string
	covers []string
}

// readVerifyRecord reads the Coverage section of a verify skill.
func readVerifyRecord(path string) (verifyRecord, bool) {
	f, err := os.Open(path)
	if err != nil {
		return verifyRecord{}, false
	}
	defer f.Close()
	r := verifyRecord{name: filepath.Base(filepath.Dir(path))}
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "#") {
			in = strings.EqualFold(strings.TrimSpace(strings.TrimLeft(l, "#")), "coverage")
			continue
		}
		if !in {
			continue
		}
		l = strings.Trim(strings.TrimPrefix(l, "- "), "`")
		if v, ok := strings.CutPrefix(l, "sync:"); ok && r.sync == "" {
			r.sync = strings.Trim(strings.TrimSpace(v), "`")
		} else if v, ok := strings.CutPrefix(l, "covers:"); ok {
			for _, c := range strings.Fields(v) {
				r.covers = append(r.covers, strings.Trim(c, "`,"))
			}
		}
	}
	return r, true
}

// VerifyDrift is an issue for each verify-<area> skill under root whose
// covered files changed since its sync marker, or that has no record to
// check against.
func VerifyDrift(root string) []Issue {
	paths, _ := filepath.Glob(filepath.Join(root, ".claude", "skills", "verify-*", "SKILL.md"))
	sort.Strings(paths)
	var out []Issue
	for _, p := range paths {
		r, ok := readVerifyRecord(p)
		if !ok {
			continue
		}
		do := "run the formal-verify skill in maintain mode on " + r.name
		if r.sync == "" || len(r.covers) == 0 {
			if isFormalVerify(p) {
				out = append(out, Issue{"drift", "user", r.name + " has no coverage record (sync: and covers: under ## Coverage), so its staleness is unknown", do})
			}
			continue
		}
		if changed := verifyChanged(root, r); len(changed) > 0 {
			short := r.sync
			if len(short) > 12 {
				short = short[:12]
			}
			out = append(out, Issue{"drift", "user", fmt.Sprintf("%s: covered files changed since its sync marker %s (%s)", r.name, short, strings.Join(changed, ", ")), do})
		}
	}
	return out
}

// isFormalVerify tells a check that formal-verify made (it has a Coverage
// heading) from another project skill that happens to start with verify-,
// such as a verify-<app> driving skill, which keeps no such record.
func isFormalVerify(path string) bool {
	b, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") && strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "#")), "coverage") {
			return true
		}
	}
	return false
}

// verifyChanged lists the covered files that differ from the sync marker:
// committed since it, edited and not committed, or gone.
func verifyChanged(root string, r verifyRecord) []string {
	var changed []string
	for _, c := range r.covers {
		if _, err := os.Stat(filepath.Join(root, c)); err != nil {
			changed = append(changed, c+" (missing)")
		}
	}
	if hexsum, ok := strings.CutPrefix(r.sync, "sha256:"); ok {
		h := sha256.New()
		for _, c := range r.covers {
			b, _ := os.ReadFile(filepath.Join(root, c))
			h.Write(b)
		}
		if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), strings.TrimSpace(hexsum)) && len(changed) == 0 {
			changed = append(changed, "content hash differs")
		}
		return changed
	}
	if exec.Command("git", "-C", root, "cat-file", "-e", r.sync+"^{commit}").Run() != nil {
		return append(changed, "sync marker "+r.sync+" not found in git")
	}
	// One path at a time, named as the record names it: git prints paths
	// from the repository's top, and the project may be a folder inside it.
	for _, c := range r.covers {
		logged, _ := exec.Command("git", "-C", root, "log", "-1", "--format=%h", r.sync+"..HEAD", "--", c).Output()
		edited, _ := exec.Command("git", "-C", root, "status", "--porcelain", "--", c).Output()
		if strings.TrimSpace(string(logged)) != "" || strings.TrimSpace(string(edited)) != "" {
			changed = append(changed, c)
		}
	}
	return changed
}

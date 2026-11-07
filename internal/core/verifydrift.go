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

// A project's formal checks (§44) live in .formal-verify/<area>/check.md,
// each with a coverage record:
//
//	## Coverage
//	sync: <commit>            (or sha256:<hex>, see coverHash)
//	covers: path/a.go path/b.go
//
// Paths are relative to the project folder. VerifyDrift reports each check
// whose covered files changed since its sync marker, so next can say that
// the check needs a maintain pass before it is trusted again.

// verifyRecord is one check's coverage record.
type verifyRecord struct {
	name   string
	sync   string
	covers []string
}

// readVerifyRecord reads the Coverage section of a check.
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

// VerifyDrift is an issue for each check under root/.formal-verify whose
// covered files changed since its sync marker, or that has no record to
// check against.
func VerifyDrift(root string) []Issue {
	paths, _ := filepath.Glob(filepath.Join(root, ".formal-verify", "*", "check.md"))
	sort.Strings(paths)
	var out []Issue
	for _, p := range paths {
		r, ok := readVerifyRecord(p)
		if !ok {
			continue
		}
		do := "run the formal-verify skill in maintain mode on .formal-verify/" + r.name
		if r.sync == "" || len(r.covers) == 0 {
			out = append(out, Issue{"drift", "user", r.name + " has no coverage record (sync: and covers: under ## Coverage), so its staleness is unknown", do})
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

// verifyChanged lists the covered files that differ from the sync marker:
// changed against it (committed or not), gone, or not in it at all.
func verifyChanged(root string, r verifyRecord) []string {
	var changed, present []string
	for _, c := range r.covers {
		if _, err := os.Stat(filepath.Join(root, c)); err != nil {
			changed = append(changed, c+" (missing)")
		} else {
			present = append(present, c)
		}
	}
	if want, ok := strings.CutPrefix(r.sync, "sha256:"); ok {
		if len(changed) > 0 {
			return changed
		}
		got, err := coverHash(root, r.covers)
		if err != nil {
			return append(changed, err.Error())
		}
		if !strings.EqualFold(got, strings.TrimSpace(want)) {
			changed = append(changed, "content hash differs")
		}
		return changed
	}
	if exec.Command("git", "-C", root, "cat-file", "-e", r.sync+"^{commit}").Run() != nil {
		return append(changed, "sync marker "+r.sync+" not found in git")
	}
	// Each file against the marker's own tree, so a checkout older than the
	// marker counts too. Paths are as the record names them: ./ and -- read
	// them from the project folder, which may sit inside the repository.
	for _, c := range present {
		if exec.Command("git", "-C", root, "cat-file", "-e", r.sync+":./"+filepath.ToSlash(c)).Run() != nil {
			changed = append(changed, c+" (not in the sync commit)")
			continue
		}
		err := exec.Command("git", "-C", root, "diff", "--quiet", r.sync, "--", c).Run()
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			changed = append(changed, c)
		} else if err != nil {
			changed = append(changed, c+" (git diff failed, so unknown)")
		}
	}
	return changed
}

// coverHash is the content marker for files outside git: the SHA-256 of
// what `shasum -a 256 <covers...>` prints in the project folder, one
// "<hex>  <path>" line per file, so a boundary moved between files counts.
func coverHash(root string, covers []string) (string, error) {
	var lines strings.Builder
	for _, c := range covers {
		b, err := os.ReadFile(filepath.Join(root, c))
		if err != nil {
			return "", fmt.Errorf("%s unreadable, so unknown", c)
		}
		fmt.Fprintf(&lines, "%x  %s\n", sha256.Sum256(b), c)
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:]), nil
}

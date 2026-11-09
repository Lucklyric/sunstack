package core

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// A project's formal checks live in <data>/formal-verify/<area>/check.md, with
// <data> from SkillDataDir.
// The formal-verify skill owns their coverage record (its "Create: the check
// folder" section); next only reads it to suggest a maintain pass:
//
//	## Coverage
//	sync: <commit>            (or sha256:<hex>, see coverHash)
//	covers: path/a.go path/b.go path/dir/
//
// Paths are relative to the project folder; a folder covers every file
// under it, including ones added after the marker. VerifyDrift reports each check
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

// VerifyDrift is an issue for each check under the project's formal-verify folder whose
// covered files changed since its sync marker, or that has no record to
// check against.
func VerifyDrift(root string) []Issue {
	dir := SkillDataDir(root)
	if dir == "" {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "formal-verify", "*", "check.md"))
	sort.Strings(paths)
	var out []Issue
	for _, p := range paths {
		r, ok := readVerifyRecord(p)
		if !ok {
			continue
		}
		do := "run the formal-verify skill in maintain mode on its check " + r.name
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
		if coverIsDir(root, c) {
			changed = append(changed, dirChanged(root, r.sync, c)...)
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

// coverIsDir says whether a covers entry is a folder: written with a
// trailing / or a folder on disk.
func coverIsDir(root, c string) bool {
	if strings.HasSuffix(c, "/") {
		return true
	}
	st, err := os.Stat(filepath.Join(root, c))
	return err == nil && st.IsDir()
}

// dirChanged lists the files under a covered folder that differ from the
// sync marker: changed, gone or added since (committed or not), and new
// files git does not track yet unless they are ignored.
func dirChanged(root, sync, c string) []string {
	diff, err := exec.Command("git", "-C", root, "diff", "--relative", "--name-only", "-z", sync, "--", c).Output()
	if err != nil {
		return []string{c + " (git diff failed, so unknown)"}
	}
	added, err := exec.Command("git", "-C", root, "ls-files", "-z", "--others", "--exclude-standard", "--", c).Output()
	if err != nil {
		return []string{c + " (git ls-files failed, so unknown)"}
	}
	var out []string
	for _, f := range strings.Split(string(diff), "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	for _, f := range strings.Split(string(added), "\x00") {
		if f != "" {
			out = append(out, f+" (new)")
		}
	}
	return out
}

// coverHash is the content marker for files outside git: the SHA-256 of
// what `shasum -a 256 <files...>` prints in the project folder, one
// "<hex>  <path>" line per file, so a boundary moved between files counts.
// A folder stands for every regular file under it, in byte order, as
// `find <folder> -type f | LC_ALL=C sort` lists them, so an added or
// removed file changes the hash too.
func coverHash(root string, covers []string) (string, error) {
	var lines strings.Builder
	for _, c := range covers {
		files := []string{c}
		if coverIsDir(root, c) {
			var err error
			if files, err = dirFiles(root, strings.TrimSuffix(c, "/")); err != nil {
				return "", fmt.Errorf("%s unreadable, so unknown", c)
			}
		}
		for _, f := range files {
			b, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				return "", fmt.Errorf("%s unreadable, so unknown", f)
			}
			fmt.Fprintf(&lines, "%x  %s\n", sha256.Sum256(b), f)
		}
	}
	sum := sha256.Sum256([]byte(lines.String()))
	return hex.EncodeToString(sum[:]), nil
}

// dirFiles lists the regular files under a folder as dir/sub/name, in
// byte order.
func dirFiles(root, dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// SkillDataDir is the folder where Sunstack skills keep a project's data
// (§44): sunstack/ unless the user picked another one, which sunstack.json
// at the project's top records as {"data": "<relative path inside the
// project>"}. An absolute path or one leaving the project, by spelling or
// through a link, is not used; "" means no folder inside the project.
func SkillDataDir(root string) string {
	var c struct{ Data string }
	if b, err := os.ReadFile(filepath.Join(root, "sunstack.json")); err == nil && json.Unmarshal(b, &c) == nil && c.Data != "" {
		rel := filepath.FromSlash(c.Data)
		if !filepath.IsAbs(rel) && filepath.VolumeName(rel) == "" && !strings.HasPrefix(c.Data, "/") && !strings.HasPrefix(c.Data, `\`) {
			if d := inside(root, filepath.Join(root, rel)); d != "" {
				return d
			}
		}
	}
	return inside(root, filepath.Join(root, "sunstack"))
}

// inside is d when it stays inside root both as written and with links
// resolved (through its nearest existing folder, as it may not exist yet),
// else "".
func inside(root, d string) string {
	if !within(root, d) {
		return ""
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		r = root
	}
	for p := d; within(root, p); p = filepath.Dir(p) {
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			continue // not created yet: its nearest existing folder decides
		} else if err != nil {
			return ""
		}
		// It exists: a broken or circular link fails here and is refused.
		if e, err := filepath.EvalSymlinks(p); err == nil && within(r, e) {
			return d
		}
		return ""
	}
	return ""
}

// within reports whether path is root or below it, by spelling.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

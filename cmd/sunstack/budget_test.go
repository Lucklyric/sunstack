package main

// §27: tidy keeps what a session loads small, and an agent over budget is
// flagged.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTidyKeepsLoadSmall(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	day := func(n int) string { return time.Now().AddDate(0, 0, -n).Format("2006-01-02") }

	// 15 Done entries, all under 7 days old, newest first.
	var b strings.Builder
	b.WriteString("## Now\n- " + day(0) + " KR20 [O1] Current work\n## Next\n## Done\n")
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&b, "- %s KR%d [O1] Finished %d (verified: test)\n", day(i/3), i+1, i+1)
	}
	must(t, os.WriteFile(filepath.Join(dir, "board.md"), []byte(b.String()), 0o644))

	// A message still waiting in the inbox, recorded as processed 9 days ago.
	r := sh(t, p, env, "send", "builder.alice", "please look", "--type", "fyi")
	expect(t, r, 0, "send")
	pending := regexp.MustCompile(`[0-9]{8}T[0-9]{6}Z-[a-z0-9._-]+-[0-9a-f]{6}`).FindString(r.out + r.stderr)
	if pending == "" {
		t.Fatalf("no message ID in %q", r.out)
	}
	ctx := "## Decisions\n- " + day(20) + " keep the API small\n## Processed messages\n" +
		"- " + day(9) + " 20261001T000000Z-pm.lead-aaaaaa from pm.lead: old, acked\n" +
		"- " + day(9) + " " + pending + " from user: still waiting\n" +
		"- " + day(2) + " 20261019T000000Z-pm.lead-bbbbbb from pm.lead: recent\n"
	for i := 0; i < 12; i++ { // 12 newer ones: the 10 newest (by date, then ID) stay
		ctx += fmt.Sprintf("- %s 20261020T0000%02dZ-pm.lead-cccc%02d from pm.lead: newer %d\n", day(i/6), i, i, i)
	}
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte(ctx), 0o644))

	r = sh(t, p, env, "tidy", "builder.alice")
	expect(t, r, 0, "tidy")
	board, _ := os.ReadFile(filepath.Join(dir, "board.md"))
	for i := 1; i <= 15; i++ {
		on := strings.Contains(string(board), fmt.Sprintf("KR%d [O1] Finished %d ", i, i))
		if on != (i <= 10) {
			t.Errorf("KR%d on the board: %v, want %v\n%s", i, on, i <= 10, board)
		}
	}
	c, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	if strings.Contains(string(c), "old, acked") || strings.Contains(string(c), "recent") || strings.Contains(string(c), "newer 7") || !strings.Contains(string(c), "newer 11") || !strings.Contains(string(c), "newer 0") || !strings.Contains(string(c), "still waiting") || !strings.Contains(string(c), "keep the API small") {
		t.Errorf("context after tidy:\n%s", c)
	}
	var archived string
	es, _ := os.ReadDir(filepath.Join(dir, "archive"))
	for _, e := range es {
		a, _ := os.ReadFile(filepath.Join(dir, "archive", e.Name()))
		archived += string(a)
	}
	if !strings.Contains(archived, "old, acked") || !strings.Contains(archived, "recent") || !strings.Contains(archived, "newer 6") || !strings.Contains(archived, "newer 7") || !strings.Contains(archived, "KR15 [O1] Finished 15") {
		t.Errorf("archive:\n%s", archived)
	}
	if strings.Contains(r.out, "over") {
		t.Errorf("under budget, tidy said: %s", r.out)
	}
}

func TestLoadBudget(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	today := time.Now().Format("2006-01-02")
	write := func(kb int) {
		line := "- " + today + " " + strings.Repeat("d", 1000) + "\n"
		must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte("## Decisions\n"+strings.Repeat(line, kb)), 0o644))
	}

	write(24)
	if r := sh(t, p, env, "tidy", "builder.alice"); strings.Contains(r.out, "over") {
		t.Errorf("under budget, tidy said: %s", r.out)
	}
	if r := sh(t, p, env, "health"); strings.Contains(r.out, "over 25 KB") {
		t.Errorf("under budget, health said: %s", r.out)
	}

	write(27)
	r := sh(t, p, env, "tidy", "builder.alice")
	expect(t, r, 0, "tidy")
	requireContains(t, r.out, "context.md is 26 KB, over 25 KB")
	requireContains(t, sh(t, p, env, "health").out, "builder.alice/context.md is 26 KB, over 25 KB")
	if n := strings.Count(sh(t, p, env, "next", "--all").out, "builder.alice/context.md is 26 KB, over 25 KB"); n != 1 {
		t.Errorf("next lists the budget %d times", n)
	}

	// A board over 40 lines after tidy.
	write(1)
	var b strings.Builder
	b.WriteString("## Now\n")
	for i := 0; i < 45; i++ {
		fmt.Fprintf(&b, "- %s KR%d [O1] Work %d\n", today, i+1, i+1)
	}
	must(t, os.WriteFile(filepath.Join(dir, "board.md"), []byte(b.String()), 0o644))
	requireContains(t, sh(t, p, env, "tidy", "builder.alice").out, "board.md has 46 lines, over 40")
}

// Same-day processed messages: the newest by message ID stay, and a record
// whose inbox cannot be read is kept.
func TestTidyProcessedOrder(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	today := time.Now().Format("2006-01-02")
	ctx := "## Processed messages\n"
	for i := 0; i < 12; i++ { // appended oldest first
		ctx += fmt.Sprintf("- %s 20261020T1200%02dZ-user-aaaaaa from user: number %02d\n", today, i, i)
	}
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte(ctx), 0o644))
	expect(t, sh(t, p, env, "tidy", "builder.alice"), 0, "tidy")
	c, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	if strings.Contains(string(c), "number 00") || strings.Contains(string(c), "number 01") || !strings.Contains(string(c), "number 11") {
		t.Errorf("context after tidy:\n%s", c)
	}

	// .taken that is not a readable folder: every record stays.
	old := time.Now().AddDate(0, 0, -9).Format("2006-01-02")
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte("## Processed messages\n- "+old+" 20261001T000000Z-user-bbbbbb from user: maybe taken\n"), 0o644))
	inbox := filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice")
	must(t, os.MkdirAll(inbox, 0o755))
	must(t, os.WriteFile(filepath.Join(inbox, ".taken"), []byte("not a folder"), 0o644))
	expect(t, sh(t, p, env, "tidy", "builder.alice"), 0, "tidy")
	if c, _ := os.ReadFile(filepath.Join(dir, "context.md")); !strings.Contains(string(c), "maybe taken") {
		t.Errorf("a record whose inbox could not be read was archived:\n%s", c)
	}
}

// A local message ID longer than other hosts' 128-character cap still keeps
// its processed record while the message waits.
func TestTidyKeepsLongPendingID(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	id := "20261001T000000Z-" + strings.Repeat("a", 120) + ".team-abcdef"
	inbox := filepath.Join(p, "sunstack", "_local", "inbox", "builder.alice")
	must(t, os.MkdirAll(inbox, 0o755))
	must(t, os.WriteFile(filepath.Join(inbox, id+".md"), []byte("from: x\n"), 0o644))
	old := time.Now().AddDate(0, 0, -9).Format("2006-01-02")
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte("## Processed messages\n- "+old+" "+id+" from x: waiting\n"), 0o644))
	expect(t, sh(t, p, env, "tidy", "builder.alice"), 0, "tidy")
	if c, _ := os.ReadFile(filepath.Join(dir, "context.md")); !strings.Contains(string(c), "waiting") {
		t.Errorf("the record of a long pending ID was archived:\n%s", c)
	}
}

// An entry's indented continuation moves with it.
func TestTidyMovesWholeEntries(t *testing.T) {
	p := fixture(t)
	env := isolatedEnv(t)
	dir := filepath.Join(p, "sunstack", "builder.alice")
	old := time.Now().AddDate(0, 0, -9).Format("2006-01-02")
	ctx := "## Processed messages\n- " + old + " 20261001T000000Z-user-aaaaaa from user: done\n  Verification: checked the output.\n\n  Second paragraph.\n\n## Open questions\n- " + old + " none\n"
	must(t, os.WriteFile(filepath.Join(dir, "context.md"), []byte(ctx), 0o644))
	expect(t, sh(t, p, env, "tidy", "builder.alice"), 0, "tidy")
	c, _ := os.ReadFile(filepath.Join(dir, "context.md"))
	a, _ := os.ReadFile(filepath.Join(dir, "archive", old[:7]+".md"))
	if strings.Contains(string(c), "Verification") || strings.Contains(string(c), "Second paragraph") || !strings.Contains(string(a), "  Verification: checked the output.") || !strings.Contains(string(a), "  Second paragraph.") || !strings.Contains(string(c), "none") {
		t.Errorf("context:\n%s\narchive:\n%s", c, a)
	}
}

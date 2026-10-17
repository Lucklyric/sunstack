package main

// v0.12.3: one feature, one command. sunstack next is the only list of what
// needs doing; board, health and org point to it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNextIsTheOnlyList(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home"), "SUNSTACK_NO_UPDATE_CHECK=1"}
	expect(t, sh(t, p, home, "init"), 0, "init")
	expect(t, sh(t, p, home, "hire", "builder", "alice"), 0, "hire")
	expect(t, sh(t, p, home, "halt", "rotating keys"), 0, "halt")
	const finding = "rotating keys"

	r := sh(t, p, home, "next")
	expect(t, r, 0, "next")
	if !strings.Contains(r.out, finding) || !strings.Contains(r.out, "sunstack halt --off") {
		t.Errorf("next lacks the halt:\n%s", r.out)
	}
	for _, c := range [][]string{{"board"}, {"health"}, {"org"}} {
		r := sh(t, p, home, c...)
		if strings.Contains(r.out, finding) || !strings.Contains(r.out, "sunstack next") {
			t.Errorf("%s should point to sunstack next, not list the findings:\n%s", c[0], r.out)
		}
	}
	if r := sh(t, p, home, "health"); strings.Contains(r.out, "next steps:\n") {
		t.Errorf("health keeps its own ranked list:\n%s", r.out)
	}

	// Outside any team: org --attention lists every team on this host, each
	// item named by team; plain next only counts them (v0.12.6).
	out := t.TempDir()
	r = sh(t, out, home, "org", "--attention")
	expect(t, r, 0, "org --attention")
	if !strings.Contains(r.out, finding) || !strings.Contains(r.out, filepath.Base(p)) {
		t.Errorf("host-wide list:\n%s", r.out)
	}
	if n := sh(t, out, home, "next"); strings.Contains(n.out, finding) || !strings.Contains(n.out, "sunstack org --attention") {
		t.Errorf("next outside a team:\n%s", n.out)
	}

	// A newer release is something to do (a dev build is never behind).
	rel := filepath.Join(t.TempDir(), "sunstack")
	if runtime.GOOS == "windows" {
		rel += ".exe"
	}
	if out, err := exec.Command("go", "build", "-ldflags", "-X main.version=0.1.0", "-o", rel, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	must(t, os.WriteFile(filepath.Join(p, ".home", "latest.json"), []byte(`{"version":"99.0.0"}`), 0o600))
	cmd := exec.Command(rel, "next", "--all")
	cmd.Dir, cmd.Env = p, append(cleanEnv(), home...)
	b, _ := cmd.Output()
	if !strings.Contains(string(b), "99.0.0") || !strings.Contains(string(b), "sunstack update") {
		t.Errorf("next lacks the update:\n%s", b)
	}
}

func TestHelpShortAndAll(t *testing.T) {
	p := t.TempDir()
	short := sh(t, p, nil, "help")
	all := sh(t, p, nil, "help", "all")
	expect(t, short, 0, "help")
	expect(t, all, 0, "help all")
	for _, c := range []string{"sunstack next", "sunstack org", "sunstack board", "sunstack send", "sunstack spawn", "sunstack help all", "sunstack kill --idle", "sunstack check", "/sunstack:<name>", "hire, hr"} {
		if !strings.Contains(short.out, c) {
			t.Errorf("short help lacks %q", c)
		}
	}
	for _, c := range []string{"sunstack snapshot", "sunstack commit", "sunstack take"} {
		if strings.Contains(short.out, c) {
			t.Errorf("short help lists the skills' %q", c)
		}
		if !strings.Contains(all.out, c) {
			t.Errorf("help all lacks %q", c)
		}
	}
	if len(strings.Split(short.out, "\n")) > 30 {
		t.Errorf("short help has %d lines", len(strings.Split(short.out, "\n")))
	}
}

func TestTeamSessions(t *testing.T) {
	p := t.TempDir()
	home := []string{"SUNSTACK_HOME=" + filepath.Join(p, ".home")}
	expect(t, sh(t, p, home, "init"), 0, "init")
	a, b := sh(t, p, home, "team", "--sessions", "--json"), sh(t, p, home, "sessions", "--json")
	expect(t, a, 0, "team --sessions")
	if a.out != b.out {
		t.Errorf("team --sessions differs from sessions:\n%s\n---\n%s", a.out, b.out)
	}
}

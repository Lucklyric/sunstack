package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sshFixture is a home with an SSH config and a socket folder short enough
// for unix sockets, and the stand-in ssh's answers.
type sshFixture struct {
	env        []string
	cm, events string
}

func newSSHFixture(t *testing.T, env []string) *sshFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	home := envValue(env, "HOME")
	cm, err := os.MkdirTemp("/tmp", "sscm")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(cm) })
	must(t, os.MkdirAll(filepath.Join(home, ".ssh"), 0o700))
	must(t, os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(`Host cluster-a cluster-a.example.org
  ControlMaster auto
  ControlPath `+cm+`/%r@%h:%p
Host plain # no sharing
Host *
  ServerAliveInterval 30
`), 0o600))
	shared := filepath.Join(cm, "u@cluster-a:22")
	other := filepath.Join(cm, "u@elsewhere:22")
	for _, p := range []string{shared, other} {
		l, err := net.Listen("unix", p)
		must(t, err)
		t.Cleanup(func() { l.Close() })
	}
	g, _ := json.Marshal(map[string]string{
		"cluster-a":             "controlmaster auto\ncontrolpath " + shared,
		"cluster-a.example.org": "controlmaster auto\ncontrolpath " + shared,
		"plain":                 "controlmaster false\ncontrolpath none",
	})
	env = append(append([]string{}, env...), "SUNSTACK_TEST_SSH_G="+string(g), "SUNSTACK_TEST_SSH_OPEN="+shared+","+other)
	return &sshFixture{env: env, cm: cm, events: envValue(env, "SUNSTACK_TEST_EVENTS")}
}

func (f *sshFixture) log() string { return readFileOr(filepath.Join(f.events, "ssh.log")) }

// sunstack ssh lists which aliases share a connection and whether it is
// open now; scan and check are the user's, and the views never read the
// SSH configuration again.
func TestSSHStatus(t *testing.T) {
	p := fixture(t)
	f := newSSHFixture(t, isolatedEnv(t))

	r := sh(t, p, f.env, "ssh")
	expect(t, r, 0, "status before a scan")
	requireContains(t, r.out, "sunstack ssh scan")

	expect(t, sh(t, p, append(f.env, "CLAUDECODE=1"), "ssh", "scan"), 2, "scan from an agent session")
	r = sh(t, p, f.env, "ssh", "scan")
	expect(t, r, 0, "scan")
	requireContains(t, r.out, "3 aliases")

	r = sh(t, p, f.env, "ssh")
	expect(t, r, 0, "status")
	requireContains(t, r.out, "cluster-a, cluster-a.example.org", "open, reuse it", "plain", "no shared connection", "u@elsewhere:22")
	before := strings.Count(f.log(), "-G")

	// A fresh login that neither makes nor uses a master.
	expect(t, sh(t, p, append(f.env, "CODEX_THREAD_ID=x"), "ssh", "check", "cluster-a"), 2, "check from an agent session")
	r = sh(t, p, f.env, "ssh", "check", "cluster-a")
	expect(t, r, 0, "check")
	requireContains(t, r.out, "cluster-a: ok")
	requireContains(t, f.log(), "-n -o BatchMode=yes -o ControlMaster=no -o ControlPath=none -o ConnectTimeout=5 -o ClearAllForwardings=yes -o PermitLocalCommand=no -- cluster-a true")
	r = sh(t, p, append(f.env, "SUNSTACK_TEST_SSH_LOGIN=denied"), "ssh", "check", "cluster-a")
	expect(t, r, 1, "a refused key")
	requireContains(t, r.out, "auth failed")
	requireContains(t, sh(t, p, f.env, "ssh").out, "last check auth failed")
	expect(t, sh(t, p, f.env, "ssh", "check", "nowhere"), 1, "an alias the scan did not find")

	// Views ask the recorded sockets only.
	sh(t, p, f.env, "ssh")
	sh(t, p, f.env, "org")
	if n := strings.Count(f.log(), "-G"); n != before {
		t.Errorf("a view ran ssh -G: %d calls, want %d", n, before)
	}
	for _, l := range strings.Split(f.log(), "\n") {
		if strings.Contains(l, "-O check") && !strings.HasPrefix(l, "-F /dev/null") {
			t.Errorf("-O check without -F /dev/null: %s", l)
		}
	}
}

// An org host mapped to an alias shows its connection in org --by host.
func TestSSHOrgHost(t *testing.T) {
	hubHost, addr, hosts := newOrg(t, "laptop")
	laptop := hosts[0]
	f := newSSHFixture(t, laptop.env)
	laptop.env = f.env
	laptop.join(t, hubHost, addr)
	expect(t, laptop.run(t, "ssh", "scan"), 0, "scan")
	expect(t, laptop.run(t, "ssh", "map", "nobody", "cluster-a"), 1, "map an unknown host")
	expect(t, laptop.run(t, "ssh", "map", "hub-host", "cluster-a"), 0, "map")
	expect(t, laptop.run(t, "org", "--refresh"), 0, "refresh")
	requireContains(t, laptop.run(t, "org", "--by", "host").out, "ssh cluster-a: connection open, reusable")
	requireContains(t, laptop.run(t, "ssh").out, "hub-host")
	// The other direction shows on the hub, from the laptop's snapshot.
	expect(t, hubHost.run(t, "org", "--refresh"), 0, "hub refresh")
	requireContains(t, hubHost.run(t, "org", "--by", "host").out, "ssh from laptop: connection open")
	// Nothing about SSH leaves this host.
	for _, h := range []*orgHost{hubHost, laptop} {
		filepath.WalkDir(filepath.Join(h.home, ".sunstack"), func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Base(path) != "ssh.json" {
				if b, _ := os.ReadFile(path); strings.Contains(string(b), "cluster-a") || strings.Contains(string(b), f.cm) {
					t.Errorf("%s holds SSH details", path)
				}
			}
			return nil
		})
	}
}

// Stop asks first, closes through the recorded socket, and start is the
// user's own, in a terminal.
func TestSSHStartStopCLI(t *testing.T) {
	p := fixture(t)
	f := newSSHFixture(t, isolatedEnv(t))
	expect(t, sh(t, p, f.env, "ssh", "scan"), 0, "scan")

	expect(t, sh(t, p, append(f.env, "CLAUDECODE=1"), "ssh", "start", "cluster-a"), 2, "start from an agent session")
	expect(t, sh(t, p, append(f.env, "CLAUDECODE=1"), "ssh", "stop", "cluster-a", "--yes"), 2, "stop from an agent session")
	r := sh(t, p, f.env, "ssh", "start", "plain")
	expect(t, r, 2, "start without a terminal")
	requireContains(t, r.stderr, "terminal")

	r = sh(t, p, f.env, "ssh", "stop", "cluster-a")
	expect(t, r, 2, "stop without --yes")
	requireContains(t, r.stderr, "--yes", "ends every shell")
	if strings.Contains(f.log(), "-O exit") {
		t.Fatal("stop closed the connection before confirming")
	}
	r = sh(t, p, f.env, "ssh", "stop", "cluster-a", "--yes")
	expect(t, r, 0, "stop")
	requireContains(t, r.out, "connection to cluster-a closed")
	requireContains(t, f.log(), "-F /dev/null -o ControlPath="+filepath.Join(f.cm, "u@cluster-a:22")+" -O exit sunstack-check")

	r = sh(t, p, f.env, "ssh", "stop", "plain", "--yes")
	expect(t, r, 1, "stop an alias that shares nothing")
	requireContains(t, r.stderr, "shares no connection")
}

// The guide only prints: placeholders without flags, quoted values, both
// OS branches, and the entry the user pastes.
func TestSSHSetup(t *testing.T) {
	p := fixture(t)
	f := newSSHFixture(t, isolatedEnv(t))
	env := f.env
	expect(t, sh(t, p, env, "ssh", "scan"), 0, "scan")
	before := f.log()

	r := sh(t, p, append(env, "CLAUDECODE=1"), "ssh", "setup", "studio")
	expect(t, r, 0, "an agent may print the guide")
	requireContains(t, r.out, "Remote Login", "sshd", "Tailscale SSH", "ssh-copy-id -i ~/.ssh/id_ed25519.pub <user>@<address>",
		"mkdir -p -m 700 ~/.ssh/cm", "Host studio", "HostName <address>", "ControlPath ~/.ssh/cm/%C", "ControlPersist 10m",
		"sunstack ssh check studio", "For the other direction")

	r = sh(t, p, env, "ssh", "setup", "studio", "--os", "macos", "--user", "me", "--address", "box name;rm", "--key", "~/.ssh/k.pub")
	expect(t, r, 0, "guide with values")
	requireContains(t, r.out, "ssh-copy-id -i ~/.ssh/k.pub me@'box name;rm'", "IdentityFile ~/.ssh/k\n")
	if strings.Contains(r.out, "sshd installed") {
		t.Error("--os macos printed the Linux branch")
	}
	expect(t, sh(t, p, env, "ssh", "setup", "studio", "--os", "windows"), 2, "an unknown OS")
	expect(t, sh(t, p, env, "ssh", "setup", "a b"), 2, "a name that is no alias")
	if after := f.log(); after != before {
		t.Errorf("the guide ran ssh: %s", after)
	}
}

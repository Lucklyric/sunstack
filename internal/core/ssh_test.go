//go:build !windows

package core

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sshStartFixture is a stand-in ssh on PATH that opens a master on -fN and
// answers -O check and -O exit from a marker file, and a recorded alias
// whose socket folder is dir.
func sshStartFixture(t *testing.T) (dir, path, events string) {
	t.Helper()
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	events = t.TempDir()
	bin := t.TempDir()
	script := `#!/bin/sh
echo "$*" >> "` + events + `/log"
[ "$1" = "-fN" ] && { touch "` + events + `/open"; exit 0; }
op=""; prev=""
for a in "$@"; do [ "$prev" = "-O" ] && op="$a"; prev="$a"; done
case "$op" in
check) [ -f "` + events + `/open" ] && exit 0; exit 255 ;;
exit) rm -f "` + events + `/open"; exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	dir, err := os.MkdirTemp("/tmp", "sscm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path = filepath.Join(dir, "u@box:22")
	deadSocket(t, path)
	s := LoadSSH()
	s.Aliases["box"] = &SSHAlias{ControlMaster: "auto", ControlPath: path}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	return dir, path, events
}

// deadSocket leaves a socket file with nothing listening on it.
func deadSocket(t *testing.T, path string) {
	t.Helper()
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
}

func readLog(events string) string {
	b, _ := os.ReadFile(filepath.Join(events, "log"))
	return string(b)
}

// Start opens a master once, on the recorded socket, and records it; stop
// closes it with -F /dev/null and forgets it.
func TestSSHStartStop(t *testing.T) {
	_, path, events := sshStartFixture(t)

	opened, err := SSHStart("box", nil, nil, nil)
	if err != nil || !opened {
		t.Fatalf("start: opened %v, err %v", opened, err)
	}
	if !strings.Contains(readLog(events), "-fN -o ControlMaster=yes -o ControlPath="+path+" -- box") {
		t.Errorf("start ran:\n%s", readLog(events))
	}
	if LoadSSH().Started["box"] == "" {
		t.Error("start not recorded")
	}
	if l := sshLineFor(t, "box"); !strings.Contains(l, "connection open, reusable (sunstack ssh start") {
		t.Errorf("status does not name the start: %s", l)
	}

	opened, err = SSHStart("box", nil, nil, nil)
	if err != nil || opened {
		t.Fatalf("second start: opened %v, err %v", opened, err)
	}
	if n := strings.Count(readLog(events), "-fN"); n != 1 {
		t.Errorf("a second start ran ssh -fN again (%d calls)", n)
	}

	closed, err := SSHStop("box")
	if err != nil || !closed {
		t.Fatalf("stop: closed %v, err %v", closed, err)
	}
	if !strings.Contains(readLog(events), "-F /dev/null -o ControlPath="+path+" -O exit sunstack-check") {
		t.Errorf("stop ran:\n%s", readLog(events))
	}
	if LoadSSH().Started["box"] != "" {
		t.Error("stop left the start recorded")
	}
	if closed, err = SSHStop("box"); err != nil || closed {
		t.Errorf("second stop: closed %v, err %v", closed, err)
	}

	if _, err := SSHStart("nowhere", nil, nil, nil); err == nil {
		t.Error("start of an unknown alias")
	}
}

func sshLineFor(t *testing.T, alias string) string {
	t.Helper()
	if err := SSHMap("host-1", alias); err != nil {
		t.Fatal(err)
	}
	return SSHLine("host-1")
}

// Start refuses a socket folder that others can enter or that is a link.
func TestSSHStartSocketFolder(t *testing.T) {
	dir, _, events := sshStartFixture(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SSHStart("box", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Errorf("a 755 folder: %v", err)
	}
	os.Chmod(dir, 0o700)

	link := dir + "-link"
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
	s := LoadSSH()
	s.Aliases["linked"] = &SSHAlias{ControlMaster: "auto", ControlPath: filepath.Join(link, "u@box:22")}
	s.save()
	if _, err := SSHStart("linked", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "not a plain folder") {
		t.Errorf("a linked folder: %v", err)
	}
	if strings.Contains(readLog(events), "-fN") {
		t.Error("ssh ran despite an unsafe folder")
	}
}

// The snapshot block names hosts by ID and holds no alias or socket.
func TestSSHPeers(t *testing.T) {
	_, path, _ := sshStartFixture(t)
	SSHMap("host-1", "box")
	peers := SSHPeers()
	if len(peers) != 1 || peers[0].Host != "host-1" || !peers[0].Sharing || peers[0].Master {
		t.Fatalf("peers: %+v", peers)
	}
	if s := peers[0].Text(); s != "no open connection" {
		t.Errorf("text: %q", s)
	}
	for _, p := range peers {
		if b := strings.Join([]string{p.Host, p.Check, p.CheckAt}, " "); strings.Contains(b, "box") || strings.Contains(b, path) {
			t.Errorf("a peer holds SSH details: %+v", p)
		}
	}
}

// Real OpenSSH, isolated by its own configuration file and socket folder.
func TestSSHRealOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh")
	}
	dir, err := os.MkdirTemp("/tmp", "sscm")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cfg := filepath.Join(dir, "config")
	os.WriteFile(cfg, []byte("Host box\n  HostName 127.0.0.1\n  Port 1\n  ControlMaster auto\n  ControlPath "+dir+"/%C\n  UserKnownHostsFile /dev/null\n"), 0o600)
	run := func(args ...string) (string, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, ssh, args...).CombinedOutput()
		if x, ok := err.(*exec.ExitError); ok {
			return string(out), x.ExitCode()
		}
		return string(out), 0
	}

	// -G resolves a name that has no entry.
	if out, code := run("-F", cfg, "-G", "no-entry-here"); code != 0 || !strings.Contains(out, "hostname no-entry-here") {
		t.Errorf("-G without an entry: %d %s", code, out)
	}

	// -O check against a socket nobody answers on.
	sock := filepath.Join(dir, "dead")
	deadSocket(t, sock)
	if MasterOpen(sock) {
		t.Error("a dead socket counts as open")
	}

	// A check uses a fresh login and leaves no master behind.
	out, code := run(append([]string{"-F", cfg}, SSHCheckArgs("box")...)...)
	if r := checkResult(out, code, false); r != "unreachable" {
		t.Errorf("check against a closed port: %s (%d %s)", r, code, out)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "config" && e.Name() != "dead" {
			t.Errorf("the check left %s behind", e.Name())
		}
	}
}

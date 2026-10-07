package hub

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

func usageErr(format string, a ...any) error {
	return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: fmt.Sprintf(format, a...)}
}

func failErr(reason, format string, a ...any) error {
	return &core.Error{Code: core.ExitFail, Reason: reason, Msg: fmt.Sprintf(format, a...)}
}

// hubConfig returns this host's config when it is the hub.
func hubConfig() (*Config, store, error) {
	c, err := LoadConfig()
	if err != nil {
		return nil, store{}, failErr("org", "%v", err)
	}
	if c == nil || !c.IsHub() {
		return nil, store{}, failErr("not_hub", "this host is not an org hub (sunstack hub init creates one)")
	}
	return c, store{hubDir(c.OrgID)}, nil
}

// Init makes this host the hub of a new org (§18.3).
func Init(name string) (*Config, error) {
	if runtime.GOOS == "windows" {
		return nil, failErr("unsupported", "a hub on Windows is not supported in v0.9")
	}
	if !nameRe.MatchString(name) {
		return nil, usageErr("org name: letters, digits, . _ - only")
	}
	if c, err := LoadConfig(); err != nil {
		return nil, failErr("org", "%v", err)
	} else if c != nil {
		return nil, failErr("org", "this host is already in org %s (sunstack org leave first)", c.OrgName)
	}
	me := core.ThisHost()
	c := &Config{OrgID: core.NewToken(), OrgName: name, Hub: LocalHub}
	s := store{hubDir(c.OrgID)}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	r := &Roster{Schema: Schema, OrgID: c.OrgID, OrgName: name, Hosts: []*Host{{ID: me.ID, Name: me.Name, CanSend: true, Added: stamp(time.Now()), Hub: true}}}
	if err := writeJSON(s.hostsFile(), r, 0o600); err != nil {
		return nil, err
	}
	return c, saveConfig(c)
}

// AuthorizedKeys is the hub user's authorized_keys file.
func AuthorizedKeys() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".ssh", "authorized_keys")
}

func keyTag(org, id string) string { return "sunstack-hub:" + org + ":" + id }

// keyLine is the authorized_keys line for a host: its key may only run
// hub serve for that host's ID, with no terminal, forwarding or ~/.ssh/rc.
func keyLine(exe, org, id, pub string) string {
	f := strings.Fields(pub)
	return fmt.Sprintf(`restrict,command="'%s' hub serve --host %s" %s %s %s`, exe, id, f[0], f[1], keyTag(org, id))
}

// Allow adds a host to the org and its key to authorized_keys (§18.3).
func Allow(name, id, pub string, canSend bool) (string, error) {
	c, s, err := hubConfig()
	if err != nil {
		return "", err
	}
	if !nameRe.MatchString(name) {
		return "", usageErr("host name: letters, digits, . _ - only")
	}
	if !idRe.MatchString(id) {
		return "", usageErr("--id must be the host ID from its ~/.sunstack/host.json")
	}
	pub = strings.TrimSpace(pub)
	if strings.ContainsAny(pub, "\n\r") {
		return "", usageErr("--key takes exactly one public key")
	}
	fp, err := Fingerprint(pub)
	if err != nil {
		return "", usageErr("--key: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if strings.ContainsAny(exe, `'"\`) {
		return "", failErr("path", "the sunstack path %s has a quote or backslash; install it elsewhere", exe)
	}
	note := ""
	err = s.withRoster(func(r *Roster) error {
		for _, h := range r.Hosts {
			if h.ID != id && strings.EqualFold(h.Name, name) {
				return failErr("exists", "the name %s is already used by another host", name)
			}
			if h.ID != id && h.Fingerprint == fp {
				return failErr("exists", "that key already belongs to host %s", h.Name)
			}
		}
		if h := r.byID(id); h != nil {
			if h.Fingerprint != fp {
				return failErr("exists", "host %s is allowed with another key; sunstack hub revoke %s first", h.Name, h.Name)
			}
			if h.Name == name && h.CanSend == canSend {
				note = "already allowed; nothing changed"
				return nil
			}
			h.Name, h.CanSend = name, canSend
			note = "updated"
			return nil
		}
		r.Hosts = append(r.Hosts, &Host{ID: id, Name: name, CanSend: canSend, Fingerprint: fp, Added: stamp(time.Now())})
		note = "allowed"
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := editKeys(func(lines []string) []string {
		tag := keyTag(c.OrgID, id)
		for _, l := range lines {
			if strings.HasSuffix(strings.TrimSpace(l), tag) {
				return lines
			}
		}
		return append(lines, keyLine(exe, c.OrgID, id, pub))
	}); err != nil {
		return "", err
	}
	return note, nil
}

// Revoke removes a host, its key line, snapshot and mail. Its open
// connections end at their next check (§18.5).
func Revoke(name string) error {
	c, s, err := hubConfig()
	if err != nil {
		return err
	}
	var gone *Host
	err = s.withRoster(func(r *Roster) error {
		h := r.byName(name)
		if h == nil {
			return failErr("not_found", "no host %s in the org (sunstack hub hosts)", name)
		}
		if h.Hub {
			return failErr("hub", "the hub cannot revoke itself")
		}
		gone = h
		var kept []*Host
		for _, x := range r.Hosts {
			if x.ID != h.ID {
				kept = append(kept, x)
			}
		}
		r.Hosts = kept
		return nil
	})
	if err != nil {
		return err
	}
	tag := keyTag(c.OrgID, gone.ID)
	if err := editKeys(func(lines []string) []string {
		var kept []string
		for _, l := range lines {
			if !strings.HasSuffix(strings.TrimSpace(l), tag) {
				kept = append(kept, l)
			}
		}
		return kept
	}); err != nil {
		return err
	}
	os.Remove(s.snapFile(gone.ID))
	os.Remove(s.seenFile(gone.ID))
	os.RemoveAll(s.mailDir(gone.ID))
	os.RemoveAll(s.receiptDir(gone.ID))
	return nil
}

// editKeys changes authorized_keys under a lock. Lines Sunstack did not
// write are kept as they are.
func editKeys(f func([]string) []string) error {
	path := AuthorizedKeys()
	unlock, err := core.LockDir(filepath.Join(core.Home(), "locks", "_authorized_keys_"), "authorized_keys")
	if err != nil {
		return err
	}
	defer unlock()
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out := f(lines)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	text := strings.Join(out, "\n")
	if text != "" {
		text += "\n"
	}
	return writeFile(path, []byte(text), 0o600)
}

// Hosts lists the roster with contact times and waiting mail.
func Hosts() (*Roster, error) {
	_, s, err := hubConfig()
	if err != nil {
		return nil, err
	}
	return s.liveRoster()
}

// KeyPath is this host's dedicated hub key.
func KeyPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".ssh", "sunstack_hub")
}

// ensureKey creates the dedicated key, without a passphrase, if missing.
func ensureKey() (string, error) {
	path := KeyPath()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", err
		}
		prog := os.Getenv("SUNSTACK_SSH_KEYGEN")
		if prog == "" {
			prog = "ssh-keygen"
		}
		out, err := exec.Command(prog, "-q", "-t", "ed25519", "-N", "", "-C", "sunstack-hub", "-f", path).CombinedOutput()
		if err != nil {
			return "", failErr("ssh_keygen", "could not create %s: %v %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", err
	}
	pub := strings.TrimSpace(string(b))
	if _, err := Fingerprint(pub); err != nil {
		return "", failErr("key", "%s.pub: %v", path, err)
	}
	return pub, nil
}

// Join adds this host to the org whose hub is target (§18.3). It first
// tries the user's own SSH access to run hub allow on the hub; when that
// fails it prints the line to run there, and a second Join finishes.
func Join(target string, canSend bool, in io.Reader, out io.Writer) (*Config, error) {
	if strings.HasPrefix(target, "-") || strings.ContainsAny(target, " \t\n'\"") {
		return nil, usageErr("invalid ssh target %q", target)
	}
	if c, err := LoadConfig(); err != nil {
		return nil, failErr("org", "%v", err)
	} else if c != nil {
		if c.Hub == target {
			fmt.Fprintf(out, "already in org %s through %s\n", c.OrgName, target)
			return c, nil
		}
		return nil, failErr("org", "this host is already in org %s (sunstack org leave first)", c.OrgName)
	}
	pub, err := ensureKey()
	if err != nil {
		return nil, err
	}
	me := core.ThisHost()
	cl := &client{target: target}
	hello, err := cl.hello()
	if err != nil {
		f := strings.Fields(pub)
		send := ""
		if canSend {
			send = " --send"
		}
		allow := fmt.Sprintf("sunstack hub allow %s --id %s --key '%s %s'%s", me.Name, me.ID, f[0], f[1], send)
		fmt.Fprintf(out, "Adding this host on %s with your own SSH login (it may ask once)...\n", target)
		cmd := exec.Command(sshProgram(), "-o", "ConnectTimeout=10", target, allow)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, out
		if rerr := cmd.Run(); rerr == nil {
			hello, err = cl.hello()
		}
		if err != nil {
			fmt.Fprintf(out, "\nRun this on the hub, then run sunstack org join %s again:\n\n  %s\n\n", target, allow)
			return nil, failErr("pending", "the hub has not allowed this host yet")
		}
	}
	c := &Config{OrgID: hello.OrgID, OrgName: hello.OrgName, Hub: target}
	if err := saveConfig(c); err != nil {
		return nil, err
	}
	if err := PushOnce(); err != nil {
		fmt.Fprintf(out, "first snapshot not pushed: %v\n", err)
	}
	if _, err := PullOnce(); err != nil {
		fmt.Fprintf(out, "roster not pulled: %v\n", err)
	}
	return c, nil
}

// Leave removes this host's org files. The hub entry goes with hub revoke.
func Leave() error {
	c, err := LoadConfig()
	if err != nil {
		return failErr("org", "%v", err)
	}
	if c == nil {
		return failErr("org", "this host is in no org")
	}
	if c.IsHub() {
		return failErr("hub", "the hub cannot leave its own org")
	}
	os.RemoveAll(remoteDir())
	os.RemoveAll(outboxDir())
	return os.Remove(configPath())
}

// readLine reads one line, for the watch protocol.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > MaxFrame+4096 {
			return nil, errors.New("frame too large")
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

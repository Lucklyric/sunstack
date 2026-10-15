package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Managed hosts (§25): a host that never dials out. The hub pulls it over
// SSH with its own key, which the managed host's authorized_keys limits to
// `sunstack org managed-serve`. A pull is a finite exchange of watch frames
// on the SSH pipe:
//
//	host: hello
//	hub:  roster, mail…, receipt…, end
//	host: ack…, mail…, snap, sent
//	hub:  stored|refused|retry…, end
//	host: bye
//
// Each side reads all of the other's turn before writing its own, so a full
// pipe never blocks both. The managed host stays the authority for itself:
// its own sunstack opens every envelope and checks its pins and grants.

// ManagedMode is org.json's mode on a managed host.
const ManagedMode = "managed"

const (
	managedLiveFor = 90 * time.Second // three pulls (§25.4)
	pullBatch      = 50               // envelopes the hub sends in one pull
	pullBytes      = 4 << 20          // and their total size
	pullWork       = 60 * time.Second // a managed host starts no request later than this into a pull
	pullLimit      = 4 * time.Minute  // the whole SSH call: the hub cannot see a request's kind, and an update takes up to 150 s
	heldMax        = 100              // held mail per managed host
	keepFor        = 14 * 24 * time.Hour
)

// managedEvery is how often the hub pulls each managed host; tests shorten
// it with SUNSTACK_MANAGED_MS.
func managedEvery() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("SUNSTACK_MANAGED_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 30 * time.Second
}

func managedErr(what string) error {
	return failErr("managed_host", "this host is managed: the hub pulls it over SSH, and it %s", what)
}

// IsManagedHost reports whether this host is a managed host.
func IsManagedHost() bool {
	c, _ := LoadConfig()
	return c.IsManaged()
}

// RefuseManaged refuses what a managed host never does (§25.5).
func RefuseManaged(what string) error {
	if IsManagedHost() {
		return managedErr(what)
	}
	return nil
}

// pullDeadline is set while this process serves a pull: control requests
// then run at once, and none starts after it.
var pullDeadline time.Time

func pulling() bool { return !pullDeadline.IsZero() }

// In a pull, an update runs only as its first request, and no request runs
// after it (§25.4).
var pullRequests, pullUpdated int

// Paths on the hub, per managed host.
func (s store) managedDir(id string) string { return filepath.Join(s.dir, "managed", id) }
func (s store) deliveredFile(id, msg string) string {
	return filepath.Join(s.managedDir(id), "delivered", msg+".json")
}
func (s store) heldDir(id string) string { return filepath.Join(s.managedDir(id), "held") }
func (s store) settledFile(id, msg string) string {
	return filepath.Join(s.managedDir(id), "settled", msg+".json")
}

// Paths on a managed host: what it imported, and from which host.
func importedDir() string { return filepath.Join(remoteDir(), "imported") }

// noteImported records, on a managed host, that a message came from its
// sending host, so a reply to it may go back (§25.5).
func noteImported(m *Mail) error {
	if !IsManagedHost() || !isFileName(m.ID) {
		return nil
	}
	return writeJSON(filepath.Join(importedDir(), m.ID+".json"), map[string]string{"from_host": m.FromHost, "at": stamp(time.Now())}, 0o600)
}

func importedFrom(id, host string) bool {
	var r struct {
		FromHost string `json:"from_host"`
	}
	return isFileName(id) && readJSON(filepath.Join(importedDir(), id+".json"), &r) == nil && r.FromHost == host
}

func pruneDir(dir string, age time.Duration) {
	for _, p := range jsonFiles(dir) {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > age {
			os.Remove(p)
		}
	}
}

// ---- On the managed host ----

var orgIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

func authorizedKeys() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "authorized_keys"), nil
}

// setHubKey puts the hub's key line in authorized_keys, replacing an
// earlier one and keeping every other line; an empty key removes it.
func setHubKey(line string) error {
	path, err := authorizedKeys()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var kept []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l != "" && !strings.Contains(l, " sunstack-managed:") {
			kept = append(kept, l)
		}
	}
	if line != "" {
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	if out == string(b) {
		return nil
	}
	return writeFile(path, []byte(out), 0o600)
}

// ManagedSetup makes this host a managed host of the org (§25.3): org.json
// in managed mode, and the hub's key, limited to managed-serve.
func ManagedSetup(orgID, orgName, hubID, hubKey string) error {
	f := strings.Fields(hubKey)
	switch {
	case !orgIDRe.MatchString(orgID):
		return usageErr("--org takes the org ID that sunstack hub manage printed")
	case !nameRe.MatchString(orgName):
		return usageErr("--org-name: letters, digits, . _ - only")
	case !idRe.MatchString(hubID):
		return usageErr("--hub takes the hub's host ID that sunstack hub manage printed")
	case len(f) < 2 || f[0] != "ssh-ed25519":
		return usageErr("--hub-key takes the hub's ssh-ed25519 public key that sunstack hub manage printed")
	}
	if raw, err := base64.StdEncoding.DecodeString(f[1]); err != nil || len(raw) < 32 {
		return usageErr("--hub-key is not a valid public key")
	}
	c, err := LoadConfig()
	if err != nil {
		return failErr("org", "%v", err)
	}
	if c != nil && !(c.IsManaged() && c.OrgID == orgID) {
		return failErr("org", "this host is already in org %s (sunstack org leave first)", c.OrgName)
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	if strings.ContainsAny(exe, "\"\\ \t\n'$`") {
		return failErr("path", "sunstack's path %q cannot go in a forced command; install it under a plain path", exe)
	}
	if _, err := LoadKeys(); err != nil {
		return err
	}
	nc := &Config{Mode: ManagedMode, OrgID: orgID, OrgName: orgName, HubID: hubID, Path: os.Getenv("PATH")}
	if err := saveConfig(nc); err != nil {
		return err
	}
	line := fmt.Sprintf(`restrict,command="%s org managed-serve" ssh-ed25519 %s sunstack-managed:%s`, exe, f[1], orgID)
	return setHubKey(line)
}

// frameWriter writes one frame per line.
type frameWriter struct{ enc *json.Encoder }

func (w frameWriter) put(f frame) error { return w.enc.Encode(f) }

// readTurn reads frames until the one named end, within limits.
func readTurn(br *bufio.Reader, end string, each func(*frame) error) error {
	total := 0
	for n := 0; ; n++ {
		line, err := readLine(br)
		if err != nil {
			return err
		}
		if total += len(line); total > pullBytes+(1<<20) || n > 4*pullBatch+16 {
			return errors.New("too much in one turn")
		}
		var f frame
		if json.Unmarshal(line, &f) != nil {
			return errors.New("not a frame")
		}
		if f.Op == end {
			return nil
		}
		if err := each(&f); err != nil {
			return err
		}
	}
}

// ManagedServe is the forced command the hub's key runs (§25.4). It takes no
// arguments and reads only frames on stdin.
func ManagedServe(in io.Reader, out io.Writer) error {
	w := frameWriter{json.NewEncoder(out)}
	c, err := LoadConfig()
	if err != nil || !c.IsManaged() {
		w.put(frame{Op: "refused", Reason: "this host is not set up as a managed host (sunstack org managed)"})
		return failErr("not_managed", "this host is not a managed host")
	}
	if c.Path != "" {
		os.Setenv("PATH", c.Path)
	}
	// One pull at a time on this host: a pull the hub gave up on may still
	// be running here.
	unlock, err := core.LockDirOnce(connectLock())
	if err != nil {
		w.put(frame{Op: "refused", Reason: "an earlier pull still runs on this host"})
		return failErr("busy", "an earlier pull still runs on this host")
	}
	defer unlock()
	// Requests an earlier pull was cut off in get their answer (§20.3).
	RecoverRequests()
	pullDeadline = time.Now().Add(pullWork)
	keys, err := LoadKeys()
	if err != nil {
		return err
	}
	me, pub := core.ThisHost(), keys.Public()
	if err := w.put(frame{Op: "hello", Hello: &Hello{OrgID: c.OrgID, OrgName: c.OrgName, Version: Version, HostID: me.ID, Name: me.Name, Machine: core.MachineName(), Keys: &pub}}); err != nil {
		return err
	}
	br := bufio.NewReaderSize(in, 64<<10)
	var mail []*Mail
	err = readTurn(br, "end", func(f *frame) error {
		switch f.Op {
		case "roster":
			if f.Roster == nil || f.Roster.OrgID != c.OrgID {
				return errors.New("a roster of another org")
			}
			return takeRoster(f.Roster, f.HubTime)
		case "mail":
			if f.Mail != nil {
				mail = append(mail, f.Mail)
			}
		case "receipt":
			if f.Receipt != nil {
				// A receipt not recorded fails the pull, so the hub keeps it.
				return record(f.Receipt)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	pruneDir(importedDir(), keepFor)
	for _, m := range mail {
		status, reason, final := Deliver(m)
		if final {
			if err := w.put(frame{Op: "ack", ID: m.ID, Status: status, Reason: reason}); err != nil {
				return err
			}
		}
	}
	size, n := 0, 0
	for _, p := range jsonFiles(outboxDir()) {
		var s Sent
		if readJSON(p, &s) != nil {
			continue
		}
		b, _ := json.Marshal(s.Mail)
		// Within the hub's limits for one turn; the rest goes next time.
		if n >= pullBatch || (n > 0 && size+len(b) > pullBytes) {
			break
		}
		size, n = size+len(b), n+1
		if err := w.put(frame{Op: "mail", Mail: &s.Mail}); err != nil {
			return err
		}
	}
	if b, err := Snapshot(); err == nil {
		if err := w.put(frame{Op: "snap", Snap: &Snap{Host: me.ID, Snapshot: b}}); err != nil {
			return err
		}
	}
	if err := w.put(frame{Op: "sent"}); err != nil {
		return err
	}
	err = readTurn(br, "end", func(f *frame) error {
		switch f.Op {
		case "stored", "refused", "retry":
			settle(f.ID, f.Op, f.Reason)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return w.put(frame{Op: "bye"})
}

// ---- On the hub ----

// ManagedKey is the hub's key for pulls, made on first use: no passphrase,
// since a launchd agent cannot unlock one (§25.3).
func ManagedKey() (path, pub string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(home, ".ssh", "sunstack_managed")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", "", err
		}
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "sunstack-managed", "-f", path).CombinedOutput(); err != nil {
			return "", "", failErr("ssh_keygen", "could not make %s: %v %s", path, err, strings.TrimSpace(string(out)))
		}
	}
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(string(b))
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return "", "", failErr("ssh_key", "%s.pub is not an ed25519 key", path)
	}
	return path, f[0] + " " + f[1], nil
}

var aliasRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// sshArgs resolves the alias to its address only, then builds the call
// with none of the user's other settings (§25.4).
func sshArgs(alias, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", alias).Output()
	if err != nil {
		return nil, failErr("ssh", "ssh -G %s failed: %v", alias, err)
	}
	cfg := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			cfg[strings.ToLower(k)] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"proxyjump", "proxycommand"} {
		if v := cfg[k]; v != "" && v != "none" {
			return nil, failErr("ssh", "%s uses %s, which a managed host's pull does not support", alias, k)
		}
	}
	host, user, port := cfg["hostname"], cfg["user"], cfg["port"]
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " @") {
		return nil, failErr("ssh", "no usable host name for %s", alias)
	}
	if port == "" {
		port = "22"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, failErr("ssh", "bad port %q for %s", port, alias)
	}
	target := host
	if user != "" {
		if strings.ContainsAny(user, " @") || strings.HasPrefix(user, "-") {
			return nil, failErr("ssh", "bad user %q for %s", user, alias)
		}
		target = user + "@" + host
	}
	return []string{"-F", "/dev/null", "-T", "-i", key,
		"-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "ControlMaster=no", "-o", "ControlPath=none",
		"-o", "ClearAllForwardings=yes", "-o", "ForwardAgent=no", "-o", "PermitLocalCommand=no",
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=~/.ssh/known_hosts",
		"-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-p", port, target}, nil
}

// notSetUp is a pull that never got a valid hello.
type notSetUp struct{ why string }

func (e *notSetUp) Error() string { return e.why }

var pullLocks sync.Map // one pull per managed host in this process

// pull runs one pull of a managed host. With register, the host is not in
// the roster yet: its hello adds it.
func (s store) pullManaged(c *Config, name, alias string, register bool) (*Hello, error) {
	mu, _ := pullLocks.LoadOrStore(strings.ToLower(name), &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	unlock, err := core.LockDir(filepath.Join(s.dir, "managed", ".pull-"+strings.ToLower(name)), "a pull of "+name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	key, _, err := ManagedKey()
	if err != nil {
		return nil, err
	}
	args, err := sshArgs(alias, key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pullLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limited{&stderr, 4096}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			cmd.Wait()
		}
	}()
	br := bufio.NewReaderSize(stdout, 64<<10)

	// Nothing is written before the managed host's hello: without the
	// forced command, the other end would be a shell.
	type first struct {
		f   frame
		err error
	}
	got := make(chan first, 1)
	go func() {
		line, err := readLine(br)
		var f frame
		if err == nil {
			err = json.Unmarshal(line, &f)
		}
		got <- first{f, err}
	}()
	var h *Hello
	select {
	case r := <-got:
		switch {
		case r.err != nil:
			return nil, &notSetUp{strings.TrimSpace(stderr.String())}
		case r.f.Op == "refused":
			return nil, &notSetUp{r.f.Reason}
		case r.f.Op != "hello" || r.f.Hello == nil:
			return nil, &notSetUp{"it did not answer as a managed host"}
		}
		h = r.f.Hello
	case <-time.After(10 * time.Second):
		return nil, &notSetUp{"no answer in 10 seconds"}
	}
	switch {
	case h.OrgID != c.OrgID:
		return nil, &notSetUp{"it is set up for another org"}
	case !idRe.MatchString(h.HostID) || !nameRe.MatchString(h.Name) || h.Keys == nil || !h.Keys.valid():
		return nil, errors.New("its hello is not valid")
	}
	if register {
		err = s.withRoster(func(r *Roster) error {
			for _, x := range r.Hosts {
				if x.ID == h.HostID {
					return failErr("conflict", "host ID %s is already in the org as %s", h.HostID, x.Name)
				}
				if strings.EqualFold(x.Name, name) {
					return failErr("conflict", "the name %s is already used by another host", name)
				}
			}
			r.Hosts = append(r.Hosts, &Host{ID: h.HostID, Name: name, Keys: *h.Keys, Added: stamp(time.Now()), Managed: true, SSH: alias})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	ro, err := s.liveRoster()
	if err != nil {
		return nil, err
	}
	me := ro.byID(h.HostID)
	if me == nil || !me.Managed || !strings.EqualFold(me.Name, name) {
		return nil, fmt.Errorf("%s answered as host %s, which is not the managed host %s", alias, h.HostID, name)
	}
	if !me.Keys.equal(*h.Keys) {
		return nil, fmt.Errorf("%s's sunstack key changed; revoke it and manage it again after checking its key (sunstack org keys there)", name)
	}

	// A revoke ends the SSH call within a second, not only at the next
	// frame: requests the host already started may still finish there.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if r, _ := s.roster(); r == nil || r.byID(me.ID) == nil {
					cancel()
					return
				}
			}
		}
	}()

	// The hub's turn.
	if r, _ := s.roster(); r == nil || r.byID(me.ID) == nil {
		return nil, errors.New("revoked during the pull")
	}
	w := frameWriter{json.NewEncoder(stdin)}
	if err := w.put(frame{Op: "roster", Roster: ro, HubTime: stamp(time.Now())}); err != nil {
		return nil, err
	}
	pruneDir(filepath.Join(s.managedDir(me.ID), "delivered"), keepFor)
	size := 0
	for i, p := range jsonFiles(s.mailDir(me.ID)) {
		var m Mail
		fi, err := os.Stat(p)
		if i >= pullBatch || err != nil || readJSON(p, &m) != nil {
			continue
		}
		if size += int(fi.Size()); size > pullBytes {
			break
		}
		if r, _ := s.roster(); r == nil || r.byID(me.ID) == nil {
			return nil, errors.New("revoked during the pull")
		}
		// Recorded first: a reply in this same pull may answer it.
		if err := writeJSON(s.deliveredFile(me.ID, m.ID), map[string]string{"from_host": m.FromHost, "at": stamp(time.Now())}, 0o600); err != nil {
			return nil, err
		}
		if err := w.put(frame{Op: "mail", Mail: &m}); err != nil {
			return nil, err
		}
	}
	s.pruneReceipts(me.ID)
	var sentRcpt []string // removed once the host has them: it records each once
	for _, p := range jsonFiles(s.receiptDir(me.ID)) {
		var rc Receipt
		if len(sentRcpt) >= 2*pullBatch || readJSON(p, &rc) != nil {
			continue
		}
		if err := w.put(frame{Op: "receipt", Receipt: &rc}); err != nil {
			return nil, err
		}
		sentRcpt = append(sentRcpt, p)
	}
	if err := w.put(frame{Op: "end"}); err != nil {
		return nil, err
	}

	// Its turn: acks, its mail, its snapshot.
	var answers []frame
	err = readTurn(br, "sent", func(f *frame) error {
		if r, _ := s.roster(); r == nil || r.byID(me.ID) == nil {
			return errors.New("revoked during the pull")
		}
		switch f.Op {
		case "ack":
			s.ack(me.ID, f.ID, f.Status, f.Reason)
		case "snap":
			if f.Snap != nil {
				if err := s.putSnap(me.ID, f.Snap.Snapshot); err != nil {
					return err
				}
			}
		case "mail":
			if f.Mail != nil {
				answers = append(answers, s.putManaged(me.ID, f.Mail))
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pull of %s: %v %s", name, err, strings.TrimSpace(stderr.String()))
	}
	for _, a := range answers {
		if err := w.put(a); err != nil {
			return nil, err
		}
	}
	if err := w.put(frame{Op: "end"}); err != nil {
		return nil, err
	}
	stdin.Close()
	if err := readTurn(br, "bye", func(*frame) error { return nil }); err != nil {
		return nil, fmt.Errorf("pull of %s ended early: %v", name, err)
	}
	waited = true
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("pull of %s: %v %s", name, err, strings.TrimSpace(stderr.String()))
	}
	if r, _ := s.roster(); r == nil || r.byID(me.ID) == nil {
		return nil, errors.New("revoked during the pull")
	}
	for _, p := range sentRcpt {
		os.Remove(p)
	}
	s.touch(me.ID, h.Version, h.Machine)
	return h, nil
}

type limited struct {
	b *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil
}

// putManaged takes an envelope from a managed host: a reply to mail the
// hub delivered to it from the host it now writes to goes on; anything
// else is held for the user (§25.5).
func (s store) putManaged(from string, m *Mail) frame {
	if !core.IsMessageID(m.ID) {
		return frame{Op: "refused", Reason: "invalid message id"}
	}
	if m.FromHost != from {
		return frame{Op: "refused", ID: m.ID, Reason: "the message names another sending host"}
	}
	var done struct {
		Status string `json:"status"`
	}
	if readJSON(s.settledFile(from, m.ID), &done) == nil {
		if done.Status == "dropped" {
			return frame{Op: "refused", ID: m.ID, Reason: "dropped by the hub's user"}
		}
		return frame{Op: "stored", ID: m.ID}
	}
	var d struct {
		FromHost string `json:"from_host"`
	}
	if isFileName(m.InReply) && readJSON(s.deliveredFile(from, m.InReply), &d) == nil && d.FromHost == m.ToHost {
		return s.putMail(from, m)
	}
	unlock, err := s.heldLock(from)
	if err != nil {
		return frame{Op: "retry", ID: m.ID, Reason: "the hub is busy"}
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(s.heldDir(from), m.ID+".json")); err == nil {
		return frame{Op: "stored", ID: m.ID}
	}
	if readJSON(s.settledFile(from, m.ID), &done) == nil {
		return frame{Op: "refused", ID: m.ID, Reason: "dropped"}
	}
	s.pruneHeld(from)
	if len(jsonFiles(s.heldDir(from))) >= heldMax {
		return frame{Op: "retry", ID: m.ID, Reason: "the hub holds too much mail from this host"}
	}
	b, _ := json.Marshal(m)
	if len(b) > MaxFrame {
		return frame{Op: "refused", ID: m.ID, Reason: "message larger than 256 KB"}
	}
	if err := writeJSON(filepath.Join(s.heldDir(from), m.ID+".json"), &held{Mail: *m, At: stamp(time.Now())}, 0o600); err != nil {
		return frame{Op: "retry", ID: m.ID, Reason: "hub could not store the message"}
	}
	return frame{Op: "stored", ID: m.ID}
}

type held struct {
	Mail Mail   `json:"mail"`
	At   string `json:"at"`
}

func (s store) heldLock(id string) (func(), error) {
	return core.LockDir(filepath.Join(s.managedDir(id), ".held"), "held mail")
}

// pruneHeld drops held mail past keepFor, keeping the decision so a resend
// is refused rather than held again; called under heldLock.
func (s store) pruneHeld(id string) {
	pruneDir(filepath.Join(s.managedDir(id), "settled"), 2*keepFor)
	for _, p := range jsonFiles(s.heldDir(id)) {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > keepFor {
			if writeJSON(s.settledFile(id, base(p)), map[string]string{"status": "dropped", "reason": "expired", "at": stamp(time.Now())}, 0o600) == nil {
				os.Remove(p)
			}
		}
	}
}

// HeldMail is one held envelope as `hub held` lists it.
type HeldMail struct {
	ID, From, To string
	Size         int
	At           string
}

// Held lists mail held from managed hosts (§25.5).
func Held() ([]HeldMail, error) {
	_, s, err := hubConfig()
	if err != nil {
		return nil, err
	}
	r, err := s.roster()
	if err != nil {
		return nil, err
	}
	name := func(id string) string {
		if h := r.byID(id); h != nil {
			return h.Name
		}
		return id
	}
	var out []HeldMail
	for _, h := range r.Hosts {
		if !h.Managed {
			continue
		}
		if unlock, err := s.heldLock(h.ID); err == nil {
			s.pruneHeld(h.ID)
			unlock()
		}
		for _, p := range jsonFiles(s.heldDir(h.ID)) {
			var x held
			if readJSON(p, &x) == nil {
				out = append(out, HeldMail{ID: x.Mail.ID, From: h.Name, To: name(x.Mail.ToHost), Size: len(x.Mail.Sealed), At: x.At})
			}
		}
	}
	return out, nil
}

// SettleHeld delivers (release) or drops one held envelope. Release skips
// only the reply rule: the roster and the receiving host's own checks
// still apply.
func SettleHeld(id string, release bool) error {
	_, s, err := hubConfig()
	if err != nil {
		return err
	}
	if !core.IsMessageID(id) {
		return usageErr("invalid message id")
	}
	r, err := s.roster()
	if err != nil {
		return err
	}
	for _, h := range r.Hosts {
		if !h.Managed {
			continue
		}
		unlock, err := s.heldLock(h.ID)
		if err != nil {
			return err
		}
		path := filepath.Join(s.heldDir(h.ID), id+".json")
		var x held
		if readJSON(path, &x) != nil {
			unlock()
			continue
		}
		defer unlock()
		status := "dropped"
		if release {
			if f := s.putMail(h.ID, &x.Mail); f.Op != "stored" {
				return failErr("refused", "the hub could not deliver %s: %s", id, f.Reason)
			}
			status = "released"
		}
		if err := writeJSON(s.settledFile(h.ID, id), map[string]string{"status": status, "at": stamp(time.Now())}, 0o600); err != nil {
			return err
		}
		return os.Remove(path)
	}
	return failErr("not_found", "no held mail %s (sunstack hub held)", id)
}

// ManageResult is what hub manage did.
type ManageResult struct {
	Name, Alias string
	Setup       string // the line to run on the host when it is not set up yet
	Why         string // why the pull did not reach a managed host
	Hello       *Hello
}

// Manage adds a managed host to the org, or prints how to set it up
// (§25.3).
func Manage(name, alias string) (*ManageResult, error) {
	c, s, err := hubConfig()
	if err != nil {
		return nil, err
	}
	if !nameRe.MatchString(name) {
		return nil, usageErr("host name: letters, digits, . _ - only")
	}
	if !aliasRe.MatchString(alias) {
		return nil, usageErr("--ssh takes an SSH alias or host name")
	}
	r, err := s.roster()
	if err != nil {
		return nil, err
	}
	if h := r.byName(name); h != nil || hasName(r, name) {
		return nil, failErr("conflict", "the name %s is already in the org (sunstack hub hosts)", name)
	}
	_, pub, err := ManagedKey()
	if err != nil {
		return nil, err
	}
	res := &ManageResult{Name: name, Alias: alias}
	h, err := s.pullManaged(c, name, alias, true)
	var ns *notSetUp
	if errors.As(err, &ns) {
		res.Why = ns.why
		res.Setup = fmt.Sprintf(`sunstack org managed --org %s --org-name %s --hub %s --hub-key "%s"`, c.OrgID, c.OrgName, c.HubID, pub)
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	res.Hello = h
	return res, nil
}

func hasName(r *Roster, name string) bool {
	for _, h := range r.Hosts {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

// The hub's pulls run in its connector (§25.4).
var (
	managedWake = make(chan struct{}, 1)
	lastPull    sync.Map // host ID -> time.Time of the last pull that finished
	lastTry     sync.Map // host ID -> time.Time of the last pull started
	running     sync.Map // host ID -> a pull is running
)

// wakeManaged asks for a pull now, when mail waits for a managed host.
func wakeManaged() {
	select {
	case managedWake <- struct{}{}:
	default:
	}
}

// refreshManaged wakes the pulls and waits, up to limit, for every managed
// host to have been pulled in the last 10 seconds.
func (s store) refreshManaged(limit time.Duration) {
	r, err := s.roster()
	if err != nil {
		return
	}
	wakeManaged()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		fresh := true
		for _, h := range r.Hosts {
			if !h.Managed {
				continue
			}
			t, _ := lastPull.Load(h.ID)
			if t == nil || time.Since(t.(time.Time)) > 10*time.Second {
				fresh = false
			}
		}
		if fresh {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pullLoop pulls every managed host on its interval, and at once when woken.
func pullLoop(c *Config, logw io.Writer) {
	s := store{hubDir(c.OrgID)}
	tick := time.NewTicker(managedEvery() / 3)
	defer tick.Stop()
	for {
		woken := false
		select {
		case <-tick.C:
		case <-managedWake:
			woken = true
		}
		r, err := s.roster()
		if err != nil {
			continue
		}
		for _, h := range r.Hosts {
			if !h.Managed || h.SSH == "" {
				continue
			}
			if t, ok := lastTry.Load(h.ID); ok && !woken && time.Since(t.(time.Time)) < managedEvery() {
				continue
			}
			// Each host on its own, so one slow host holds up no other; a
			// host still being pulled is skipped.
			if _, busy := running.LoadOrStore(h.ID, true); busy {
				continue
			}
			lastTry.Store(h.ID, time.Now())
			go func(h *Host) {
				defer running.Delete(h.ID)
				if _, err := s.pullManaged(c, h.Name, h.SSH, false); err != nil {
					fmt.Fprintf(logw, "%s pull of %s: %v\n", stamp(time.Now()), h.Name, err)
					return
				}
				lastPull.Store(h.ID, time.Now())
			}(h)
		}
	}
}

// OrgID is this host's org ID, or empty.
func OrgID() string {
	if c, _ := LoadConfig(); c != nil {
		return c.OrgID
	}
	return ""
}

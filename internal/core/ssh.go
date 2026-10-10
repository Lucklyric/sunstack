package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SSH status (§23): which SSH aliases share one connection (ControlMaster),
// whether that connection is open now, and the last login check. Sunstack
// never opens SSH for agents. It reads ~/.ssh/config only when the user runs
// sunstack ssh scan, and the views ask only the recorded sockets.

// SSHAlias is what sunstack ssh scan found for one alias.
type SSHAlias struct {
	ControlMaster string `json:"control_master"`
	ControlPath   string `json:"control_path"`
}

// SSHCheck is the result of the last sunstack ssh check.
type SSHCheck struct {
	Result string `json:"result"` // ok, auth failed, host key failed, timeout, unreachable, unknown
	At     string `json:"at"`
}

// SSHState is ~/.sunstack/ssh.json.
type SSHState struct {
	Scanned string               `json:"scanned,omitempty"`
	Aliases map[string]*SSHAlias `json:"aliases"`
	Hosts   map[string]string    `json:"hosts"` // org host ID -> alias
	Checks  map[string]*SSHCheck `json:"checks"`
	Started map[string]string    `json:"started,omitempty"` // alias -> when sunstack ssh start opened its master
}

func sshStatePath() string { return filepath.Join(Home(), "ssh.json") }

// LoadSSH reads the recorded SSH state; a missing file is an empty state.
func LoadSSH() *SSHState {
	s := &SSHState{}
	if b, err := os.ReadFile(sshStatePath()); err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Aliases == nil {
		s.Aliases = map[string]*SSHAlias{}
	}
	if s.Hosts == nil {
		s.Hosts = map[string]string{}
	}
	if s.Checks == nil {
		s.Checks = map[string]*SSHCheck{}
	}
	if s.Started == nil {
		s.Started = map[string]string{}
	}
	return s
}

func (s *SSHState) save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return err
	}
	return writeAtomic(sshStatePath(), append(b, '\n'))
}

// Shared says whether an alias's connections go through one master.
func (a *SSHAlias) Shared() bool {
	switch strings.ToLower(a.ControlMaster) {
	case "yes", "auto", "ask", "autoask":
		return a.ControlPath != "" && a.ControlPath != "none"
	}
	return false
}

// configAliases are the Host names in ~/.ssh/config without wildcards.
func configAliases() []string {
	home, _ := os.UserHomeDir()
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil
	}
	defer f.Close()
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
			continue
		}
		for _, n := range fields[1:] {
			if strings.HasPrefix(n, "#") {
				break
			}
			if strings.ContainsAny(n, "*?!") || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// sshRun runs ssh with an argument list, no stdin, within limit.
func sshRun(limit time.Duration, args ...string) (stdout, stderr string, code int, timedOut bool) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &limitWriter{&o, 64 << 10}, &limitWriter{&e, 64 << 10}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return o.String(), e.String(), -1, true
	}
	if x, ok := err.(*exec.ExitError); ok {
		return o.String(), e.String(), x.ExitCode(), false
	}
	if err != nil {
		return o.String(), err.Error(), -1, false
	}
	return o.String(), e.String(), 0, false
}

type limitWriter struct {
	b *strings.Builder
	n int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if left := w.n - w.b.Len(); left > 0 {
		if len(p) > left {
			w.b.Write(p[:left])
		} else {
			w.b.Write(p)
		}
	}
	return len(p), nil
}

// SSHScan records each alias's effective ControlMaster and ControlPath,
// from ssh -G. It evaluates the user's configuration, so it is user-only.
func SSHScan() (*SSHState, error) {
	s := LoadSSH()
	s.Aliases = map[string]*SSHAlias{}
	for _, alias := range configAliases() {
		out, _, code, _ := sshRun(5*time.Second, "-G", "--", alias)
		if code != 0 {
			continue
		}
		a := &SSHAlias{}
		for _, line := range strings.Split(out, "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
			switch strings.ToLower(k) {
			case "controlmaster":
				a.ControlMaster = v
			case "controlpath":
				a.ControlPath = v
			}
		}
		s.Aliases[alias] = a
	}
	s.Scanned = now()
	return s, s.save()
}

// SSHMap names the alias for an org host, by the host's ID.
func SSHMap(hostID, alias string) error {
	s := LoadSSH()
	if alias == "" {
		delete(s.Hosts, hostID)
	} else {
		s.Hosts[hostID] = alias
	}
	return s.save()
}

// MasterOpen says whether a master answers on path. It runs with -F
// /dev/null, so the user's configuration is not evaluated again.
func MasterOpen(path string) bool {
	if path == "" || path == "none" {
		return false
	}
	if st, err := os.Stat(path); err != nil || st.Mode()&os.ModeSocket == 0 {
		return false
	}
	_, _, code, _ := sshRun(3*time.Second, "-F", "/dev/null", "-o", "ControlPath="+path, "-O", "check", "sunstack-check")
	return code == 0
}

// RunSSHCheck tries a fresh login to alias that neither makes nor uses a
// master, and records the result.
func RunSSHCheck(alias string) (string, error) {
	_, errOut, code, timedOut := sshRun(15*time.Second, SSHCheckArgs(alias)...)
	result := checkResult(errOut, code, timedOut)
	s := LoadSSH()
	s.Checks[alias] = &SSHCheck{Result: result, At: now()}
	return result, s.save()
}

// SSHCheckArgs are the arguments of a check: a fresh login with no stdin
// that neither makes nor uses a master.
func SSHCheckArgs(alias string) []string {
	return []string{"-n", "-o", "BatchMode=yes", "-o", "ControlMaster=no", "-o", "ControlPath=none",
		"-o", "ConnectTimeout=5", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "--", alias, "true"}
}

func checkResult(errOut string, code int, timedOut bool) string {
	result := "unknown"
	low := strings.ToLower(errOut)
	switch {
	case timedOut:
		result = "timeout"
	case code == 0:
		result = "ok"
	case strings.Contains(low, "host key verification failed") || strings.Contains(low, "remote host identification has changed"):
		result = "host key failed"
	case strings.Contains(low, "permission denied"):
		result = "auth failed"
	case strings.Contains(low, "timed out"):
		result = "timeout"
	case strings.Contains(low, "could not resolve") || strings.Contains(low, "connection refused") || strings.Contains(low, "no route") || strings.Contains(low, "network is unreachable"):
		result = "unreachable"
	}
	return result
}

// SSHMasterPath is the recorded socket of a shared alias, or an error that
// says why there is none.
func SSHMasterPath(alias string) (string, error) {
	a := LoadSSH().Aliases[alias]
	switch {
	case a == nil:
		return "", &Error{Code: ExitFail, Reason: "not_found", Msg: "no SSH alias " + alias + " (sunstack ssh scan reads ~/.ssh/config)"}
	case !a.Shared():
		return "", &Error{Code: ExitFail, Reason: "ssh", Msg: alias + " shares no connection: its ~/.ssh/config entry needs ControlMaster auto and a ControlPath (sunstack ssh setup prints one), then sunstack ssh scan"}
	}
	return a.ControlPath, nil
}

// SSHStart opens a master for alias in the user's terminal, so a passphrase
// or key-agent prompt can be answered, and confirms that its socket answers.
// It reports false when a master was already open.
func SSHStart(alias string, stdin io.Reader, stdout, stderr io.Writer) (bool, error) {
	path, err := SSHMasterPath(alias)
	if err != nil {
		return false, err
	}
	if MasterOpen(path) {
		return false, nil
	}
	if err := sshDirSafe(filepath.Dir(path)); err != nil {
		return false, &Error{Code: ExitFail, Reason: "ssh", Msg: err.Error()}
	}
	cmd := exec.Command("ssh", "-fN", "-o", "ControlMaster=yes", "-o", "ControlPath="+path, "--", alias)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return false, &Error{Code: ExitFail, Reason: "ssh", Msg: "ssh " + alias + " did not open a connection: " + err.Error()}
	}
	if !MasterOpen(path) {
		return false, &Error{Code: ExitFail, Reason: "ssh", Msg: "ssh " + alias + " exited, but no connection answers at " + path}
	}
	s := LoadSSH()
	s.Started[alias] = now()
	return true, s.save()
}

// SSHStop closes the master of alias, and with it every shell running
// through it. It reports false when none was open.
func SSHStop(alias string) (bool, error) {
	path, err := SSHMasterPath(alias)
	if err != nil {
		return false, err
	}
	s := LoadSSH()
	if !MasterOpen(path) {
		delete(s.Started, alias)
		return false, s.save()
	}
	_, errOut, code, _ := sshRun(5*time.Second, "-F", "/dev/null", "-o", "ControlPath="+path, "-O", "exit", "sunstack-check")
	if code != 0 {
		return false, &Error{Code: ExitFail, Reason: "ssh", Msg: "ssh -O exit: " + strings.TrimSpace(errOut)}
	}
	delete(s.Started, alias)
	return true, s.save()
}

// sshDirSafe requires the socket folder to be a real folder, owned by this
// user and closed to everyone else.
func sshDirSafe(dir string) error {
	st, err := os.Lstat(dir)
	switch {
	case err != nil:
		return fmt.Errorf("the socket folder %s is missing: mkdir -p -m 700 %s", dir, dir)
	case st.Mode()&os.ModeSymlink != 0 || !st.IsDir():
		return fmt.Errorf("the socket folder %s is not a plain folder", dir)
	case st.Mode().Perm() != 0o700:
		return fmt.Errorf("the socket folder %s is mode %o: chmod 700 %s", dir, st.Mode().Perm(), dir)
	case !ownedByMe(st):
		return fmt.Errorf("the socket folder %s belongs to another user", dir)
	}
	return nil
}

// SSHPeer is what a host's snapshot says about its SSH to one other org
// host: no alias, user, address, key or socket path, since snapshots are not
// sealed (§23.1).
type SSHPeer struct {
	Host    string `json:"host"` // the destination's host ID
	Sharing bool   `json:"sharing"`
	Master  bool   `json:"master"`
	Check   string `json:"check,omitempty"`
	CheckAt string `json:"check_at,omitempty"`
}

// SSHPeers are this host's mapped org hosts, for the snapshot.
func SSHPeers() []SSHPeer {
	s := LoadSSH()
	var out []SSHPeer
	for id, alias := range s.Hosts {
		p := SSHPeer{Host: id}
		if a := s.Aliases[alias]; a != nil && a.Shared() {
			p.Sharing, p.Master = true, MasterOpen(a.ControlPath)
		}
		if c := s.Checks[alias]; c != nil {
			p.Check, p.CheckAt = c.Result, c.At
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// Text is a peer's state for the views: "ok 3m ago, connection open".
func (p SSHPeer) Text() string {
	var parts []string
	if p.Check != "" {
		parts = append(parts, "last check "+p.Check+" "+ageSince(p.CheckAt)+" ago")
	}
	switch {
	case p.Master:
		parts = append(parts, "connection open")
	case p.Sharing:
		parts = append(parts, "no open connection")
	default:
		parts = append(parts, "no shared connection")
	}
	return strings.Join(parts, ", ")
}

// SSHRow is one shared connection (or one alias without sharing).
type SSHRow struct {
	Aliases []string
	Path    string
	Shared  bool
	Open    bool
	Check   *SSHCheck
	HostIDs []string // org hosts mapped to one of the aliases
}

// SSHStatus groups the scanned aliases by connection and asks each socket
// whether a master answers. Other sockets in the same folders are listed
// with only their path.
func SSHStatus() (rows []SSHRow, other []string) {
	s := LoadSSH()
	byPath := map[string]*SSHRow{}
	dirs := map[string]bool{}
	names := make([]string, 0, len(s.Aliases))
	for n := range s.Aliases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := s.Aliases[n]
		key := "alias:" + n
		if a.Shared() {
			key = a.ControlPath
			dirs[filepath.Dir(a.ControlPath)] = true
		}
		r := byPath[key]
		if r == nil {
			r = &SSHRow{Path: a.ControlPath, Shared: a.Shared()}
			byPath[key] = r
		}
		r.Aliases = append(r.Aliases, n)
		if c := s.Checks[n]; c != nil && (r.Check == nil || c.At > r.Check.At) {
			r.Check = c
		}
	}
	for id, alias := range s.Hosts {
		for _, r := range byPath {
			if contains(r.Aliases, alias) {
				r.HostIDs = append(r.HostIDs, id)
			}
		}
	}
	for _, r := range byPath {
		if r.Shared {
			r.Open = MasterOpen(r.Path)
		}
		sort.Strings(r.HostIDs)
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Open != rows[j].Open {
			return rows[i].Open
		}
		return rows[i].Aliases[0] < rows[j].Aliases[0]
	})
	for d := range dirs {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			p := filepath.Join(d, e.Name())
			if _, known := byPath[p]; known || e.Type()&os.ModeSocket == 0 {
				continue
			}
			if MasterOpen(p) {
				other = append(other, p)
			}
		}
	}
	sort.Strings(other)
	return rows, other
}

// SSHLine is this host's SSH view of an org host, for org --by host and the
// Hosts tab: empty when no alias is mapped.
func SSHLine(hostID string) string {
	s := LoadSSH()
	alias := s.Hosts[hostID]
	if alias == "" {
		return ""
	}
	line := "ssh " + alias + ": "
	a := s.Aliases[alias]
	switch {
	case a == nil:
		line += "not scanned (sunstack ssh scan)"
	case !a.Shared():
		line += "no shared connection"
	case MasterOpen(a.ControlPath):
		line += "connection open, reusable"
		if StartedBySunstack(alias, a.ControlPath) {
			line += " (sunstack ssh start, " + ageSince(s.Started[alias]) + ")"
		}
	default:
		line += "no open connection"
	}
	if c := s.Checks[alias]; c != nil {
		line += ", last check " + c.Result + " " + ageSince(c.At) + " ago"
	}
	return line
}

// StartedBySunstack says whether the open master on path is the one
// sunstack ssh start opened: its socket is not newer than the record, so a
// master reopened later by a plain ssh is not taken for it.
func StartedBySunstack(alias, path string) bool {
	at, err := time.Parse("2006-01-02T15:04:05Z", LoadSSH().Started[alias])
	if err != nil {
		return false
	}
	st, err := os.Lstat(path)
	return err == nil && !st.ModTime().After(at.Add(time.Second))
}

var shSafe = regexp.MustCompile(`^[A-Za-z0-9._@%+=:,/~-]+$`)

// ShellQuote quotes a value for a POSIX shell command line.
func ShellQuote(v string) string {
	// zsh expands a word starting with = to a command's path; a leading ~
	// is left to expand, as in the ~/.ssh paths setup prints.
	if shSafe.MatchString(v) && v[0] != '=' {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// SSHAttach is the command that attaches to a tmux session on another host
// through its SSH alias, quoted for the remote shell and for this one.
func SSHAttach(alias, tmuxSession string) string {
	return "ssh -t " + ShellQuote(alias) + " " + ShellQuote("tmux attach -t "+ShellQuote("="+tmuxSession))
}

// SSHAge prints a recorded stamp as an age.
func SSHAge(stamp string) string { return ageSince(stamp) }

func ageSince(stamp string) string {
	t, err := time.Parse("2006-01-02T15:04:05Z", stamp)
	if err != nil {
		return "?"
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

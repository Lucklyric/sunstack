// Package hub connects the hosts of one org through a hub (design §18, §19).
//
// One always-on host of the org is the hub. It keeps plain files: the roster
// of hosts, each host's latest snapshot, mail waiting for each host, and the
// final receipt of each message. It owns no work. Every host calls it over
// HTTP on the tailnet with its own token; mail bodies are sealed to the
// receiving host, so the hub cannot read them.
package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// Schema versions org.json, the roster and the wire frames.
const Schema = 1

// Limits on what one host may put on the hub (§18.4).
const (
	MaxFrame   = 256 << 10 // one message or snapshot
	MaxWaiting = 1000      // mail waiting for one host
)

// Liveness thresholds by contact age (§18.7).
const (
	LiveFor    = 30 * time.Second
	OfflineFor = 10 * time.Minute
	// ReceiptsFor is how long the hub keeps a message's final receipt.
	ReceiptsFor = 14 * 24 * time.Hour
)

// LocalHub is the hub target of the hub itself.
const LocalHub = "local"

// Port is the hub's default port on the tailnet.
const Port = "7731"

// Config is ~/.sunstack/org.json: the org this host belongs to.
type Config struct {
	Schema  int    `json:"schema"`
	OrgID   string `json:"org_id"`
	OrgName string `json:"org_name"`
	Hub     string `json:"hub"`              // the hub's address as given to join, or "local" on the hub itself
	HubURL  string `json:"hub_url"`          // http://<address>:7731; on the hub, its own listener
	HubID   string `json:"hub_id"`           // the hub's host ID
	Token   string `json:"token"`            // this host's token
	Listen  string `json:"listen,omitempty"` // on the hub: the address to listen on (default: its tailnet address)
}

// IsHub reports whether this host is the org's hub.
func (c *Config) IsHub() bool { return c.Hub == LocalHub }

func configPath() string { return filepath.Join(core.Home(), "org.json") }

// LoadConfig reads org.json. It returns nil, nil when this host has joined
// no org.
func LoadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil || c.OrgID == "" || c.Hub == "" || c.Token == "" {
		return nil, fmt.Errorf("%s is not a valid org file", configPath())
	}
	return &c, nil
}

func saveConfig(c *Config) error {
	c.Schema = Schema
	return writeJSON(configPath(), c, 0o600)
}

// Paths on every host.
func remoteDir() string   { return filepath.Join(core.Home(), "remote") }
func outboxDir() string   { return filepath.Join(core.Home(), "outbox") }
func sentDir() string     { return filepath.Join(outboxDir(), "sent") }
func connectLock() string { return filepath.Join(core.Home(), "locks", "_hub_connect_") }

// Paths on the hub.
func hubDir(org string) string { return filepath.Join(core.Home(), "hub", org) }

type store struct{ dir string }

func (s store) hostsFile() string           { return filepath.Join(s.dir, "hosts.json") }
func (s store) snapFile(id string) string   { return filepath.Join(s.dir, "snap", id+".json") }
func (s store) mailDir(id string) string    { return filepath.Join(s.dir, "mail", id) }
func (s store) receiptDir(id string) string { return filepath.Join(s.dir, "receipts", id) }
func (s store) seenFile(id string) string   { return filepath.Join(s.dir, "seen", id) }
func (s store) lockDir() string             { return filepath.Join(s.dir, ".lock") }
func (s store) inviteDir() string           { return filepath.Join(s.dir, "invites") }
func (s store) addrFile() string            { return filepath.Join(s.dir, "listen.addr") }

// Host is one host in the roster.
type Host struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	CanSend bool       `json:"can_send"`
	Keys    PublicKeys `json:"keys"`
	Added   string     `json:"added"`
	Contact string     `json:"contact,omitempty"` // last time the hub heard from it (hub clock)
	Waiting int        `json:"waiting"`           // mail waiting for it on the hub
	Hub     bool       `json:"hub,omitempty"`
	// Kept on the hub only, never sent: the token's hash and the tailnet
	// address the host joined from.
	TokenHash string `json:"token_hash,omitempty"`
	IP        string `json:"ip,omitempty"`
}

// public is the host as other hosts see it.
func (h *Host) public() *Host {
	c := *h
	c.TokenHash, c.IP = "", ""
	return &c
}

func tokenHash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Roster is hosts.json on the hub.
type Roster struct {
	Schema  int     `json:"schema"`
	OrgID   string  `json:"org_id"`
	OrgName string  `json:"org_name"`
	Hosts   []*Host `json:"hosts"`
}

func (r *Roster) byID(id string) *Host {
	for _, h := range r.Hosts {
		if h.ID == id {
			return h
		}
	}
	return nil
}

func (r *Roster) byName(name string) *Host {
	for _, h := range r.Hosts {
		if strings.EqualFold(h.Name, name) {
			return h
		}
	}
	return nil
}

func (s store) roster() (*Roster, error) {
	b, err := os.ReadFile(s.hostsFile())
	if err != nil {
		return nil, err
	}
	var r Roster
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("hosts.json: %v", err)
	}
	return &r, nil
}

// withRoster changes the roster under the hub lock.
func (s store) withRoster(f func(*Roster) error) error {
	unlock, err := core.LockDir(s.lockDir(), "the hub roster")
	if err != nil {
		return err
	}
	defer unlock()
	r, err := s.roster()
	if err != nil {
		return err
	}
	if err := f(r); err != nil {
		return err
	}
	return writeJSON(s.hostsFile(), r, 0o600)
}

// liveRoster adds contact times and waiting counts to the stored roster.
// Tokens and addresses are left out: it is what hosts receive.
func (s store) liveRoster() (*Roster, error) {
	r, err := s.roster()
	if err != nil {
		return nil, err
	}
	for i, h := range r.Hosts {
		h = h.public()
		r.Hosts[i] = h
		if b, err := os.ReadFile(s.seenFile(h.ID)); err == nil {
			h.Contact = strings.TrimSpace(string(b))
		}
		h.Waiting = len(jsonFiles(s.mailDir(h.ID)))
	}
	return r, nil
}

func (s store) touch(id string) {
	_ = writeFile(s.seenFile(id), []byte(stamp(time.Now())+"\n"), 0o600)
}

// Snap is a host's snapshot as the hub stores and forwards it.
type Snap struct {
	Host       string          `json:"host"`
	ReceivedAt string          `json:"received_at"` // hub clock
	Snapshot   json.RawMessage `json:"snapshot"`
}

// Mail is a message between hosts: what the hub sees and stores. Its ID is
// the message's ID on every host; the letter inside is sealed (§19.4).
type Mail struct {
	ID       string `json:"id"`
	FromHost string `json:"from_host"` // host ID, checked by the hub against the caller's token
	ToHost   string `json:"to_host"`   // host ID
	Eph      string `json:"eph"`       // the sender's one-time X25519 key
	Sealed   string `json:"sealed"`    // the letter, AES-256-GCM
	Sig      string `json:"sig"`       // Ed25519 over id, hosts, eph and sealed
}

// Receipt is the final outcome of a message.
type Receipt struct {
	ID     string `json:"id"`
	ToHost string `json:"to_host"`
	Status string `json:"status"` // delivered or refused
	Reason string `json:"reason,omitempty"`
	At     string `json:"at"`
}

// RemoteTypes are the message types that may cross hosts (§18.8).
var RemoteTypes = []string{"task", "question", "handoff", "fyi", "done"}

var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
	idRe     = regexp.MustCompile(`^[0-9a-f]{16}$`)
	senderRe = regexp.MustCompile(`^(user|[A-Za-z0-9._ -]{1,80}/[a-z0-9][a-z0-9._-]{0,80})$`)
	addrRe   = regexp.MustCompile(`^[A-Za-z0-9._ -]{1,80}/[a-z0-9][a-z0-9._-]{0,80}$`)
)

// checkLetter validates an opened letter, on the receiving host: the hub
// cannot see these fields.
func checkLetter(l *Letter) error {
	m := l
	switch {
	case !contains(RemoteTypes, m.Type):
		return fmt.Errorf("type %q cannot be sent between hosts", m.Type)
	case !senderRe.MatchString(m.From):
		return errors.New("invalid sender")
	case !ValidAddress(m.To):
		return errors.New("address must be <team>/<agent>[_task] or a session ID")
	case m.ReplyTo != "" && !core.IsMessageID(m.ReplyTo):
		return errors.New("invalid reply_to")
	case m.FromSession != "" && !core.IsSessionID(m.FromSession):
		return errors.New("invalid from_session")
	case strings.TrimSpace(m.Body) == "":
		return errors.New("empty message")
	}
	return nil
}

// ValidAddress accepts what a message may be addressed to on another host:
// a team-qualified agent or a session ID. A bare agent is ambiguous there,
// and tmux pane IDs are reused after a restart.
func ValidAddress(a string) bool { return addrRe.MatchString(a) || core.IsSessionID(a) }

// SplitAddress splits "<host>:<address>". ok is false when s has no host part.
func SplitAddress(s string) (host, addr string, ok bool) {
	h, a, found := strings.Cut(s, ":")
	if !found || h == "" || a == "" || !nameRe.MatchString(h) {
		return "", "", false
	}
	return h, a, true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseStamp(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// writeFile writes atomically and durably: temporary file, fsync, rename.
// Nothing is acknowledged before it returns (§18.2).
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	err = tmp.Chmod(mode)
	if err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

func writeJSON(path string, v any, mode os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'), mode)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// jsonFiles lists the .json files in dir, sorted, ignoring temporary ones.
func jsonFiles(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".json") && !strings.HasPrefix(n, ".") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

func base(path string) string { return strings.TrimSuffix(filepath.Base(path), ".json") }

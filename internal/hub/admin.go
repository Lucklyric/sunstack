package hub

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
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

// Init makes this host the hub of a new org (§18.3, §19.3).
func Init(name string) (*Config, error) {
	if runtime.GOOS == "windows" {
		return nil, failErr("unsupported", "a hub on Windows is not supported yet")
	}
	if !nameRe.MatchString(name) {
		return nil, usageErr("org name: letters, digits, . _ - only")
	}
	if c, err := LoadConfig(); err != nil {
		return nil, failErr("org", "%v", err)
	} else if c != nil {
		return nil, failErr("org", "this host is already in org %s (sunstack org leave first)", c.OrgName)
	}
	keys, err := LoadKeys()
	if err != nil {
		return nil, err
	}
	me := core.ThisHost()
	token := core.NewToken() + core.NewToken()
	c := &Config{OrgID: core.NewToken(), OrgName: name, Hub: LocalHub, HubID: me.ID, Token: token}
	s := store{hubDir(c.OrgID)}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	r := &Roster{Schema: Schema, OrgID: c.OrgID, OrgName: name, Hosts: []*Host{{
		ID: me.ID, Name: me.Name, CanSend: true, Keys: keys.Public(), Added: stamp(time.Now()), Hub: true, TokenHash: tokenHash(token),
	}}}
	if err := writeJSON(s.hostsFile(), r, 0o600); err != nil {
		return nil, err
	}
	return c, saveConfig(c)
}

// Invite makes a one-time join code: 10 minutes, dead after 3 wrong tries
// (§19.3).
func Invite(readOnly bool) (string, error) {
	_, s, err := hubConfig()
	if err != nil {
		return "", err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	code := string(b[:4]) + "-" + string(b[4:])
	inv := &invite{Expires: stamp(time.Now().Add(10 * time.Minute)), CanSend: !readOnly}
	if err := writeJSON(s.inviteFile(code), inv, 0o600); err != nil {
		return "", err
	}
	return code, nil
}

type invite struct {
	Expires string `json:"expires"`
	CanSend bool   `json:"can_send"`
	Wrong   int    `json:"wrong"`
}

func normCode(c string) string {
	c = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(c), "-", ""))
	if len(c) != 8 {
		return ""
	}
	return c[:4] + "-" + c[4:]
}

func (s store) inviteFile(code string) string {
	return s.inviteDir() + "/" + tokenHash(normCode(code)) + ".json"
}

// Revoke removes a host: its token, snapshot and mail. Its open stream ends
// at the next check (§19.3).
func Revoke(name string) error {
	_, s, err := hubConfig()
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
	os.Remove(s.snapFile(gone.ID))
	os.Remove(s.seenFile(gone.ID))
	os.RemoveAll(s.mailDir(gone.ID))
	os.RemoveAll(s.receiptDir(gone.ID))
	return nil
}

// Hosts lists the roster with contact times and waiting mail.
func Hosts() (*Roster, error) {
	_, s, err := hubConfig()
	if err != nil {
		return nil, err
	}
	return s.liveRoster()
}

// joinRequest and joinAnswer are POST /v1/join.
type joinRequest struct {
	Code string     `json:"code"`
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Keys PublicKeys `json:"keys"`
}

type joinAnswer struct {
	OrgID   string  `json:"org_id"`
	OrgName string  `json:"org_name"`
	HubID   string  `json:"hub_id"`
	Token   string  `json:"token"`
	Roster  *Roster `json:"roster"`
	HubTime string  `json:"hub_time"`
}

// HubURL turns what the user typed into the hub's URL: a Tailscale name or
// address, with :7731 unless a port is given.
func HubURL(target string) (string, error) {
	t := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(target, "http://"), "https://"), "/")
	if t == "" || strings.ContainsAny(t, " /?#@") {
		return "", usageErr("give the hub as a Tailscale name or address, like assistant or 100.74.1.2")
	}
	if _, _, err := net.SplitHostPort(t); err != nil {
		t = net.JoinHostPort(t, Port)
	}
	return "http://" + t, nil
}

// Join adds this host to the org of the hub at target with a code from
// `sunstack hub invite` (§19.3).
func Join(target, code string) (*Config, error) {
	if c, err := LoadConfig(); err != nil {
		return nil, failErr("org", "%v", err)
	} else if c != nil {
		return nil, failErr("org", "this host is already in org %s (sunstack org leave first)", c.OrgName)
	}
	if normCode(code) == "" {
		return nil, usageErr("--code takes the 8-character code from sunstack hub invite on the hub")
	}
	url, err := HubURL(target)
	if err != nil {
		return nil, err
	}
	keys, err := LoadKeys()
	if err != nil {
		return nil, err
	}
	me := core.ThisHost()
	body, _ := json.Marshal(joinRequest{Code: normCode(code), ID: me.ID, Name: me.Name, Keys: keys.Public()})
	resp, err := httpClient(10*time.Second).Post(url+"/v1/join", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, failErr("unreachable", "the hub at %s did not answer: %v (is it on your tailnet, with sunstack hub connect running?)", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, failErr("refused", "the hub refused: %s", readError(resp))
	}
	var a joinAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxFrame)).Decode(&a); err != nil || a.Token == "" || a.Roster == nil {
		return nil, failErr("refused", "the hub sent no token")
	}
	if err := pinRoster(a.Roster); err != nil {
		return nil, err
	}
	if err := saveRoster(a.Roster, a.HubTime); err != nil {
		return nil, err
	}
	c := &Config{OrgID: a.OrgID, OrgName: a.OrgName, Hub: target, HubURL: url, HubID: a.HubID, Token: a.Token}
	if err := saveConfig(c); err != nil {
		return nil, err
	}
	_ = PushOnce()
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

func readError(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	if s := strings.TrimSpace(string(b)); s != "" {
		return s
	}
	return resp.Status
}

// readLine reads one line of a stream.
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

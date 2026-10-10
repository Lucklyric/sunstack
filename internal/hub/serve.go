package hub

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The hub's HTTP side (§19.2): it listens on the hub's tailnet address only,
// and every request but join carries a host's token from the address the
// host joined from.

// Version is the CLI version, reported by hello. Set by main.
var Version = "dev"

// frame is one line of the watch stream, and the answer of one request.
type frame struct {
	Op      string   `json:"op"`
	ID      string   `json:"id,omitempty"`
	Status  string   `json:"status,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Mail    *Mail    `json:"mail,omitempty"`
	Snap    *Snap    `json:"snap,omitempty"`
	Roster  *Roster  `json:"roster,omitempty"`
	Snaps   []*Snap  `json:"snaps,omitempty"`
	Receipt *Receipt `json:"receipt,omitempty"`
	HubTime string   `json:"hub_time,omitempty"`
	Hello   *Hello   `json:"hello,omitempty"`
}

// Hello is the hub's answer to hello.
type Hello struct {
	OrgID   string `json:"org_id"`
	OrgName string `json:"org_name"`
	Version string `json:"version"`
	HostID  string `json:"host_id"`
	Name    string `json:"name"`
}

// pollEvery is how often a watch stream looks at the hub's files.
var pollEvery = time.Second

// tailnet is the address range Tailscale gives devices (100.64.0.0/10).
var tailnet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// testListen is the test override: listen there, and accept any address.
func testListen() string { return os.Getenv("SUNSTACK_HUB_LISTEN") }

// TailnetAddr is this machine's Tailscale address, from its interfaces.
func TailnetAddr() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && tailnet.Contains(n.IP) {
			return n.IP.String(), nil
		}
	}
	return "", errors.New("no Tailscale address on this machine (is Tailscale running?)")
}

// Listen opens the hub's listener: org.json's listen, else the tailnet
// address on port 7731.
func Listen(c *Config) (net.Listener, error) {
	addr := testListen()
	if addr == "" {
		addr = c.Listen
	}
	if addr == "" {
		ip, err := TailnetAddr()
		if err != nil {
			return nil, err
		}
		addr = net.JoinHostPort(ip, Port)
	}
	return net.Listen("tcp", addr)
}

// Server answers the hosts of one org.
type Server struct {
	c          *Config
	s          store
	mu         sync.Mutex // serializes joins
	lastInvite invite     // the invite the current join spent; under mu
}

// NewServer serves this host's org; it must be the hub.
func NewServer() (*Server, error) {
	c, s, err := hubConfig()
	if err != nil {
		return nil, err
	}
	return &Server{c: c, s: s}, nil
}

// Announce records where the hub listens, for the hub's own connection.
func (sv *Server) Announce(l net.Listener) error {
	return writeFile(sv.s.addrFile(), []byte(l.Addr().String()+"\n"), 0o600)
}

// Handler is the hub's HTTP routes.
func (sv *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/join", sv.join)
	mux.HandleFunc("GET /v1/hello", sv.auth(sv.hello))
	mux.HandleFunc("POST /v1/push", sv.auth(sv.push))
	mux.HandleFunc("GET /v1/pull", sv.auth(sv.pull))
	mux.HandleFunc("POST /v1/mail", sv.auth(sv.mail))
	mux.HandleFunc("POST /v1/ack", sv.auth(sv.ack))
	mux.HandleFunc("POST /v1/ping", sv.auth(func(w http.ResponseWriter, r *http.Request, h *Host) {
		writeFrame(w, frame{Op: "pong", HubTime: stamp(time.Now())})
	}))
	mux.HandleFunc("GET /v1/watch", sv.auth(sv.watch))
	return sv.onlyTailnet(mux)
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// onlyTailnet refuses callers outside 100.64.0.0/10.
func (sv *Server) onlyTailnet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if testListen() == "" {
			ip := net.ParseIP(peerIP(r))
			if ip == nil || !tailnet.Contains(ip) {
				httpError(w, http.StatusForbidden, "the hub answers devices on its tailnet only")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeFrame(w http.ResponseWriter, f frame) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f)
}

// auth finds the host by its token, and checks it calls from the address it
// joined from.
func (sv *Server) auth(next func(http.ResponseWriter, *http.Request, *Host)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			httpError(w, http.StatusUnauthorized, "no token")
			return
		}
		ro, err := sv.s.roster()
		if err != nil {
			httpError(w, http.StatusInternalServerError, "hub roster unreadable")
			return
		}
		want := tokenHash(tok)
		var h *Host
		for _, x := range ro.Hosts {
			if x.TokenHash != "" && subtle.ConstantTimeCompare([]byte(x.TokenHash), []byte(want)) == 1 {
				h = x
			}
		}
		if h == nil {
			httpError(w, http.StatusUnauthorized, "this host is not in the org")
			return
		}
		if ip := peerIP(r); h.IP != "" && h.IP != ip && testListen() == "" {
			httpError(w, http.StatusForbidden, fmt.Sprintf("this token belongs to the device at %s", h.IP))
			return
		} else if h.IP == "" {
			_ = sv.s.withRoster(func(ro *Roster) error {
				if x := ro.byID(h.ID); x != nil && x.IP == "" {
					x.IP = ip
				}
				return nil
			})
		}
		sv.s.touch(h.ID, r.Header.Get("X-Sunstack-Version"), r.Header.Get("X-Sunstack-Machine"))
		next(w, r, h)
	}
}

func (sv *Server) join(w http.ResponseWriter, r *http.Request) {
	var req joinRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "not a join request")
		return
	}
	switch {
	case !idRe.MatchString(req.ID):
		httpError(w, http.StatusBadRequest, "invalid host ID")
		return
	case !nameRe.MatchString(req.Name):
		httpError(w, http.StatusBadRequest, "invalid host name")
		return
	case !req.Keys.valid():
		httpError(w, http.StatusBadRequest, "invalid public keys")
		return
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if msg := sv.useCode(req.Code); msg != "" {
		httpError(w, http.StatusForbidden, msg)
		return
	}
	inv := sv.lastInvite
	token := core.NewToken() + core.NewToken()
	err := sv.s.withRoster(func(ro *Roster) error {
		for _, h := range ro.Hosts {
			if h.ID != req.ID && strings.EqualFold(h.Name, req.Name) {
				return fmt.Errorf("the name %s is already used by another host", req.Name)
			}
		}
		if h := ro.byID(req.ID); h != nil {
			if h.Hub {
				return errors.New("that is the hub's own host ID")
			}
			// A host keeps its keys across org leave; other keys under its
			// ID would take over its name and mail.
			if h.Keys != req.Keys {
				return fmt.Errorf("host ID %s is in the org with other keys; if that host made new keys, the hub's user runs sunstack hub revoke %s first", req.ID, h.Name)
			}
			// Joining again replaces the token and keys (after org leave).
			h.Name, h.Keys, h.CanSend, h.TokenHash, h.IP = req.Name, req.Keys, inv.CanSend, tokenHash(token), peerIP(r)
			return nil
		}
		ro.Hosts = append(ro.Hosts, &Host{ID: req.ID, Name: req.Name, CanSend: inv.CanSend, Keys: req.Keys, Added: stamp(time.Now()), TokenHash: tokenHash(token), IP: peerIP(r)})
		return nil
	})
	if err != nil {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	ro, _ := sv.s.liveRoster()
	json.NewEncoder(w).Encode(joinAnswer{OrgID: sv.c.OrgID, OrgName: sv.c.OrgName, HubID: sv.c.HubID, Token: token, Roster: ro, HubTime: stamp(time.Now())})
}

// useCode spends a code, or says why it cannot be used. Called with sv.mu.
func (sv *Server) useCode(code string) string {
	if normCode(code) == "" {
		return "that is not a join code"
	}
	path := sv.s.inviteFile(code)
	var inv invite
	if readJSON(path, &inv) != nil {
		sv.wrongCode()
		return "unknown or used code (sunstack hub invite makes a new one)"
	}
	os.Remove(path) // one use
	if t, ok := parseStamp(inv.Expires); !ok || time.Now().After(t) {
		return "that code expired (sunstack hub invite makes a new one)"
	}
	sv.lastInvite = inv
	return ""
}

// wrongCode counts a wrong code against every open invite: after 3, they
// are all dead, so a code cannot be guessed.
func (sv *Server) wrongCode() {
	for _, p := range jsonFiles(sv.s.inviteDir()) {
		var inv invite
		if readJSON(p, &inv) != nil {
			continue
		}
		inv.Wrong++
		if inv.Wrong >= 3 {
			os.Remove(p)
			continue
		}
		_ = writeJSON(p, &inv, 0o600)
	}
}

func (sv *Server) hello(w http.ResponseWriter, r *http.Request, h *Host) {
	writeFrame(w, frame{Op: "hello", Hello: &Hello{OrgID: sv.c.OrgID, OrgName: sv.c.OrgName, Version: Version, HostID: h.ID, Name: h.Name}, HubTime: stamp(time.Now())})
}

func readBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, MaxFrame+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFrame {
		return nil, errors.New("larger than 256 KB")
	}
	return b, nil
}

func (sv *Server) push(w http.ResponseWriter, r *http.Request, h *Host) {
	raw, err := readBody(r)
	if err == nil {
		err = sv.s.putSnap(h.ID, raw)
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeFrame(w, frame{Op: "stored"})
}

func (sv *Server) pull(w http.ResponseWriter, r *http.Request, h *Host) {
	f, err := sv.s.pull(h.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFrame(w, *f)
}

func (sv *Server) mail(w http.ResponseWriter, r *http.Request, h *Host) {
	raw, err := readBody(r)
	if err != nil {
		writeFrame(w, frame{Op: "refused", Reason: err.Error()})
		return
	}
	var m Mail
	if err := json.Unmarshal(raw, &m); err != nil {
		writeFrame(w, frame{Op: "refused", Reason: "not a message"})
		return
	}
	writeFrame(w, sv.s.putMail(h.ID, &m))
}

func (sv *Server) ack(w http.ResponseWriter, r *http.Request, h *Host) {
	var f frame
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&f); err != nil {
		httpError(w, http.StatusBadRequest, "not an ack")
		return
	}
	sv.s.ack(h.ID, f.ID, f.Status, f.Reason)
	writeFrame(w, frame{Op: "ok"})
}

func (s store) putSnap(id string, raw []byte) error {
	if len(raw) > MaxFrame {
		return errors.New("snapshot larger than 256 KB")
	}
	var probe struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Schema < 1 {
		return errors.New("not a snapshot")
	}
	return writeJSON(s.snapFile(id), &Snap{Host: id, ReceivedAt: stamp(time.Now()), Snapshot: json.RawMessage(raw)}, 0o600)
}

func (s store) pull(id string) (*frame, error) {
	r, err := s.liveRoster()
	if err != nil {
		return nil, err
	}
	f := &frame{Op: "pull", Roster: r, HubTime: stamp(time.Now())}
	for _, h := range r.Hosts {
		if h.ID == id {
			continue
		}
		var sn Snap
		if readJSON(s.snapFile(h.ID), &sn) == nil {
			f.Snaps = append(f.Snaps, &sn)
		}
	}
	return f, nil
}

// putMail stores a sealed message for its host, or refuses it. The hub sees
// the hosts, the ID and the size only; the receiving host checks the rest.
// A message the hub already holds or decided gets the same answer again.
func (s store) putMail(from string, m *Mail) frame {
	refuse := func(reason string, final bool) frame {
		if final && isFileName(m.ID) {
			_ = writeJSON(filepath.Join(s.receiptDir(from), m.ID+".json"), &Receipt{ID: m.ID, ToHost: m.ToHost, Status: "refused", Reason: reason, At: stamp(time.Now())}, 0o600)
		}
		return frame{Op: "refused", ID: m.ID, Reason: reason}
	}
	if !core.IsMessageID(m.ID) {
		return frame{Op: "refused", Reason: "invalid message id"}
	}
	var rc Receipt
	if readJSON(filepath.Join(s.receiptDir(from), m.ID+".json"), &rc) == nil {
		if rc.Status == "refused" {
			return frame{Op: "refused", ID: m.ID, Reason: rc.Reason}
		}
		return frame{Op: "stored", ID: m.ID}
	}
	r, err := s.roster()
	if err != nil {
		return refuse("hub roster unreadable", false)
	}
	sender, to := r.byID(from), r.byID(m.ToHost)
	switch {
	case sender == nil:
		return refuse("this host is not in the org", false)
	case m.FromHost != sender.ID:
		return refuse("the message names another sending host", true)
	case !sender.CanSend:
		return refuse("this host may not send (sunstack hub invite without --read-only, then join again)", true)
	case to == nil:
		return refuse("no such host in the org", true)
	case to.ID == sender.ID:
		return refuse("a message to this same host is sent locally", true)
	case m.Eph == "" || m.Sealed == "" || m.Sig == "":
		return refuse("the message is not sealed", true)
	}
	path := filepath.Join(s.mailDir(to.ID), m.ID+".json")
	if _, err := os.Stat(path); err == nil {
		return frame{Op: "stored", ID: m.ID}
	}
	b, _ := json.Marshal(m)
	if len(b) > MaxFrame {
		return refuse("message larger than 256 KB", true)
	}
	if len(jsonFiles(s.mailDir(to.ID))) >= MaxWaiting {
		return refuse("too much mail waiting for "+to.Name+"; try later", false)
	}
	if err := writeFile(path, append(b, '\n'), 0o600); err != nil {
		return refuse("hub could not store the message", false)
	}
	return frame{Op: "stored", ID: m.ID}
}

// ack records the outcome of mail for this host and removes it. Only the
// host the mail was for can ack it.
func (s store) ack(host, id, status, reason string) {
	if !isFileName(id) || (status != "delivered" && status != "refused") {
		return
	}
	if len(reason) > 500 {
		reason = reason[:500]
	}
	path := filepath.Join(s.mailDir(host), id+".json")
	var m Mail
	if readJSON(path, &m) != nil || !isFileName(m.FromHost) {
		return
	}
	rc := &Receipt{ID: id, ToHost: host, Status: status, Reason: reason, At: stamp(time.Now())}
	if writeJSON(filepath.Join(s.receiptDir(m.FromHost), id+".json"), rc, 0o600) == nil {
		os.Remove(path)
	}
}

func isFileName(s string) bool { return s != "" && filepath.Base(s) == s && s != "." && s != ".." }

// pruneReceipts drops receipts past ReceiptsFor.
func (s store) pruneReceipts(host string) {
	for _, p := range jsonFiles(s.receiptDir(host)) {
		if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) > ReceiptsFor {
			os.Remove(p)
		}
	}
}

// watch streams what a host needs (§18.5): the roster, other hosts'
// snapshots, its mail and receipts, until it hangs up or is revoked.
func (sv *Server) watch(w http.ResponseWriter, r *http.Request, me *Host) {
	fl, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	send := func(f frame) error {
		if err := enc.Encode(f); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	s := sv.s
	s.pruneReceipts(me.ID)
	sentMail := map[string]bool{}
	sentRcpt := map[string]bool{}
	snapSeen := map[string]time.Time{}
	var lastRoster time.Time
	var rosterSum []byte
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		ro, err := s.liveRoster()
		if err != nil || ro.byID(me.ID) == nil {
			send(frame{Op: "bye", Reason: "this host is no longer in the org"})
			return
		}
		rb, _ := json.Marshal(ro)
		if string(rb) != string(rosterSum) || time.Since(lastRoster) > 5*time.Second {
			if send(frame{Op: "roster", Roster: ro, HubTime: stamp(time.Now())}) != nil {
				return
			}
			rosterSum, lastRoster = rb, time.Now()
		}
		for _, h := range ro.Hosts {
			if h.ID == me.ID {
				continue
			}
			fi, err := os.Stat(s.snapFile(h.ID))
			if err != nil || !fi.ModTime().After(snapSeen[h.ID]) {
				continue
			}
			var sn Snap
			if readJSON(s.snapFile(h.ID), &sn) == nil {
				send(frame{Op: "snap", Snap: &sn, HubTime: stamp(time.Now())})
				snapSeen[h.ID] = fi.ModTime()
			}
		}
		for _, p := range jsonFiles(s.mailDir(me.ID)) {
			if sentMail[p] {
				continue
			}
			var m Mail
			if readJSON(p, &m) == nil {
				send(frame{Op: "mail", Mail: &m})
				sentMail[p] = true
			}
		}
		for _, p := range jsonFiles(s.receiptDir(me.ID)) {
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s@%d", p, fi.ModTime().UnixNano())
			if sentRcpt[key] {
				continue
			}
			var rc Receipt
			if readJSON(p, &rc) == nil {
				send(frame{Op: "receipt", Receipt: &rc})
				sentRcpt[key] = true
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

// Serve runs the hub's listener until it fails; the hub's connector starts
// it beside its own connection, after Announce.
func (sv *Server) Serve(l net.Listener) error {
	srv := &http.Server{Handler: sv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(l)
}

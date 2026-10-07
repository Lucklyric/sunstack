package hub

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Verbs are the only things a host's key can ask the hub (§18.4).
var Verbs = []string{"hello", "push", "pull", "mail", "watch"}

// Version is the CLI version, reported by hello. Set by main.
var Version = "dev"

// frame is one line of the watch protocol, and the reply of one-shot verbs.
type frame struct {
	Op       string          `json:"op"`
	ID       string          `json:"id,omitempty"`
	Status   string          `json:"status,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Mail     *Mail           `json:"mail,omitempty"`
	Snap     *Snap           `json:"snap,omitempty"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
	Roster   *Roster         `json:"roster,omitempty"`
	Snaps    []*Snap         `json:"snaps,omitempty"`
	Receipt  *Receipt        `json:"receipt,omitempty"`
	HubTime  string          `json:"hub_time,omitempty"`
	Hello    *Hello          `json:"hello,omitempty"`
}

// Hello is the hub's answer to hello.
type Hello struct {
	OrgID   string `json:"org_id"`
	OrgName string `json:"org_name"`
	Version string `json:"version"`
	HostID  string `json:"host_id"`
	Name    string `json:"name"`
}

// pollEvery is how often a watch handler looks at the hub's files.
var pollEvery = time.Second

// Serve runs one verb for the host whose key called (its ID comes from the
// key's forced command, never from the caller).
func Serve(hostID, verb string, in io.Reader, out io.Writer) error {
	if !contains(Verbs, verb) {
		return usageErr("the hub accepts only: hello, push, pull, mail, watch")
	}
	c, s, err := hubConfig()
	if err != nil {
		return err
	}
	r, err := s.roster()
	if err != nil {
		return failErr("hub", "%v", err)
	}
	me := r.byID(hostID)
	if me == nil {
		return failErr("not_allowed", "this host is not in the org")
	}
	s.touch(me.ID)
	enc := json.NewEncoder(out)
	switch verb {
	case "hello":
		return enc.Encode(frame{Op: "hello", Hello: &Hello{OrgID: c.OrgID, OrgName: c.OrgName, Version: Version, HostID: me.ID, Name: me.Name}, HubTime: stamp(time.Now())})
	case "push":
		raw, err := readAll(in)
		if err == nil {
			err = s.putSnap(me.ID, raw)
		}
		if err != nil {
			return failErr("refused", "%v", err)
		}
		return enc.Encode(frame{Op: "stored"})
	case "pull":
		f, err := s.pull(me.ID)
		if err != nil {
			return failErr("hub", "%v", err)
		}
		return enc.Encode(f)
	case "mail":
		raw, err := readAll(in)
		if err != nil {
			return failErr("refused", "%v", err)
		}
		var m Mail
		if err := json.Unmarshal(raw, &m); err != nil {
			return failErr("refused", "not a message")
		}
		return enc.Encode(s.putMail(me.ID, &m))
	}
	return s.watch(me.ID, in, out)
}

func readAll(in io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(in, MaxFrame+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFrame {
		return nil, errors.New("larger than 256 KB")
	}
	return b, nil
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

// putMail stores a message for its host, or refuses it. A message the hub
// already holds or decided gets the same answer again (§18.5).
func (s store) putMail(from string, m *Mail) frame {
	refuse := func(reason string, final bool) frame {
		if final && m.ID != "" && filepath.Base(m.ID) == m.ID {
			_ = writeJSON(filepath.Join(s.receiptDir(from), m.ID+".json"), &Receipt{ID: m.ID, ToHost: m.ToHost, Status: "refused", Reason: reason, At: stamp(time.Now())}, 0o600)
		}
		return frame{Op: "refused", ID: m.ID, Reason: reason}
	}
	if m.ID == "" || filepath.Base(m.ID) != m.ID {
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
	if sender == nil {
		return refuse("this host is not in the org", false)
	}
	m.FromHost, m.FromName = sender.ID, sender.Name
	switch {
	case !sender.CanSend:
		return refuse("this host may not send (sunstack hub allow --send on the hub)", true)
	case to == nil:
		return refuse("no such host in the org", true)
	case to.ID == sender.ID:
		return refuse("a message to this same host is sent locally", true)
	}
	if err := checkMail(m); err != nil {
		return refuse(err.Error(), true)
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

// watch is the long connection (§18.5). The handler is its own process, so
// it learns of new mail, receipts and snapshots by looking at the files.
func (s store) watch(me string, in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	w := bufio.NewWriter(out)
	send := func(f frame) error {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		w.Write(append(b, '\n'))
		return w.Flush()
	}
	s.pruneReceipts(me)
	done := make(chan error, 1)
	go func() {
		br := bufio.NewReaderSize(in, 64<<10)
		lastTouch := time.Now()
		for {
			line, err := readLine(br)
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = nil
				}
				done <- err
				return
			}
			if time.Since(lastTouch) > 2*time.Second {
				s.touch(me)
				lastTouch = time.Now()
			}
			var f frame
			if json.Unmarshal(line, &f) != nil {
				continue
			}
			switch f.Op {
			case "snap":
				if err := s.putSnap(me, f.Snapshot); err != nil {
					send(frame{Op: "refused", Reason: "snapshot: " + err.Error()})
				}
			case "mail":
				if f.Mail != nil {
					send(s.putMail(me, f.Mail))
				}
			case "ack":
				s.ack(me, f.ID, f.Status, f.Reason)
			}
		}
	}()
	sentMail := map[string]bool{}
	sentRcpt := map[string]bool{}
	snapSeen := map[string]time.Time{}
	var lastRoster time.Time
	var rosterSum []byte
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		r, err := s.liveRoster()
		if err != nil || r.byID(me) == nil {
			send(frame{Op: "bye", Reason: "this host is no longer in the org"})
			return failErr("not_allowed", "this host is no longer in the org")
		}
		// The roster with contact times, at least every 5 seconds.
		rb, _ := json.Marshal(r)
		if !bytes.Equal(rb, rosterSum) || time.Since(lastRoster) > 5*time.Second {
			if send(frame{Op: "roster", Roster: r, HubTime: stamp(time.Now())}) != nil {
				return nil
			}
			rosterSum, lastRoster = rb, time.Now()
		}
		for _, h := range r.Hosts {
			if h.ID == me {
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
		for _, p := range jsonFiles(s.mailDir(me)) {
			if sentMail[p] {
				continue
			}
			var m Mail
			if readJSON(p, &m) == nil {
				send(frame{Op: "mail", Mail: &m})
				sentMail[p] = true
			}
		}
		for _, p := range jsonFiles(s.receiptDir(me)) {
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
		case err := <-done:
			return err
		case <-tick.C:
		}
	}
}

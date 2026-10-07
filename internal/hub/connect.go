package hub

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// snapEvery is how often the connector checks this host's snapshot; tests
// shorten it with SUNSTACK_HUB_SNAP_MS.
func snapEvery() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("SUNSTACK_HUB_SNAP_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 10 * time.Second
}

// ErrRevoked ends the connector: the hub no longer knows this host.
var ErrRevoked = errors.New("this host is no longer in the org; the connector stops")

// ConnectorRunning reports whether a connector holds the lock on this host.
func ConnectorRunning() bool {
	unlock, err := core.LockDirOnce(connectLock())
	if err != nil {
		return true
	}
	unlock()
	return false
}

// Connect keeps one connection to the hub open, reconnecting with backoff,
// until the hub says this host was revoked (§18.5). On the hub it also runs
// the listener the other hosts call (§19.2).
func Connect(logw io.Writer) error {
	c, cl, err := joined()
	if err != nil {
		return err
	}
	unlock, err := core.LockDirOnce(connectLock())
	if err != nil {
		return &core.Error{Code: core.ExitClaim, Reason: "busy", Msg: "a connector already runs on this host"}
	}
	defer unlock()
	if c.IsHub() {
		sv, err := NewServer()
		if err != nil {
			return err
		}
		l, err := Listen(c)
		if err != nil {
			return failErr("listen", "the hub cannot listen: %v", err)
		}
		fmt.Fprintf(logw, "%s hub listening on %s\n", stamp(time.Now()), l.Addr())
		go func() {
			if err := sv.Serve(l); err != nil {
				fmt.Fprintf(logw, "%s hub listener stopped: %v\n", stamp(time.Now()), err)
				os.Exit(1) // the service manager restarts it
			}
		}()
	}
	backoff := time.Second
	for {
		start := time.Now()
		err := session(cl, logw)
		if errors.Is(err, ErrRevoked) {
			return err
		}
		if time.Since(start) > 30*time.Second {
			backoff = time.Second
		}
		fmt.Fprintf(logw, "%s hub connection ended (%v); retry in %s\n", stamp(time.Now()), err, backoff)
		time.Sleep(backoff)
		if backoff *= 2; backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

// session runs one watch stream until it drops: frames come down the
// stream, and this host's snapshots, pings, mail and acks go up as requests.
func session(cl *client, logw io.Writer) error {
	resp, err := cl.request("GET", "/v1/watch", nil, 0)
	if err != nil {
		var rv *revoked
		if errors.As(err, &rv) {
			return ErrRevoked
		}
		return err
	}
	defer resp.Body.Close()
	post := func(path string, v any) (*frame, error) {
		b, _ := json.Marshal(v)
		return cl.call("POST", path, b)
	}
	var mu sync.Mutex // one upload at a time
	send := func(f frame) error {
		mu.Lock()
		defer mu.Unlock()
		switch f.Op {
		case "ack":
			_, err := post("/v1/ack", f)
			return err
		case "mail":
			a, err := post("/v1/mail", f.Mail)
			if err == nil {
				settle(f.Mail.ID, a.Op, a.Reason)
			}
			return err
		}
		return nil
	}
	ended := make(chan error, 1)
	// The hub sends the roster at least every 5 seconds; a stream that
	// stays silent for 30 is dead, even if the network never said so.
	dog := time.AfterFunc(30*time.Second, func() { resp.Body.Close() })
	defer dog.Stop()
	go func() {
		br := bufio.NewReaderSize(resp.Body, 64<<10)
		for {
			line, err := readLine(br)
			dog.Reset(30 * time.Second)
			if err != nil {
				ended <- err
				return
			}
			var f frame
			if json.Unmarshal(line, &f) != nil {
				continue
			}
			if handle(&f, send) {
				ended <- ErrRevoked
				return
			}
		}
	}()
	inflight := map[string]bool{}
	var lastSum [32]byte
	var lastSent time.Time
	snapTick := time.NewTicker(snapEvery())
	defer snapTick.Stop()
	mailTick := time.NewTicker(time.Second)
	defer mailTick.Stop()
	pushSnap := func() error {
		b, err := Snapshot()
		if err != nil {
			fmt.Fprintf(logw, "snapshot: %v\n", err)
		}
		// The time of the scan is not a change.
		if err == nil {
			if sum := sha256.Sum256(withoutAt(b)); sum != lastSum || time.Since(lastSent) > 5*time.Minute {
				mu.Lock()
				_, err = cl.call("POST", "/v1/push", b)
				mu.Unlock()
				if err == nil {
					lastSum, lastSent = sum, time.Now()
				}
				return err
			}
		}
		mu.Lock()
		defer mu.Unlock()
		_, err = cl.call("POST", "/v1/ping", nil)
		return err
	}
	flush := func() error {
		for _, p := range jsonFiles(outboxDir()) {
			if inflight[p] {
				continue
			}
			var s Sent
			if readJSON(p, &s) != nil {
				continue
			}
			if err := send(frame{Op: "mail", Mail: &s.Mail}); err != nil {
				return err
			}
			inflight[p] = true
		}
		return nil
	}
	if err := pushSnap(); err != nil {
		return err
	}
	for {
		var err error
		select {
		case err = <-ended:
			return err
		case <-snapTick.C:
			err = pushSnap()
		case <-mailTick.C:
			err = flush()
		}
		if err != nil {
			var rv *revoked
			if errors.As(err, &rv) {
				return ErrRevoked
			}
			return err
		}
	}
}

// handle applies one frame from the hub. It returns true when the hub says
// this host is gone.
func handle(f *frame, send func(frame) error) bool {
	switch f.Op {
	case "roster":
		if f.Roster != nil {
			_ = takeRoster(f.Roster, f.HubTime)
		}
	case "snap":
		if f.Snap != nil {
			_ = saveSnap(f.Snap, f.HubTime)
		}
	case "mail":
		if f.Mail == nil {
			return false
		}
		status, reason, final := Deliver(f.Mail)
		if final {
			_ = send(frame{Op: "ack", ID: f.Mail.ID, Status: status, Reason: reason})
		}
	case "receipt":
		if f.Receipt != nil {
			record(f.Receipt)
		}
	case "bye":
		return true
	}
	return false
}

// withoutAt drops the snapshot's own time, so an unchanged host is not sent
// again every interval.
func withoutAt(b []byte) []byte {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return b
	}
	delete(m, "at")
	out, _ := json.Marshal(m)
	return out
}

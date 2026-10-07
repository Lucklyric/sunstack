package hub

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// until the hub says this host was revoked (§18.5).
func Connect(logw io.Writer) error {
	_, cl, err := joined()
	if err != nil {
		return err
	}
	unlock, err := core.LockDirOnce(connectLock())
	if err != nil {
		return &core.Error{Code: core.ExitClaim, Reason: "busy", Msg: "a connector already runs on this host"}
	}
	defer unlock()
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

// session runs one watch connection until it drops.
func session(cl *client, logw io.Writer) error {
	cmd, err := cl.command("watch")
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return err
	}
	waited := false
	defer func() {
		if !waited {
			stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	var mu sync.Mutex
	send := func(f frame) error {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		_, err = stdin.Write(append(b, '\n'))
		return err
	}
	ended := make(chan error, 1)
	go func() {
		br := bufio.NewReaderSize(stdout, 64<<10)
		for {
			line, err := readLine(br)
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
			return send(frame{Op: "ping"})
		}
		// The time of the scan is not a change.
		sum := sha256.Sum256(withoutAt(b))
		if sum == lastSum && time.Since(lastSent) < 5*time.Minute {
			return send(frame{Op: "ping"})
		}
		lastSum, lastSent = sum, time.Now()
		return send(frame{Op: "snap", Snapshot: b})
	}
	flush := func() error {
		for _, p := range jsonFiles(outboxDir()) {
			if inflight[p] {
				continue
			}
			var m Mail
			if readJSON(p, &m) != nil {
				continue
			}
			if err := send(frame{Op: "mail", Mail: &m}); err != nil {
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
			// Read the hub's error only after the process is done writing it.
			stdin.Close()
			_ = cmd.Wait()
			waited = true
			if msg := bytes.TrimSpace(errb.Bytes()); len(msg) > 0 && !errors.Is(err, ErrRevoked) {
				err = errors.New(string(msg))
			}
			return err
		case <-snapTick.C:
			err = pushSnap()
		case <-mailTick.C:
			err = flush()
		}
		if err != nil {
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
			_ = saveRoster(f.Roster, f.HubTime)
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
	case "stored", "refused":
		if f.ID == "" || !isFileName(f.ID) {
			return false
		}
		var m Mail
		if readJSON(filepath.Join(outboxDir(), f.ID+".json"), &m) == nil {
			settle(&m, f.Op, f.Reason)
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

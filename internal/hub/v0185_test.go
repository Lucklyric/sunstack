package hub

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review of v0.18.4 (design §45): the connector and the hub's snapshot store.

// A snapshot is bounded as the watch stream carries it: one that grows past
// the frame limit when <, > and & are escaped is refused, so it cannot
// break every other host's stream.
func TestSnapshotBoundedAsSent(t *testing.T) {
	s := store{dir: t.TempDir()}
	big := []byte(`{"schema":1,"x":"` + strings.Repeat("<", 200<<10) + `"}`)
	if len(big) > MaxFrame {
		t.Fatal("the test body must fit the raw limit")
	}
	if err := s.putSnap("aaaaaaaaaaaaaaaa", big); err == nil {
		t.Error("a snapshot that outgrows the frame limit when sent was stored")
	}
	ok := []byte(`{"schema":1,"x":"a<b"}`)
	if err := s.putSnap("aaaaaaaaaaaaaaaa", ok); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.snapFile("aaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	var sn Snap
	if err := json.Unmarshal(b, &sn); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(frame{Op: "snap", Snap: &sn})
	if len(line) > MaxFrame+4096 {
		t.Errorf("stored snapshot leaves as a %d-byte frame", len(line))
	}
	var back struct{ X string }
	if json.Unmarshal(sn.Snapshot, &back) != nil || back.X != "a<b" {
		t.Errorf("snapshot content changed: %s", sn.Snapshot)
	}
}

func connectorHome(t *testing.T, url string) *client {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SUNSTACK_HOME", home)
	t.Setenv("SUNSTACK_MACHINE_NAME", "laptop")
	agents := filepath.Join(home, "agents.json")
	if err := os.WriteFile(agents, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUNSTACK_CLAUDE_AGENTS", agents)
	t.Setenv("SUNSTACK_CODEX_SCAN", "off")
	t.Setenv("SUNSTACK_TMUX_SOCKET", filepath.Join(home, "no-tmux"))
	cl := &client{c: &Config{OrgID: "0123456789abcdef", OrgName: "o", Hub: "h", HubURL: url, Token: "t"}}
	if err := saveConfig(cl.c); err != nil {
		t.Fatal(err)
	}
	return cl
}

// A hub that accepts the connection and never answers ends the session
// instead of holding the connector forever.
func TestWatchGivesUpOnSilentHub(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close() // read nothing, answer nothing
		}
	}()
	old := watchTransport
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 300 * time.Millisecond
	watchTransport = tr
	defer func() { watchTransport = old }()

	cl := connectorHome(t, "http://"+l.Addr().String())
	var log bytes.Buffer
	var up atomic.Bool
	ended := make(chan error, 1)
	go func() { ended <- session(cl, &log, &up) }()
	select {
	case err := <-ended:
		if err == nil || up.Load() {
			t.Errorf("session on a silent hub: err %v, up %v", err, up.Load())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session still waits on a hub that never answers")
	}
}

// Only a stream that worked marks the session up, which is what starts the
// retry wait over; an attempt that fails leaves it down however long it took.
func TestSessionUpOnlyWhenStreamWorked(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeFrame(w, frame{Op: "ok", HubTime: stamp(time.Now())})
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeFrame(w, frame{Op: "ok", HubTime: stamp(time.Now())})
	})
	ts := httptest.NewServer(mux)
	cl := connectorHome(t, ts.URL)
	var log bytes.Buffer
	var up atomic.Bool
	ended := make(chan error, 1)
	go func() { ended <- session(cl, &log, &up) }()
	for deadline := time.Now().Add(5 * time.Second); !up.Load() && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if !up.Load() {
		t.Error("a working stream did not mark the session up")
	}
	ts.CloseClientConnections()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Error("the session did not end when the stream closed")
	}
	ts.Close()

	var down atomic.Bool
	if err := session(cl, &log, &down); err == nil || down.Load() {
		t.Errorf("session on a closed hub: err %v, up %v", err, down.Load())
	}
	// 1s, 2s, 4s ... capped at a minute.
	d, seen := time.Second, []time.Duration{}
	for i := 0; i < 8; i++ {
		d = nextBackoff(d)
		seen = append(seen, d)
	}
	if seen[0] != 2*time.Second || seen[5] != time.Minute || seen[7] != time.Minute {
		t.Errorf("backoff steps: %v", seen)
	}
}

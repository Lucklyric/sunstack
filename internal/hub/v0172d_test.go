package hub

import (
	"encoding/json"
	"github.com/Lucklyric/sunstack/internal/core"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func review172bHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("SUNSTACK_HOME", filepath.Join(h, ".sunstack"))
	t.Setenv("SUNSTACK_CODEX_SCAN", "off")
	t.Setenv("SUNSTACK_TMUX_SOCKET", filepath.Join(h, "sunstack-test-absent"))
	a := filepath.Join(h, "agents.json")
	must172(t, os.WriteFile(a, []byte("[]"), 0600))
	t.Setenv("SUNSTACK_CLAUDE_AGENTS", a)
	t.Setenv("SUNSTACK_HUB_NO_LOAD", "1")
	t.Setenv("SUNSTACK_HUB_SNAP_MS", "100")
}
func review172bKeys(t *testing.T) (*Keys, *Keys) {
	t.Helper()
	review172bHome(t)
	a, e := LoadKeys()
	must172(t, e)
	review172bHome(t)
	b, e := LoadKeys()
	must172(t, e)
	review172bHome(t)
	must172(t, saveConfig(&Config{OrgID: "review", Hub: "hub", Token: "token"}))
	return a, b
}
func TestReview172bTrustExistingReplacement(t *testing.T) {
	a, b := review172bKeys(t)
	hostA := &Host{ID: "aaaaaaaaaaaaaaaa", Name: "assistant", Keys: a.Public()}
	hostB := &Host{ID: "bbbbbbbbbbbbbbbb", Name: "other", Keys: b.Public()}
	must172(t, takeRoster(&Roster{Hosts: []*Host{hostA, hostB}}, stamp(time.Now())))
	hostB.Name = "assistant"
	must172(t, takeRoster(&Roster{Hosts: []*Host{hostB}}, stamp(time.Now()))) // old host was revoked
	fp, e := Trust("assistant")
	t.Logf("trust replacement: fingerprint=%s err=%v", fp, e)
	sent, se := Queue("assistant:shop/builder.alice", "user", "fyi", "marker", "", "")
	t.Logf("send=%v err=%v", sent, se)
	if e != nil || se != nil {
		t.Error("explicit trust cannot move the name of a revoked host to an already-pinned replacement")
	}
}
func TestReview172bTrustNewReplacementOwnership(t *testing.T) {
	a, b := review172bKeys(t)
	hostA := &Host{ID: "aaaaaaaaaaaaaaaa", Name: "assistant", Keys: a.Public()}
	hostB := &Host{ID: "bbbbbbbbbbbbbbbb", Name: "assistant", Keys: b.Public()}
	must172(t, takeRoster(&Roster{Hosts: []*Host{hostA}}, stamp(time.Now())))
	hostA.Name = "retired"
	must172(t, takeRoster(&Roster{Hosts: []*Host{hostA, hostB}}, stamp(time.Now())))
	_, e := Trust("assistant")
	must172(t, e)
	p := loadPins()
	t.Logf("after trust A=%+v B=%+v", p[hostA.ID], p[hostB.ID])
	if len(p[hostA.ID].Former) > 0 {
		t.Error("trust kept the previous owner's historical binding")
	}
	// Attempt the old-name takeover with only roster updates, and count outcomes.
	initial, _ := os.ReadFile(pinsPath())
	accepted := 0
	localKeys, e := LoadKeys()
	must172(t, e)
	decrypted := ""
	for i := 0; i < 100; i++ {
		must172(t, os.WriteFile(pinsPath(), initial, 0600))
		hostA.Name = "assistant"
		hostB.Name = "replacement"
		must172(t, takeRoster(&Roster{Hosts: []*Host{hostA, hostB}}, stamp(time.Now())))
		s, e := Queue("assistant:shop/builder.alice", "user", "fyi", "PRIVATE-MARKER", "", "")
		if e == nil && s.Mail.ToHost == hostA.ID {
			accepted++
			letter, de := open(a, localKeys.Public(), &s.Mail)
			must172(t, de)
			decrypted = letter.Body
		}
	}
	t.Logf("old host took back explicitly moved name in %d/100 roster updates, decrypted=%q", accepted, decrypted)
	if accepted > 0 {
		t.Error("roster reclaimed a name after explicit trust moved it to a different key")
	}
}
func TestReview172bLeavePendingWatchUploads(t *testing.T) {
	review172bHome(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	pushed := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/v1/push", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		pushed <- string(b)
		writeFrame(w, frame{Op: "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeFrame(w, frame{Op: "ok"}) })
	ts := httptest.NewServer(mux)
	defer ts.Close()
	must172(t, saveConfig(&Config{OrgID: "0123456789abcdef", Hub: "h", HubURL: ts.URL, Token: "old-token"}))
	ended := make(chan error, 1)
	go func() { ended <- Connect(io.Discard) }()
	<-started
	err := Leave()
	t.Logf("leave returned=%v configMissing=%v connectorRunning=%v", err, func() bool { _, e := os.Stat(configPath()); return os.IsNotExist(e) }(), ConnectorRunning())
	t.Setenv("SUNSTACK_HOST_NAME", "after-leave-private-marker")
	close(release)
	select {
	case body := <-pushed:
		t.Logf("old hub received snapshot after Leave returned: marker=%v bytes=%d", strings.Contains(body, "after-leave-private-marker"), len(body))
		t.Error("initial/reconnect snapshot was sent after membership removal")
	case <-time.After(5 * time.Second):
	}
	select {
	case e := <-ended:
		t.Logf("connector finally ended=%v", e)
	case <-time.After(5 * time.Second):
		ts.CloseClientConnections()
		t.Fatal("connector did not stop")
	}
}
func TestReview172bConnectorExpiredCleanup(t *testing.T) {
	review172bHome(t)
	id := core.NewMessageID("user")
	must172(t, writeJSON(filepath.Join(askedDir(), id+".json"), asked{ID: id, Expires: stamp(time.Now().Add(-time.Hour))}, 0600))
	must172(t, writeJSON(filepath.Join(repliesDir(), id+".json"), map[string]string{"sealed": "old reply"}, 0600))
	posts := make(chan struct{}, 20)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { posts <- struct{}{}; writeFrame(w, frame{Op: "ok"}) })
	ts := httptest.NewServer(mux)
	defer ts.Close()
	c := &Config{OrgID: "0123456789abcdef", Hub: "h", HubURL: ts.URL, Token: "t"}
	must172(t, saveConfig(c))
	ended := make(chan error, 1)
	go func() { ended <- Connect(io.Discard) }()
	for i := 0; i < 6; i++ {
		select {
		case <-posts:
		case <-time.After(5 * time.Second):
			t.Fatal("connector did not tick")
		}
	}
	_, ae := os.Stat(filepath.Join(askedDir(), id+".json"))
	_, re := os.Stat(filepath.Join(repliesDir(), id+".json"))
	must172(t, os.Remove(configPath()))
	<-ended
	t.Logf("after startup and six push/ping cycles expiredRequestPresent=%v expiredReplyPresent=%v", ae == nil, re == nil)
	if ae == nil || re == nil {
		t.Error("connector did not clean expired requests and replies")
	}
}
func TestReview172bRemoteUpdateSetsHostOnly(t *testing.T) {
	review172bHome(t)
	t.Setenv("SUNSTACK_SERVICE", "1")
	t.Setenv("SUNSTACK_UPDATE_CMD", `test "$SUNSTACK_HOST_ONLY" = 1`)
	r, e := updateHandler("peer", "id", json.RawMessage(`{}`))
	t.Logf("result=%v err=%v", r, e)
	must172(t, e)
	restartAfterReply.Store(false)
}
func TestReview172bLegitimateRenameAndMove(t *testing.T) {
	a, b := review172bKeys(t)
	hostA := &Host{ID: "aaaaaaaaaaaaaaaa", Name: "assistant", Keys: a.Public()}
	hostB := &Host{ID: "bbbbbbbbbbbbbbbb", Name: "other", Keys: b.Public()}
	roster := &Roster{Hosts: []*Host{hostA, hostB}}
	must172(t, takeRoster(roster, stamp(time.Now())))
	hostA.Name = "retired"
	must172(t, takeRoster(roster, stamp(time.Now())))
	s, e := Queue("retired:shop/builder.alice", "user", "fyi", "normal rename", "", "")
	must172(t, e)
	if s.Mail.ToHost != hostA.ID {
		t.Fatal("normal rename changed recipient")
	}
	hostB.Name = "assistant"
	must172(t, takeRoster(roster, stamp(time.Now())))
	if _, e := Queue("assistant:shop/builder.alice", "user", "fyi", "before trust", "", ""); e == nil {
		t.Fatal("blocked rename unexpectedly allowed")
	}
	_, e = Trust("assistant")
	must172(t, e)
	s, e = Queue("assistant:shop/builder.alice", "user", "fyi", "after trust", "", "")
	must172(t, e)
	if s.Mail.ToHost != hostB.ID {
		t.Fatal("explicit move did not address replacement")
	}
	t.Log("legitimate rename and existing-peer name move after the original rename both work")
}

package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReview172dLeaveDuringSnapshot(t *testing.T) {
	review172bHome(t)
	fifo := filepath.Join(t.TempDir(), "scan.fifo")
	must172(t, syscall.Mkfifo(fifo, 0600))
	t.Setenv("SUNSTACK_CLAUDE_AGENTS", fifo)
	scanStarted := make(chan *os.File, 1)
	go func() {
		f, e := os.OpenFile(fifo, os.O_WRONLY, 0600)
		if e != nil {
			scanStarted <- nil
			return
		}
		scanStarted <- f
	}()
	pushed := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/watch", func(w http.ResponseWriter, r *http.Request) {
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
	var writer *os.File
	select {
	case writer = <-scanStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("snapshot did not start")
	}
	if writer == nil {
		t.Fatal("FIFO open failed")
	}
	defer writer.Close()
	e := Leave()
	t.Logf("Leave=%v membershipMissing=%v connectorRunning=%v", e, func() bool { c, _ := LoadConfig(); return c == nil }(), ConnectorRunning())
	newDir := filepath.Join(t.TempDir(), "B4-PRIVATE-AFTER-LEAVE")
	must172(t, os.MkdirAll(newDir, 0700))
	b, _ := json.Marshal([]map[string]any{{"sessionId": "00000000-0000-4000-8000-000000000044", "cwd": newDir, "kind": "interactive", "status": "idle"}})
	_, e = writer.Write(b)
	must172(t, e)
	must172(t, writer.Close())
	select {
	case body := <-pushed:
		t.Logf("post-leave snapshot=%s", body)
		if strings.Contains(body, "B4-PRIVATE-AFTER-LEAVE") {
			t.Error("snapshot assembled across leave uploaded new private state to old org")
		}
	case <-time.After(5 * time.Second):
	}
	select {
	case e := <-ended:
		t.Logf("connector ended=%v", e)
	case <-time.After(5 * time.Second):
		ts.CloseClientConnections()
		t.Fatal("connector did not end")
	}
}

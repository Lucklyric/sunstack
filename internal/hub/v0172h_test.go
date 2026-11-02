//go:build !windows

package hub

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReview172eManagedLeaveDuringSnapshot(t *testing.T) {
	review172bHome(t)
	fifo := filepath.Join(t.TempDir(), "scan.fifo")
	must172(t, syscall.Mkfifo(fifo, 0600))
	t.Setenv("SUNSTACK_CLAUDE_AGENTS", fifo)
	ready := make(chan *os.File, 1)
	go func() {
		f, e := os.OpenFile(fifo, os.O_WRONLY, 0600)
		if e != nil {
			ready <- nil
			return
		}
		ready <- f
	}()
	must172(t, saveConfig(&Config{Mode: ManagedMode, OrgID: "0123456789abcdef", HubID: "aaaaaaaaaaaaaaaa"}))
	var out bytes.Buffer
	ended := make(chan error, 1)
	go func() { ended <- ManagedServe(strings.NewReader("{\"op\":\"end\"}\n{\"op\":\"end\"}\n"), &out) }()
	var w *os.File
	select {
	case w = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("snapshot did not start")
	}
	if w == nil {
		t.Fatal("FIFO failed")
	}
	defer w.Close()
	leave := Leave()
	c, _ := LoadConfig()
	t.Logf("Leave=%v configGone=%v", leave, c == nil)
	dir := filepath.Join(t.TempDir(), "B5-PRIVATE-AFTER-LEAVE")
	must172(t, os.MkdirAll(dir, 0700))
	b, _ := json.Marshal([]map[string]any{{"sessionId": "00000000-0000-4000-8000-000000000055", "cwd": dir, "kind": "interactive", "status": "idle"}})
	_, e := w.Write(b)
	must172(t, e)
	must172(t, w.Close())
	select {
	case e = <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("managed pull did not finish")
	}
	t.Logf("pull=%v post-leave-output=%s", e, out.String())
	if strings.Contains(out.String(), "B5-PRIVATE-AFTER-LEAVE") {
		t.Error("managed pull sent new private snapshot after leave")
	}
}

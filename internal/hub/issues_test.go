package hub

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// §26: the connector's failures reach the issue log, once per window.
func TestConnectorRecordsIssues(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	connIssues = core.IssueLimiter{}
	for i := 0; i < 3; i++ {
		m := &Mail{ID: core.NewMessageID("user"), FromHost: "aaaaaaaaaaaaaaaa", ToHost: "bbbbbbbbbbbbbbbb", Eph: "e", Sealed: "s", Sig: "g"}
		handle(&frame{Op: "mail", Mail: m}, func(frame) error { return nil }, io.Discard)
	}
	got := core.ReadIssues(time.Time{})
	if len(got) != 1 || got[0].Source != "connector" || got[0].Cmd != "hub connect" || got[0].Code != "mail" {
		t.Fatalf("entries: %+v", got)
	}

	snapshotOf(bigOrg(300, 10))
	got = core.ReadIssues(time.Time{})
	if len(got) != 2 || got[1].Code != "snapshot_trimmed" {
		t.Fatalf("entries: %+v", got)
	}
}

// At connector start a hub-connect.log over 1 MB moves to hub-connect.1.log
// and starts empty; the service manager keeps appending to the same file.
func TestRotateConnectLog(t *testing.T) {
	t.Setenv("SUNSTACK_HOME", t.TempDir())
	dir := filepath.Join(core.Home(), "log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hub-connect.log")
	big := bytes.Repeat([]byte("x"), connectLogMax+1)
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	rotateConnectLog()
	if fi, err := os.Stat(path); err != nil || fi.Size() != 0 {
		t.Fatalf("hub-connect.log: %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "hub-connect.1.log")); err != nil || fi.Size() != int64(len(big)) {
		t.Fatalf("hub-connect.1.log: %v %v", fi, err)
	}
	rotateConnectLog() // small: left alone
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

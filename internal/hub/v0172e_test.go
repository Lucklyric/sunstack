package hub

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview172cTrustUnrelatedNameDoesNotTransferFormer(t *testing.T) {
	a, b := review172bKeys(t)
	ha := &Host{ID: "aaaaaaaaaaaaaaaa", Name: "assistant", Keys: a.Public()}
	hb := &Host{ID: "bbbbbbbbbbbbbbbb", Name: "assistant", Keys: b.Public()}
	must172(t, takeRoster(&Roster{Hosts: []*Host{ha}}, stamp(time.Now())))
	ha.Name = "retired"
	must172(t, takeRoster(&Roster{Hosts: []*Host{ha, hb}}, stamp(time.Now())))
	hb.Name = "newhost"
	must172(t, takeRoster(&Roster{Hosts: []*Host{ha, hb}}, stamp(time.Now())))
	fp, e := Trust("newhost")
	must172(t, e)
	p := loadPins()
	t.Logf("trusted only newhost=%s A=%+v B=%+v", fp, p[ha.ID], p[hb.ID])
	initial, e := os.ReadFile(pinsPath())
	must172(t, e)
	local, e := LoadKeys()
	must172(t, e)
	accepted := 0
	for i := 0; i < 100; i++ {
		must172(t, os.WriteFile(pinsPath(), initial, 0600))
		hb.Name = "assistant"
		must172(t, takeRoster(&Roster{Hosts: []*Host{ha, hb}}, stamp(time.Now())))
		s, e := Queue("assistant:shop/builder.alice", "user", "fyi", "B3-PRIVATE-MARKER", "", "")
		if e == nil && s.Mail.ToHost == hb.ID {
			accepted++
			l, e := open(b, local.Public(), &s.Mail)
			must172(t, e)
			if l.Body != "B3-PRIVATE-MARKER" {
				t.Fatal("unexpected plaintext")
			}
		}
	}
	t.Logf("assistant reassigned without trust of assistant: %d/100; replacement decrypted marker", accepted)
	if accepted > 0 {
		t.Error("trusting newhost silently transferred an earlier untrusted name")
	}
}

func TestReview172cManagedLeaveInFlight(t *testing.T) {
	review172bHome(t)
	cfg := &Config{OrgID: "0123456789abcdef", OrgName: "review", HubID: "aaaaaaaaaaaaaaaa", Mode: ManagedMode}
	must172(t, saveConfig(cfg))
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inW.Close()
	defer outR.Close()
	ended := make(chan error, 1)
	go func() { e := ManagedServe(inR, outW); outW.Close(); ended <- e }()
	br := bufio.NewReader(outR)
	first, e := br.ReadBytes('\n')
	must172(t, e)
	t.Logf("initial=%s", strings.TrimSpace(string(first)))
	if !strings.Contains(string(first), `"op":"hello"`) {
		t.Fatal("managed fixture did not start")
	}
	e = Leave()
	t.Logf("Leave=%v connectorRunning=%v", e, ConnectorRunning())
	t.Setenv("SUNSTACK_HOST_NAME", "b3-after-leave-marker")
	roster := frame{Op: "roster", Roster: &Roster{OrgID: cfg.OrgID, Hosts: []*Host{}}, HubTime: stamp(time.Now())}
	b, _ := json.Marshal(roster)
	go func() { inW.Write(append(b, []byte("\n{\"op\":\"end\"}\n{\"op\":\"end\"}\n")...)); inW.Close() }()
	rest, e := io.ReadAll(br)
	must172(t, e)
	err := <-ended
	t.Logf("serve ended=%v post-leave=%s", err, rest)
	_, re := os.Stat(filepath.Join(remoteDir(), "roster.json"))
	t.Logf("roster recreated=%v orgConfigMissing=%v", re == nil, func() bool { c, _ := LoadConfig(); return c == nil }())
	if strings.Contains(string(rest), "b3-after-leave-marker") {
		t.Error("managed pull uploaded post-leave snapshot")
	}
}

package main

// v0.9 and v0.9.1: hosts connected through an org hub (design §18, §19).
// Three temporary homes play the hub and two hosts; the hub listens on
// 127.0.0.1 (SUNSTACK_HUB_LISTEN), the only case where the tailnet check is
// skipped.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

type orgHost struct {
	name, home string
	env        []string
}

// newOrg makes the hub (listening, with its connector running) and n hosts,
// each with its own home. addr is where the hub listens.
func newOrg(t *testing.T, names ...string) (hub *orgHost, addr string, hosts []*orgHost) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a hub on Windows is not supported")
	}
	mk := func(name string) *orgHost {
		home := t.TempDir()
		agents := filepath.Join(home, "agents.json")
		must(t, os.WriteFile(agents, []byte("[]"), 0o600))
		return &orgHost{name: name, home: home, env: []string{
			"HOME=" + home, "USERPROFILE=" + home, "SUNSTACK_HOME=" + filepath.Join(home, ".sunstack"),
			"SUNSTACK_HOST_NAME=" + name, "SUNSTACK_CLAUDE_AGENTS=" + agents, "SUNSTACK_CODEX_SCAN=off",
			"SUNSTACK_HUB_NO_LOAD=1", "SUNSTACK_HUB_SNAP_MS=300", "SUNSTACK_HUB_LISTEN=127.0.0.1:0",
		}}
	}
	hub = mk("hub-host")
	expect(t, hub.run(t, "hub", "init", "testorg"), 0, "hub init")
	if len(names) == 0 {
		return hub, "", nil // the caller starts the hub
	}
	hub.connector(t)
	soon(t, "the hub listener", func() bool {
		m, _ := filepath.Glob(filepath.Join(hub.home, ".sunstack", "hub", "*", "listen.addr"))
		if len(m) == 1 {
			b, _ := os.ReadFile(m[0])
			addr = strings.TrimSpace(string(b))
		}
		return addr != ""
	})
	for _, n := range names {
		hosts = append(hosts, mk(n))
	}
	return hub, addr, hosts
}

func (h *orgHost) run(t *testing.T, args ...string) result {
	t.Helper()
	return sh(t, h.home, h.env, args...)
}

func (h *orgHost) file(rel string) string { return filepath.Join(h.home, ".sunstack", rel) }

var codeRe = regexp.MustCompile(`join code: (\S+)`)

// join lets h into the org with a fresh code.
func (h *orgHost) join(t *testing.T, hub *orgHost, addr string, inviteArgs ...string) {
	t.Helper()
	r := hub.run(t, append([]string{"hub", "invite"}, inviteArgs...)...)
	expect(t, r, 0, "invite")
	expect(t, h.run(t, "org", "join", addr, "--code", field(codeRe, r.out)), 0, "join "+h.name)
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func hostID(t *testing.T, h *orgHost) string {
	t.Helper()
	var hi struct{ ID string }
	must(t, json.Unmarshal([]byte(readText(t, h.file("host.json"))), &hi))
	return hi.ID
}

type orgFile struct {
	OrgID string `json:"org_id"`
	Token string `json:"token"`
}

func orgOf(t *testing.T, h *orgHost) orgFile {
	t.Helper()
	var c orgFile
	must(t, json.Unmarshal([]byte(readText(t, h.file("org.json"))), &c))
	return c
}

func TestHubJoin(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server", "spare")
	a, b, c := hs[0], hs[1], hs[2]
	expect(t, hub.run(t, "org", "leave"), 1, "the hub cannot leave")

	r := hub.run(t, "hub", "invite")
	code := field(codeRe, r.out)
	if !regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}$`).MatchString(code) {
		t.Fatalf("code %q", code)
	}
	r = a.run(t, "org", "join", addr, "--code", strings.ToLower(code))
	expect(t, r, 0, "join with the code, any case")
	requireContains(t, r.out, "joined org testorg")
	if fi, _ := os.Stat(a.file("org.json")); fi == nil || fi.Mode().Perm()&0o077 != 0 {
		t.Error("org.json is missing or readable by others")
	}
	if fi, _ := os.Stat(a.file("keys.json")); fi == nil || fi.Mode().Perm()&0o077 != 0 {
		t.Error("keys.json is missing or readable by others")
	}
	r = b.run(t, "org", "join", addr, "--code", code)
	expect(t, r, 1, "a used code")
	requireContains(t, r.stderr, "unknown or used code")

	// Three wrong codes kill every open invite.
	live := field(codeRe, hub.run(t, "hub", "invite").out)
	for i := 0; i < 3; i++ {
		expect(t, b.run(t, "org", "join", addr, "--code", "AAAA-AAAA"), 1, "wrong code")
	}
	expect(t, b.run(t, "org", "join", addr, "--code", live), 1, "an invite after three wrong codes")

	// An expired code.
	live = field(codeRe, hub.run(t, "hub", "invite").out)
	inv, _ := filepath.Glob(filepath.Join(hub.file("hub"), "*", "invites", "*.json"))
	for _, p := range inv {
		must(t, os.WriteFile(p, []byte(`{"expires":"2000-01-01T00:00:00Z","can_send":true}`), 0o600))
	}
	r = b.run(t, "org", "join", addr, "--code", live)
	expect(t, r, 1, "an expired code")
	requireContains(t, r.stderr, "expired")

	// The same name from another host, and a read-only host.
	b.join(t, hub, addr, "--read-only")
	c2 := &orgHost{name: c.name, home: c.home, env: append(append([]string{}, c.env...), "SUNSTACK_HOST_NAME=laptop")}
	live = field(codeRe, hub.run(t, "hub", "invite").out)
	r = c2.run(t, "org", "join", addr, "--code", live)
	expect(t, r, 1, "a name already taken")
	requireContains(t, r.stderr, "already used")

	r = hub.run(t, "hub", "hosts")
	requireContains(t, r.out, "hub-host (hub)", "laptop  can send, key ", "server  read only, key ")
	keys := a.run(t, "org", "keys")
	requireContains(t, keys.out, "This host (laptop): ", "hub-host: ")

	// Revoke ends the host's access at once.
	expect(t, hub.run(t, "hub", "revoke", "server"), 0, "revoke")
	r = b.run(t, "org", "--refresh")
	requireContains(t, r.stderr, "not in the org")
	expect(t, hub.run(t, "hub", "revoke", "hub-host"), 1, "the hub cannot revoke itself")
	expect(t, a.run(t, "org", "leave"), 0, "leave")
	if _, err := os.Stat(a.file("org.json")); err == nil {
		t.Error("leave kept org.json")
	}
}

// call makes an HTTP request to the hub as a host.
func call(t *testing.T, addr, token, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestHubRefusals(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	a.join(t, hub, addr)
	b.join(t, hub, addr, "--read-only")
	ta, tb := orgOf(t, a).Token, orgOf(t, b).Token
	ida, idb := hostID(t, a), hostID(t, b)

	if code, _ := call(t, addr, "", "GET", "/v1/hello", ""); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code, _ := call(t, addr, "0123456789abcdef0123456789abcdef", "GET", "/v1/hello", ""); code != 401 {
		t.Errorf("a wrong token: %d", code)
	}
	if code, _ := call(t, addr, ta, "GET", "/v1/serve", ""); code != 404 {
		t.Errorf("an unknown route: %d", code)
	}
	if code, out := call(t, addr, ta, "GET", "/v1/hello", ""); code != 200 || !strings.Contains(out, `"name":"laptop"`) {
		t.Errorf("hello: %d %s", code, out)
	}
	mail := func(id, from, to string, sealed bool) string {
		m := map[string]string{"id": id, "from_host": from, "to_host": to}
		if sealed {
			m["eph"], m["sealed"], m["sig"] = "x", "x", "x"
		}
		bs, _ := json.Marshal(m)
		return string(bs)
	}
	for _, c := range []struct{ token, body, want string }{
		{tb, mail("20261007T000000Z-user-aaaaaa", idb, ida, true), "may not send"},
		{ta, mail("20261007T000000Z-user-bbbbbb", ida, "0123456789abcdef", true), "no such host"},
		{ta, mail("20261007T000000Z-user-cccccc", idb, idb, true), "names another sending host"},
		{ta, mail("20261007T000000Z-user-dddddd", ida, idb, false), "not sealed"},
		{ta, mail("not-an-id", ida, idb, true), "invalid message id"},
	} {
		_, out := call(t, addr, c.token, "POST", "/v1/mail", c.body)
		requireContains(t, out, `"op":"refused"`, c.want)
	}
	if code, _ := call(t, addr, ta, "POST", "/v1/push", strings.Repeat("x", 300<<10)); code != 400 {
		t.Errorf("an oversized snapshot: %d", code)
	}
	if code, _ := call(t, addr, ta, "POST", "/v1/push", `{"not":"a snapshot"}`); code != 400 {
		t.Errorf("not a snapshot: %d", code)
	}
	// Stored once; a resend gets the same answer. Only the recipient acks.
	id := "20261007T000001Z-user-abcdef"
	_, out := call(t, addr, ta, "POST", "/v1/mail", mail(id, ida, idb, true))
	requireContains(t, out, `"op":"stored"`)
	_, out = call(t, addr, ta, "POST", "/v1/mail", mail(id, ida, idb, true))
	requireContains(t, out, `"op":"stored"`)
	waiting := filepath.Join(hub.file("hub"), orgOf(t, hub).OrgID, "mail", idb, id+".json")
	call(t, addr, ta, "POST", "/v1/ack", `{"id":"`+id+`","status":"delivered"}`)
	if _, err := os.Stat(waiting); err != nil {
		t.Error("another host's ack removed the mail")
	}
	call(t, addr, tb, "POST", "/v1/ack", `{"id":"`+id+`","status":"delivered"}`)
	if _, err := os.Stat(waiting); err == nil {
		t.Error("the recipient's ack did not remove the mail")
	}
	if _, err := os.Stat(filepath.Join(hub.file("hub"), orgOf(t, hub).OrgID, "receipts", ida, id+".json")); err != nil {
		t.Error("no receipt for the sender")
	}
}

// team makes a team with builder.alice in a host's project folder.
func (h *orgHost) team(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(h.home, name)
	must(t, os.MkdirAll(p, 0o755))
	expect(t, sh(t, p, h.env, "init"), 0, "init")
	expect(t, sh(t, p, h.env, "hire", "builder", "alice"), 0, "hire")
	return p
}

// connector runs hub connect on a host until the test ends or stop is called.
func (h *orgHost) connector(t *testing.T) (stop func(), log func() string) {
	t.Helper()
	cmd := exec.Command(bin, "hub", "connect")
	cmd.Dir = h.home
	cmd.Env = append(cleanEnv(), h.env...)
	var mu bytes.Buffer
	lines := make(chan string, 1000)
	pr, pw := io.Pipe()
	cmd.Stderr = pw
	go func() {
		s := bufio.NewScanner(pr)
		for s.Scan() {
			select {
			case lines <- s.Text():
			default:
			}
		}
	}()
	must(t, cmd.Start())
	done := make(chan struct{})
	go func() { cmd.Wait(); pw.Close(); close(done) }()
	stopped := false
	stop = func() {
		if !stopped {
			stopped = true
			cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	return stop, func() string {
		for {
			select {
			case l := <-lines:
				mu.WriteString(l + "\n")
			default:
				return mu.String()
			}
		}
	}
}

// soon waits up to 15 s for f.
func soon(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 150; i++ {
		if f() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func inboxFiles(t *testing.T, p, agent string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(p, "sunstack", "_local", "inbox", agent, "*.md"))
	return m
}

var sentIDRe = regexp.MustCompile(`sent (\S+) to`)

func TestHubMail(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	a.join(t, hub, addr)
	b.join(t, hub, addr)
	pb := b.team(t, "shop")
	expect(t, a.run(t, "org", "--refresh"), 0, "refresh roster")

	// Refused before anything leaves the host.
	expect(t, a.run(t, "send", "server:builder.alice", "hi"), 2, "bare agent")
	expect(t, a.run(t, "send", "server:%12", "hi"), 2, "pane id")
	expect(t, a.run(t, "send", "server:shop/builder.alice", "stop", "--type", "shutdown"), 2, "shutdown")
	expect(t, a.run(t, "send", "nohost:shop/builder.alice", "hi"), 1, "unknown host")

	// No connector here: the message goes to the hub at once, sealed.
	secret := "the export password is in the vault"
	r := a.run(t, "send", "server:shop/builder.alice", secret, "--type", "question")
	expect(t, r, 0, "send")
	requireContains(t, r.out, "(at hub)")
	id := field(sentIDRe, r.out)
	org := orgOf(t, hub).OrgID
	stored := filepath.Join(hub.file("hub"), org, "mail", hostID(t, b), id+".json")
	if text := readText(t, stored); strings.Contains(text, "export") || strings.Contains(text, "builder.alice") || strings.Contains(text, "question") {
		t.Errorf("the hub can read the message: %s", text)
	}
	r = a.run(t, "send", "server:noteam/builder.alice", "hi")
	expect(t, r, 0, "send to a team that does not exist")
	bad := field(sentIDRe, r.out)

	// A tampered message is refused by the receiving host.
	r = a.run(t, "send", "server:shop/builder.alice", "tampered")
	tampered := field(sentIDRe, r.out)
	tp := filepath.Join(hub.file("hub"), org, "mail", hostID(t, b), tampered+".json")
	var m map[string]string
	must(t, json.Unmarshal([]byte(readText(t, tp)), &m))
	ct, _ := base64.StdEncoding.DecodeString(m["sealed"])
	ct[len(ct)-1] ^= 1
	m["sealed"] = base64.StdEncoding.EncodeToString(ct)
	bs, _ := json.Marshal(m)
	must(t, os.WriteFile(tp, bs, 0o600))

	// The receiving host's connector opens and delivers, keeping the ID.
	stop, _ := b.connector(t)
	soon(t, "delivery", func() bool { return len(inboxFiles(t, pb, "builder.alice")) == 1 })
	got := readText(t, inboxFiles(t, pb, "builder.alice")[0])
	requireContains(t, got, "id: "+id, "from: laptop:user", "from_host: laptop", "type: question", secret)
	soon(t, "the refusal receipts", func() bool {
		_, e1 := os.Stat(filepath.Join(hub.file("hub"), org, "receipts", hostID(t, a), bad+".json"))
		_, e2 := os.Stat(filepath.Join(hub.file("hub"), org, "receipts", hostID(t, a), tampered+".json"))
		return e1 == nil && e2 == nil
	})
	requireContains(t, readText(t, filepath.Join(hub.file("hub"), org, "receipts", hostID(t, a), tampered+".json")), "bad signature")
	stop()

	// The same mail again (a lost ack) is not delivered twice.
	again := filepath.Join(hub.file("hub"), org, "mail", hostID(t, b), id+".json")
	copyOf := filepath.Join(t.TempDir(), "again.json")
	sentFile := filepath.Join(a.file("outbox"), "sent", id+".json")
	var s struct {
		Mail json.RawMessage `json:"mail"`
	}
	must(t, json.Unmarshal([]byte(readText(t, sentFile)), &s))
	must(t, os.WriteFile(copyOf, s.Mail, 0o600))
	must(t, os.WriteFile(again, s.Mail, 0o600))
	stop, _ = b.connector(t)
	soon(t, "the second ack", func() bool { _, err := os.Stat(again); return err != nil })
	stop()
	if n := len(inboxFiles(t, pb, "builder.alice")); n != 1 {
		t.Errorf("%d messages after a resend, want 1", n)
	}

	// The sender learns every outcome when it connects.
	stop, _ = a.connector(t)
	soon(t, "receipts at the sender", func() bool {
		out := a.run(t, "tasks", "--all", "--root", pb).out
		return strings.Contains(out, id+"  question -> server:shop/builder.alice  delivered") && strings.Contains(out, bad) && strings.Contains(out, "bad signature")
	})
	stop()

	// The reply goes back to the sender's address on its own host.
	pa := a.team(t, "home")
	tok := field(tokenRe, sh(t, pb, b.env, "as", "builder.alice").out)
	r = sh(t, pb, b.env, "send", "laptop:home/builder.alice", "looked; it is fine", "--type", "done", "--reply-to", id, "--from", "builder.alice", "--token", tok)
	expect(t, r, 0, "reply")
	stop, _ = a.connector(t)
	soon(t, "the reply", func() bool { return len(inboxFiles(t, pa, "builder.alice")) == 1 })
	stop()
	requireContains(t, readText(t, inboxFiles(t, pa, "builder.alice")[0]), "from: server:shop/builder.alice", "reply_to: "+id)
}

// A key that changes on the hub stops that host's mail until org trust.
func TestHubKeyChange(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	a.join(t, hub, addr)
	b.join(t, hub, addr)
	b.team(t, "shop")
	expect(t, a.run(t, "org", "--refresh"), 0, "refresh")
	expect(t, b.run(t, "org", "--refresh"), 0, "refresh")

	// The hub's roster now shows another signing key for laptop.
	hosts := filepath.Join(hub.file("hub"), orgOf(t, hub).OrgID, "hosts.json")
	orig := readText(t, hosts)
	var ro map[string]any
	must(t, json.Unmarshal([]byte(orig), &ro))
	for _, h := range ro["hosts"].([]any) {
		hm := h.(map[string]any)
		if hm["name"] == "laptop" {
			hm["keys"].(map[string]any)["sign"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
		}
	}
	bs, _ := json.Marshal(ro)
	must(t, os.WriteFile(hosts, bs, 0o600))
	expect(t, b.run(t, "org", "--refresh"), 0, "refresh after the change")
	requireContains(t, b.run(t, "org", "keys").out, "laptop: ", "KEY CHANGED")
	requireContains(t, b.run(t, "org").out, "its key changed")
	expect(t, b.run(t, "send", "laptop:home/builder.alice", "hi"), 1, "sending to a host whose key changed")

	// Mail from it is refused while the change stands.
	r := a.run(t, "send", "server:shop/builder.alice", "hello")
	id := field(sentIDRe, r.out)
	stop, _ := b.connector(t)
	rc := filepath.Join(hub.file("hub"), orgOf(t, hub).OrgID, "receipts", hostID(t, a), id+".json")
	soon(t, "the refusal", func() bool { _, err := os.Stat(rc); return err == nil })
	stop()
	requireContains(t, readText(t, rc), "key changed")

	// The real key back on the hub clears it; trust then has nothing to do.
	must(t, os.WriteFile(hosts, []byte(orig), 0o600))
	expect(t, b.run(t, "org", "--refresh"), 0, "refresh after the fix")
	if strings.Contains(b.run(t, "org", "keys").out, "KEY CHANGED") {
		t.Error("the change is still flagged after the key came back")
	}
	expect(t, b.run(t, "org", "trust", "laptop"), 1, "trust with no change")
}

func TestHubConnectorEndsOnRevoke(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop")
	a := hs[0]
	a.join(t, hub, addr)
	cmd := exec.Command(bin, "hub", "connect")
	cmd.Env = append(cleanEnv(), a.env...)
	var se strings.Builder
	cmd.Stderr = &se
	must(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	time.Sleep(time.Second)
	expect(t, a.run(t, "hub", "connect"), 4, "a second connector")
	expect(t, hub.run(t, "hub", "revoke", "laptop"), 0, "revoke")
	select {
	case <-done:
		requireContains(t, se.String(), "no longer in the org")
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the connector kept running after revoke")
	}
}

func TestHubInstallFiles(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop")
	a := hs[0]
	a.join(t, hub, addr)
	r := a.run(t, "hub", "connect", "--install")
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		expect(t, r, 1, "install elsewhere")
		return
	}
	expect(t, r, 0, "install")
	path := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(r.out, "\n", 2)[0], "wrote "))
	requireContains(t, readText(t, path), "hub", "connect")
	if !strings.HasPrefix(path, a.home) {
		t.Errorf("service written outside the test home: %s", path)
	}
	expect(t, a.run(t, "hub", "connect", "--uninstall"), 0, "uninstall")
	if _, err := os.Stat(path); err == nil {
		t.Error("uninstall left the service file")
	}
}

func TestHubViews(t *testing.T) {
	hub, addr, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	a.join(t, hub, addr)
	b.join(t, hub, addr, "--read-only")
	b.team(t, "shop")
	stop, _ := b.connector(t)
	soon(t, "the other host in org", func() bool {
		return strings.Contains(a.run(t, "org", "--refresh").out, "Team shop")
	})
	stop()
	r := a.run(t, "org")
	requireContains(t, r.out, "Other hosts (org testorg, hub hub-host)", "== server (live", "Team shop", "== hub-host (")
	if strings.Contains(a.run(t, "org", "--json").out, "shop") {
		t.Error("org --json carries another host's teams; it is what this host pushes")
	}
	requireContains(t, a.run(t, "org", "--by", "host").out, "Host server")
}

// The hub's own connection starts only once its listener is ready, so its
// log shows no failed first attempt (v0.9.2).
func TestHubStartsWithoutRetry(t *testing.T) {
	hub, _, _ := newOrg(t)
	stop, log := hub.connector(t)
	defer stop()
	time.Sleep(1500 * time.Millisecond)
	if l := log(); strings.Contains(l, "connection ended") || !strings.Contains(l, "hub listening on") {
		t.Errorf("hub log:\n%s", l)
	}
}

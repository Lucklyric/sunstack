package main

// v0.9: hosts connected through an org hub (design §18). Three temporary
// homes play the hub and two hosts; the stand-in ssh plays sshd on the hub.

import (
	"bufio"
	"encoding/json"
	"io"
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

func needHub(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a hub on Windows is not supported")
	}
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not found")
	}
}

// newOrg makes the hub and n hosts, each with its own home.
func newOrg(t *testing.T, names ...string) (hub *orgHost, hosts []*orgHost) {
	t.Helper()
	needHub(t)
	mk := func(name string, hubHome string) *orgHost {
		home := t.TempDir()
		if hubHome == "" {
			hubHome = home
		}
		agents := filepath.Join(home, "agents.json")
		must(t, os.WriteFile(agents, []byte("[]"), 0o600))
		return &orgHost{name: name, home: home, env: []string{
			"HOME=" + home, "USERPROFILE=" + home, "SUNSTACK_HOME=" + filepath.Join(home, ".sunstack"),
			"SUNSTACK_HOST_NAME=" + name, "SUNSTACK_TEST_HUB_HOME=" + hubHome, "SUNSTACK_TEST_SUNSTACK=" + bin,
			"SUNSTACK_CLAUDE_AGENTS=" + agents, "SUNSTACK_CODEX_SCAN=off", "SUNSTACK_HUB_NO_LOAD=1", "SUNSTACK_HUB_SNAP_MS=300",
		}}
	}
	hub = mk("hub-host", "")
	expect(t, sh(t, hub.home, hub.env, "hub", "init", "testorg"), 0, "hub init")
	for _, n := range names {
		hosts = append(hosts, mk(n, hub.home))
	}
	return hub, hosts
}

func (h *orgHost) run(t *testing.T, args ...string) result {
	t.Helper()
	return sh(t, h.home, h.env, args...)
}

func (h *orgHost) with(env ...string) *orgHost {
	c := *h
	c.env = append(append([]string{}, h.env...), env...)
	return &c
}

func (h *orgHost) file(rel string) string { return filepath.Join(h.home, ".sunstack", rel) }

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

var keyLineRe = regexp.MustCompile(`^restrict,command="'[^']+' hub serve --host ([0-9a-f]{16})" ssh-ed25519 \S+ sunstack-hub:([0-9a-f]{16}):([0-9a-f]{16})$`)

func TestHubEnrollment(t *testing.T) {
	hub, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	expect(t, hub.run(t, "org", "leave"), 1, "the hub cannot leave")

	// Join through the user's own login: allowed and connected in one go.
	r := a.run(t, "org", "join", "hubbox", "--send")
	expect(t, r, 0, "join")
	requireContains(t, r.out, "joined org testorg")
	keys := strings.TrimSpace(readText(t, filepath.Join(hub.home, ".ssh", "authorized_keys")))
	m := keyLineRe.FindStringSubmatch(keys)
	if m == nil {
		t.Fatalf("key line has the wrong form: %q", keys)
	}
	if m[1] != hostID(t, a) || m[3] != m[1] {
		t.Errorf("key line names host %s, want %s", m[1], hostID(t, a))
	}
	if fi, _ := os.Stat(filepath.Join(a.home, ".ssh", "sunstack_hub")); fi == nil || fi.Mode().Perm()&0o077 != 0 {
		t.Error("the dedicated key is missing or readable by others")
	}
	// Joining again changes nothing; allowing again changes nothing.
	expect(t, a.run(t, "org", "join", "hubbox"), 0, "join again")
	pub := strings.TrimSpace(readText(t, filepath.Join(a.home, ".ssh", "sunstack_hub.pub")))
	r = hub.run(t, "hub", "allow", "laptop", "--id", hostID(t, a), "--key", pub, "--send")
	expect(t, r, 0, "allow again")
	requireContains(t, r.out, "nothing changed")
	if got := strings.TrimSpace(readText(t, filepath.Join(hub.home, ".ssh", "authorized_keys"))); got != keys {
		t.Errorf("a repeated allow changed authorized_keys:\n%s", got)
	}
	// A name or key that belongs to another host is refused.
	expect(t, hub.run(t, "hub", "allow", "laptop", "--id", "0123456789abcdef", "--key", pub), 1, "duplicate name")
	expect(t, hub.run(t, "hub", "allow", "other", "--id", "0123456789abcdef", "--key", pub), 1, "duplicate key")
	expect(t, hub.run(t, "hub", "allow", "x", "--id", "0123456789abcdef", "--key", pub+"\nssh-ed25519 AAAA"), 2, "two keys")

	// Without the user's own login, join prints the line to run on the hub.
	b2 := b.with("SUNSTACK_TEST_SSH_LOGIN=deny")
	r = b2.run(t, "org", "join", "hubbox")
	expect(t, r, 1, "join without login")
	line := regexp.MustCompile(`(?m)^  (sunstack hub allow .*)$`).FindStringSubmatch(r.out)
	if line == nil {
		t.Fatalf("join did not print the allow line:\n%s", r.out)
	}
	args := splitQuoted(strings.TrimPrefix(line[1], "sunstack "))
	expect(t, hub.run(t, args...), 0, "allow from the printed line")
	expect(t, b2.run(t, "org", "join", "hubbox"), 0, "join finishes")

	r = hub.run(t, "hub", "hosts")
	requireContains(t, r.out, "hub-host (hub)", "laptop  can send", "server  read only")

	// Revoke removes the key line and the host's files.
	must(t, os.MkdirAll(filepath.Join(hub.file("hub"), "x"), 0o700))
	expect(t, hub.run(t, "hub", "revoke", "server"), 0, "revoke")
	if strings.Contains(readText(t, filepath.Join(hub.home, ".ssh", "authorized_keys")), hostID(t, b)) {
		t.Error("revoke left the key line")
	}
	expect(t, b.run(t, "org", "--refresh"), 0, "a revoked host still runs org")
	r = b.run(t, "org", "--refresh")
	requireContains(t, r.stderr, "hub not reached")
	expect(t, hub.run(t, "hub", "revoke", "hub-host"), 1, "the hub cannot revoke itself")
	expect(t, a.run(t, "org", "leave"), 0, "leave")
	if _, err := os.Stat(a.file("org.json")); err == nil {
		t.Error("leave kept org.json")
	}
}

// splitQuoted splits a printed command line, keeping single-quoted parts.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	in, any := false, false
	for _, r := range s {
		switch {
		case r == '\'':
			in, any = !in, true
		case r == ' ' && !in:
			if any {
				out = append(out, cur.String())
				cur.Reset()
				any = false
			}
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if any {
		out = append(out, cur.String())
	}
	return out
}

// serve runs hub serve on the hub as sshd would for host id.
func serve(t *testing.T, hub *orgHost, id, verb, stdin string) result {
	t.Helper()
	cmd := exec.Command(bin, "hub", "serve", "--host", id)
	cmd.Dir = hub.home
	cmd.Env = append(append(cleanEnv(), hub.env...), "SSH_ORIGINAL_COMMAND="+verb)
	cmd.Stdin = strings.NewReader(stdin)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return result{code, so.String(), se.String()}
}

func TestHubServeRefusals(t *testing.T) {
	hub, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	expect(t, a.run(t, "org", "join", "hubbox", "--send"), 0, "join a")
	expect(t, b.run(t, "org", "join", "hubbox"), 0, "join b (read only)")
	ida, idb := hostID(t, a), hostID(t, b)

	expect(t, serve(t, hub, ida, "hello", ""), 0, "hello")
	for _, verb := range []string{"", "kill", "mail; touch pwned", "hello && touch pwned", "pull extra"} {
		r := serve(t, hub, ida, verb, "")
		if r.code == 0 {
			t.Errorf("verb %q was accepted", verb)
		}
	}
	if _, err := os.Stat(filepath.Join(hub.home, "pwned")); err == nil {
		t.Error("a verb with shell syntax ran")
	}
	expect(t, serve(t, hub, "0123456789abcdef", "hello", ""), 1, "unknown host")

	mail := func(id, to, typ, addr string) string {
		b, _ := json.Marshal(map[string]string{"id": id, "to_host": to, "from": "user", "to": addr, "type": typ, "at": "2026-10-07T00:00:00Z", "body": "hi"})
		return string(b)
	}
	id1 := "20261007T000000Z-user-aaaaaa"
	r := serve(t, hub, idb, "mail", mail(id1, ida, "fyi", "team/builder.alice"))
	requireContains(t, r.out, `"op":"refused"`, "may not send")
	for _, c := range [][2]string{
		{mail("20261007T000000Z-user-bbbbbb", "0123456789abcdef", "fyi", "team/builder.alice"), "no such host"},
		{mail("20261007T000000Z-user-cccccc", idb, "shutdown", "team/builder.alice"), "cannot be sent between hosts"},
		{mail("20261007T000000Z-user-dddddd", idb, "fyi", "builder.alice"), "address must be"},
		{mail("20261007T000000Z-user-eeeeee", idb, "fyi", "%12"), "address must be"},
		{mail("20261007T000000Z-user-ffffff", idb, "answer", "team/builder.alice"), "cannot be sent between hosts"},
	} {
		requireContains(t, serve(t, hub, ida, "mail", c[0]).out, `"op":"refused"`, c[1])
	}
	big := strings.Repeat("x", 300<<10)
	expect(t, serve(t, hub, ida, "push", big), 1, "an oversized snapshot")
	expect(t, serve(t, hub, ida, "push", `{"not":"a snapshot"}`), 1, "not a snapshot")

	// Stored once; a resend gets the same answer and no second copy.
	id2 := "20261007T000001Z-user-abcdef"
	requireContains(t, serve(t, hub, ida, "mail", mail(id2, idb, "fyi", "team/builder.alice")).out, `"op":"stored"`)
	requireContains(t, serve(t, hub, ida, "mail", mail(id2, idb, "fyi", "team/builder.alice")).out, `"op":"stored"`)
	org := orgID(t, hub)
	waiting := filepath.Join(hub.file("hub"), org, "mail", idb, id2+".json")
	if _, err := os.Stat(waiting); err != nil {
		t.Fatal("the mail was not stored for the host")
	}
	// Only the host the mail is for can ack it.
	serve(t, hub, ida, "watch", `{"op":"ack","id":"`+id2+`","status":"delivered"}`+"\n")
	if _, err := os.Stat(waiting); err != nil {
		t.Error("another host's ack removed the mail")
	}
	serve(t, hub, idb, "watch", `{"op":"ack","id":"`+id2+`","status":"delivered"}`+"\n")
	if _, err := os.Stat(waiting); err == nil {
		t.Error("the recipient's ack did not remove the mail")
	}
	if _, err := os.Stat(filepath.Join(hub.file("hub"), org, "receipts", ida, id2+".json")); err != nil {
		t.Error("no receipt for the sender")
	}
	// A revoked host is refused at once.
	expect(t, hub.run(t, "hub", "revoke", "server"), 0, "revoke")
	expect(t, serve(t, hub, idb, "hello", ""), 1, "revoked host")
}

func orgID(t *testing.T, h *orgHost) string {
	t.Helper()
	var c struct {
		OrgID string `json:"org_id"`
	}
	must(t, json.Unmarshal([]byte(readText(t, h.file("org.json"))), &c))
	return c.OrgID
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
	var buf strings.Builder
	pr, pw := io.Pipe()
	cmd.Stderr = pw
	go func() {
		s := bufio.NewScanner(pr)
		for s.Scan() {
			buf.WriteString(s.Text() + "\n")
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
	return stop, func() string { return buf.String() }
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

func TestHubMail(t *testing.T) {
	hub, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	expect(t, a.run(t, "org", "join", "hubbox", "--send"), 0, "join a")
	expect(t, b.run(t, "org", "join", "hubbox", "--send"), 0, "join b")
	pb := b.team(t, "shop")
	expect(t, a.run(t, "org", "--refresh"), 0, "refresh roster")

	// Refused before anything leaves the host.
	expect(t, a.run(t, "send", "server:builder.alice", "hi"), 2, "bare agent")
	expect(t, a.run(t, "send", "server:%12", "hi"), 2, "pane id")
	expect(t, a.run(t, "send", "server:shop/builder.alice", "stop", "--type", "shutdown"), 2, "shutdown")
	expect(t, a.run(t, "send", "nohost:shop/builder.alice", "hi"), 1, "unknown host")

	// No connector: the message goes to the hub at once.
	r := a.run(t, "send", "server:shop/builder.alice", "please look at the export", "--type", "question")
	expect(t, r, 0, "send")
	requireContains(t, r.out, "(at hub)")
	id := field(regexp.MustCompile(`sent (\S+) to`), r.out)
	r = a.run(t, "send", "server:noteam/builder.alice", "hi")
	expect(t, r, 0, "send to a team that does not exist")
	bad := field(regexp.MustCompile(`sent (\S+) to`), r.out)

	// The receiving host's connector delivers it, keeping its ID.
	stop, _ := b.connector(t)
	soon(t, "delivery", func() bool { return len(inboxFiles(t, pb, "builder.alice")) == 1 })
	got := readText(t, inboxFiles(t, pb, "builder.alice")[0])
	requireContains(t, got, "id: "+id, "from: laptop:user", "from_host: laptop", "type: question")
	org := orgID(t, hub)
	soon(t, "the refusal receipt", func() bool {
		_, err := os.Stat(filepath.Join(hub.file("hub"), org, "receipts", hostID(t, a), bad+".json"))
		return err == nil
	})
	stop()

	// The same mail again (a lost ack) is not delivered twice.
	again := filepath.Join(hub.file("hub"), org, "mail", hostID(t, b), id+".json")
	m, _ := json.Marshal(map[string]string{"id": id, "from_host": hostID(t, a), "from_name": "laptop", "to_host": hostID(t, b), "from": "user", "to": "shop/builder.alice", "type": "question", "at": "x", "body": "please look at the export"})
	must(t, os.WriteFile(again, m, 0o600))
	stop, _ = b.connector(t)
	soon(t, "the second ack", func() bool { _, err := os.Stat(again); return err != nil })
	stop()
	if n := len(inboxFiles(t, pb, "builder.alice")); n != 1 {
		t.Errorf("%d messages after a resend, want 1", n)
	}

	// The sender learns both outcomes when it connects.
	stop, _ = a.connector(t)
	soon(t, "receipts at the sender", func() bool {
		out := a.run(t, "tasks", "--all", "--root", pb).out
		return strings.Contains(out, id+"  question -> server:shop/builder.alice  delivered") && strings.Contains(out, bad) && strings.Contains(out, "refused")
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

func TestHubConnectorEndsOnRevoke(t *testing.T) {
	hub, hs := newOrg(t, "laptop")
	a := hs[0]
	expect(t, a.run(t, "org", "join", "hubbox", "--send"), 0, "join")
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
	_, hs := newOrg(t, "laptop")
	a := hs[0]
	expect(t, a.run(t, "org", "join", "hubbox"), 0, "join")
	r := a.run(t, "hub", "connect", "--install")
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		expect(t, r, 1, "install elsewhere")
		return
	}
	expect(t, r, 0, "install")
	path := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(r.out, "\n", 2)[0], "wrote "))
	text := readText(t, path)
	requireContains(t, text, "hub", "connect")
	if !strings.HasPrefix(path, a.home) {
		t.Errorf("service written outside the test home: %s", path)
	}
	expect(t, a.run(t, "hub", "connect", "--uninstall"), 0, "uninstall")
	if _, err := os.Stat(path); err == nil {
		t.Error("uninstall left the service file")
	}
}

func TestHubViews(t *testing.T) {
	_, hs := newOrg(t, "laptop", "server")
	a, b := hs[0], hs[1]
	expect(t, a.run(t, "org", "join", "hubbox", "--send"), 0, "join a")
	expect(t, b.run(t, "org", "join", "hubbox"), 0, "join b")
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
	// Without a hub, org shows the last known state and never fails.
	a2 := a.with("SUNSTACK_TEST_HUB_HOME=")
	r = a2.run(t, "org", "--refresh")
	expect(t, r, 0, "org without the hub")
	requireContains(t, r.stderr, "hub not reached")
	requireContains(t, r.out, "Team shop")
}

package main

// v0.12: managed hosts, pulled by the hub over SSH (design §25). The
// stand-in ssh plays sshd for the managed host: it checks the hub's key
// against that host's authorized_keys and runs the forced command with a
// bare PATH.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var manageRe = regexp.MustCompile(`--org (\S+) --org-name (\S+) --hub (\S+) --hub-key "([^"]+)"`)

// newManaged makes the hub, one connected host (laptop) and one managed
// host (gcloud), set up through hub manage and org managed.
func newManaged(t *testing.T, hubEnv []string) (hubHost, laptop, gcloud *orgHost) {
	t.Helper()
	events := t.TempDir()
	envFile := filepath.Join(t.TempDir(), "gcloud.env")
	m, _ := json.Marshal(map[string]string{"gcloud.example": envFile})
	common := []string{
		"SUNSTACK_TEST_EVENTS=" + events, "SUNSTACK_MANAGED_MS=300", "SUNSTACK_NO_UPDATE_CHECK=1",
		"SUNSTACK_TEST_SSH_MANAGED=" + string(m),
		`SUNSTACK_TEST_SSH_G={"gcloud":"hostname gcloud.example\nuser dev\nport 2222"}`,
	}
	hubHost, addr, hosts := newOrgEnv(t, append(common, hubEnv...), "laptop", "gcloud")
	laptop, gcloud = hosts[0], hosts[1]
	hubHost.env = append(hubHost.env, common...)
	laptop.env = append(laptop.env, "SUNSTACK_NO_UPDATE_CHECK=1")
	gcloud.env = append(gcloud.env, "SUNSTACK_NO_UPDATE_CHECK=1")
	// The stand-in sshd starts the forced command with this environment.
	var lines []string
	for _, e := range append(cleanEnv(), gcloud.env...) {
		if !strings.HasPrefix(e, "PATH=") {
			lines = append(lines, e)
		}
	}
	must(t, os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	laptop.join(t, hubHost, addr)
	laptop.connector(t)

	// Not set up there yet: hub manage prints the line to run on it.
	r := hubHost.run(t, "hub", "manage", "gcloud", "--ssh", "gcloud")
	expect(t, r, 0, "hub manage before the host is set up")
	requireContains(t, r.out, "not set up yet", "sunstack org managed")
	f := manageRe.FindStringSubmatch(r.out)
	if f == nil {
		t.Fatalf("no setup line in %q", r.out)
	}
	if fi, err := os.Stat(filepath.Join(hubHost.home, ".ssh", "sunstack_managed")); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the hub's key is missing or readable by others: %v", err)
	}
	setup := []string{"org", "managed", "--org", f[1], "--org-name", f[2], "--hub", f[3], "--hub-key", f[4]}
	auth := filepath.Join(gcloud.home, ".ssh", "authorized_keys")
	must(t, os.MkdirAll(filepath.Dir(auth), 0o700))
	must(t, os.WriteFile(auth, []byte("ssh-ed25519 AAAAothers user@elsewhere\n"), 0o600))
	expect(t, gcloud.run(t, setup...), 0, "org managed")
	expect(t, gcloud.run(t, setup...), 0, "org managed again")
	got := readText(t, auth)
	exe, _ := filepath.EvalSymlinks(bin)
	if strings.Count(got, "sunstack-managed:") != 1 || !strings.Contains(got, "AAAAothers") ||
		!strings.Contains(got, `restrict,command="`+exe+` org managed-serve"`) {
		t.Fatalf("authorized_keys:\n%s", got)
	}
	agent := &orgHost{name: gcloud.name, home: gcloud.home, env: append(append([]string{}, gcloud.env...), "CLAUDECODE=1")}
	expect(t, agent.run(t, setup...), 2, "org managed from an agent session")

	r = hubHost.run(t, "hub", "manage", "gcloud", "--ssh", "gcloud")
	expect(t, r, 0, "hub manage")
	requireContains(t, r.out, "gcloud is managed")
	// Killing the hub's connector leaves a running pull's managed-serve
	// behind, still writing into gcloud's home while it is removed. Shut
	// the hub out first and let the last serve finish.
	t.Cleanup(func() {
		os.WriteFile(auth, nil, 0o600)
		lock := gcloud.file(filepath.Join("locks", "_hub_connect_"))
		free := 0
		for i := 0; i < 300 && free < 10; i++ {
			if _, err := os.Stat(lock); err == nil {
				free = 0
			} else {
				free++
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return hubHost, laptop, gcloud
}

func TestManagedHostSetup(t *testing.T) {
	hubHost, laptop, gcloud := newManaged(t, nil)

	// The pull's SSH call takes the address from the alias and nothing else.
	log := readFileOr(filepath.Join(envValue(hubHost.env, "SUNSTACK_TEST_EVENTS"), "ssh.log"))
	requireContains(t, log, "-G gcloud", "-F /dev/null", "IdentitiesOnly=yes", "IdentityAgent=none", "ControlMaster=no",
		"ControlPath=none", "StrictHostKeyChecking=yes", "BatchMode=yes", "-p 2222", "dev@gcloud.example")

	r := hubHost.run(t, "hub", "hosts")
	requireContains(t, r.out, "gcloud (managed)")
	expect(t, hubHost.run(t, "hub", "manage", "gcloud", "--ssh", "gcloud"), 1, "a name already in the org")
	// The roster other hosts get marks it managed, without the alias.
	soon(t, "the laptop sees gcloud live through the hub", func() bool {
		return strings.Contains(laptop.run(t, "org", "--by", "host").out, "gcloud (managed, live")
	})
	if b := readFileOr(laptop.file("remote/roster.json")); !strings.Contains(b, `"managed": true`) || strings.Contains(b, `"ssh"`) {
		t.Errorf("laptop roster: %s", b)
	}

	// Refused on the managed host, each with managed_host.
	for _, args := range [][]string{
		{"org", "join", "127.0.0.1:1", "--code", "ABCD-EFGH"},
		{"hub", "init", "other"},
		{"hub", "invite"},
		{"hub", "connect"},
		{"hub", "connect", "--install"},
		{"org", "--refresh"},
		{"send", "laptop:shop/builder.alice", "--type", "fyi", "hello"},
		{"send", "nowhere:shop/builder.alice", "--type", "fyi", "hello"},
		{"peek", "laptop:shop/builder.alice"},
		{"spawn", "laptop:shop/builder.alice", "--yes"},
		{"org", "update", "laptop", "--yes"},
	} {
		r := gcloud.run(t, args...)
		if r.code == 0 || !strings.Contains(r.stderr, "managed_host") {
			t.Errorf("%v: code %d, %s", args, r.code, r.stderr)
		}
	}
	if m, _ := filepath.Glob(gcloud.file("outbox/*.json")); len(m) > 0 {
		t.Errorf("a refused send left mail: %v", m)
	}

	// The forced command ignores what the caller asks for.
	r = gcloud.run(t, "org", "managed-serve", "rm", "-rf", "/")
	if strings.Contains(r.stderr, "usage") {
		t.Errorf("managed-serve took arguments: %s", r.stderr)
	}

	// Revoke: the hub stops pulling it, and says which line to remove.
	r = hubHost.run(t, "hub", "revoke", "gcloud")
	expect(t, r, 0, "revoke")
	requireContains(t, r.out, "sunstack-managed:")
	expect(t, gcloud.run(t, "org", "leave"), 0, "leave")
	if strings.Contains(readText(t, filepath.Join(gcloud.home, ".ssh", "authorized_keys")), "sunstack-managed:") {
		t.Error("org leave kept the hub's key")
	}
}

func TestManagedHostMail(t *testing.T) {
	hubHost, laptop, gcloud := newManaged(t, nil)
	lp := laptop.team(t, "shop")
	gp := gcloud.team(t, "shop")
	soon(t, "pins both ways", func() bool {
		return strings.Contains(laptop.run(t, "org", "keys").out, "gcloud") && strings.Contains(gcloud.run(t, "org", "keys").out, "laptop")
	})

	// To the managed host, through the hub's pull.
	r := laptop.run(t, "send", "gcloud:shop/builder.alice", "--type", "question", "ready?")
	expect(t, r, 0, "send to the managed host")
	id := field(regexp.MustCompile(`sent (\S+) to`), r.out)
	soon(t, "the question in gcloud's inbox", func() bool { return len(inboxFiles(t, gp, "builder.alice")) == 1 })

	// A reply goes back; anything else is refused there first.
	r = gcloud.run(t, "send", "laptop:shop/builder.alice", "--type", "done", "--reply-to", "20991231T000000Z-user-abcdef", "no")
	expect(t, r, 1, "a reply to a message it never got")
	requireContains(t, r.stderr, "managed_host")
	r = gcloud.run(t, "send", "laptop:shop/builder.alice", "--type", "done", "--reply-to", id, "yes, ready")
	expect(t, r, 0, "the reply")
	requireContains(t, r.out, "queued")
	soon(t, "the reply in the laptop's inbox", func() bool {
		for _, f := range inboxFiles(t, lp, "builder.alice") {
			if strings.Contains(readText(t, f), "yes, ready") {
				return true
			}
		}
		return false
	})
	soon(t, "the reply leaves gcloud's outbox", func() bool {
		m, _ := filepath.Glob(gcloud.file("outbox/*.json"))
		return len(m) == 0
	})
	if r := hubHost.run(t, "hub", "held"); !strings.Contains(r.out, "nothing held") {
		t.Errorf("held: %s", r.out)
	}
}

func TestManagedHostControl(t *testing.T) {
	events := t.TempDir()
	s := privateTmux(t, t.TempDir(), []string{"SUNSTACK_TEST_EVENTS=" + events})
	hubHost, laptop, gcloud := newManaged(t, nil)
	// gcloud's sessions run on the private server; the forced command
	// inherits this from the stand-in sshd's environment file.
	gcloud.env = append(gcloud.env, "SUNSTACK_TMUX_SOCKET="+s.socket)
	var hosts map[string]string
	must(t, json.Unmarshal([]byte(envValue(hubHost.env, "SUNSTACK_TEST_SSH_MANAGED")), &hosts))
	f, err := os.OpenFile(hosts["gcloud.example"], os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	f.WriteString("SUNSTACK_TMUX_SOCKET=" + s.socket + "\nSUNSTACK_UPDATE_CMD=echo updated\n")
	f.Close()
	gcloud.team(t, "shop")
	soon(t, "pins both ways", func() bool {
		return strings.Contains(laptop.run(t, "org", "keys").out, "gcloud") && strings.Contains(gcloud.run(t, "org", "keys").out, "laptop")
	})

	r := laptop.run(t, "spawn", "gcloud:shop/builder.alice", "--task", "remote", "--tool", "claude", "--yes")
	expect(t, r, 1, "spawn without a grant")
	requireContains(t, r.stderr, "not allowed")

	expect(t, gcloud.run(t, "org", "allow", "laptop", "spawn", "peek", "update"), 0, "allow")
	brief := filepath.Join(t.TempDir(), "brief.md")
	must(t, os.WriteFile(brief, []byte("Goal: Say hello.\nScope: Nothing else.\nDone when: hello is said\nVerify: none\nReport: one line\n"), 0o600))
	r = laptop.run(t, "spawn", "gcloud:shop/builder.alice", "--task", "remote", "--tool", "claude", "--brief", brief, "--yes")
	expect(t, r, 0, "remote spawn through the hub")
	requireContains(t, r.out, "builder.alice_remote on gcloud", "ss-shop-")
	// The brief took the spawn request's ID, so a reply to it passes the
	// managed host's and the hub's reply checks.
	gp := filepath.Join(gcloud.home, "shop")
	var briefID string
	for _, f := range inboxFiles(t, gp, "builder.alice") {
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		if _, err := os.Stat(gcloud.file("remote/imported/" + id + ".json")); err == nil {
			briefID = id
		}
	}
	if briefID == "" {
		t.Fatalf("no brief under the request's ID: %v", inboxFiles(t, gp, "builder.alice"))
	}
	lp := laptop.team(t, "shop")
	expect(t, gcloud.run(t, "send", "laptop:shop/builder.alice", "--type", "done", "--reply-to", briefID, "hello said"), 0, "reply to the brief")
	soon(t, "the brief's reply on the laptop", func() bool {
		for _, f := range inboxFiles(t, lp, "builder.alice") {
			if strings.Contains(readText(t, f), "hello said") {
				return true
			}
		}
		return false
	})
	soon(t, "peek the spawned session", func() bool {
		r := laptop.run(t, "peek", "gcloud:shop/builder.alice_remote", "--lines", "5")
		return r.code == 0 && strings.Contains(r.out, "builder.alice_remote on gcloud")
	})
	expect(t, gcloud.run(t, "org", "deny", "laptop", "peek"), 0, "deny peek")
	r = laptop.run(t, "peek", "gcloud:shop/builder.alice_remote", "--lines", "5")
	expect(t, r, 1, "peek after the deny")
	requireContains(t, r.stderr, "not allowed")

	// A request a cut-off pull left running is answered "uncertain" by the
	// next pull, never run again.
	cut := "20261013T000000Z-user-c0ffee"
	must(t, os.WriteFile(gcloud.file("remote/imported/"+cut+".json"), []byte(`{"from_host":"`+hostID(t, laptop)+`"}`), 0o600))
	j := map[string]any{}
	_ = json.Unmarshal([]byte(readFileOr(gcloud.file("remote/requests.json"))), &j)
	j[cut] = map[string]string{"from": "laptop", "from_host": hostID(t, laptop), "kind": "spawn", "digest": "x", "status": "running", "expires": "2099-01-01T00:00:00Z"}
	b, _ := json.Marshal(j)
	must(t, os.WriteFile(gcloud.file("remote/requests.json"), b, 0o600))
	soon(t, "the cut-off request answered", func() bool {
		var j map[string]struct{ Status string }
		json.Unmarshal([]byte(readFileOr(gcloud.file("remote/requests.json"))), &j)
		return j[cut].Status == "done"
	})

	r = laptop.run(t, "org", "update", "gcloud", "--yes")
	expect(t, r, 0, "update the managed host")
	requireContains(t, r.out, "gcloud updated")
}

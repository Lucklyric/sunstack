// Test-only CLI stand-ins. No real Claude/Codex executable is ever invoked.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

func run(path string, args ...string) {
	if path == "" {
		os.Exit(1)
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func privateSocket(s string) bool { return strings.HasPrefix(filepath.Base(s), "sunstack-test-") }

func main() {
	args := os.Args[1:]
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	switch name {
	case "tmux":
		// Production passes -u first; keep it and check what follows.
		utf8 := len(args) > 0 && args[0] == "-u"
		if utf8 {
			args = args[1:]
		}
		// The production scanner probes the default server. Deny that probe,
		// or map it to a second explicitly supplied PRIVATE server for tests.
		if len(args) < 2 || (args[0] != "-L" && args[0] != "-S") {
			sock := os.Getenv("SUNSTACK_TEST_DEFAULT_SOCKET")
			if !privateSocket(sock) {
				os.Exit(1)
			}
			args = append([]string{"-S", sock}, args...)
		}
		if !privateSocket(args[1]) {
			os.Exit(1)
		}
		if utf8 {
			args = append([]string{"-u"}, args...)
		}
		run(os.Getenv("SUNSTACK_TEST_REAL_TMUX"), args...)
		return
	case "ssh":
		// Records every call. -G prints what SUNSTACK_TEST_SSH_G holds for the
		// alias; -O check answers for sockets listed in SUNSTACK_TEST_SSH_OPEN;
		// a login follows SUNSTACK_TEST_SSH_LOGIN (empty: ok).
		if ev := os.Getenv("SUNSTACK_TEST_EVENTS"); ev != "" {
			f, _ := os.OpenFile(filepath.Join(ev, "ssh.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
		last := args[len(args)-1]
		switch {
		case len(args) > 0 && args[0] == "-G":
			var g map[string]string
			json.Unmarshal([]byte(os.Getenv("SUNSTACK_TEST_SSH_G")), &g)
			fmt.Printf("hostname %s\n%s\n", last, g[last])
			return
		case len(args) >= 3 && args[len(args)-3] == "-O":
			for _, a := range args {
				if p, ok := strings.CutPrefix(a, "ControlPath="); ok {
					for _, open := range strings.Split(os.Getenv("SUNSTACK_TEST_SSH_OPEN"), ",") {
						if open != "" && open == p {
							if args[len(args)-2] == "exit" {
								fmt.Fprintln(os.Stderr, "Exit request sent.")
							} else {
								fmt.Fprintln(os.Stderr, "Master running")
							}
							return
						}
					}
				}
			}
			fmt.Fprintln(os.Stderr, "Control socket connect: No such file or directory")
			os.Exit(255)
		}
		if managedLogin(args) {
			return
		}
		switch os.Getenv("SUNSTACK_TEST_SSH_LOGIN") {
		case "denied":
			fmt.Fprintln(os.Stderr, "user@host: Permission denied (publickey).")
			os.Exit(255)
		case "hang":
			select {}
		}
		return
	case "ps":
		if pid := os.Getenv("SUNSTACK_TEST_SCAN_PID"); pid != "" {
			if len(args) == 2 && args[0] == "-axo" {
				fmt.Printf("%s ttysTEST /test/codex\n", pid)
				return
			}
			if len(args) == 2 && args[0] == "-p" && args[1] == pid {
				return
			}
			os.Exit(1)
		}
		if len(args) == 4 && args[0] == "-o" && args[1] == "tty=" && args[2] == "-p" {
			var ttys map[string]string
			json.Unmarshal([]byte(os.Getenv("SUNSTACK_TEST_PROCESS_TTYS")), &ttys)
			if tty := ttys[args[3]]; tty != "" {
				fmt.Println(tty)
				return
			}
			os.Exit(1)
		}
		// Only inspect foreground processes on a terminal of our private server.
		if len(args) == 4 && args[0] == "-o" && (args[1] == "stat=,comm=" || args[1] == "stat=,pid=,lstart=,comm=") && args[2] == "-t" {
			sockets := []string{strings.Split(os.Getenv("TMUX"), ",")[0], os.Getenv("SUNSTACK_TEST_DEFAULT_SOCKET"), os.Getenv("SUNSTACK_TMUX_SOCKET")}
			for _, sock := range sockets {
				if !privateSocket(sock) {
					continue
				}
				out, err := exec.Command(os.Getenv("SUNSTACK_TEST_REAL_TMUX"), "-S", sock, "list-panes", "-a", "-F", "#{pane_tty}").Output()
				if err != nil {
					continue
				}
				for _, tty := range strings.Fields(string(out)) {
					if strings.TrimPrefix(tty, "/dev/") == args[3] {
						run(os.Getenv("SUNSTACK_TEST_REAL_PS"), args...)
						return
					}
				}
			}
		}
		os.Exit(1)
	case "lsof":
		if os.Getenv("SUNSTACK_TEST_LSOF_FAIL") != "" {
			os.Exit(1)
		}
		if len(args) == 3 && args[0] == "-p" && args[2] == "-Fftn" {
			// A drive stand-in (§47) holds its own rollout open.
			if r := filepath.Join(os.Getenv("SUNSTACK_TEST_EVENTS"), "rollout-"+args[1]+".jsonl"); fileExists(r) {
				fmt.Printf("fcwd\nn/\nf3\nn%s\n", r)
				return
			}
		}
		if len(args) != 3 || args[0] != "-p" || args[1] != os.Getenv("SUNSTACK_TEST_SCAN_PID") || args[2] != "-Fftn" {
			os.Exit(1)
		}
		fmt.Printf("fcwd\nn%s\nf3\nn%s\n", os.Getenv("SUNSTACK_TEST_SCAN_CWD"), os.Getenv("SUNSTACK_TEST_ROLLOUT"))
		return
	case "claude", "codex":
		if len(args) > 0 && args[0] == "agents" {
			fmt.Println("[]")
			return
		}
		if len(args) > 0 && args[0] == "--help" {
			// spawn asks whether Claude Code takes --name (§46).
			if os.Getenv("SUNSTACK_TEST_CLI_NAME") != "" {
				fmt.Println("  -n, --name <name>  Set a display name for this session")
			}
			return
		}
		if os.Getenv("SUNSTACK_TEST_MODE") == "exit" {
			return
		}
		interactive(name, args)
	default:
		panic("unknown test executable: " + name)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// driver plays the parts of a real CLI that sunstack drive reads (§47): a
// transcript with one record per event, a turn that takes time, and the
// ways a prompt gets lost. SUNSTACK_TEST_DRIVE turns it on; its value picks
// a twist: late (no transcript before the first prompt), approval (the turn
// stops at a menu), dropenter (the first Enter is lost), notranscript,
// noduration (Claude Code ends a turn with no turn_duration record).
type driver struct {
	mu       sync.Mutex
	tool     string
	twist    string
	path     string
	f        *os.File
	busy     bool
	approval bool
	abort    chan struct{}
	dropped  bool
	turn     time.Duration
}

var driveFile = regexp.MustCompile(`(?:@|file )(/\S+\.md)`)

func newDriver(tool, dir string) *driver {
	tw := os.Getenv("SUNSTACK_TEST_DRIVE")
	if tw == "" {
		return nil
	}
	d := &driver{tool: tool, twist: tw, turn: 300 * time.Millisecond}
	if ms, err := strconv.Atoi(os.Getenv("SUNSTACK_TEST_TURN_MS")); err == nil {
		d.turn = time.Duration(ms) * time.Millisecond
	}
	pid := os.Getpid()
	sid := fmt.Sprintf("00000000-0000-4000-8000-%012d", pid)
	if tool == "codex" {
		d.path = filepath.Join(dir, "rollout-"+strconv.Itoa(pid)+".jsonl")
	} else {
		home, _ := os.UserHomeDir()
		d.path = filepath.Join(home, ".claude", "projects", "test", sid+".jsonl")
		os.MkdirAll(filepath.Dir(d.path), 0o755)
		// Claude Code lists the session before its first prompt.
		if agents := os.Getenv("SUNSTACK_CLAUDE_AGENTS"); agents != "" {
			var list []map[string]any
			b, _ := os.ReadFile(agents)
			json.Unmarshal(b, &list)
			cwd, _ := os.Getwd()
			list = append(list, map[string]any{"pid": pid, "sessionId": sid, "cwd": cwd, "kind": "interactive", "status": "idle"})
			b, _ = json.Marshal(list)
			os.WriteFile(agents, b, 0o600)
		}
	}
	if tw != "late" && tw != "notranscript" && tool == "codex" {
		d.emit(map[string]any{"type": "session_meta", "payload": map[string]any{"id": sid}})
	}
	return d
}

// status rewrites this session's line in the list `claude agents` prints.
func (d *driver) status(st string) {
	agents := os.Getenv("SUNSTACK_CLAUDE_AGENTS")
	if d.tool != "claude" || agents == "" {
		return
	}
	var list []map[string]any
	b, _ := os.ReadFile(agents)
	json.Unmarshal(b, &list)
	for _, e := range list {
		if pid, _ := e["pid"].(float64); int(pid) == os.Getpid() {
			e["status"] = st
		}
	}
	b, _ = json.Marshal(list)
	os.WriteFile(agents+".tmp", b, 0o600)
	os.Rename(agents+".tmp", agents)
}

func (d *driver) emit(rec map[string]any) {
	if d.twist == "notranscript" {
		return
	}
	if d.f == nil {
		f, err := os.OpenFile(d.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			panic(err)
		}
		d.f = f
		if d.tool == "codex" && (d.twist == "late") {
			b, _ := json.Marshal(map[string]any{"type": "session_meta", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "payload": map[string]any{"id": "late"}})
			d.f.Write(append(b, '\n'))
		}
	}
	rec["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(rec)
	d.f.Write(append(b, '\n'))
}

func msg(typ string, text string) map[string]any {
	return map[string]any{"type": typ, "message": map[string]any{"role": typ, "content": []map[string]any{{"type": "text", "text": text}}}}
}

// submit starts a turn for a prompt; redraw runs when the screen changes.
// Call with d.mu held.
func (d *driver) submit(prompt string, redraw func()) {
	body := prompt
	if m := driveFile.FindStringSubmatch(prompt); m != nil {
		if b, err := os.ReadFile(m[1]); err == nil {
			body = strings.TrimSpace(string(b))
		}
	}
	d.busy, d.abort = true, make(chan struct{})
	d.status("busy")
	if d.tool == "codex" {
		d.emit(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_started"}})
	} else {
		d.emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
	}
	abort := d.abort
	go func() {
		select {
		case <-abort:
			d.mu.Lock()
			if d.tool == "codex" {
				d.emit(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "turn_aborted"}})
			} else {
				d.emit(msg("user", "[Request interrupted by user]"))
			}
		case <-time.After(d.turn):
			d.mu.Lock()
			if d.twist == "approval" {
				d.approval = true
				redraw()
				d.mu.Unlock()
				return
			}
			reply := "reply: " + body
			if d.tool == "codex" {
				for _, t := range []string{"working on it", reply} {
					d.emit(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": t}}}})
				}
				d.emit(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_complete", "last_agent_message": reply}})
			} else {
				d.emit(msg("assistant", "let me look"))
				d.emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "content": "ok"}}}})
				d.emit(msg("assistant", reply))
				if d.twist != "noduration" {
					d.emit(map[string]any{"type": "system", "subtype": "turn_duration"})
				}
			}
		}
		d.busy = false
		d.status("idle")
		redraw()
		d.mu.Unlock()
	}()
}

func interactive(tool string, args []string) {
	dir := os.Getenv("SUNSTACK_TEST_EVENTS")
	if dir == "" {
		panic("missing test event directory")
	}
	cwd, _ := os.Getwd()
	launch, _ := json.Marshal(struct {
		PID  int
		Cwd  string
		Args []string
	}{os.Getpid(), cwd, args})
	if err := os.WriteFile(filepath.Join(dir, "launch-"+strconv.Itoa(os.Getpid())+".json"), launch, 0o600); err != nil {
		panic(err)
	}
	log, err := os.OpenFile(filepath.Join(dir, "input-"+strconv.Itoa(os.Getpid())+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	keys, err := os.OpenFile(filepath.Join(dir, "keys-"+strconv.Itoa(os.Getpid())+".txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		panic(err)
	}
	defer keys.Close()
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	defer term.Restore(int(os.Stdin.Fd()), old)
	mode := os.Getenv("SUNSTACK_TEST_MODE")
	drv := newDriver(tool, dir)
	if drv == nil {
		drv = &driver{} // its lock only
	}
	// A CLI that is still loading reads no keys: what is typed then is lost.
	ready := time.Now()
	if ms, err := strconv.Atoi(os.Getenv("SUNSTACK_TEST_SLOW_MS")); err == nil {
		ready = ready.Add(time.Duration(ms) * time.Millisecond)
	}
	wrap, _ := strconv.Atoi(os.Getenv("SUNSTACK_TEST_WRAP"))
	wrapped := func(text string) string {
		r := []rune(text)
		if wrap <= 0 || len(r) <= wrap {
			return text
		}
		var parts []string
		for len(r) > wrap {
			parts, r = append(parts, string(r[:wrap])), r[wrap:]
		}
		return strings.Join(append(parts, string(r)), "\r\n  ")
	}
	// Adversarial screens for §37: a changed answer label, a conversation
	// that quotes the screen above the input box, and a menu that turns into
	// the input box after the first key.
	twist := mode
	switch mode {
	case "unsafelabel", "unsafechange", "unsafewrap", "unsafenofooter", "unsafefooter", "unsafeafter":
		mode = "codexhooks"
	case "unsafetitle", "unsafeafterclaude":
		mode = "claudetrust"
	case "drafttext":
		mode = ""
	case "unsafeinput":
		mode = ""
	}
	draft := []rune{}
	if mode == "draft" {
		draft = []rune("my unsent draft")
	}
	// The two trust screens of §37, as the real CLIs draw them: arrows move
	// the selection, Enter picks it and is recorded as choice:<label>.
	trust := map[string][]string{
		"codexhooks":  {"Review hooks", "Trust all and continue", "Continue without trusting (hooks won't run)"},
		"claudetrust": {"No, exit", "Yes, I trust this folder"},
	}[mode]
	if twist == "unsafelabel" {
		trust[1] = "Do NOT select Trust all and continue"
	}
	if twist == "unsafeinput" {
		draft = []rune("Trust all and continue")
	}
	if twist == "drafttext" {
		draft = []rune("would you like to explain this code? Enter to confirm")
	}
	sel := 0
	draw := func() {
		fmt.Print("\x1b[2J\x1b[H")
		if trust != nil {
			if mode == "codexhooks" {
				fmt.Print("Hooks need review\r\n2 hooks are new or changed.\r\nHooks can run outside the sandbox after you trust them.\r\n\r\n")
				for i, o := range trust {
					m := "  "
					if i == sel {
						m = "› "
					}
					fmt.Printf("%s%d. %s\r\n", m, i+1, o)
					if twist == "unsafewrap" && i == 1 {
						fmt.Print("    and disable all future safety checks\r\n")
					}
				}
				switch twist {
				case "unsafenofooter":
				case "unsafefooter":
					fmt.Print("\r\nenter confirms deletion of all saved sessions · esc skip")
				default:
					fmt.Print("\r\nenter confirm · esc skip")
				}
				if twist == "unsafeafter" {
					fmt.Print("\r\nWARNING: Enter also authorizes all future hooks without review.")
				}
			} else {
				title := " Quick safety check: Is this a project you created or one you trust? (Like your own code, a well-known open source\r\n project, or work from your team). If not, take a moment to review what's in this folder first.\r\n"
				if twist == "unsafetitle" {
					title = " Quick safety check: Is this a project you created or one you trust? CHANGED: trust all folders on this host\r\n"
				}
				fmt.Print(" Accessing workspace:\r\n" + title + "\r\n Claude Code'll be able to read, edit, and execute files here.\r\n\r\n Security guide\r\n\r\n")
				for i, o := range trust {
					m := "   "
					if i == sel {
						m = " ❯ "
					}
					fmt.Printf("%s%s\r\n", m, o)
				}
				fmt.Print(" Enter to confirm · Esc to cancel")
				if twist == "unsafeafterclaude" {
					fmt.Print("\r\n WARNING: Enter also trusts every folder on this host.")
				}
			}
			return
		}
		if mode == "menu" {
			fmt.Print("Trust this folder?\r\n› 1. Yes\r\n  2. No\r\nEnter to confirm · Esc to cancel\r\n")
			return
		}
		if time.Now().Before(ready) {
			fmt.Print("Starting, loading plugins ...\r\n")
			return
		}
		if drv.approval {
			fmt.Print("Run the command?\r\nDo you want to proceed?\r\n› 1. Yes\r\n  2. No\r\n")
			return
		}
		if drv.busy {
			fmt.Print("Working (esc to interrupt)\r\n")
		}
		if tool == "claude" {
			fmt.Printf("Test Claude\r\n──────────────────────────────\r\n❯ %s\r\n──────────────────────────────\r\n  -- INSERT --", wrapped(string(draft)))
		} else if wrap > 0 {
			fmt.Printf("Test Codex\r\n› %s\r\n\r\n  test-model high · ~/x", wrapped(string(draft)))
		} else {
			if twist == "unsafeinput" {
				fmt.Print("Hooks need review\r\n  2. Trust all and continue\r\n")
			}
			fmt.Printf("Test Codex\r\n› %s\r\n  ? for shortcuts", string(draft))
		}
	}
	draw()
	if time.Now().Before(ready) {
		go func() {
			time.Sleep(time.Until(ready))
			drv.mu.Lock()
			draw()
			drv.mu.Unlock()
		}()
	}
	rd := bufio.NewReader(os.Stdin)
	for {
		ch, _, err := rd.ReadRune()
		if err != nil {
			return
		}
		if time.Now().Before(ready) {
			continue
		}
		drv.mu.Lock()
		if drv.busy && ch == '\x1b' && drv.tool != "" {
			close(drv.abort)
			drv.mu.Unlock()
			continue
		}
		if drv.tool != "" && (ch == '\r' || ch == '\n') && trust == nil {
			if drv.twist == "dropenter" && !drv.dropped {
				drv.dropped = true
				drv.mu.Unlock()
				continue
			}
			if len(draft) > 0 && !drv.busy {
				json.NewEncoder(log).Encode(struct{ Line string }{string(draft)})
				drv.submit(string(draft), draw)
				draft = nil
				draw()
			}
			drv.mu.Unlock()
			continue
		}
		drv.mu.Unlock()
		if _, err := fmt.Fprint(keys, string(ch)); err != nil {
			panic(err)
		}
		if trust != nil {
			switch ch {
			case '\x1b':
				if b, _, _ := rd.ReadRune(); b == '[' {
					if d, _, _ := rd.ReadRune(); d == 'B' && sel < len(trust)-1 {
						sel++
						if twist == "unsafechange" {
							trust, mode, draft = nil, "", []rune("Trust all and continue")
							draw()
							continue
						}
					} else if d == 'A' && sel > 0 {
						sel--
					}
				}
			case '\r', '\n':
				json.NewEncoder(log).Encode(struct{ Line string }{"choice:" + trust[sel]})
				if strings.HasPrefix(trust[sel], "No") {
					return
				}
				trust, mode = nil, ""
			}
			draw()
			continue
		}
		drv.mu.Lock()
		switch ch {
		case '\r', '\n':
			if err := json.NewEncoder(log).Encode(struct{ Line string }{string(draft)}); err != nil {
				panic(err)
			}
			draft = nil
		case '\x7f', '\b':
			if len(draft) > 0 {
				draft = draft[:len(draft)-1]
			}
		default:
			draft = append(draft, ch)
		}
		draw()
		drv.mu.Unlock()
	}
}

// managedLogin plays sshd for a managed host (design §25): the login's
// host maps, in SUNSTACK_TEST_SSH_MANAGED, to a file of that host's
// environment. The key given with -i must be in that host's
// authorized_keys, and its forced command runs with a bare PATH, as sshd
// runs it. It reports false when the host is not a managed one.
func managedLogin(args []string) bool {
	var hosts map[string]string
	if json.Unmarshal([]byte(os.Getenv("SUNSTACK_TEST_SSH_MANAGED")), &hosts) != nil || len(args) == 0 {
		return false
	}
	target := args[len(args)-1]
	if _, h, ok := strings.Cut(target, "@"); ok {
		target = h
	}
	envFile, ok := hosts[target]
	if !ok {
		return false
	}
	b, _ := os.ReadFile(envFile)
	env := []string{"PATH=/usr/bin:/bin"}
	home := ""
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			env = append(env, l)
			if k == "HOME" {
				home = v
			}
		}
	}
	key := ""
	for i, a := range args {
		if a == "-i" && i+1 < len(args) {
			key = args[i+1]
		}
	}
	pub, _ := os.ReadFile(key + ".pub")
	f := strings.Fields(string(pub))
	auth, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	for _, line := range strings.Split(string(auth), "\n") {
		if len(f) < 2 || !strings.Contains(line, f[1]) {
			continue
		}
		_, rest, ok := strings.Cut(line, `command="`)
		if !ok {
			fmt.Println("a login shell")
			return true
		}
		command, _, _ := strings.Cut(rest, `"`)
		parts := strings.Fields(command)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Env, cmd.Dir = env, home
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				os.Exit(e.ExitCode())
			}
			os.Exit(255)
		}
		return true
	}
	fmt.Fprintln(os.Stderr, "user@"+target+": Permission denied (publickey).")
	os.Exit(255)
	return true
}

// Test-only CLI stand-ins. No real Claude/Codex executable is ever invoked.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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
		if len(args) == 4 && args[0] == "-o" && args[1] == "stat=,comm=" && args[2] == "-t" {
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
		if os.Getenv("SUNSTACK_TEST_MODE") == "exit" {
			return
		}
		interactive(name, args)
	default:
		panic("unknown test executable: " + name)
	}
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
	draft := []rune{}
	if mode == "draft" {
		draft = []rune("my unsent draft")
	}
	draw := func() {
		fmt.Print("\x1b[2J\x1b[H")
		if mode == "menu" {
			fmt.Print("Trust this folder?\r\n› 1. Yes\r\n  2. No\r\nEnter to confirm · Esc to cancel\r\n")
			return
		}
		if tool == "claude" {
			fmt.Printf("Test Claude\r\n──────────────────────────────\r\n❯ %s\r\n──────────────────────────────\r\n  -- INSERT --", string(draft))
		} else {
			fmt.Printf("Test Codex\r\n› %s\r\n  ? for shortcuts", string(draft))
		}
	}
	draw()
	rd := bufio.NewReader(os.Stdin)
	for {
		ch, _, err := rd.ReadRune()
		if err != nil {
			return
		}
		if _, err := fmt.Fprint(keys, string(ch)); err != nil {
			panic(err)
		}
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
	}
}

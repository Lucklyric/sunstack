// Command ssh is a test stand-in for the OpenSSH client (design §18.12). It
// plays sshd on the hub too: with -i, it looks the key up in the hub user's
// authorized_keys and runs that line's forced command with
// SSH_ORIGINAL_COMMAND set, exactly as sshd would. Without -i (the user's
// own login) it runs the command it was given.
//
// SUNSTACK_TEST_HUB_HOME is the hub user's home folder.
// SUNSTACK_TEST_SUNSTACK is the sunstack binary for a plain command.
// SUNSTACK_TEST_SSH_LOGIN=deny refuses the user's own login.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	args := os.Args[1:]
	key, target := "", ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case target != "":
			rest = append(rest, a)
		case a == "-i" || a == "-o" || a == "-p" || a == "-l":
			if a == "-i" && i+1 < len(args) {
				key = args[i+1]
			}
			i++
		case strings.HasPrefix(a, "-"):
		default:
			target = a
		}
	}
	hubHome := os.Getenv("SUNSTACK_TEST_HUB_HOME")
	if target == "" || hubHome == "" || target == "unreachable" {
		fmt.Fprintln(os.Stderr, "ssh: connect to host "+target+": Connection refused")
		os.Exit(255)
	}
	command := strings.Join(rest, " ")
	env := hubEnv(hubHome)
	if key == "" {
		if os.Getenv("SUNSTACK_TEST_SSH_LOGIN") == "deny" {
			fmt.Fprintln(os.Stderr, "Permission denied (publickey).")
			os.Exit(255)
		}
		argv := split(command)
		if len(argv) == 0 || argv[0] != "sunstack" {
			fmt.Fprintln(os.Stderr, "stand-in ssh runs only sunstack commands")
			os.Exit(127)
		}
		os.Exit(runCmd(os.Getenv("SUNSTACK_TEST_SUNSTACK"), argv[1:], env))
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Load key: ", err)
		os.Exit(255)
	}
	f := strings.Fields(string(pub))
	keys, _ := os.ReadFile(filepath.Join(hubHome, ".ssh", "authorized_keys"))
	forced := ""
	found := false
	for _, l := range strings.Split(string(keys), "\n") {
		if len(f) >= 2 && strings.Contains(l, " "+f[0]+" "+f[1]) {
			found = true
			if m := regexp.MustCompile(`command="([^"]*)"`).FindStringSubmatch(l); m != nil {
				forced = m[1]
			}
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "Permission denied (publickey).")
		os.Exit(255)
	}
	if forced == "" {
		fmt.Fprintln(os.Stderr, "stand-in ssh expects a forced command")
		os.Exit(255)
	}
	argv := split(forced)
	os.Exit(runCmd(argv[0], argv[1:], append(env, "SSH_ORIGINAL_COMMAND="+command)))
}

func hubEnv(home string) []string {
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch k {
		case "HOME", "USERPROFILE", "SUNSTACK_HOME", "SUNSTACK_HOST_NAME", "SSH_ORIGINAL_COMMAND":
			continue
		}
		env = append(env, e)
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "SUNSTACK_HOME="+filepath.Join(home, ".sunstack"), "SUNSTACK_HOST_NAME=hub-host")
}

func runCmd(prog string, args []string, env []string) int {
	cmd := exec.Command(prog, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 255
	}
	return 0
}

// split breaks a command line at spaces, keeping single-quoted parts whole.
func split(s string) []string {
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

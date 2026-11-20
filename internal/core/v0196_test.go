package core

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The last row keeps its empty trailing fields.
func TestPaneRowsKeepsTrailingFields(t *testing.T) {
	rows := paneRows([]byte("%1\ta\t\t\n%2\troot|x\t\t\t\t\n"))
	if len(rows) != 2 || rows[1] != "%2\troot|x\t\t\t\t" {
		t.Fatalf("rows: %q", rows)
	}
	if paneRows([]byte("\n")) != nil {
		t.Errorf("an empty list has rows")
	}
}

// A pane counts as exited only when it is a shell with nothing else under
// it. A wrapper shell with the real shell in the foreground needs a
// terminal, so the tmux test covers it.
func TestOnlyShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps is Unix")
	}
	start := func(script string, kids int) *exec.Cmd {
		cmd := exec.Command("sh", "-c", script)
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { in.Close(); cmd.Process.Kill(); cmd.Wait() })
		for i := 0; i < 50; i++ {
			out, _ := exec.Command("ps", "-A", "-o", "ppid=").Output()
			n := 0
			for _, l := range strings.Fields(string(out)) {
				if l == strconv.Itoa(cmd.Process.Pid) {
					n++
				}
			}
			if n >= kids {
				return cmd
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%q: no %d child process(es)", script, kids)
		return nil
	}
	idle := start("read x", 0)
	fifo := filepath.Join(t.TempDir(), "ff")
	if err := exec.Command("mkfifo", fifo).Run(); err != nil {
		t.Fatal(err)
	}
	sub := start("(read x < "+fifo+") & read x", 1)
	job := start("sleep 30 & read x", 1)
	time.Sleep(100 * time.Millisecond)
	if !onlyShells(strconv.Itoa(idle.Process.Pid)) {
		t.Errorf("a shell at its prompt runs something")
	}
	if onlyShells(strconv.Itoa(sub.Process.Pid)) {
		t.Errorf("a shell with a background subshell runs only shells")
	}
	if onlyShells(strconv.Itoa(job.Process.Pid)) {
		t.Errorf("a shell with a background job runs only shells")
	}
	if onlyShells("") {
		t.Errorf("no pid counts as only shells")
	}
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { sleeper.Process.Kill(); sleeper.Wait() }()
	if onlyShells(strconv.Itoa(sleeper.Process.Pid)) {
		t.Errorf("a pane whose own process is no shell counts as only shells")
	}
	if onlyShells(strconv.Itoa(job.Process.Pid + 1000000)) {
		t.Errorf("a pid not listed counts as only shells")
	}
}

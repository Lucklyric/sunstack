package core

import (
	"os/exec"
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

// A pane counts as exited only when every process under it is a shell: a
// wrapper "sh -c sh" passes, a shell with a background job does not.
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
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		return cmd
	}
	idle := start("read x", 0)
	wrapper := start("sh -c 'read x'; :", 1)
	job := start("sleep 30 & read x", 1)
	time.Sleep(100 * time.Millisecond)
	if !onlyShells(strconv.Itoa(idle.Process.Pid)) {
		t.Errorf("a shell at its prompt runs something")
	}
	if !onlyShells(strconv.Itoa(wrapper.Process.Pid)) {
		t.Errorf("a shell under a wrapper shell runs something")
	}
	if onlyShells(strconv.Itoa(job.Process.Pid)) {
		t.Errorf("a shell with a background job runs only shells")
	}
	if onlyShells("") {
		t.Errorf("no pid counts as only shells")
	}
}

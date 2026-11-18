package core

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
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

// A shell with a background job still runs something.
func TestHasChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ps is Unix")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if !hasChildren(strconv.Itoa(os.Getpid())) {
		t.Errorf("this test process has a child")
	}
	if hasChildren(strconv.Itoa(cmd.Process.Pid)) {
		t.Errorf("sleep has no child")
	}
}

package hub

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Lucklyric/sunstack/internal/setup"
)

// Remote update (§20.6): a host that allows it updates sunstack and both
// CLIs' plugins, answers with the old and new version, then ends its
// connector so the service manager restarts it on the new binary.

// UpdateArgs is an update request: the version the asking host wants.
type UpdateArgs struct {
	Version string `json:"version"`
}

// UpdateResult is what the updated host reports.
type UpdateResult struct {
	From, To string
}

var restartAfterReply atomic.Bool

func updateHandler(_, _ string, raw json.RawMessage) (any, error) {
	var a UpdateArgs
	if json.Unmarshal(raw, &a) != nil {
		return nil, errors.New("bad update request")
	}
	if os.Getenv("SUNSTACK_SERVICE") == "" {
		return nil, errors.New("update it by hand: its connector runs without a service to restart it")
	}
	if a.Version != "" && setup.Older(a.Version, Version) {
		return nil, errors.New("it runs " + Version + ", newer than " + a.Version + "; no downgrade")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// SUNSTACK_UPDATE_CMD stands in for the update in tests.
	cmd := exec.Command(exe, "update")
	if c := os.Getenv("SUNSTACK_UPDATE_CMD"); c != "" {
		cmd = exec.Command("/bin/sh", "-c", c)
	}
	cmd.Stdin = nil
	if out, err := cmd.CombinedOutput(); err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return nil, errors.New("sunstack update failed: " + lines[len(lines)-1])
	}
	to := Version
	if out, err := exec.Command(exe, "--version").Output(); err == nil {
		if f := strings.Fields(string(out)); len(f) > 1 {
			to = f[1]
		}
	}
	restartAfterReply.Store(true)
	return UpdateResult{From: Version, To: to}, nil
}

// afterReply ends the connector once an update's reply is queued; the new
// connector sends it.
func afterReply() {
	if restartAfterReply.Load() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
}

// Update asks the host called hostName to update to version.
func Update(hostName, version string) (*UpdateResult, error) {
	id, err := Request(hostName, "update", UpdateArgs{Version: version}, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	_ = FlushOnce()
	c, err := Await(id, 3*time.Minute)
	if err != nil {
		return nil, err
	}
	var r UpdateResult
	if json.Unmarshal(c.Result, &r) != nil {
		return nil, failErr("bad_reply", "the update reply cannot be read")
	}
	return &r, nil
}

// RestartService restarts this host's connector service, after a local
// update.
func RestartService() error {
	path, err := ServiceFile()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return failErr("no_service", "no connector service on this host (sunstack hub connect --install)")
	}
	if strings.HasSuffix(path, ".plist") {
		return exec.Command("launchctl", "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+serviceName).Run()
	}
	return exec.Command("systemctl", "--user", "restart", "sunstack-hub-connect.service").Run()
}

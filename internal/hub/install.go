package hub

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Lucklyric/sunstack/internal/core"
)

const serviceName = "org.sunstack.hub-connect"

// ServiceFile is where --install writes the connector's service.
func ServiceFile() (string, error) {
	h, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(h, "Library", "LaunchAgents", serviceName+".plist"), nil
	case "linux":
		return filepath.Join(h, ".config", "systemd", "user", "sunstack-hub-connect.service"), nil
	}
	return "", failErr("unsupported", "hub connect --install is for macOS and Linux; on %s run sunstack hub connect in a terminal", runtime.GOOS)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// Install writes the service that keeps the connector running, then loads
// it. SUNSTACK_HUB_NO_LOAD leaves it unloaded (tests).
func Install(out io.Writer) error {
	if _, _, err := joined(); err != nil {
		return err
	}
	path, err := ServiceFile()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	logf := filepath.Join(core.Home(), "log", "hub-connect.log")
	if err := os.MkdirAll(filepath.Dir(logf), 0o700); err != nil {
		return err
	}
	// The service starts with a bare PATH; keep this one so tmux and
	// the CLIs are found.
	env := map[string]string{"PATH": os.Getenv("PATH")}
	if h := os.Getenv("SUNSTACK_HOME"); h != "" {
		env["SUNSTACK_HOME"] = h
	}
	var text string
	if runtime.GOOS == "darwin" {
		var e strings.Builder
		for k, v := range env {
			fmt.Fprintf(&e, "\t\t<key>%s</key><string>%s</string>\n", k, xmlEscape(v))
		}
		text = fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>hub</string><string>connect</string></array>
	<key>EnvironmentVariables</key>
	<dict>
%s	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>10</integer>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, serviceName, xmlEscape(exe), e.String(), xmlEscape(logf), xmlEscape(logf))
	} else {
		var e strings.Builder
		for k, v := range env {
			fmt.Fprintf(&e, "Environment=%s\n", strconv.Quote(k+"="+v))
		}
		text = fmt.Sprintf(`[Unit]
Description=Sunstack org hub connector

[Service]
ExecStart=%s hub connect
%sRestart=always
RestartSec=10

[Install]
WantedBy=default.target
`, strconv.Quote(exe), e.String())
	}
	if err := writeFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "wrote %s\n", path)
	if os.Getenv("SUNSTACK_HUB_NO_LOAD") != "" {
		return nil
	}
	if runtime.GOOS == "darwin" {
		uid := strconv.Itoa(os.Getuid())
		_ = exec.Command("launchctl", "bootout", "gui/"+uid, path).Run()
		if b, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, path).CombinedOutput(); err != nil {
			return failErr("launchctl", "%v: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintln(out, "started; it runs while you are logged in")
		return nil
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", "sunstack-hub-connect.service"}} {
		if b, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return failErr("systemctl", "%v: %s", err, strings.TrimSpace(string(b)))
		}
	}
	fmt.Fprintln(out, "started; to keep it running without a login: loginctl enable-linger $USER")
	return nil
}

// Uninstall stops and removes the service.
func Uninstall(out io.Writer) error {
	path, err := ServiceFile()
	if err != nil {
		return err
	}
	if os.Getenv("SUNSTACK_HUB_NO_LOAD") == "" {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), path).Run()
		} else {
			_ = exec.Command("systemctl", "--user", "disable", "--now", "sunstack-hub-connect.service").Run()
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Fprintf(out, "removed %s\n", path)
	return nil
}

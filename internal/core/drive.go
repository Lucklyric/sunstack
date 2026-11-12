package core

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Driving a session (§47): hand a Claude Code or Codex session in a tmux
// pane a prompt, wait for the turn to end and read the reply. The turn's end
// and the reply come from the session's transcript, never from the screen;
// the screen only says whether the input box is ready and took the prompt.

// DriveOptions are the inputs of sunstack drive.
type DriveOptions struct {
	Target string // a free or plain session: label, session ID or pane
	Text   string // the prompt; empty with Wait, Reply or Cancel
	NoWait bool   // return once the prompt is accepted
	Wait   bool   // send nothing: wait for the running turn
	Reply  bool   // send nothing: the last finished turn's reply
	Cancel bool   // interrupt the running turn
	// Timeout bounds the wait for the turn's end; 0 is driveTimeout.
	Timeout time.Duration
	// The caller's pane.
	Pane, Socket string
}

// DriveResult is what a drive did. Status is sent (accepted, not waited
// for), done, idle (nothing ran) or cancelled.
type DriveResult struct {
	Pane, Tool, Status, Reply string
	Note                      string // a trust screen sunstack answered on the way
}

var (
	driveTimeout = 10 * time.Minute
	// driveReady is how long a session may take to show its input box, and
	// driveAccept how long the transcript may take to show the new turn.
	driveReady  = 90 * time.Second
	driveAccept = 30 * time.Second
	drivePoll   = 400 * time.Millisecond
	// driveQuiet is how long the transcript must be still before a menu on
	// the screen counts as the session waiting for an answer.
	driveQuiet = 4 * time.Second
	// driveSettle is how long a Claude Code transcript must be still before
	// its idle report ends a turn with no end record.
	driveSettle = 3 * time.Second
	// driveMenu is how long a menu may show before a prompt is given up.
	driveMenu = 10 * time.Second
)

func driveDir() string { return filepath.Join(Home(), "drive") }

// plainPrompt reports whether text can be typed into the input box as it
// is: one short line that opens no completion popup or command, and holds
// nothing ReadyForInput would read as a menu. Anything else goes in a file.
func plainPrompt(text string) bool {
	if text == "" || len([]rune(text)) > 160 || strings.ContainsAny(text, "\n\r\t@$\\") || strings.ContainsAny(text[:1], "/!#?") {
		return false
	}
	if text != strings.TrimSpace(text) || hasDialog(text) || menuLine.MatchString(text) {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// driveLine is what drive types for a prompt: the prompt itself when it is
// plain, else one fixed line naming a file that holds it. Claude Code takes
// the file as an @ mention, which it reads without asking; Codex reads a
// path in any sandbox.
func driveLine(tool, text string) (string, error) {
	if plainPrompt(text) {
		return text, nil
	}
	if err := os.MkdirAll(driveDir(), 0o700); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	// Old prompt files go: a session reads its file within the turn.
	if old, _ := filepath.Glob(filepath.Join(driveDir(), "*.md")); len(old) > 0 {
		for _, f := range old {
			if st, err := os.Stat(f); err == nil && time.Since(st.ModTime()) > 7*24*time.Hour {
				os.Remove(f)
			}
		}
	}
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	path := filepath.Join(driveDir(), hex.EncodeToString(b)+".md")
	if err := os.WriteFile(path, []byte(strings.TrimRight(text, "\n")+"\n"), 0o600); err != nil {
		return "", fail(ExitFail, "fs", "%v", err)
	}
	if strings.ContainsAny(path, " \t") {
		return "", fail(ExitFail, "fs", "the sunstack home %s has a space in its path; a prompt file there cannot be named in one word", Home())
	}
	if tool == "claude" {
		return "Read @" + path + " and follow the instructions in it.", nil
	}
	return "Read the file " + path + " and follow the instructions in it.", nil
}

// inputText is everything typed in the CLI's input box, with the lines a
// narrow pane wraps it into joined and all spaces dropped, so it compares
// with what was typed however it wraps. ok is false with no input box.
func inputText(tool, screen string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	rule := func(l string) bool {
		t := strings.TrimSpace(l)
		return strings.HasPrefix(t, "───") || strings.HasSuffix(t, "───")
	}
	squash := func(parts []string) string { return strings.Join(strings.Fields(strings.Join(parts, " ")), "") }
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		switch tool {
		case "claude":
			if !strings.HasPrefix(t, "❯") || i == 0 || !rule(lines[i-1]) {
				continue
			}
			parts := []string{strings.TrimPrefix(t, "❯")}
			for j := i + 1; j < len(lines); j++ {
				if rule(lines[j]) {
					return squash(parts), true
				}
				parts = append(parts, lines[j])
			}
		case "codex":
			if !strings.HasPrefix(t, "›") {
				continue
			}
			parts := []string{strings.TrimPrefix(t, "›")}
			// Wrapped lines follow, indented, up to the blank line above
			// the footer.
			for j := i + 1; j < len(lines) && strings.TrimSpace(lines[j]) != ""; j++ {
				if j == len(lines)-1 || strings.TrimSpace(lines[j+1]) == "" && onlyBlank(lines[j+2:]) {
					break // the footer is the last line of the block
				}
				parts = append(parts, lines[j])
			}
			return squash(parts), true
		}
	}
	return "", false
}

func onlyBlank(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	return true
}

// turn is what a transcript says about its last turn.
type turn struct {
	started, ended bool
	how            string // done or interrupted, once ended
	reply          []string
	at             time.Time // when the last record was read
	last           time.Time // the last record's own time
}

// transcript follows one session's transcript from an offset, a whole line
// at a time, and keeps the state of its last turn.
type transcript struct {
	tool, path string
	off        int64
	since      time.Time // records before it are not this drive's
	turn
}

// poll reads the lines added since the last poll.
func (t *transcript) poll() {
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return // a line still being written is read at the next poll
		}
		t.off += int64(len(line))
		t.at = time.Now()
		t.record(line)
	}
}

func (t *transcript) record(line []byte) {
	var e struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		Timestamp string `json:"timestamp"`
		Sidechain bool   `json:"isSidechain"`
		Meta      bool   `json:"isMeta"`
		Compact   bool   `json:"isCompactSummary"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Last    string `json:"last_agent_message"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Sidechain {
		return
	}
	at, terr := time.Parse(time.RFC3339Nano, e.Timestamp)
	if terr == nil && at.After(t.last) {
		t.last = at
	}
	if !t.since.IsZero() && (terr != nil || at.Before(t.since)) {
		return
	}
	begin := func() { t.started, t.ended, t.how, t.reply = true, false, "", nil }
	end := func(how string) {
		if t.started {
			t.ended, t.how = true, how
		}
	}
	text := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			t.reply = append(t.reply, s)
		}
	}
	if t.tool == "codex" {
		switch {
		case e.Type == "event_msg" && e.Payload.Type == "task_started":
			begin()
		case e.Type == "event_msg" && e.Payload.Type == "task_complete":
			if s := strings.TrimSpace(e.Payload.Last); s != "" {
				t.reply = []string{s}
			} else if n := len(t.reply); n > 1 {
				t.reply = t.reply[n-1:] // the last message is the answer
			}
			end("done")
		case e.Type == "event_msg" && e.Payload.Type == "turn_aborted":
			end("interrupted")
		case e.Type == "response_item" && e.Payload.Type == "message" && e.Payload.Role == "assistant":
			for _, c := range e.Payload.Content {
				if c.Type == "output_text" {
					text(c.Text)
				}
			}
		}
		return
	}
	switch e.Type {
	case "user":
		// A prompt is a string, or text parts; a tool result is neither.
		var s string
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &s) != nil {
			if json.Unmarshal(e.Message.Content, &parts) != nil {
				return
			}
			for _, p := range parts {
				if p.Type == "text" {
					s += p.Text
				}
			}
			if s == "" {
				// A tool result: what Claude wrote before it was not the answer.
				t.reply = nil
				return
			}
		}
		if strings.HasPrefix(strings.TrimSpace(s), "[Request interrupted") {
			end("interrupted")
			return
		}
		// A compaction summary, a local command and its output are user
		// records too, and no turn follows them.
		if e.Meta || e.Compact || strings.HasPrefix(strings.TrimSpace(s), "<") {
			return
		}
		begin()
	case "assistant":
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &parts) == nil {
			for _, p := range parts {
				if p.Type == "text" {
					text(p.Text)
				}
			}
		}
	case "system":
		if e.Subtype == "turn_duration" {
			end("done")
		}
	}
}

// settle ends a Claude Code turn that the transcript leaves open: a version
// that writes no turn_duration record, or a prompt no turn followed. Claude
// Code's own report decides: idle, with the transcript still for a moment.
func (t *transcript) settle(socket, pane string) {
	if t.tool != "claude" || !t.started || t.ended || time.Since(t.last) < driveSettle {
		return
	}
	// Idle alone is not enough: at an approval prompt the turn is not over.
	if screen := paneScreen(socket, pane); !ReadyForInput("claude", screen) || hasMenu(screen) {
		return
	}
	pid, _, ok := paneToolProc(socket, pane, "claude")
	if !ok {
		return
	}
	list, _ := claudeSessions()
	for _, s := range list {
		if s.PID == pid && s.Status == "idle" {
			t.ended, t.how = true, "done"
		}
	}
}

// paneTranscript finds the transcript of the session in a pane: the rollout
// the Codex process holds open, or the file of the session ID Claude Code
// reports for the process. Empty when there is none yet, or it cannot be told.
func paneTranscript(socket, pane, tool string) string {
	pid, _, ok := paneToolProc(socket, pane, tool)
	if !ok {
		return ""
	}
	if tool == "codex" {
		files, _, _ := openFiles(pid)
		return mainRollout(files)
	}
	list, _ := claudeSessions()
	for _, s := range list {
		if s.PID == pid {
			return s.Transcript
		}
	}
	return ""
}

// driveTarget resolves the session to drive and checks it may be driven: a
// Claude Code or Codex session in a tmux pane that works as no agent, and
// not the caller's own.
func driveTarget(p *Project, o DriveOptions) (pane, socket, tool string, err error) {
	if strings.HasPrefix(o.Target, "%") {
		// A pane of the caller's tmux server, or the default one (as peek).
		pane, socket = o.Target, o.Socket
		if socket == "" {
			socket, _ = DefaultSocket()
		}
	} else {
		at, err := FindAttach(p, o.Target)
		if err != nil {
			return "", "", "", err
		}
		pane, socket = at.Pane, at.Socket
	}
	if pane == o.Pane && socket == o.Socket {
		return "", "", "", fail(ExitUsage, "own_session", "%s is this session; a session does not drive itself", o.Target)
	}
	out, terr := quickTmux(socket, "display-message", "-p", "-t", pane, "#{@sunstack}\t#{@sunstack_tool}\t#{pane_current_path}")
	if terr != nil {
		return "", "", "", fail(ExitFail, "tmux", "could not read pane %s: %v", pane, terr)
	}
	marks := strings.Split(strings.TrimRight(string(out), "\n"), "\t")
	agent := marks[0] != ""
	// A session that took an agent with sunstack as carries no mark: its
	// claim names the pane. The caller's team, the team of the pane's
	// folder and every team this host knows are looked at.
	teams := IndexedProjects()
	if p != nil {
		teams = append(teams, p)
	}
	if len(marks) > 2 {
		if t, err := FindProject(marks[2]); err == nil {
			teams = append(teams, t)
		}
	}
	seen := map[string]bool{}
	for _, t := range teams {
		if agent || seen[t.Root] {
			continue
		}
		seen[t.Root] = true
		for _, id := range t.Agents() {
			claims, _ := t.Claims(id)
			for _, c := range claims {
				if samePane(c, pane, socket) {
					agent = true
				}
			}
		}
	}
	if agent {
		return "", "", "", fail(ExitUsage, "agent_session", "%s works as an agent; its work comes through its inbox: sunstack send", o.Target)
	}
	if len(marks) > 1 && (marks[1] == "claude" || marks[1] == "codex") {
		tool = marks[1]
	}
	// The mark says what was started there; the foreground says what runs.
	for _, t := range []string{tool, "codex", "claude"} {
		if t != "" && paneRunsTool(socket, pane, t) {
			return pane, socket, t, nil
		}
	}
	if tool != "" {
		return pane, socket, tool, nil // still starting, or exited: the caller says which
	}
	return "", "", "", fail(ExitFail, "no_cli", "pane %s runs neither Claude Code nor Codex in the foreground", pane)
}

// Drive runs one sunstack drive. p is the team the caller is in, if any.
func Drive(p *Project, o DriveOptions) (*DriveResult, error) {
	pane, socket, tool, err := driveTarget(p, o)
	if err != nil {
		return nil, err
	}
	res := &DriveResult{Pane: pane, Tool: tool}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = driveTimeout
	}
	if o.Text == "" {
		// The state of the session's last turn, whoever started it.
		path := paneTranscript(socket, pane, tool)
		if path == "" {
			if !paneRunsTool(socket, pane, tool) {
				return nil, exitedErr(pane, tool)
			}
			return nil, fail(ExitFail, "no_transcript", "the session in pane %s has no transcript yet (no prompt so far), or sunstack could not find it; sunstack peek %s shows the pane", pane, pane)
		}
		tr := &transcript{tool: tool, path: path}
		if st, err := os.Stat(path); err == nil && st.Size() > 8<<20 {
			tr.off = lineStart(path, st.Size()-8<<20)
		}
		tr.poll()
		tr.settle(socket, pane)
		switch {
		case o.Reply:
			if !tr.started {
				return nil, fail(ExitFail, "no_turn", "the session in pane %s has had no turn yet", pane)
			}
			if !tr.ended {
				return nil, fail(ExitWait, "running", "the session in pane %s has no finished turn to read; sunstack drive %s --wait", pane, o.Target)
			}
			res.Status, res.Reply = tr.how, strings.Join(tr.reply, "\n\n")
			return res, nil
		case o.Cancel:
			if !tr.started || tr.ended {
				res.Status = "idle"
				return res, nil
			}
			if _, err := quickTmux(socket, "send-keys", "-t", pane, "Escape"); err != nil {
				return nil, fail(ExitFail, "tmux", "could not reach pane %s: %v", pane, err)
			}
			if err := follow(tr, socket, pane, 15*time.Second); err != nil && tr.how != "interrupted" {
				return nil, fail(ExitWait, "running", "Esc was pressed in pane %s, but the turn has not stopped; sunstack peek %s shows the pane", pane, pane)
			}
			res.Status = "cancelled"
			return res, nil
		}
		if !tr.started || tr.ended {
			res.Status, res.Reply = "idle", strings.Join(tr.reply, "\n\n")
			return res, nil
		}
		return finish(res, tr, socket, pane, o.Target, timeout)
	}

	line, err := driveLine(tool, o.Text)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(Home(), "locks"), 0o700); err != nil {
		return nil, fail(ExitFail, "fs", "%v", err)
	}
	// One driver types into a pane at a time.
	unlock, err := LockDirOnce(filepath.Join(Home(), "locks", "drive-"+paneKey(socket, pane)))
	if err != nil {
		return nil, fail(ExitClaim, "busy", "another sunstack drive is sending a prompt to pane %s", pane)
	}
	tr, note, err := sendPrompt(socket, pane, tool, line, o.Target)
	unlock()
	res.Note = note
	if err != nil {
		return res, err
	}
	if o.NoWait {
		res.Status = "sent"
		return res, nil
	}
	return finish(res, tr, socket, pane, o.Target, timeout)
}

func paneKey(socket, pane string) string {
	return strings.NewReplacer("/", "_", "%", "").Replace(filepath.Base(socket) + "-" + pane)
}

func exitedErr(pane, tool string) error {
	return fail(ExitFail, "exited", "%s is not running in pane %s (it exited, or the pane shows a shell)", toolName(tool), pane)
}

func toolName(tool string) string {
	if tool == "claude" {
		return "Claude Code"
	}
	return "Codex"
}

// lineStart is the offset of the first whole line at or after off.
func lineStart(path string, off int64) int64 {
	f, err := os.Open(path)
	if err != nil || off <= 0 {
		return 0
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return 0
	}
	skipped, err := bufio.NewReaderSize(f, 1<<16).ReadBytes('\n')
	if err != nil {
		return 0
	}
	return off + int64(len(skipped))
}

// sendPrompt waits for the session's empty input box, types line, presses
// Enter and confirms from the transcript that a turn began. It never
// reports a prompt as sent that the session did not take.
func sendPrompt(socket, pane, tool, line, target string) (*transcript, string, error) {
	note := ""
	begin := time.Now()
	var menu time.Time // since when a menu shows
	inTeam := false
	if out, err := quickTmux(socket, "display-message", "-p", "-t", pane, "#{pane_current_path}"); err == nil {
		inTeam = TeamOf(strings.TrimSpace(string(out)), IndexedProjects()) != nil
	}
	for {
		screen := paneScreen(socket, pane)
		runs, known := paneTool(socket, pane, tool)
		switch {
		case screen == "":
			return nil, note, fail(ExitFail, "no_pane", "could not read tmux pane %s", pane)
		case known && !runs && time.Since(begin) > 5*time.Second:
			return nil, note, exitedErr(pane, tool)
		case runs && ReadyForInput(tool, screen):
		default:
			if runs {
				// A trust screen: answered when the user chose on (§37).
				switch got := AnswerTrust(socket, pane, tool, inTeam, true); {
				case strings.HasPrefix(got, "answered"):
					note = got
					continue
				case got != "":
					return nil, note, fail(ExitWait, "blocked", "the session in pane %s is %s", pane, got)
				}
			}
			switch {
			case !runs || !(hasDialog(screen) || hasMenu(screen)):
				menu = time.Time{}
			case menu.IsZero():
				menu = time.Now()
			}
			if time.Since(begin) > driveReady || (!menu.IsZero() && time.Since(menu) > driveMenu) {
				e := fail(ExitWait, "blocked", "the session in pane %s shows no empty input box (a menu, an approval prompt, or it is still starting); nothing was sent", pane)
				e.Stdout = maskTokens(strings.Join(screenTail(screen, 15), "\n")) + "\n"
				return nil, note, e
			}
			time.Sleep(drivePoll)
			continue
		}
		break
	}
	// A turn already running would queue the prompt behind it.
	path := paneTranscript(socket, pane, tool)
	tr := &transcript{tool: tool, path: path}
	if path != "" {
		if st, err := os.Stat(path); err == nil && st.Size() > 8<<20 {
			tr.off = lineStart(path, st.Size()-8<<20)
		}
		tr.poll()
		tr.settle(socket, pane)
		if tr.started && !tr.ended {
			return nil, note, fail(ExitClaim, "busy", "the session in pane %s is in a turn; wait for it (sunstack drive %s --wait) or stop it (--cancel)", pane, target)
		}
	}
	// The line is typed, read back, and only then sent (as a nudge is).
	want := strings.Join(strings.Fields(line), "")
	if _, err := quickTmux(socket, "send-keys", "-t", pane, "-l", line); err != nil {
		return nil, note, fail(ExitFail, "tmux", "could not type into pane %s: %v", pane, err)
	}
	erase := func() {
		quickTmux(socket, "send-keys", "-t", pane, "-N", fmt.Sprint(len([]rune(line))), "BSpace")
	}
	typed := false
	for i := 0; i < 10 && !typed; i++ {
		time.Sleep(300 * time.Millisecond)
		got, ok := inputText(tool, paneScreen(socket, pane))
		typed = ok && got == want
	}
	if !typed {
		screen := paneScreen(socket, pane)
		got, ok := inputText(tool, screen)
		if ok && strings.HasSuffix(got, want) {
			erase()
			return nil, note, fail(ExitClaim, "draft", "the input box of pane %s held a draft; it was left as it was and nothing was sent", pane)
		}
		// Only what is surely ours is erased: a part of the line, alone.
		if ok && got != "" && strings.HasPrefix(want, got) {
			quickTmux(socket, "send-keys", "-t", pane, "-N", fmt.Sprint(len([]rune(line))), "BSpace")
		} else if ok && got != "" {
			return nil, note, fail(ExitFail, "not_accepted", "the input box of pane %s holds text sunstack did not type in full; it was left as it is and nothing was sent", pane)
		}
		return nil, note, fail(ExitFail, "not_accepted", "the input box of pane %s did not take the prompt (a menu or popup opened, or keys were lost); nothing was sent", pane)
	}
	since := time.Now().Add(-2 * time.Second)
	if _, err := quickTmux(socket, "send-keys", "-t", pane, "Enter"); err != nil {
		erase()
		return nil, note, fail(ExitFail, "tmux", "could not press Enter in pane %s: %v", pane, err)
	}
	// Accepted: the transcript shows a turn that began after Enter.
	tr.started, tr.ended, tr.how, tr.reply, tr.since = false, false, "", nil, since
	again := false
	for sent := time.Now(); time.Since(sent) < driveAccept; time.Sleep(drivePoll) {
		if tr.path == "" {
			// A new session's transcript starts with its first prompt.
			tr.path = paneTranscript(socket, pane, tool)
		}
		if tr.path != "" {
			if tr.poll(); tr.started {
				return tr, note, nil
			}
		}
		// Enter can be lost while the CLI is still loading: once more.
		if got, ok := inputText(tool, paneScreen(socket, pane)); ok && got == want && !again && time.Since(sent) > 2*time.Second {
			again = true
			quickTmux(socket, "send-keys", "-t", pane, "Enter")
		}
	}
	if got, ok := inputText(tool, paneScreen(socket, pane)); ok && got == want {
		erase()
		return nil, note, fail(ExitFail, "not_accepted", "pane %s did not take Enter; the prompt was erased and nothing was sent", pane)
	}
	if tr.path == "" {
		return nil, note, fail(ExitWait, "unconfirmed", "the prompt left the input box of pane %s, but sunstack cannot find the session's transcript, so it cannot tell when the turn ends; it was probably sent, so do not send it again: sunstack peek %s shows the pane", pane, pane)
	}
	return nil, note, fail(ExitWait, "unconfirmed", "the prompt left the input box of pane %s, but its transcript shows no new turn after %s; it was probably sent, so do not send it again: sunstack peek %s shows the pane", pane, driveAccept, pane)
}

// follow reads the transcript until the turn ends. It gives up when the
// time is over, the CLI exits, or the screen shows a menu while the
// transcript is still.
func follow(tr *transcript, socket, pane string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tr.at = time.Now()
	for looked := time.Now(); ; time.Sleep(drivePoll) {
		if tr.poll(); tr.ended {
			return nil
		}
		if time.Now().After(deadline) {
			return fail(ExitWait, "timeout", "the turn in pane %s has not ended after %s", pane, timeout.Round(time.Second))
		}
		if time.Since(looked) < 3*time.Second {
			continue
		}
		looked = time.Now()
		if runs, known := paneTool(socket, pane, tr.tool); known && !runs {
			return exitedErr(pane, tr.tool)
		}
		if time.Since(tr.at) > driveQuiet {
			if screen := paneScreen(socket, pane); screen != "" && !ReadyForInput(tr.tool, screen) && (hasDialog(screen) || hasMenu(screen)) {
				e := fail(ExitWait, "blocked", "the session in pane %s waits at a menu or an approval prompt; answer it there", pane)
				e.Stdout = maskTokens(strings.Join(screenTail(screen, 15), "\n")) + "\n"
				return e
			}
		}
		if tr.settle(socket, pane); tr.ended {
			return nil
		}
	}
}

func hasMenu(screen string) bool {
	for _, l := range screenTail(screen, 15) {
		if menuLine.MatchString(l) {
			return true
		}
	}
	return false
}

// finish waits for the turn and fills in its reply.
func finish(res *DriveResult, tr *transcript, socket, pane, target string, timeout time.Duration) (*DriveResult, error) {
	if err := follow(tr, socket, pane, timeout); err != nil {
		if e, ok := err.(*Error); ok && e.Reason == "timeout" {
			e.Msg += "; it is still running: sunstack drive " + target + " --wait"
		}
		return res, err
	}
	res.Status, res.Reply = tr.how, strings.Join(tr.reply, "\n\n")
	if tr.how == "interrupted" {
		return res, fail(ExitWait, "interrupted", "the turn in pane %s was interrupted before it ended", pane)
	}
	return res, nil
}

var (
	nudgeCodex  = regexp.MustCompile(`(?i)(^|[^a-z0-9_])codex([^a-z0-9_]|$)`)
	nudgeClaude = regexp.MustCompile(`(?i)(^|[^a-z0-9_])((spawn|launch|start|drive|open|another|second)[a-z ]{0,24} claude([^a-z0-9_]|$)|claude +(pane|co-?worker|worker|instance|cli))`)
)

// DriveNudge is the line the prompt hook adds when a prompt names Codex, or
// a Claude Code co-worker: skills are picked by their description, which can
// miss, and the co-worker must go through the drive skill. Empty otherwise.
func DriveNudge(prompt string) string {
	who := ""
	switch {
	case nudgeCodex.MatchString(prompt):
		who = "Codex"
	case nudgeClaude.MatchString(prompt):
		who = "a Claude Code co-worker"
	default:
		return ""
	}
	return " [sunstack] This prompt names " + who + ". If it asks " + who + " to do something, load the Sunstack drive skill first: the co-worker runs in a tmux pane beside this one (sunstack spawn --free, sunstack drive), never headless. Skip this when the skill is already loaded, or " + who + " is only the topic."
}

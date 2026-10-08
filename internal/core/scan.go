package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The session scanner (org design, v0.8): every Claude Code and Codex session
// running on this host, found from the processes themselves.
//
//   - Claude Code: `claude agents --json`, its documented session list.
//   - Codex: no list exists, so each codex process with a terminal is asked
//     which transcript (rollout) file it holds open; that names its thread,
//     and the process's working directory names its folder.
//
// The tmux pane of either comes from matching the process's terminal against
// the panes of the default tmux server and the caller's.

// HostSession is one running CLI session on this host.
type HostSession struct {
	Tool       string   `json:"tool"`                 // claude or codex
	Kind       string   `json:"kind,omitempty"`       // interactive or background (Claude)
	PID        int      `json:"pid,omitempty"`        // 0 for a Claude background session
	SessionID  string   `json:"session_id,omitempty"` // Claude session ID or Codex thread ID
	Name       string   `json:"name,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Status     string   `json:"status"`            // idle, busy, waiting, blocked, unknown
	Reach      string   `json:"reach"`             // nudge (a tmux pane: woken at once), next prompt (no pane), background
	Pending    int      `json:"pending,omitempty"` // messages in its host inbox (sessions without an agent)
	TTY        string   `json:"tty,omitempty"`
	Pane       string   `json:"pane,omitempty"`
	Where      string   `json:"where,omitempty"` // tmux session:window.pane
	socket     string   // tmux server the pane was found on
	Transcript string   `json:"-"`
	Activity   Activity `json:"activity"`

	// Classification
	Team      string `json:"team,omitempty"` // team ID, or the root when the team has no TEAM file
	TeamName  string `json:"team_name,omitempty"`
	Agent     string `json:"agent,omitempty"` // the agent it works as, if it holds a claim
	Label     string `json:"label,omitempty"` // <id>_<task>
	Doing     string `json:"doing,omitempty"`
	DoingAt   string `json:"doing_at,omitempty"`
	TeamRoot  string `json:"team_root,omitempty"`
	Group     string `json:"group,omitempty"` // free sessions outside a team: git remote, else folder
	SameAgent int    `json:"-"`
}

// Activity is what a session did last, read from its transcript.
type Activity struct {
	Title      string `json:"title,omitempty"`
	LastPrompt string `json:"last_prompt,omitempty"`
	LastReply  string `json:"last_reply,omitempty"`
	LastActive string `json:"last_active,omitempty"`
}

// ScanSessions lists this host's sessions. Notes say which part of the scan
// could not run; a failed check never empties the list silently.
func ScanSessions() ([]*HostSession, []string) {
	var out []*HostSession
	var notes []string
	panes := tmuxPanes()
	cl, err := claudeSessions()
	if err != nil {
		notes = append(notes, "Claude Code sessions not listed: "+err.Error())
	}
	out = append(out, cl...)
	cx, err := codexSessions()
	if err != nil {
		notes = append(notes, "Codex sessions not listed: "+err.Error())
	}
	out = append(out, cx...)
	for _, s := range out {
		if s.TTY == "" && s.PID > 0 {
			s.TTY = processTTY(s.PID)
		}
		if p, ok := panes[s.TTY]; ok && s.TTY != "" {
			s.Pane, s.Where, s.socket = p.id, p.where, p.socket
			if p.label != "" {
				// A free session started by spawn --free (§20.2).
				s.Name = p.label
			}
		}
		switch {
		case s.Kind == "background":
			s.Reach = "background"
		case s.Pane != "":
			s.Reach = "nudge"
		default:
			s.Reach = "next prompt"
		}
		s.Pending = HostPending(s.SessionID)
		if s.Tool == "codex" && s.Pane != "" && s.Status != "busy" {
			if screen := paneScreen(s.socket, s.Pane); screen != "" && !ReadyForInput("codex", screen) && hasDialog(screen) {
				s.Status = "waiting"
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Cwd != out[j].Cwd {
			return out[i].Cwd < out[j].Cwd
		}
		return out[i].PID < out[j].PID
	})
	return out, notes
}

func hasDialog(screen string) bool {
	low := strings.ToLower(strings.Join(screenTail(screen, 15), "\n"))
	for _, m := range dialogMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// claudeSessions reads `claude agents --json`. SUNSTACK_CLAUDE_AGENTS names a
// file with the same JSON instead (tests).
func claudeSessions() ([]*HostSession, error) {
	var data []byte
	if f := os.Getenv("SUNSTACK_CLAUDE_AGENTS"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		data = b
	} else {
		if _, err := exec.LookPath("claude"); err != nil {
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, "claude", "agents", "--json").Output()
		if err != nil {
			return nil, err
		}
		data = b
	}
	var list []struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		State     string `json:"state"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	var out []*HostSession
	for _, e := range list {
		s := &HostSession{Tool: "claude", Kind: e.Kind, PID: e.PID, SessionID: e.SessionID, Name: e.Name, Cwd: e.Cwd, Status: e.Status}
		if s.Status == "" {
			s.Status = e.State // background sessions report a state instead
		}
		if s.Status == "" {
			s.Status = "unknown"
		}
		if s.SessionID != "" {
			home, _ := os.UserHomeDir()
			if m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", s.SessionID+".jsonl")); len(m) > 0 {
				s.Transcript = m[0]
				s.Activity = claudeActivity(s.Transcript)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// codexSessions finds codex processes with a terminal and the thread each
// holds open.
func codexSessions() ([]*HostSession, error) {
	var failed []string
	if runtime.GOOS == "windows" || os.Getenv("SUNSTACK_CODEX_SCAN") == "off" {
		return nil, nil
	}
	out, err := exec.Command("ps", "-axo", "pid=,tty=,comm=").Output()
	if err != nil {
		return nil, err
	}
	var list []*HostSession
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || filepath.Base(strings.Join(f[2:], " ")) != "codex" || f[1] == "??" || f[1] == "?" {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		files, cwd, ferr := openFiles(pid)
		rollout := mainRollout(files)
		if ferr != nil && rollout == "" {
			// The check failed before it found the transcript: that is not
			// the same as "no session here".
			if processAlive(pid) {
				failed = append(failed, strconv.Itoa(pid))
			}
			continue
		}
		if rollout == "" {
			continue // a helper process, not a session
		}
		s := &HostSession{Tool: "codex", Kind: "interactive", PID: pid, TTY: strings.TrimPrefix(f[1], "/dev/"), Cwd: cwd, Transcript: rollout}
		s.SessionID, s.Status, s.Activity = codexRollout(rollout)
		if s.Cwd == "" {
			s.Cwd = rolloutCwd(rollout)
		}
		list = append(list, s)
	}
	if len(failed) > 0 {
		return list, fmt.Errorf("could not inspect codex process(es) %s", strings.Join(failed, ", "))
	}
	return list, nil
}

// openFiles lists the files a process holds open and its working directory.
func openFiles(pid int) ([]string, string, error) {
	if runtime.GOOS == "linux" {
		cwd, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		if err != nil {
			return nil, "", err
		}
		fds, err := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/fd/*")
		if err != nil {
			return nil, "", err
		}
		var files []string
		var ferr error
		for _, fd := range fds {
			if t, err := os.Readlink(fd); err == nil {
				files = append(files, t)
			} else if !os.IsNotExist(err) {
				ferr = err
			}
		}
		return files, cwd, ferr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-p", strconv.Itoa(pid), "-Fftn").Output()
	var lerr error
	if err != nil {
		lerr = fmt.Errorf("lsof: %v", err) // the caller decides with what was listed
	}
	var files []string
	cwd, fd := "", ""
	for _, l := range strings.Split(string(out), "\n") {
		if len(l) < 2 {
			continue
		}
		switch l[0] {
		case 'f':
			fd = l[1:]
		case 'n':
			if fd == "cwd" {
				cwd = l[1:]
			} else {
				files = append(files, l[1:])
			}
		}
	}
	return files, cwd, lerr
}

// processAlive reports whether pid still exists, so a process that exited
// during the scan is not reported as a failed check.
func processAlive(pid int) bool {
	return exec.Command("ps", "-p", strconv.Itoa(pid)).Run() == nil
}

// mainRollout picks the session's own rollout among the open files, skipping
// subagent threads (for example the approvals reviewer).
func mainRollout(files []string) string {
	var cands []string
	for _, f := range files {
		if strings.HasSuffix(f, ".jsonl") && strings.HasPrefix(filepath.Base(f), "rollout-") {
			cands = append(cands, f)
		}
	}
	for _, f := range cands {
		if src, ok := rolloutSource(f); ok && src {
			return f
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

// rolloutSource reads the session_meta line: ok, and whether the thread is a
// top-level session rather than a subagent.
func rolloutSource(path string) (bool, bool) {
	line := firstLine(path)
	var m struct {
		Type    string `json:"type"`
		Payload struct {
			Source json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "session_meta" {
		return false, false
	}
	return true, len(m.Payload.Source) == 0 || m.Payload.Source[0] == '"'
}

func rolloutCwd(path string) string {
	var m struct {
		Payload struct {
			Cwd string `json:"cwd"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(firstLine(path), &m)
	return m.Payload.Cwd
}

func firstLine(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	l, _ := r.ReadBytes('\n')
	return l
}

// tail returns up to the last n bytes of a file, from a line start.
func tail(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := st.Size() - n
	if off < 0 {
		off = 0
	}
	b := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil
	}
	if off > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b
}

func modTime(path string) string {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime().UTC().Format("2006-01-02T15:04:05Z")
	}
	return ""
}

// codexRollout reads a rollout's tail: the thread ID, busy or idle (a turn
// started and not yet complete), and the last prompt and reply.
func codexRollout(path string) (id, status string, a Activity) {
	var meta struct {
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(firstLine(path), &meta)
	id = meta.Payload.ID
	status = "idle"
	for _, l := range bytes.Split(tail(path, 2<<20), []byte("\n")) {
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(l, &e) != nil {
			continue
		}
		if e.Type == "response_item" && e.Payload.Type == "message" {
			// Newer Codex builds record prompts and replies only here.
			for _, c := range e.Payload.Content {
				t := oneLine(c.Text)
				switch {
				case t == "":
				case e.Payload.Role == "user" && c.Type == "input_text" && !strings.HasPrefix(t, "<"):
					a.LastPrompt = t
				case e.Payload.Role == "assistant" && c.Type == "output_text":
					a.LastReply = t
				}
			}
			continue
		}
		if e.Type != "event_msg" {
			continue
		}
		switch e.Payload.Type {
		case "task_started":
			status = "busy"
		case "task_complete", "turn_aborted":
			status = "idle"
		case "user_message":
			if t := oneLine(e.Payload.Message); t != "" && !strings.HasPrefix(t, "<") {
				a.LastPrompt = t
			}
		case "agent_message":
			if t := oneLine(e.Payload.Message); t != "" {
				a.LastReply = t
			}
		}
	}
	a.LastActive = modTime(path)
	return id, status, a
}

// claudeActivity reads a Claude Code transcript's tail: its title, the last
// prompt the user typed and the first line of the last text reply.
func claudeActivity(path string) Activity {
	var a Activity
	for _, l := range bytes.Split(tail(path, 512<<10), []byte("\n")) {
		var e struct {
			Type       string `json:"type"`
			AITitle    string `json:"aiTitle"`
			LastPrompt string `json:"lastPrompt"`
			Message    struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(l, &e) != nil {
			continue
		}
		switch e.Type {
		case "ai-title":
			a.Title = oneLine(e.AITitle)
		case "last-prompt":
			if t := oneLine(e.LastPrompt); t != "" {
				a.LastPrompt = t
			}
		case "user":
			var s string
			if json.Unmarshal(e.Message.Content, &s) == nil {
				if t := oneLine(s); t != "" && !strings.HasPrefix(t, "<") {
					a.LastPrompt = t
				}
			}
		case "assistant":
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(e.Message.Content, &parts) == nil {
				for _, p := range parts {
					if p.Type == "text" {
						if t := oneLine(p.Text); t != "" {
							a.LastReply = t
						}
					}
				}
			}
		}
	}
	a.LastActive = modTime(path)
	return a
}

// oneLine is the first non-empty line of s, at most 160 characters.
func oneLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > 160 {
				return string(r[:159]) + "…"
			}
			return l
		}
	}
	return ""
}

type paneInfo struct{ id, where, socket, label string }

// tmuxPanes maps terminal names (ttys001, pts/3) to panes, on the default
// tmux server and the caller's.
func tmuxPanes() map[string]paneInfo {
	out := map[string]paneInfo{}
	if _, err := exec.LookPath("tmux"); err != nil {
		return out
	}
	sockets := []string{""}
	if sock, override := DefaultSocket(); override {
		// Tests name their stand-in default server (§20.1).
		sockets[0] = sock
	}
	if t := os.Getenv("TMUX"); t != "" {
		sockets = append(sockets, strings.SplitN(t, ",", 2)[0])
	}
	for _, sock := range sockets {
		cmd := exec.Command("tmux", TmuxArgs(sock, "list-panes", "-a", "-F", "#{pane_tty}\t#{pane_id}\t#{session_name}:#{window_index}.#{pane_index}\t#{@sunstack_label}")...)
		if sock == "" {
			// The default server: inside another server, $TMUX would
			// point tmux there instead.
			cmd.Env = withoutTMUX(os.Environ())
		}
		b, err := cmd.Output()
		if err != nil {
			continue
		}
		if sock == "" {
			// Name the default server by its path, so later commands reach it
			// even from inside another server.
			q := exec.Command("tmux", "-u", "list-sessions", "-F", "#{socket_path}")
			q.Env = withoutTMUX(os.Environ())
			if out, err := q.Output(); err == nil {
				if path := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]; path != "" {
					sock = path
				}
			}
		}
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.SplitN(l, "\t", 4)
			if len(f) == 4 {
				tty := strings.TrimPrefix(f[0], "/dev/")
				if _, seen := out[tty]; !seen {
					out[tty] = paneInfo{f[1], f[2], sock, f[3]}
				}
			}
		}
	}
	return out
}

func processTTY(pid int) string {
	b, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	t := strings.TrimPrefix(strings.TrimSpace(string(b)), "/dev/")
	if t == "??" || t == "?" {
		return ""
	}
	return t
}

func withoutTMUX(env []string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, "TMUX=") && !strings.HasPrefix(e, "TMUX_PANE=") {
			out = append(out, e)
		}
	}
	return out
}

// Socket is the tmux server the session's pane was found on.
func (s *HostSession) Socket() string { return s.socket }

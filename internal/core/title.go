package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

// SessionTitle reads the title the user sees for a Claude Code or Codex
// session (§38): Claude's last /rename record in the transcript, Codex's last
// thread name in its session index. known is false when the file is missing,
// so the title cannot be told; "" with known true means never named.
func SessionTitle(tool, sid string) (title string, known bool) {
	if !IsSessionID(sid) {
		return "", false
	}
	home, _ := os.UserHomeDir()
	var path string
	var mark []byte
	switch tool {
	case "claude":
		m, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sid+".jsonl"))
		if len(m) == 0 {
			return "", false
		}
		path, mark = m[0], []byte(`"type":"custom-title"`)
	case "codex":
		path, mark = filepath.Join(home, ".codex", "session_index.jsonl"), []byte(`"id":"`+sid+`"`)
	default:
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	// Lines are read whole, with no length cap: a transcript line can be
	// many megabytes. Only lines holding the record's key unescaped are
	// parsed (inside a quoted tool result its quotes are escaped), and only
	// a top-level record counts.
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, mark) {
			var e struct {
				Type       string `json:"type"`
				ID         string `json:"id"`
				Session    string `json:"sessionId"`
				Custom     string `json:"customTitle"`
				ThreadName string `json:"thread_name"`
			}
			if json.Unmarshal(line, &e) == nil {
				if tool == "claude" && e.Type == "custom-title" && (e.Session == "" || e.Session == sid) {
					title = e.Custom
				} else if tool == "codex" && e.ID == sid {
					title = e.ThreadName
				}
			}
		}
		if err != nil {
			return title, true
		}
	}
}

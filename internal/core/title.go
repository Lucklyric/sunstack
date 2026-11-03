package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
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
	// A title record is a short line; any line longer than the buffer is
	// skipped a piece at a time, so memory stays bounded however large a
	// transcript line is. Only lines holding the record's key unescaped are
	// parsed (inside a quoted tool result its quotes are escaped), and only
	// a top-level record counts.
	r := bufio.NewReaderSize(f, 1<<20)
	found, long := false, false
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = true
			continue
		}
		if !long && bytes.Contains(line, mark) {
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
					title, found = e.ThreadName, true
				}
			}
		}
		long = false
		if err == io.EOF {
			// Claude: a transcript with no record was never renamed. Codex
			// indexes a thread by itself, so no entry means not known yet.
			return title, tool == "claude" || found
		}
		if err != nil {
			return "", false
		}
	}
}

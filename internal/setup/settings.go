package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeSettingsPath is the user-level Claude Code settings file.
func ClaudeSettingsPath() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// field is one key of a JSON object, kept in file order.
type field struct {
	Key string
	Val json.RawMessage
}

// parseObject decodes a JSON object without losing key order.
func parseObject(b []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var fs []field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		fs = append(fs, field{t.(string), v})
	}
	return fs, nil
}

func encodeObject(fs []field) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range fs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(f.Val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func get(fs []field, key string) (json.RawMessage, int) {
	for i, f := range fs {
		if f.Key == key {
			return f.Val, i
		}
	}
	return nil, -1
}

func set(fs []field, key string, v json.RawMessage) []field {
	if _, i := get(fs, key); i >= 0 {
		fs[i].Val = v
		return fs
	}
	return append(fs, field{key, v})
}

// editAllow adds (add=true) or removes rule in permissions.allow, keeping the
// rest of the file and its key order. It reports whether anything changed.
func editAllow(src []byte, rule string, add bool) ([]byte, bool, error) {
	if len(bytes.TrimSpace(src)) == 0 {
		src = []byte("{}")
	}
	top, err := parseObject(src)
	if err != nil {
		return nil, false, err
	}
	permRaw, _ := get(top, "permissions")
	if permRaw == nil {
		permRaw = json.RawMessage("{}")
	}
	perm, err := parseObject(permRaw)
	if err != nil {
		return nil, false, fmt.Errorf("permissions: %w", err)
	}
	var allow []string
	if raw, _ := get(perm, "allow"); raw != nil {
		if err := json.Unmarshal(raw, &allow); err != nil {
			return nil, false, fmt.Errorf("permissions.allow: %w", err)
		}
	}
	has := false
	var kept []string
	for _, r := range allow {
		if r == rule {
			has = true
			if !add {
				continue
			}
		}
		kept = append(kept, r)
	}
	if has == add {
		return src, false, nil
	}
	if add {
		kept = append(kept, rule)
	}
	if kept == nil {
		kept = []string{}
	}
	allowRaw, _ := json.Marshal(kept)
	perm = set(perm, "allow", allowRaw)
	top = set(top, "permissions", encodeObject(perm))
	var out bytes.Buffer
	if err := json.Indent(&out, encodeObject(top), "", "  "); err != nil {
		return nil, false, err
	}
	out.WriteByte('\n')
	return out.Bytes(), true, nil
}

// HasAllowRule reports whether the Claude Code settings allow sunstack.
func HasAllowRule() bool {
	b, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		return false
	}
	_, changed, err := editAllow(b, AllowRule, true)
	return err == nil && !changed
}

// SetAllowRule adds or removes the rule, keeping a .bak copy of the old file.
func SetAllowRule(add bool) (bool, error) {
	path := ClaudeSettingsPath()
	src, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	out, changed, err := editAllow(src, AllowRule, add)
	if err != nil || !changed {
		return false, err
	}
	if len(src) > 0 {
		if err := os.WriteFile(path+".bak", src, 0o600); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, out, 0o600)
}

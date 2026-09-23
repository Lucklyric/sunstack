package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// editRule adds (add=true) or removes rule in permissions.<list> ("allow" or
// "ask"), keeping the rest of the file and its key order. It reports whether
// anything changed.
func editRule(src []byte, list, rule string, add bool) ([]byte, bool, error) {
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
	if raw, _ := get(perm, list); raw != nil {
		if err := json.Unmarshal(raw, &allow); err != nil {
			return nil, false, fmt.Errorf("permissions.%s: %w", list, err)
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
	perm = set(perm, list, allowRaw)
	top = set(top, "permissions", encodeObject(perm))
	var out bytes.Buffer
	if err := json.Indent(&out, encodeObject(top), "", "  "); err != nil {
		return nil, false, err
	}
	out.WriteByte('\n')
	return out.Bytes(), true, nil
}

// claudeRules are the two Claude Code permission rules Sunstack manages:
// run sunstack without asking, but always ask before amend (design §6, §10).
var claudeRules = func() [][2]string {
	r := [][2]string{{"allow", AllowRule}}
	for _, a := range AskRules {
		r = append(r, [2]string{"ask", a})
	}
	return r
}()

// HasClaudeRules reports whether the Claude Code settings hold both rules.
func HasClaudeRules() bool {
	b, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		return false
	}
	for _, r := range claudeRules {
		if _, changed, err := editRule(b, r[0], r[1], true); err != nil || changed {
			return false
		}
	}
	return true
}

// SetClaudeRules adds or removes both rules, keeping a .bak copy of the old file.
func SetClaudeRules(add bool) (bool, error) {
	path := ClaudeSettingsPath()
	src, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	out, changedAny := src, false
	for _, r := range claudeRules {
		var changed bool
		if out, changed, err = editRule(out, r[0], r[1], add); err != nil {
			return false, err
		}
		changedAny = changedAny || changed
	}
	if !changedAny {
		return false, nil
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

// codexRules makes Codex ask before every sunstack amend.
const codexRules = `# Managed by sunstack install. Codex asks before any change to an agent's
# pillars or AGENT.md, and before an agent is created or deleted.
prefix_rule(
    pattern = ["sunstack", "amend"],
    decision = "prompt",
    justification = "Sunstack amend changes an agent's pillars or AGENT.md; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "hire"],
    decision = "prompt",
    justification = "Sunstack hire creates an agent and its prompt; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "fire"],
    decision = "prompt",
    justification = "Sunstack fire deletes an agent; the user must approve it.",
)
`

// CodexRulesPath is where the Codex exec-policy rule lives.
func CodexRulesPath() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, "rules", "sunstack.rules")
}

// SetCodexRules writes or removes the Codex rule file.
func SetCodexRules(add bool) error {
	path := CodexRulesPath()
	if !add {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(codexRules), 0o644)
}

// CodexAutoReviewsApprovals reports whether Codex sends approval prompts to an
// automatic reviewer instead of the user, which makes the amend rule advisory.
func CodexAutoReviewsApprovals() bool {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(CodexRulesPath())), "config.toml"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "approvals_reviewer" && !strings.Contains(v, "user") {
			return true
		}
	}
	return false
}

package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// parseObject decodes one complete JSON object without losing key order. It
// rejects anything else: truncated input, trailing content, comments, and
// duplicate keys, so a file is never rewritten from a partial reading.
func parseObject(b []byte) ([]field, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var fs []field
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, errors.New("invalid object key")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = true
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		fs = append(fs, field{key, v})
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("unterminated JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected content after the JSON object")
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

// HasAllowRule reports whether the user already allowed sunstack, which is
// the consent update relies on to add newer ask rules.
func HasAllowRule() bool {
	b, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		return false
	}
	_, changed, err := editRule(b, "allow", AllowRule, true)
	return err == nil && !changed
}

// SetClaudeRules adds or removes the rules, keeping a .bak copy of the old
// file. The new file is written beside the old one and renamed over it, and
// only if nothing else changed the file in the meantime.
func SetClaudeRules(add bool) (bool, error) {
	path := ClaudeSettingsPath()
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("%s is a symlink; edit it by hand", path)
	}
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
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return true, replaceIfUnchanged(path, src, out, mode)
}

// replaceIfUnchanged atomically replaces path with data, unless its content no
// longer equals want (someone edited it since we read it).
func replaceIfUnchanged(path string, want, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".sunstack.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	cur, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(cur, want) {
		return fmt.Errorf("%s changed while sunstack was editing it; run the command again", path)
	}
	return os.Rename(name, path)
}

// codexRules makes Codex ask before every sunstack amend.
const codexRules = `# Managed by sunstack install. Codex asks before any change to an agent's
# pillars, AGENT.md or the team objectives, before an agent is created,
# renamed or deleted, before a session is closed, and before a directive is
# added in the user's name.
prefix_rule(
    pattern = ["sunstack", "amend"],
    decision = "prompt",
    justification = "Sunstack amend changes pillars, an AGENT.md or the team objectives; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "hire"],
    decision = "prompt",
    justification = "Sunstack hire creates an agent and its prompt; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "rename"],
    decision = "prompt",
    justification = "Sunstack rename changes an agent's ID; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "fire"],
    decision = "prompt",
    justification = "Sunstack fire deletes an agent; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "kill"],
    decision = "prompt",
    justification = "Sunstack kill closes a session's pane; the user must approve it.",
)
prefix_rule(
    pattern = ["sunstack", "direct"],
    decision = "prompt",
    justification = "Sunstack direct adds a directive in the user's name; only the user may do that.",
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

const codexRulesHeader = "# Managed by sunstack install."

// CodexRulesState says whether the Codex rule file is missing, ours and
// current, ours but outdated, or someone else's.
func CodexRulesState() string {
	path := CodexRulesPath()
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil || st.Mode()&os.ModeSymlink != 0 {
		return "foreign"
	}
	b, err := os.ReadFile(path)
	switch {
	case err != nil || !strings.HasPrefix(string(b), codexRulesHeader):
		return "foreign"
	case string(b) == codexRules:
		return "current"
	}
	return "outdated"
}

// SetCodexRules writes or removes the Codex rule file. It only ever replaces
// or deletes a file it wrote itself.
func SetCodexRules(add bool) error {
	path := CodexRulesPath()
	state := CodexRulesState()
	if state == "foreign" {
		return fmt.Errorf("%s exists and was not written by sunstack; merge the sunstack rules by hand", path)
	}
	if !add {
		if state == "missing" {
			return nil
		}
		return os.Remove(path)
	}
	if state == "current" {
		return nil
	}
	cur, _ := os.ReadFile(path)
	return replaceIfUnchanged(path, cur, []byte(codexRules), 0o644)
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

package core

// v0.17.3 (§35): an unedited PROTOCOL.md refreshes itself.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/assets"
)

func hash173(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestProtocolHistoryHasCurrent(t *testing.T) {
	cur := hash173(assets.Protocol())
	for _, h := range assets.ProtocolHistory() {
		if h == cur {
			return
		}
	}
	t.Fatal("protocol-history.txt lacks this version's PROTOCOL.md: run scripts/protocol-history.sh")
}

func TestProtocolAutoRefresh(t *testing.T) {
	old := []byte("# Sunstack protocol, an older built-in version\n\nRule one.\n")
	defer func(h []string) { protocolHistory = h }(protocolHistory)
	protocolHistory = append(append([]string{}, protocolHistory...), hash173(old))
	for name, body := range map[string][]byte{
		"older":      old,
		"older CRLF": bytes.ReplaceAll(old, []byte("\n"), []byte("\r\n")),
		"edited":     append(append([]byte{}, old...), "A team's own line.\n"...),
	} {
		t.Run(name, func(t *testing.T) {
			p := secTeam(t)
			path := filepath.Join(p.Dir, "PROTOCOL.md")
			must172(t, os.WriteFile(path, body, 0o644))
			var step *MigrateStep
			for _, s := range p.MigratePlan() {
				if strings.Contains(s.What, "PROTOCOL.md") {
					s := s
					step = &s
				}
			}
			if step == nil {
				t.Fatal("no PROTOCOL step")
			}
			_, err := p.Migrate(false)
			must172(t, err)
			got, _ := os.ReadFile(path)
			if name == "edited" {
				if step.Safe || !bytes.Equal(got, body) {
					t.Errorf("an edited PROTOCOL.md was treated as safe (safe=%v) or rewritten", step.Safe)
				}
				return
			}
			if !step.Safe || !bytes.Equal(got, assets.Protocol()) {
				t.Errorf("an unedited older PROTOCOL.md: safe=%v, refreshed=%v", step.Safe, bytes.Equal(got, assets.Protocol()))
			}
		})
	}
}

func TestHealthProtocolCases(t *testing.T) {
	old := []byte("# an older built-in version\n")
	defer func(h []string) { protocolHistory = h }(protocolHistory)
	protocolHistory = append(append([]string{}, protocolHistory...), hash173(old))
	p := secTeam(t)
	path := filepath.Join(p.Dir, "PROTOCOL.md")
	msg := func() string {
		for _, c := range p.Health() {
			if strings.Contains(c.Msg, "PROTOCOL.md") {
				return c.Msg
			}
		}
		return ""
	}
	must172(t, os.WriteFile(path, old, 0o644))
	if m := msg(); !strings.Contains(m, "unedited older version") {
		t.Errorf("unedited case: %q", m)
	}
	must172(t, os.WriteFile(path, []byte("# edited\n"), 0o644))
	if m := msg(); !strings.Contains(m, "edited") || strings.Contains(m, "unedited") {
		t.Errorf("edited case: %q", m)
	}
}

// §35 review: a symlinked PROTOCOL.md is the team's own choice: never safe.
func TestProtocolSymlinkAsks(t *testing.T) {
	old := []byte("# an older built-in version\n")
	defer func(h []string) { protocolHistory = h }(protocolHistory)
	protocolHistory = append(append([]string{}, protocolHistory...), hash173(old))
	p := secTeam(t)
	shared := filepath.Join(p.Root, "shared-protocol.md")
	must172(t, os.WriteFile(shared, old, 0o644))
	link := filepath.Join(p.Dir, "PROTOCOL.md")
	os.Remove(link)
	if err := os.Symlink(shared, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	for _, s := range p.MigratePlan() {
		if strings.Contains(s.What, "PROTOCOL.md") && s.Safe {
			t.Error("a symlinked PROTOCOL.md was planned as safe")
		}
	}
	_, err := p.Migrate(false)
	must172(t, err)
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("safe migration replaced the team's PROTOCOL.md symlink")
	}
}

// §35 review: old hashes are never dropped (a shallow clone would).
func TestProtocolHistoryKeepsOldest(t *testing.T) {
	h := assets.ProtocolHistory()
	if len(h) < 17 || h[0] != "efe58d4602e67e3df04ef71f3820cc7387aff554333f3fcfb78ae6be34e6ae03" {
		t.Fatalf("protocol-history.txt lost old hashes: %d entries, first %q", len(h), h[0])
	}
}

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

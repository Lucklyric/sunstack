package main

import (
	"strings"
	"testing"

	"github.com/Lucklyric/sunstack/internal/core"
)

// A message from another host or agent cannot drive the reader's terminal.
func TestPrintMessagesControls(t *testing.T) {
	var b strings.Builder
	printMessages(&b, []*core.Message{{ID: "m", From: "x\x1b[2J", Type: "fyi", Body: "a\x1b]52;c;ZXZpbA==\x07b\n\tc"}})
	if strings.ContainsAny(b.String(), "\x1b\x07") {
		t.Errorf("printed: %q", b.String())
	}
	if !strings.Contains(b.String(), "\tc") {
		t.Errorf("tabs and lines must stay: %q", b.String())
	}
}

//go:build !windows

package core

import "testing"

func TestOwnerDead(t *testing.T) {
	if ownerDead([]byte("2026-10-01T00:00:00Z pid 1 start Mon_Jan_1_00:00:00_1990\n")) != true {
		t.Error("a live pid with another start time is a reused pid")
	}
	if ownerDead([]byte("2026-10-01T00:00:00Z pid 1\n")) {
		t.Error("an old-style owner line with a live pid is not dead")
	}
}

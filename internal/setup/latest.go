package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// The newest release, checked at most once a day and cached (§20.6), so
// views can say when a host is behind without reaching the network.

type latestCache struct {
	Version string `json:"version"`
	Checked string `json:"checked"`
}

func latestPath() string { return filepath.Join(core.Home(), "latest.json") }

// CachedLatest is the newest release seen by the last check, or "".
func CachedLatest() string {
	var c latestCache
	if b, err := os.ReadFile(latestPath()); err == nil && json.Unmarshal(b, &c) == nil {
		return c.Version
	}
	return ""
}

// RefreshLatest checks GitHub once a day; offline, or with
// SUNSTACK_NO_UPDATE_CHECK set, it does nothing.
func RefreshLatest() {
	if os.Getenv("SUNSTACK_NO_UPDATE_CHECK") != "" {
		return
	}
	var c latestCache
	if b, err := os.ReadFile(latestPath()); err == nil && json.Unmarshal(b, &c) == nil {
		if t, err := time.Parse(time.RFC3339, c.Checked); err == nil && time.Since(t) < 24*time.Hour {
			return
		}
	}
	v, err := LatestVersion()
	if err != nil || v == "" {
		return
	}
	b, _ := json.Marshal(latestCache{Version: v, Checked: time.Now().UTC().Format(time.RFC3339)})
	_ = os.MkdirAll(filepath.Dir(latestPath()), 0o700)
	_ = os.WriteFile(latestPath(), b, 0o600)
}

// Older reports whether version a is older than b ("0.9.4" < "0.10.0"). A
// version that is not numbers (a development build) is never older.
func Older(a, b string) bool {
	pa, oka := parts(a)
	pb, okb := parts(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parts(v string) ([3]int, bool) {
	var p [3]int
	f := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(f) != 3 {
		return p, false
	}
	for i, s := range f {
		n, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p[i] = n
	}
	return p, true
}

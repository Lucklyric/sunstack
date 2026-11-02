//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// §34.10: the pane CLI's PID and start time parse the same in any zone and locale.
func TestReview172bProcessStartFormats(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TZ", "America/Edmonton")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	must172(t, os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nprintf '/dev/pts/7\\n'\n"), 0700))
	for _, line := range []string{"S+ 4242 Mon Nov  2 00:01:28 2026 claude", "S+  4242 Mon Nov  2 00:01:28 2026 /fixture/tools/claude"} {
		script := "#!/bin/sh\n[ \"$TZ\" = UTC ] && [ \"$LC_ALL\" = C ] || exit 1\nprintf '%s\\n' '" + line + "'\n"
		must172(t, os.WriteFile(filepath.Join(bin, "ps"), []byte(script), 0700))
		pid, got, ok := paneToolProc("/fixture/sunstack-test-format", "%0", "claude")
		want := time.Date(2026, 11, 2, 0, 1, 28, 0, time.UTC)
		t.Logf("ps=%q parsed=%v known=%v", line, got, ok)
		if !ok || pid != 4242 || !got.Equal(want) {
			t.Fatal("portable process start parse failed")
		}
	}
}

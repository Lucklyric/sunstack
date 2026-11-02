//go:build !windows

package main

// §36 review: a stalled tmux never holds up the prompt hook.

import (
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestPromptHookStalledTmux(t *testing.T) {
	p, env := team086(t)
	s := privateTmux(t, p, append(append([]string{}, env...), "SUNSTACK_HOME="+envValue(env, "SUNSTACK_HOME")))
	pid, err := strconv.Atoi(s.pid)
	must(t, err)
	must(t, syscall.Kill(pid, syscall.SIGSTOP))
	defer syscall.Kill(pid, syscall.SIGCONT)
	hookEnv := append(append([]string{}, s.env...), "TMUX_PANE="+s.pane)
	start := time.Now()
	out := runHook(t, hookEnv, "UserPromptSubmit", "", "00000000-0000-4000-8000-000000000362", p)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the hook took %s with tmux stalled", d)
	}
	requireContains(t, hookContextText(t, out, "UserPromptSubmit"), "[sunstack] this session holds no agent in ")
}

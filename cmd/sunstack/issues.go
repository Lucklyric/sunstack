package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/core"
)

// issues prints this host's issue log grouped by code (§26).
func issues(a *args, out io.Writer) error {
	since := time.Now().Add(-7 * 24 * time.Hour)
	if v := a.flags["since"]; v != "" {
		if d, ok := strings.CutSuffix(v, "d"); ok {
			var n int
			if _, err := fmt.Sscanf(d, "%d", &n); err != nil || n < 0 {
				return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--since takes a number of days (7d) or a date (2026-10-20)"}
			}
			since = time.Now().Add(-time.Duration(n) * 24 * time.Hour)
		} else if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			since = t
		} else {
			return &core.Error{Code: core.ExitUsage, Reason: "usage", Msg: "--since takes a number of days (7d) or a date (2026-10-20)"}
		}
	}
	var list []core.IssueEntry
	for _, e := range core.ReadIssues(since) {
		if c := a.flags["code"]; c == "" || e.Code == c {
			list = append(list, e)
		}
	}
	if a.has("json") {
		if list == nil {
			list = []core.IssueEntry{}
		}
		b, _ := json.MarshalIndent(list, "", "  ")
		fmt.Fprintln(out, string(b))
		return nil
	}
	if len(list) == 0 {
		fmt.Fprintf(out, "No issues on this host since %s.\n", since.Format("2006-01-02"))
		return nil
	}
	type group struct {
		n    int
		last core.IssueEntry
		cmds map[string]bool
	}
	groups := map[string]*group{}
	counts := map[string]int{}
	for _, e := range list {
		g := groups[e.Code]
		if g == nil {
			g = &group{cmds: map[string]bool{}}
			groups[e.Code] = g
		}
		g.n += max(e.N, 1)
		g.last = e
		if e.Cmd != "" {
			g.cmds[e.Cmd] = true
		} else {
			g.cmds[e.Source] = true
		}
		counts[e.Code] = g.n
	}
	codes := sortedCodes(counts)
	sort.SliceStable(codes, func(i, j int) bool { return counts[codes[i]] > counts[codes[j]] })
	fmt.Fprintf(out, "Issues on this host since %s:\n", since.Format("2006-01-02"))
	for _, c := range codes {
		g := groups[c]
		times := "times"
		if g.n == 1 {
			times = "time"
		}
		var cmds []string
		for k := range g.cmds {
			cmds = append(cmds, k)
		}
		sort.Strings(cmds)
		fmt.Fprintf(out, "\n%s  %d %s, last %s (%s)\n", c, g.n, times, g.last.At, strings.Join(cmds, ", "))
		if g.last.Msg != "" {
			fmt.Fprintf(out, "  %s\n", g.last.Msg)
		}
	}
	return nil
}

func sortedCodes(m map[string]int) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

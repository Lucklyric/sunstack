package core

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucklyric/sunstack/internal/assets"
)

// Check is one line of `sunstack health`.
type Check struct {
	Level string // ok | warn | fail
	Area  string
	Msg   string
	Fix   string
}

func ok(area, msg string) Check          { return Check{"ok", area, msg, ""} }
func warn(area, msg, fix string) Check   { return Check{"warn", area, msg, fix} }
func failed(area, msg, fix string) Check { return Check{"fail", area, msg, fix} }

// Health runs the read-only project, file and runtime checks (design §6
// health). It never changes anything.
func (p *Project) Health() []Check {
	var cs []Check
	// Project.
	if b, err := os.ReadFile(filepath.Join(p.Dir, "PROTOCOL.md")); err != nil {
		cs = append(cs, failed("project", "sunstack/PROTOCOL.md is missing", "sunstack init"))
	} else if !bytes.Equal(b, assets.Protocol()) {
		cs = append(cs, warn("project", "sunstack/PROTOCOL.md differs from this sunstack version's", "sunstack init --refresh, then review the git diff"))
	} else {
		cs = append(cs, ok("project", "sunstack/ with a current PROTOCOL.md at "+p.Root))
	}
	agents, _, _ := readMaybe(filepath.Join(p.Root, "AGENTS.md"))
	nb := strings.Count(string(agents), assets.RouteBegin)
	updated, changed, err := routeBlock(agents)
	switch {
	case err != nil:
		cs = append(cs, failed("project", "AGENTS.md sunstack block is malformed", "fix AGENTS.md by hand, then sunstack init"))
	case nb == 0:
		cs = append(cs, failed("project", "AGENTS.md has no sunstack block, so agents are not routed to sunstack/", "sunstack init"))
	case changed && len(updated) > 0:
		cs = append(cs, warn("project", "AGENTS.md sunstack block is from an older version", "sunstack init"))
	default:
		cs = append(cs, ok("project", "AGENTS.md has one current sunstack block"))
	}
	gi, _, _ := readMaybe(filepath.Join(p.Root, ".gitignore"))
	if hasLine(gi, "sunstack/_local/") {
		cs = append(cs, ok("project", ".gitignore keeps sunstack/_local/ out of git"))
	} else {
		cs = append(cs, warn("project", ".gitignore does not list sunstack/_local/", "sunstack init"))
	}

	// Files.
	for _, id := range p.Agents() {
		agent, _ := os.ReadFile(filepath.Join(p.AgentDir(id), "AGENT.md"))
		if !ValidID(id) {
			cs = append(cs, failed("files", id+" is not a valid ID", "rename the folder to <title> or <title>.<name>"))
		}
		if m := titleRe.FindSubmatch(agent); !strings.HasPrefix(string(agent), "---") || m == nil || string(m[1]) != strings.SplitN(id, ".", 2)[0] {
			cs = append(cs, failed("files", id+"/AGENT.md frontmatter is missing or its title does not match the ID", "fix the frontmatter"))
		}
		for _, f := range []string{"AGENT.md", "pillars.md", "context.md"} {
			if fileHasConflict(filepath.Join(p.AgentDir(id), f)) {
				cs = append(cs, failed("files", id+"/"+f+" has merge conflict markers", "resolve the conflict"))
			}
		}
		ctx, _ := os.ReadFile(filepath.Join(p.AgentDir(id), "context.md"))
		if n := strings.Count(string(ctx), "\n"); n > 200 {
			cs = append(cs, warn("files", fmt.Sprintf("%s/context.md has %d lines", id, n), "compact it in a save as "+id))
		}
	}
	if fileHasConflict(filepath.Join(p.Dir, "PILLARS.md")) {
		cs = append(cs, failed("files", "PILLARS.md has merge conflict markers", "resolve the conflict"))
	}
	if u, _ := p.uncommitted("sunstack"); len(u) > 0 {
		cs = append(cs, warn("files", fmt.Sprintf("%d uncommitted change(s) under sunstack/", len(u)), "review and commit them"))
	}

	// Runtime.
	locks, _ := os.ReadDir(p.local("locks"))
	for _, l := range locks {
		info, err := l.Info()
		age := "unknown age"
		if err == nil {
			age = time.Since(info.ModTime()).Round(time.Second).String()
		}
		owner, _ := os.ReadFile(p.local("locks", l.Name(), "owner"))
		cs = append(cs, warn("runtime", fmt.Sprintf("lock %s held %s (%s)", l.Name(), age, trimNL(owner)),
			"if no sunstack command is running, remove "+p.local("locks", l.Name())))
	}
	for _, s := range p.Status() {
		if s.ClaimErr != nil {
			cs = append(cs, failed("runtime", s.ClaimErr.Error(), "inspect the file, then delete it to free "+s.ID))
		}
		if s.Claim != nil && s.Where == "pane closed" {
			cs = append(cs, warn("runtime", fmt.Sprintf("%s is claimed from pane %s, which is gone", s.ID, s.Claim.TmuxPane),
				"take it over with sunstack as "+s.ID+" --takeover, or release it"))
		}
	}
	inboxes, _ := os.ReadDir(p.local("inbox"))
	for _, d := range inboxes {
		if !p.HasAgent(d.Name()) {
			cs = append(cs, warn("runtime", "inbox for "+d.Name()+", which is not hired", "remove "+p.local("inbox", d.Name())))
		}
		entries, _ := os.ReadDir(p.local("inbox", d.Name()))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				cs = append(cs, warn("runtime", "unfinished message "+d.Name()+"/"+e.Name(), "remove it"))
			}
		}
	}
	if st, err := os.Stat(p.EventsPath()); err == nil && st.Size() > 5<<20 {
		cs = append(cs, warn("runtime", fmt.Sprintf("events.log is %d MB", st.Size()>>20), "archive or truncate it"))
	}
	return cs
}

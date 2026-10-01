package core

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/Lucklyric/sunstack/internal/assets"
)

// Migration (v0.8.1): bring a team made by an older version up to this one.
// Steps that only add something are safe and run on their own, from update
// and as. Steps that rewrite a committed file the agents read wait for the
// user's yes, which the as and checkup skills ask for.

// MigrateStep is one change a team needs.
type MigrateStep struct {
	What string `json:"what"`
	Safe bool   `json:"safe"` // applied without asking
}

// MigratePlan lists what this team needs.
func (p *Project) MigratePlan() []MigrateStep {
	var out []MigrateStep
	if _, ok := p.Team(); !ok {
		out = append(out, MigrateStep{"add sunstack/TEAM (the team's ID on every host)", true})
	}
	if !p.indexed() {
		out = append(out, MigrateStep{"add the team to this host's index", true})
	}
	if _, err := os.Stat(p.teamBoardPath()); err != nil {
		out = append(out, MigrateStep{"create sunstack/BOARD.md (team objectives and directives)", true})
	}
	if gi, _, _ := readMaybe(filepath.Join(p.Root, ".gitignore")); !hasLine(gi, "sunstack/_local/") {
		out = append(out, MigrateStep{"add sunstack/_local/ to .gitignore", true})
	}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "PROTOCOL.md")); err == nil && !bytes.Equal(b, assets.Protocol()) {
		out = append(out, MigrateStep{"refresh sunstack/PROTOCOL.md to this version (what every agent follows)", false})
	}
	if b, err := os.ReadFile(filepath.Join(p.Dir, "README.md")); err != nil || !bytes.Equal(b, assets.Readme()) {
		out = append(out, MigrateStep{"refresh sunstack/README.md to this version", false})
	}
	agents, _, _ := readMaybe(filepath.Join(p.Root, "AGENTS.md"))
	if _, changed, err := routeBlock(agents); err == nil && changed {
		out = append(out, MigrateStep{"add or refresh the sunstack block in AGENTS.md", false})
	}
	return out
}

func (p *Project) indexed() bool {
	for _, e := range readIndex() {
		if e.Root == p.Root {
			return true
		}
	}
	return false
}

// Migrate applies the safe steps, or every step with all. It returns what
// it did; files it changed are left for the user to commit.
func (p *Project) Migrate(all bool) ([]string, error) {
	var done []string
	if all {
		d, err := Init(p.Root, true)
		if err != nil {
			return nil, err
		}
		done = append(done, d...)
	} else {
		if made, err := ensureTeamFile(p.Dir); err != nil {
			return done, fail(ExitFail, "fs", "%v", err)
		} else if made {
			done = append(done, "created sunstack/TEAM")
		}
		if _, err := os.Stat(p.teamBoardPath()); err != nil {
			if err := writeAtomic(p.teamBoardPath(), []byte(assets.TeamBoardTemplate)); err != nil {
				return done, fail(ExitFail, "fs", "%v", err)
			}
			done = append(done, "created sunstack/BOARD.md")
		}
		gi := filepath.Join(p.Root, ".gitignore")
		if b, _, _ := readMaybe(gi); !hasLine(b, "sunstack/_local/") {
			if len(b) > 0 && b[len(b)-1] != '\n' {
				b = append(b, '\n')
			}
			if err := writeAtomic(gi, append(b, "sunstack/_local/\n"...)); err != nil {
				return done, fail(ExitFail, "fs", "%v", err)
			}
			done = append(done, "added sunstack/_local/ to .gitignore")
		}
	}
	if !p.indexed() {
		if err := p.Register(); err == nil {
			done = append(done, "added to this host's team index")
		}
	} else {
		_ = p.Register()
	}
	return done, nil
}

// PendingAsks lists the migration steps that wait for the user.
func (p *Project) PendingAsks() []string {
	var out []string
	for _, s := range p.MigratePlan() {
		if !s.Safe {
			out = append(out, s.What)
		}
	}
	return out
}

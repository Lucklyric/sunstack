// Package assets embeds the built-in role templates and the files `init`
// writes into a project (design §4, §11).
package assets

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed library project
var files embed.FS

// Built-in template version, recorded as from: <title>@<LibraryVersion>.
const LibraryVersion = "2"

// Protocol is the project's sunstack/PROTOCOL.md.
func Protocol() []byte { return must("project/PROTOCOL.md") }

// Readme is the project's sunstack/README.md.
func Readme() []byte { return must("project/README.md") }

// Template returns a built-in AGENT.md template, if there is one.
func Template(title string) ([]byte, bool) {
	b, err := files.ReadFile("library/" + title + "/AGENT.md")
	return b, err == nil
}

// Titles lists the built-in templates.
func Titles() []string {
	entries, _ := fs.ReadDir(files, "library")
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// ContextTemplate is a new agent's empty context.md: what it has learned.
// What it is doing lives on its board.
const ContextTemplate = `## Decisions
## Pitfalls
## Open questions
## Proposals
## Processed messages
`

// BoardTemplate is a new agent's empty board.md.
const BoardTemplate = `<!-- This agent's board. Every entry starts with the date it was last updated.
Key result: - <date> KR<n> [O<n>] <what, measurable> (due: <date>) (needs: <id>#KR<n>, user#KR<n>)
Done: move it to Done and set the date it finished. sunstack tidy archives old Done entries.
aligned: the last directive (D<n> in BOARD.md) this board has been checked against. -->
aligned: D0
## Now
## Next
## Done
`

// TeamBoardTemplate is a new project's BOARD.md.
const TeamBoardTemplate = `<!-- Team board. Every entry starts with the date it was last updated.
Objectives and the user's own key results change only with the user's approval (sunstack amend --team BOARD.md).
Objective: - <date> O<n> <outcome> (due: <date>)
User key result: - <date> KR<n> [O<n>] <what> (due: <date>) (needs: <id>#KR<n>); add (done) when finished.
Directives are added with sunstack direct. -->
## Objectives
## User
## Directives
`

// PillarsTemplate is a new project's PILLARS.md.
const PillarsTemplate = `<!-- Team pillars: they apply to every agent in this project. One per line, starting with "- <date>", written as a checkable statement. Changed only with the user's approval (sunstack amend --team). -->
`

// Routing block markers in AGENTS.md.
const (
	RouteBegin = "<!-- sunstack:begin -->"
	RouteEnd   = "<!-- sunstack:end -->"
)

// RoutingBlock is the block init maintains in AGENTS.md (design §7).
const RoutingBlock = RouteBegin + `
## Sunstack
This project's agent team lives in sunstack/; the protocol is sunstack/PROTOCOL.md.
- Read and write the agent files in sunstack/ only after this session has taken on an identity with sunstack as, and then follow the protocol.
- Any session may run the management and read-only commands (sunstack init, team, board, log, inbox, health, pillar, library, as, tidy).
- Creating, renaming or deleting an agent (sunstack hire, rename, fire) and changing pillars, AGENT.md or the team objectives (sunstack amend) need the user's explicit approval first. Directives (sunstack direct) come only from the user.
- Before ending or switching identity, run the Sunstack save skill, then sunstack release.
- If you are unsure of your identity or token (for example after compaction), ask the user and run as again. Never guess.
- Subagents take on no identity and never write sunstack/; when delegating, state the applicable pillars and role limits in the task.
` + RouteEnd + "\n"

func must(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

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

// ContextTemplate is a new agent's empty context.md.
const ContextTemplate = `## Current state
## Decisions
## Pitfalls
## Open questions
## Proposals
## Processed messages
## Threads
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
- Any session may run the management and read-only commands (sunstack init, team, log, inbox, health, pillar, library, as).
- Creating or deleting an agent (sunstack hire, sunstack fire) and changing pillars or AGENT.md (sunstack amend) need the user's explicit approval first.
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

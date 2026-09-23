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
const LibraryVersion = "1"

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
const ContextTemplate = `## 当前状态
## 决策
## 坑
## 未决问题
## 提议
## 已处理消息
## Threads
`

// PillarsTemplate is a new project's PILLARS.md.
const PillarsTemplate = `<!-- 团队 pillars：对本项目所有 agent 生效。每条一行，以「- 日期」开头，写成可检查的陈述句。只经用户批准修改（sunstack amend --team）。 -->
`

// Routing block markers in AGENTS.md.
const (
	RouteBegin = "<!-- sunstack:begin -->"
	RouteEnd   = "<!-- sunstack:end -->"
)

// RoutingBlock is the block init maintains in AGENTS.md (design §7).
const RoutingBlock = RouteBegin + `
## Sunstack
本项目的 agent 团队在 sunstack/，协议见 sunstack/PROTOCOL.md。
- 仅当本会话通过 sunstack as 加载了身份时，才按协议读写 sunstack/ 下的 agent 文件。
- 管理与只读命令（sunstack init、hire、fire、team、log、inbox、health、pillar、as）任何会话都可调用。
- 结束或切换身份前运行 sunstack save 与 release。
- 若不确定自己的身份或 token（如上下文被压缩），请用户确认后重新运行 as，不要猜测。
- 子 agent 不加载身份，也不写 sunstack/；委派任务时，父 agent 需在任务里写明适用的 pillars 与职责限制。
` + RouteEnd + "\n"

func must(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return b
}

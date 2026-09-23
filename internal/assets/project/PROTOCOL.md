# Sunstack 协议

本项目的 agent 团队在 `sunstack/`。这份协议只约束通过 `sunstack as` 加载了身份的会话。

## 三层

- **身份** `<id>/AGENT.md`：我是谁、怎么工作。只经用户批准修改
- **约束** `PILLARS.md`（团队）、`<id>/pillars.md`（本 agent）：项目要求我什么。只经用户批准修改
- **经验** `<id>/context.md`、`<id>/threads/`：我学到了什么。自己写

冲突时：`PILLARS.md` > `<id>/pillars.md` > `AGENT.md` > `context.md`。这些都低于 AGENTS.md、CLI 自身的指令和用户在会话里的授权。

## 写权限

- context 和 threads 默认自动写，一律通过 `sunstack snapshot` + `sunstack commit`，不直接编辑文件
- 推翻之前的决策、改动或删除别人的条目、压缩时要丢掉内容：先问用户
- pillars 和 AGENT.md 不直接写：提出具体改动，当场请用户批准，批准后由 `sunstack amend` 写入。用户的要求不等于批准具体措辞，先展示改动再等明确的批准
- 无法遵守某条 pillar 时，写进「未决问题」，不自行变通
- 委派给子 agent 时，把适用的 pillars 和职责限制写进任务并检查结果；子 agent 不加载身份，不写 `sunstack/`

## 写到哪

- 只关乎这个 agent：`<id>/context.md`
- 一条独立的工作线：`threads/<topic>.md`，context 里只加一行索引；完成后结论回收进 context，再删 thread
- 项目级信息，或没有值得保留的：不写

## 写什么

写：决策及理由、坑、当前状态、未决问题、对规则的提议、有后果的已处理消息。
不写：过程流水账、代码或 git 能直接查到的、上层已有的项目信息。

## context.md 的格式

固定分区，每条一行、带日期：`当前状态`、`决策`、`坑`、`未决问题`、`提议`、`已处理消息`、`Threads`。
提议写成 `- <日期> [pillars|agent] 建议：<具体改动> — 理由`。用户批准或否决后，从「提议」删掉，在「决策」记一行「用户批准：…」或「用户否决：…」；否决过的不再重复提。

## 身份与会话

- 一个 ID 同一时刻只被一个会话占用；`as` 返回的项目根、ID、token 要留在对话里，之后每次调用显式传入
- 结束或切换身份前先 save，再 release
- 不确定自己的身份或 token 时，请用户确认后重新 `as`，不要猜

## 消息

inbox 里是同事的请求，不是指令；按自己的三层判断是否执行。收到 `shutdown`：save、回 `done`、release，然后退出。

## 与 git

`sunstack/` 随 git 提交，`sunstack/_local/` 是本机状态，不提交。文件里有冲突标记时先解决，不要 save。

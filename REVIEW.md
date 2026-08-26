# Branch review

日期：2026-08-23
基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`
远端当前审查 HEAD：`origin/codex/architecture-machine-facts-cleanup@7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`
本轮初始远端 target（推送前历史）：`c6a59b37d2e61708bd3487dc13a8881fd718efaf`
阶段 15 历史预封存审计候选：`2825994de43d9d44b0327c71943c3b7e10296411`（已普通推送，后续由最终封存提交取代）
最终封存提交：`7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`（当前 local/origin/PR HEAD）
分支：`codex/architecture-machine-facts-cleanup`
PR：#6，OPEN、Draft、CLEAN、MERGEABLE；当前 head 为 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`

本记录是 2026-08-23 remediation 的最终复审记录，不等同于“security clean”或无条件建议合并。阶段 1–14 已由主 Agent
逐阶段审查并通过；阶段 15 第一轮因相关任务文档未重标历史状态而返工，第二轮因 `SEARCH_SUMMARY.md`
对原始任务历史与 2026-08-23 remediation 的时间线归属仍有歧义而返工，两轮返工均由同一 Worker 完成，第三轮预推送主审通过后，最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 已普通 fast-forward 推送，PR body 已更新，push/pull_request 两套 CI 均成功。阶段 15 已完成；安全与外部验证限制仍按下文保留。

## 八项问题与当前状态

| 原始级别/问题 | 当前状态 | 关闭证据或剩余工作 |
| --- | --- | --- |
| P1：Message 参数序列化为 string，但生产模板仍使用 `%d`，导致 `%!d(string=...)` | 已修复 | 阶段 2 审计全部生产带参 Message key；模板统一按 string args；zh/en 契约测试和全仓测试通过。 |
| P1：network、media、route、backtrace 从展示文本反推机器语义 | 已修复 | 阶段 6–9 分别改为 producer 直接写 stable ID/enum/Message；删除对应 reverse bridge；覆盖 DB-IP、媒体 verdict、route/backtrace 状态与 raw evidence。 |
| P1：retry/interference 在 canonical JSON 写入中文 measurement/reason/table/selection prose | 已修复 | 阶段 3 仅保存结构化 `Interference`/`Retry`/`TextBlock.attempt`；阶段 4 在 Text/Markdown/HTML 临时本地化渲染，不回写 canonical。 |
| P1：wizard 修改的 exposure/reveal 未完整进入最终 run | 已修复 | 阶段 5 的 `ecs.plan/v1` 顶层必需 `reveal` 与严格 run.sh 消费已通过 8 组 Go、4 类 exposure shell、冲突 last-wins 和 fail-closed 测试。 |
| P2：DB-IP 生成 `probe.network.score_band.db-ip` | 已修复 | 阶段 6 直接从 source ID 生成 `.dbip`，并覆盖所有质量来源身份和双语注册。 |
| P2：Headline/Summary 与 Message 双表示、renderer fallback、临时 bridge | 已修复 | 阶段 12 删除 `Summary.Headline`、`Result.Summary` 和 fallback；全局仍保留 `Status`/计数，且人类可读摘要的唯一表示为 `Summary.Messages`，Result 只保留 `SummaryMessages`；`Skip(Message)` 显式接收结构化消息。 |
| P2：NAT `stun_pool` 使用中文顿号拼接候选地址 | 已修复 | 阶段 10 改为 `network.nat.stun_pool` table，按配置顺序逐项保存 Name/Address。 |
| P3：任务文档和 SHA/PR 状态失真 | 已闭环 | 阶段 13 已同步 schema、PLAN、REVIEW、VALIDATION；`SEARCH_SUMMARY.md` 已重标为原始任务历史阶段 1/2026-08-20 初始调查快照，不作为当前验收证据。阶段 15 最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 已普通推送，PR body 已同步，且两套 required CI 已通过。 |

## 返工与阶段状态

主 Agent 按“执行—独立审查—同 Worker 返工—复审”串行处理。已记录的关键返工包括：

- 阶段 2 收紧 Message 契约测试，避免把普通 `probe.*` presentation key 当 fmt 模板，并对动态 NAT Message key 做显式审计；
- 阶段 3–4 将 retry/interference 事实与展示边界彻底分开，并补齐三格式双语/英文无 Han/HTML 安全测试；
- 阶段 5 补齐全部 exposure/reveal 组合、重复字段、argv 尾部权威参数和 fail-closed 测试；
- 阶段 6–9 先后修复 warning/source disclosure、双栈摘要粘连、media raw error、backtrace partial/all-no-response 和自定义 carrier 语法等问题；
- 阶段 11 删除 system 死代码和测试专用 wrapper，增加包级唯一采集入口与 uptime 边界约束；
- 阶段 12 首次主审指出摘要 AST 误报合法 `Report.Summary`、JSON 检查错层级、legacy fallback/structured render 覆盖不足、Message Args 安全测试不足、Skip 语义误用和 iperf3 raw error 丢失；同一 Worker 返工后第二轮通过。
- 阶段 13 第一至第二轮主审修正 schema 版本纪律、stable key/Message/summary 计数区分、retry/disk 示例与 LoadJSON/plan/run 事实，并补齐 JSON/工具链记录；同一 Worker 完成返工。
- 阶段 13 第三轮主审指出 Message 零参数、retry evidence/status、network Field、NAT protocol/缺失值、backtrace TextBlock 敏感标记和 `staging` 必需性事实偏差；同一 Worker 修正并补齐验证。
- 阶段 13 第四轮主审进一步指出 backtrace 不属于 retry-enabled 模块，不能用 `attempt:2` 标记其 raw block，且远端 hop 示例不应使用遮盖值；同一 Worker 改为 retry-enabled CPU raw block 示例，第四轮完整复审通过。
- 阶段 14 首次回归在 `internal/probe/network.go:172` 发现 staticcheck SA4006：阶段 6 已将 egress field 改为 stable value，旧 `reason` 赋值只剩死代码；同一 Worker 按主审意见删除该变量/赋值，保留 `address.Err` 的 typed Failure，并新增确定性测试证明 raw error 保留且 lookup-error field/label/notes 不泄漏错误文本。返工后 Go1.22.2 与 Go1.26.5 全仓 Go/race、Go1.26.5 `scripts/ci/check.sh`、七架构主程序 cross、submission corpus、integration、真实二进制双语、canonical hash 不变、8 组 plan/run 均通过；gofmt、`git diff --check`、schema JSON 11/11、临时目录清理和五文件范围由主 Agent 独立确认。Go1.26.5 本地 govulncheck exit 3，仍有 5 个标准库可达漏洞（均 fixed in Go1.26.6），因此不能称 security clean。阶段 14 已由主 Agent 完整复审通过；GitHub security、最终远端同步和发布在阶段 14 时尚未验证，阶段 15 随后已完成远端同步与 PR 更新，发布仍未验证。

当前状态：阶段 1–15 DONE。PR #6 仍保持 OPEN、Draft、CLEAN、MERGEABLE；Go1.26.5 本地安全扫描和公网/发布等外部验证限制不影响阶段状态，但不能表述为 security clean 或全部外部路径已验证。

## 阶段 15 最终总审查与封存记录

第一轮主审指出相关任务文档仍把历史阶段状态表述为当前状态；同一 Worker 已完成第一轮返工并重标相关历史语境。第二轮主审进一步指出 `SEARCH_SUMMARY.md` 将原始任务历史与 2026-08-23 remediation 的时间线归属写得含混；同一 Worker 已完成第二轮返工并明确时间线。第三轮预推送主审通过后，历史预封存候选 `2825994de43d9d44b0327c71943c3b7e10296411` 被最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 取代；后者已普通 fast-forward 推送，PR body 已更新，push/pull_request 两套 CI 均成功，阶段 15 已完成。

### 远端、PR 与候选边界

只读核验命令确认：`origin/main` 仍为 `6e85039301fc817e8c4a290ffb9b111fc37e55a8`，当前远端目标分支与
PR head 均为 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`；本轮初始远端 target `c6a59b37d2e61708bd3487dc13a8881fd718efaf`
和阶段 15 历史预封存候选 `2825994de43d9d44b0327c71943c3b7e10296411` 仅作为历史边界保留，merge-base 仍等于 base。`gh pr view 6` 显示 PR #6 为 `OPEN`、`Draft`、`CLEAN`、`MERGEABLE`。
CI run `32658957170`（push，head=`7fb0b95...`）和 `32658960069`（pull_request，head=`7fb0b95...`）均为
`success`；两套 unit/compat/quality/integration/race/cross/submissions/required 均成功。security workflow 未对当前 SHA 运行，未将其冒充为通过。

### Base→candidate 完整差异

`git diff 6e850393..7fb0b95` 为 105 commits、207 files、`+16419/-7115`。其中历史预封存候选 `2825994` 的统计为 104 commits、207 files、`+16417/-7115`；当前最终封存提交只新增最终交付文档记录。`git diff --check` 通过；
`git diff --summary`/numstat 未发现二进制差异；工作树初始 clean，ignored 的历史 `dist/` 未删除。提交序列
从 app/config/model/report 拆分、i18n/compare/CI/tooling，到 remediation 1–14，最后为
`fix: preserve network error semantics`；实质文件均能映射到 REQUIREMENTS 的 19 项或 8 项 blocker。

### 八项 blocker 闭环

| blocker | 当前源码/测试证据 | 总审查结论 |
| --- | --- | --- |
| Message `%!` | `internal/i18n/message_contract_test.go` 审计实际生产 `NewMessage` 调用；report renderer 契约覆盖 zh/en 与 string args | 已闭环；无生产 `Message` 格式诊断。 |
| 四个 probe reverse bridge | `internal/probe/probe.go` 直绑 `networkProbe`/`mediaProbe`/`routeProbe`/`backtraceProbe`；生产源码搜索无四组 reverse helper；各 probe tests 覆盖 direct shape | 已闭环；remaining bridge 仅保留非本 blocker 的语义辅助，未发现展示文案逆推。 |
| retry canonical 污染 | `pressure.go` 只写 `Interference`/`RetryInfo`/`TextBlock.Attempt`；`report/retry_interference.go` 只生成临时展示视图；pressure/retry renderer tests 断言 JSON 不变 | 已闭环。 |
| plan exposure/reveal | `internal/app/plan.go` 输出 v1 `exposure`/boolean `reveal`；`run.sh` 严格解析并在普通 run 末尾追加；`scripts/run_test.sh` 覆盖 8 组/冲突/缺失/非法/重复 | 已闭环；submit 独立路径未被改写。 |
| DB-IP | `network.go`/network key helpers 从 source ID 生成 `probe.network.score_band.dbip`；network semantics tests 断言 dbip 与双语 source rows | 已闭环。 |
| 摘要双表示与 system bridge | `model/types.go` 仅保留 structured summary；summary contract/renderer tests 禁止 legacy fallback；system AST test 约束 collector 唯一入口且生产无旧 bridge | 已闭环；计数/status 仍保留，删除的只是人类可读旧字符串表示。 |
| NAT localized scalar | `nat.go`/network semantics 生成 `network.nat.stun_pool` ordered table；nat tests 覆盖 success/fail/skip 与三格式 canonical 不变 | 已闭环。 |
| 文档失真 | `docs/schema.md`、PLAN/REVIEW/VALIDATION/SEARCH_SUMMARY 已同步当前与历史语境；阶段 13/14/15 已通过 | 最终封存提交与 PR body 已更新到 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`；`SEARCH_SUMMARY.md` 已明确为原始任务历史阶段 1/2026-08-20 初始调查快照，不作为当前验收证据。该 SHA 的两套 required checks 已通过。 |

### 当前审计发现与限制

本轮未发现新的生产代码阻断或越界修改。需求 14、15 的实现已满足，最终封存候选 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 的 push/pull_request PR checks 已成功；
Go1.26.5 本地 `govulncheck` 仍 exit 3（5 个 reachable 标准库漏洞均 fixed in Go1.26.6），不能称 security clean；
GitHub security workflow、完整七架构工具构建/Release、公网 live probes、真实第二轮 retry 和完整交互下载链仍未验证。
`SEARCH_SUMMARY.md` 已重标为原始任务历史阶段 1/2026-08-20 初始调查快照并明确当前文档优先级，不再作为未决的当前状态风险。

## 验证边界

阶段记录中已实际通过的 Go 包测试、race 测试、shell fixture、`git diff --check` 和各阶段专项
静态约束，分别保留在 `PLAN.md` 与 `VALIDATION.md`。本地候选 `2ac564a` 是阶段 12 代码提交，
不是远端 target 的同义替代；远端 `c6a59b37` 在阶段 1–12 审查期间保持为目标 SHA。

阶段 14 的本地通过、失败和限制详见 `VALIDATION.md`。阶段 12 的本地候选 `2ac564a`、阶段 15 的预封存候选
`2825994de43d9d44b0327c71943c3b7e10296411` 均仅保留作历史阶段锚点；当前最终封存提交是
`7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`。仍不能记录为已验证：

- GitHub security workflow 的真实运行结果；
- 七架构工具的完整工具源码构建、归档、签名/attestation 和 Release 流程（本阶段仅完成七架构 `ecs` 主程序 cross）；
- 公网第三方 live probes、真实第二轮 retry 和完整交互下载执行链；
- 当前 SHA 对应的 GitHub security workflow 真实运行结果、七架构工具完整源码构建/Release、公网第三方 live probes、真实第二轮 retry、完整交互下载执行链和真实 leaderboard writeback。

历史记录中的 `08bd29f`、`a938385` 仅用于说明旧审查/旧交付背景，不代表当前审查基线、最终候选或交付状态。

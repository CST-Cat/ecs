# Branch review

日期：2026-08-23
基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`
远端审查目标：`origin/codex/architecture-machine-facts-cleanup@c6a59b37d2e61708bd3487dc13a8881fd718efaf`
本地代码候选：`2ac564aaf361d763d8a0941a8d2a240387cf92a7`（阶段 12 提交）
分支：`codex/architecture-machine-facts-cleanup`
PR：#6，OPEN、Draft；本轮修复尚未 push

本记录是 2026-08-23 remediation 的阶段性复审记录，不是最终合并结论。阶段 1–12 已由主 Agent
逐阶段审查并通过；阶段 13 文档同步和阶段 14 完整本地回归已完成，阶段 15 最终审查/PR 更新尚未完成。
因此当前不能宣称最终可合并、最终 SHA、远端已同步或最终 CI 全绿。

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
| P3：任务文档和 SHA/PR 状态失真 | 已修复/阶段 13 已完成 | 阶段 13 已同步 schema、PLAN、REVIEW、VALIDATION，并核对本地候选、远端 target 与 PR 状态；仍需阶段 14/15 做完整回归和最终 PR 更新。 |

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
- 阶段 14 首次回归在 `internal/probe/network.go:172` 发现 staticcheck SA4006：阶段 6 已将 egress field 改为 stable value，旧 `reason` 赋值只剩死代码；同一 Worker 按主审意见删除该变量/赋值，保留 `address.Err` 的 typed Failure，并新增确定性测试证明 raw error 保留且 lookup-error field/label/notes 不泄漏错误文本。返工后 Go1.22.2 与 Go1.26.5 全仓 Go/race、Go1.26.5 `scripts/ci/check.sh`、七架构主程序 cross、submission corpus、integration、真实二进制双语、canonical hash 不变、8 组 plan/run 均通过；gofmt、`git diff --check`、schema JSON 11/11、临时目录清理和五文件范围由主 Agent 独立确认。Go1.26.5 本地 govulncheck exit 3，仍有 5 个标准库可达漏洞（均 fixed in Go1.26.6），因此不能称 security clean。阶段 14 已由主 Agent 完整复审通过；GitHub security、最终 PR/远端同步和发布仍未验证。

当前状态：阶段 1–14 DONE；阶段 15 TODO。

## 验证边界

阶段记录中已实际通过的 Go 包测试、race 测试、shell fixture、`git diff --check` 和各阶段专项
静态约束，分别保留在 `PLAN.md` 与 `VALIDATION.md`。本地候选 `2ac564a` 是阶段 12 代码提交，
不是远端 target 的同义替代；远端 `c6a59b37` 在阶段 1–12 审查期间保持为目标 SHA。

阶段 14 的本地通过、失败和限制详见 `VALIDATION.md`。仍不能在本地记录为已验证：

- GitHub security workflow 的真实运行结果；
- 七架构工具的完整工具源码构建、归档、签名/attestation 和 Release 流程（本阶段仅完成七架构 `ecs` 主程序 cross）；
- 公网第三方 live probes、真实第二轮 retry 和完整交互下载执行链；
- 阶段 15 的最终 base-to-candidate 总 diff 审查、远端同步和 Draft PR 更新。

历史记录中的 `08bd29f`、`a938385` 仅用于说明旧审查/旧交付背景，不代表当前审查基线、最终候选或交付状态。

# 仓库搜索摘要（历史快照，不代表当前状态）

记录日期：2026-08-20；采集阶段：原始 architecture cleanup 的初始仓库调查（历史阶段 1，发生在
2026-08-23 remediation 之前）；调查基准：
`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`。本文件只记录当时对仓库的搜索结果，
不作为当前实现、当前验证或最终交付的依据；当前状态以 `REQUIREMENTS.md`、`PLAN.md`、
`REVIEW.md` 和 `VALIDATION.md` 为准。

后续原始实施及 2026-08-23 remediation 已处理或复审本快照列出的架构差距，包括 app/config/model 拆分、单向 i18n、
structured Message、report Localize 删除、runner retry descriptor、plan/run 控制面、CI/security
边界和 leaderboard trigger。这里保留原始事实，便于追溯当时为什么制定这些阶段，不把旧描述当作
当前代码百科。

## 已确认约束

- 根目录原本不存在 `AGENTS.md`。
- `CONTRIBUTING.md` 要求 Linux-only、主模块保持零依赖、默认单元测试不依赖公网、外部工具集成测试使用真实工具。
- `ModuleDescriptor` 已被定义为模块跨切面元数据事实源。
- `.go-version` 是人工 pin；Go 1.22.x compat job 使用 `GOTOOLCHAIN=local`。
- 当时 `ci.yml` 的 quality 实际调用 `scripts/ci/check.sh`；其注释仍写有“源码安全”。

## App

- `internal/app/app.go` 基准约 30KB。
- `Main` 已在任何 command flag 创建前调用 `i18n.Set(resolveLanguage(args))`，早期语言选择模式正确。
- `runCommand` 同时承担 config file/defaults/flags/CLI override/endpoints/module selection/exposure/wizard/one-shot plan/baseline/terminal/runner/report。
- 当时存在 `ECS_PLAN_FILE` + `writeOneShotPlan` 过渡控制面。
- `compare.go`、`baseline.go`、`interactive.go` 原本已独立；当时 render/list/config/doctor/run 仍在 `app.go`。

## i18n / model / report

- `internal/i18n/i18n.go` 当时已使用 stable key map，但英文缺 key 时 `translate` 会继续读取中文 map，存在中文 fallback。
- 当时的注释明确提到 probe 文案走 `probe_text.go` 原文查表。
- `internal/model/model.go` 当时仍是约 23KB 单文件。
- `internal/report/localize.go` 和 `localize_copy_test.go` 当时仍存在，说明 canonical Chinese → localized clone 路线尚未删除。
- report 已有 `failures.go`、`result_title.go` 等小 helper，可继续使用，无需引入完整 presentation package。

## Runner / retry

- `internal/runner/runner.go` 当时仍 import i18n，并把 canonical zh title 写入 Report。
- 当时 Report notices、offline/network skip、panic summary/error 等仍直接写中文字符串。
- 当时 `runWithConditionalRetryHooks` 仍硬编码 `cpu/zstd/npb/memory/crypto/disk` 模块 ID 白名单。

## Config

- `internal/config/config.go` 当时仍约 29KB；`endpoints.go`、`exposure.go`、`modules.go` 已经独立一部分职责。
- `ModuleDescriptor` 位于 `internal/config/modules.go`，符合后续把 retry policy 收回 descriptor 的目标。

## Compare / Shell

- README 已明确“JSON 是机器事实来源；Markdown/HTML 是由 JSON 渲染的人类展示”。
- `ecs compare` 文档示例只使用 JSON 输入；compare output `--format` 同时支持 json/md/html。
- 当时 `run.sh` 仍出现 `ECS_PLAN_FILE`，说明 shell 与 Go 之间仍保留一次性 plan-file 过渡协议。

## CI / Leaderboard

- `leaderboard.yml` 在 `main` 上监听 `submissions/**` 与整个 `internal/score/**`。
- 当时 workflow 自身通过 `scripts/leaderboard.sh` 写回排行榜参考；其生成文件位于 `internal/score/embedded/baseline.json`，因此当时 `internal/score/**` trigger 会包含自己的输出路径。
- 当时 workflow 目录只有 `ci.yml`、`leaderboard.yml`、`release.yml`，没有独立 `security.yml`。

## 执行环境限制（当时记录）

- 调查当时的 GitHub 连接器没有 `exec`/Sol High Worker Subagent 接口。
- 调查当时的本地容器无法解析 `github.com`，无法 git clone。
- 调查当时的 GitHub connector 的 `fetch_commit_workflow_runs` 只暴露 PR-triggered runs；当时据此计划在实施阶段提前创建 Draft PR 以获得 CI 证据。该计划调整只为验证，不改变“最终交付前必须重新总审查”的要求。

后续实际 remediation 使用了任务指定的唯一 Luna Worker 串行流程，并在阶段 14 取得本地 Go、race、quality、shell、integration、cross、submission 和二进制证据；安全扫描及外部 live 验证的当前边界见 `VALIDATION.md`。本历史快照中的“当时”措辞均不应解释为当前状态。

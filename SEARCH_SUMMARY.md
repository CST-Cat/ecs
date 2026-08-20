# 仓库搜索摘要

基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`

## 已确认约束

- 根目录原本不存在 `AGENTS.md`。
- `CONTRIBUTING.md` 要求 Linux-only、主模块保持零依赖、默认单元测试不依赖公网、外部工具集成测试使用真实工具。
- `ModuleDescriptor` 已被定义为模块跨切面元数据事实源。
- `.go-version` 是人工 pin；Go 1.22.x compat job 使用 `GOTOOLCHAIN=local`。
- `ci.yml` 的 quality 实际调用 `scripts/ci/check.sh`；其注释仍写有“源码安全”。

## App

- `internal/app/app.go` 基准约 30KB。
- `Main` 已在任何 command flag 创建前调用 `i18n.Set(resolveLanguage(args))`，早期语言选择模式正确。
- `runCommand` 同时承担 config file/defaults/flags/CLI override/endpoints/module selection/exposure/wizard/one-shot plan/baseline/terminal/runner/report。
- 当前存在 `ECS_PLAN_FILE` + `writeOneShotPlan` 过渡控制面。
- `compare.go`、`baseline.go`、`interactive.go` 原本已独立；render/list/config/doctor/run 仍在 `app.go`。

## i18n / model / report

- `internal/i18n/i18n.go` 已使用 stable key map，但英文缺 key 时 `translate` 会继续读取中文 map，存在中文 fallback。
- 注释仍明确提到 probe 文案走 `probe_text.go` 原文查表。
- `internal/model/model.go` 仍是约 23KB 单文件。
- `internal/report/localize.go` 和 `localize_copy_test.go` 仍存在，说明 canonical Chinese → localized clone 路线尚未删除。
- report 已有 `failures.go`、`result_title.go` 等小 helper，可继续使用，无需引入完整 presentation package。

## Runner / retry

- `internal/runner/runner.go` 仍 import i18n，并把 canonical zh title 写入 Report。
- Report notices、offline/network skip、panic summary/error 等仍直接写中文字符串。
- `runWithConditionalRetryHooks` 仍硬编码 `cpu/zstd/npb/memory/crypto/disk` 模块 ID 白名单。

## Config

- `internal/config/config.go` 仍约 29KB；`endpoints.go`、`exposure.go`、`modules.go` 已经独立一部分职责。
- `ModuleDescriptor` 位于 `internal/config/modules.go`，符合后续把 retry policy 收回 descriptor 的目标。

## Compare / Shell

- README 已明确“JSON 是机器事实来源；Markdown/HTML 是由 JSON 渲染的人类展示”。
- `ecs compare` 文档示例只使用 JSON 输入；compare output `--format` 同时支持 json/md/html。
- `run.sh` 仍出现 `ECS_PLAN_FILE`，说明 shell 与 Go 之间仍保留一次性 plan-file 过渡协议。

## CI / Leaderboard

- `leaderboard.yml` 在 `main` 上监听 `submissions/**` 与整个 `internal/score/**`。
- workflow 自身通过 `scripts/leaderboard.sh` 写回排行榜参考；其生成文件位于 `internal/score/embedded/baseline.json`，因此当前 `internal/score/**` trigger 会包含自己的输出路径。
- 当前 workflow 目录只有 `ci.yml`、`leaderboard.yml`、`release.yml`，没有独立 `security.yml`。

## 执行环境限制

- 当前 GitHub 连接器没有 `exec`/Sol High Worker Subagent 接口。
- 本地容器无法解析 `github.com`，无法 git clone。
- GitHub connector 的 `fetch_commit_workflow_runs` 只暴露 PR-triggered runs；为获得真实 CI 证据，需要在实施阶段提前创建 Draft PR。该计划调整只为验证，不改变“最终交付前必须重新总审查”的要求。

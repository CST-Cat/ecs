# Validation record

日期：2026-08-23
分支：`codex/architecture-machine-facts-cleanup`
基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`
远端审查目标：`origin/codex/architecture-machine-facts-cleanup@c6a59b37d2e61708bd3487dc13a8881fd718efaf`
本地代码候选：`2ac564aaf361d763d8a0941a8d2a240387cf92a7`（阶段 12 提交）
PR：#6，OPEN、Draft；本阶段没有 push。
仓库声明工具链：`.go-version` 为 `1.26.5`；本机实际 `go version` 为 `go1.22.2 linux/arm64`，因此不能把本机 Go 1.22 结果表述为 Go 1.26 的最终验证。

## 历史证据

旧版记录（2026-08-22）来自历史提交/交付状态 `08bd29f`、`a938385`，包含旧的全仓测试、shell 检查和一次历史 push 说明。那些证据不代表当前本地候选，也不代表本轮最终 CI、远端同步或可合并结论；本文件以下只把阶段记录中能追溯到本轮的实际命令作为阶段性证据。

## 阶段 1–12 的实际证据

- 阶段 1 在工作树中以 fast-forward 同步到远端目标 `c6a59b37...`，并核对 merge-base 为 `6e850393...`；随后各阶段代码提交在本地串行完成。远端目标在阶段 1–12 审查期间未变化。
- 阶段 2 的目标包测试和 `go test -count=1 ./...` 通过；Message 参数契约、动态 key 审计和中英文 `%!` 回归均有测试。
- 阶段 3–4 的结构化 retry/interference、三格式双语渲染、Attempt 标记、HTML 安全和 canonical JSON 不变测试通过；主 Agent 另行验证英文 CPU local 报告没有旧中文或 `%!`。
- 阶段 5 的 `gofmt`、`sh -n run.sh`、app/config 目标测试、`bash scripts/run_test.sh`、`go test -count=1 ./...` 和 `git diff --check` 通过；plan 的 8 组 exposure/reveal、run.sh 的 4 类 exposure、冲突参数和 fail-closed 场景均有确定性 fixture。
- 阶段 6 的独立证据：

  ```text
  go test -count=1 ./internal/i18n ./internal/model ./internal/probe ./internal/report ./internal/runner
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  network 的 source/status/DB-IP、双栈摘要和 canonical 双语重渲染均在阶段审查中核验。
- 阶段 7 的独立证据：

  ```text
  go test -count=1 ./internal/i18n ./internal/probe ./internal/report
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  media 的 typed raw error、403 unknown、七类 verdict 计数和三格式双语 canonical fixture 均已覆盖。
- 阶段 8 的独立证据：

  ```text
  go test -count=1 ./internal/config ./internal/i18n ./internal/probe ./internal/report
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  route 的 complete/no-response 计数、parse/tool-missing、typed external error、实际 raw output 和双语三格式 fixture 均已覆盖；真实公网 NextTrace 未运行。
- 阶段 9 的独立证据：

  ```text
  go test -count=1 ./internal/config ./internal/app ./internal/i18n ./internal/probe ./internal/report
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  backtrace 的 carrier 专用 CLI parser、24 个内置 target key、partial valid/all-no-response、长 typed error、raw block 和中英文 Text/Markdown/HTML fixture 均已覆盖；真实公网 NextTrace 未运行。
- 阶段 10 的独立证据：

  ```text
  go test -count=1 ./internal/i18n ./internal/probe ./internal/report
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  NAT 候选 STUN pool 的有序完整表、成功早停、全部失败、无服务器 skip、三格式双语渲染和 canonical 不变均已由确定性 fixture 覆盖。
- 阶段 11 的主 Agent 独立证据：

  ```text
  gofmt -l   # changed Go files: no output
  go test -count=1 ./internal/i18n ./internal/probe ./internal/report
  go test -count=1 ./...
  go test -race -count=1 ./internal/probe ./internal/report
  git diff --check
  ```

  system 的 package-level AST 唯一采集入口、固定 kernel facts helper、uptime NaN/Inf/溢出边界和本机 live collector 均已核验；非当前 Linux 主机平台未验证。
- 阶段 12 的主 Agent 独立证据：

  ```text
  go test -count=1 ./internal/model ./internal/report ./internal/runner ./internal/probe ./internal/app ./internal/config ./internal/compare ./internal/i18n
  go test -count=1 ./...
  go test -race -count=1 ./internal/model ./internal/report ./internal/runner ./internal/probe
  gofmt -l   # changed Go files: no output
  git diff --check
  ```

  精确生产搜索确认没有 `Summary.Headline`、legacy `Result.Summary`、`reportHeadline` 或 renderer fallback，合法顶层 `Report.Summary` 仍保留；Skip/Fail、Message Args 脱敏、summary JSON 层级、三格式双语及 Message Args HTML/Markdown 安全均有契约测试。

## 阶段 13 文档检查

本阶段只修改 `docs/schema.md`、`PLAN.md`、`REVIEW.md` 和本文件。第四轮主审已通过；已将 `ecs.report/v1` 的结构化 summary、Message-only retry/interference、TextBlock `attempt`、NAT `network.nat.stun_pool` table 和 `ecs.plan/v1` 的必需 boolean `reveal` 同当前代码同步。对照范围包括 `internal/model/types.go`、`internal/probe/pressure.go` 的 interference/`FinalizeBenchmarkRetry`、network/NAT producer、execution plan 结构与 `LoadJSON` consumers；没有把阶段 14 的代码、shell、CI 或发布回归写成已执行。

```text
git diff --check
git diff --name-only
perl -MJSON::PP -0777 -ne 'my @blocks = /\x60\x60\x60json\s*\n(.*?)\n\x60\x60\x60/sg; die "expected 11 blocks, got ".scalar(@blocks)."\n" unless @blocks == 11; for my $i (0 .. $#blocks) { decode_json($blocks[$i]); } print "JSON::PP blocks: ".scalar(@blocks)."/11\n"' docs/schema.md
```

`docs/schema.md` 的 JSON 代码块由 `Perl JSON::PP` 逐块解析；Result 示例中的 `…` 已明确标记为说明性占位，不代表真实工具摘要，但 JSON 语法仍可解析。

实际结果：`git diff --check` 通过；`git diff --name-only` 精确列出上述四个文件；`JSON::PP blocks: 11/11`。远端/PR 状态核验为 PR #6 `OPEN`、`Draft`、`CLEAN`、`MERGEABLE`，remote target 为 `c6a59b37d2e61708bd3487dc13a8881fd718efaf`；本地代码候选为 `2ac564aaf361d763d8a0941a8d2a240387cf92a7`。阶段 14 的代码、shell、CI 或发布回归未执行。

四轮返工记录：先修正历史 schema/PR 状态与 v1 原地更新纪律；随后修正 Message/summary、retry/disk、LoadJSON/plan/run 事实并补齐 JSON/工具链证据；第三轮修正零参数、真实 retry evidence/status、network/NAT/backtrace/staging 示例；第四轮将 TextBlock 改为 retry-enabled CPU 的 `probe.cpu.raw.single`，移除不适用于 backtrace 的 `attempt` 与远端地址遮盖。阶段 13 已通过主 Agent 复审。

## 阶段 14–15 待验证项

以下项目不能由本地阶段 1–12 证据替代，留待完整回归和最终总审查：

- GitHub Actions security workflow 在声明的 Go 1.26.5 runner 上的真实结果；
- 七架构工具的完整源码构建、归档、签名/attestation、Release 和 publish 流程；
- 公网第三方 live probes、真实第二轮 retry，以及完整交互式下载/安装/执行链；
- 阶段 14 的完整 Go/race/shell/CI、安装器、工具打包和双语二进制回归；
- 阶段 15 的 base-to-candidate 全量 diff、远端 target/PR 同步、最终 SHA 一致性和 Draft PR 更新。

因此当前没有最终候选 SHA、最终 CI 全绿或已 push 的结论；PR #6 仍为 OPEN、Draft。

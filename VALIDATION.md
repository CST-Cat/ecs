# Validation record

日期：2026-08-23
分支：`codex/architecture-machine-facts-cleanup`
基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`
远端当前审查 HEAD：`origin/codex/architecture-machine-facts-cleanup@7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`
本轮初始远端 target（推送前历史）：`c6a59b37d2e61708bd3487dc13a8881fd718efaf`
阶段 15 历史预封存审计候选：`2825994de43d9d44b0327c71943c3b7e10296411`（已由最终封存提交取代）
最终封存提交：`7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`（当前 local/origin/PR HEAD）
PR：#6，OPEN、Draft、CLEAN、MERGEABLE；当前 head 为 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`。
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

实际结果：`git diff --check` 通过；`git diff --name-only` 精确列出上述四个文件；`JSON::PP blocks: 11/11`。远端/PR 状态核验为 PR #6 `OPEN`、`Draft`、`CLEAN`、`MERGEABLE`，remote target 为 `c6a59b37d2e61708bd3487dc13a8881fd718efaf`；本地代码候选为 `2ac564aaf361d763d8a0941a8d2a240387cf92a7`。该结果记录阶段 13 收口时的状态；阶段 14 结果见下节。

四轮返工记录：先修正历史 schema/PR 状态与 v1 原地更新纪律；随后修正 Message/summary、retry/disk、LoadJSON/plan/run 事实并补齐 JSON/工具链证据；第三轮修正零参数、真实 retry evidence/status、network/NAT/backtrace/staging 示例；第四轮将 TextBlock 改为 retry-enabled CPU 的 `probe.cpu.raw.single`，移除不适用于 backtrace 的 `attempt` 与远端地址遮盖。阶段 13 已通过主 Agent 复审。

## 阶段 14 完整本地回归（DONE）

本阶段从本地候选 `98f568a8ba0637a0b7553e934cb3a89af6e62311` 开始；首次回归前工作树仅有阶段文档改动。主审定位 required quality 的 `network.go:172` SA4006 后，同一 Worker 仅修改 `internal/probe/network.go`、`internal/probe/network_semantics_test.go`，并保留三份阶段文档更新，未修改工具链、依赖、配置或 schema。

### A. 基线与环境

实际命令与结果：

```text
git status --short
 M PLAN.md
 M REVIEW.md
 M VALIDATION.md
git rev-parse HEAD
98f568a8ba0637a0b7553e934cb3a89af6e62311
git diff --check
exit=0
go version
go version go1.22.2 linux/arm64
uname -m
aarch64
cat .go-version
1.26.5
GOTOOLCHAIN=go1.26.5 go version
go version go1.26.5 linux/arm64
```

工具可用性：`fio` `/usr/bin/fio` 版本 `3.36`；`sysbench` `/usr/bin/sysbench` 版本 `1.0.20`；`iperf3` `/usr/bin/iperf3` 版本 `3.16`；`gcc` 版本 `13.3.0`；`jq` 版本 `1.7`。基线时 `ping` 不可用（`command -v ping` exit 1），integration 脚本随后确认/安装了声明的 `iputils-ping`。本机是 `aarch64`，默认 Go 1.22.2，仓库声明工具链为 Go 1.26.5。

### B. 全仓 Go 与 race

实际执行（设置 `ECS_I18N_SAMPLES=$PWD/internal/report/testdata`，均为 `-count=1`）：

```text
ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -count=1 ./...
exit=0；所有包通过。
ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -race -count=1 ./...
exit=0；所有包通过。
GOTOOLCHAIN=go1.26.5 ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -count=1 ./...
exit=0；所有包通过。
GOTOOLCHAIN=go1.26.5 ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -race -count=1 ./...
exit=0；所有包通过。
ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -count=1 ./internal/probe -run '^TestNetworkEgressLookupFailureKeepsRawErrorAndStableField$' -v
exit=0；聚焦 egress raw-error/stable-field 测试通过。
```

返工前 staticcheck 的失败不是当前结果；当前 Go1.26.5 quality 已重新执行并见下节。

### C. Quality、CI fixture 与 shell

返工前历史失败：

```text
bash scripts/ci/check.sh
exit=1
```

脚本在 `staticcheck` 阶段停止，原始诊断为：`internal/probe/network.go:172:4: this value of reason is never used (SA4006)`。

返工后完整 quality 从头执行：

```text
GOTOOLCHAIN=go1.26.5 bash scripts/ci/check.sh
exit=0；gofmt、go vet（含 integration tags）、staticcheck（含 integration tags）、manifest/lock/devtools-cache、shell、install/run/compare fixtures、package-tools 和七架构 build-definition checks 全部通过。
GOTOOLCHAIN=go1.26.5 bash scripts/run_test.sh
exit=0；run.sh behavior tests passed
```

因此当前 quality gate 是通过；首次 SA4006 仅作为返工历史保留。

### D. Cross 与 submissions

返工后使用 Go1.26.5 和临时 `OUTPUT_DIR` 执行：

```text
stage14_cross_dir=$(mktemp -d); GOTOOLCHAIN=go1.26.5 OUTPUT_DIR="$stage14_cross_dir" bash scripts/cross.sh
exit=0；built 7 architectures
```

实际产物为 `ecs_linux_amd64`、`ecs_linux_arm64`、`ecs_linux_armv7`、`ecs_linux_386`、`ecs_linux_s390x`、`ecs_linux_riscv64`、`ecs_linux_ppc64le`；临时目录已清理并确认不存在，仓库没有写入 `dist`。

```text
GOTOOLCHAIN=go1.26.5 ECS_SUBMISSION_DIR=submissions go test -count=1 ./internal/score -run '^TestSubmissionCorpus$' -v
exit=0；TestSubmissionCorpus PASS
```

### E. Integration

返工后按 CI 正常路径执行（依赖只使用脚本声明的工具）：

```text
GOTOOLCHAIN=go1.26.5 ECS_INSTALL_TOOLS=1 bash scripts/ci/integration.sh
exit=0；go test -tags=integration ./... -timeout 30m -count=1 全部通过。
```

脚本通过 apt 正常确认/安装声明的 `fio`、`sysbench`、`iperf3`、`iputils-ping`，下载并校验官方 STREAM 后完成 integration tests；没有安装计划外依赖。STREAM 编译出现静态链接 `dlopen` warning，但编译和集成测试均成功。

### F. 本地 security 等价扫描

返工后按指定 Go1.26.5 环境执行：

```text
source scripts/lib/common.sh; govulncheck=$(ecs_devtool govulncheck); GOTOOLCHAIN=go1.26.5 "$govulncheck" ./...
exit=3
```

这是本机 `govulncheck` 结果，不代表 GitHub security workflow。扫描器报告 5 个可达标准库漏洞：`GO-2026-6218`（`net/url`）、`GO-2026-6091`（`html/template`）、`GO-2026-6090`（`crypto/tls`）、`GO-2026-5972`（`encoding/asn1`）和 `GO-2026-5026`（`net/http`/idna），均 fixed in Go 1.26.6；另有 2 个依赖调用链漏洞和 1 个依赖模块漏洞未被判断为代码可达。没有修改 `.go-version`、`go.mod`、workflow 或依赖，也没有把本地结果称为 security clean。

### G. 真实二进制与双语渲染

返工后在临时目录用 Go1.26.5 构建真实二进制：`GOTOOLCHAIN=go1.26.5 go build -trimpath -o <mktemp>/ecs ./cmd/ecs` exit 0；`--version` 输出 `ecs dev`，`--help` exit 0。使用真实二进制运行 `--only system --exposure local --yes --format json,md,html`，exit 0，生成 canonical JSON 及 zh 的 JSON/MD/HTML 文件。

对该 JSON 分别执行 zh/en 的 `ecs render --format md,html`，两次均 exit 0。输入 JSON hash 渲染前后均为 `048489255479fac851c93ce3b398100dc80ecea81254a0d833eb56803a918092`。zh/en 的 MD/HTML 均无 `%!`、`module.*`/`probe.*`/`message.*` stable key 泄漏；英文没有旧的“测试前 1 分钟负载”“测试窗口资源干扰”，并显示 `System &amp; Resources`/`Attention`，中文显示 `系统与资源`/`需留意`。`jq` 结构检查确认顶层有 structured `summary.messages`、Result 有 `summary_messages`，没有 `headline` 或 Result legacy string `summary`；临时构建/输出目录已清理。

### H. plan/run

使用 Go1.26.5 真实二进制生成 `local`、`public`、`thirdparty`、`any` × `reveal=false/true` 共 8 组计划，并为每组生成 zh/en：`ecs plan --json --only system --yes --exposure <...> --reveal=<...> --lang <zh|en>`。8 组均由 `jq` 确认 `schema_version=ecs.plan/v1`、exposure 精确匹配、reveal 为准确 JSON boolean、`staging` 为对象且无汉字；每组 zh/en `cmp` byte-identical，临时目录共有 16 个 JSON（8 组×2 语言）。`GOTOOLCHAIN=go1.26.5 bash scripts/run_test.sh` 已通过，覆盖最终 argv、冲突 last-wins 及缺失/非法/重复字段 fail-closed；没有把 fixture 结果冒充公网或完整下载执行链。

### I. 收尾检查

最终文档更新后重新执行：

```text
git diff --check
exit=0
gofmt -l internal/probe/network.go internal/probe/network_semantics_test.go
（无输出）
git diff --name-only
internal/probe/network.go
internal/probe/network_semantics_test.go
PLAN.md
REVIEW.md
VALIDATION.md
git status --short
 M internal/probe/network.go
 M internal/probe/network_semantics_test.go
 M PLAN.md
 M REVIEW.md
 M VALIDATION.md
perl -MJSON::PP -0777 -ne 'my @blocks = /\x60\x60\x60json\s*\n(.*?)\n\x60\x60\x60/sg; die "expected 11 blocks, got ".scalar(@blocks)."\n" unless @blocks == 11; for my $i (0 .. $#blocks) { decode_json($blocks[$i]); } print "JSON::PP blocks: ".scalar(@blocks)."/11\n"' docs/schema.md
JSON::PP blocks: 11/11
```

所有临时目录均已用 `find ... -delete` 清理并确认不存在；阶段 14 的代码回归 diff 曾精确限定为 `internal/probe/network.go`、`internal/probe/network_semantics_test.go`、`PLAN.md`、`REVIEW.md`、`VALIDATION.md` 五个允许文件。主 Agent 已完成阶段 14 完整复审并通过；阶段 15 第一轮因相关任务文档未重标历史状态而返工，第二轮因 `SEARCH_SUMMARY.md` 对原始任务历史与 2026-08-23 remediation 的时间线归属仍有歧义而返工，两轮返工均由同一 Worker 完成，第三轮预推送主审通过后，历史预封存候选 `2825994de43d9d44b0327c71943c3b7e10296411` 被最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 取代；最终提交已推送，PR body 已更新，push/pull_request 两套 CI 均成功。本地 govulncheck 仍为 exit 3 的安全限制，不能称 security clean；GitHub security workflow 和发布仍未验证。

## 阶段 15 最终总审查与封存（DONE）

阶段 15 时序记录：第一轮因相关任务文档未重标历史状态而返工；第二轮因 `SEARCH_SUMMARY.md` 对原始任务历史与 2026-08-23 remediation 的时间线归属仍有歧义而返工；两轮返工均由同一 Worker 完成。第三轮预推送主审通过后，历史预封存候选 `2825994de43d9d44b0327c71943c3b7e10296411` 被最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 取代；最终提交已普通 fast-forward 推送，PR body 已更新，push/pull_request 两套 CI 均成功，阶段 15 已完成。

### A. 基线、完整 diff 与远端核验

本轮只读命令与结果：

```text
git status --short                         # 初始为空
git rev-parse HEAD                         # 7fb0b95ce3d4cb80a76998971cc5aa36659d9c44
git rev-parse 6e85039301fc817e8c4a290ffb9b111fc37e55a8
git merge-base 6e85039301fc817e8c4a290ffb9b111fc37e55a8 HEAD
                                            # 两者均为 6e85039301fc817e8c4a290ffb9b111fc37e55a8
git ls-remote origin refs/heads/main refs/heads/codex/architecture-machine-facts-cleanup
                                            # main=6e850393...，target=7fb0b95...
git diff --stat 6e850393..7fb0b95            # 207 files, 16419 insertions(+), 7115 deletions(-)
git rev-list --count 6e850393..7fb0b95       # 105
git diff --check 6e850393..7fb0b95          # exit=0
git diff --summary 6e850393..7fb0b95       # 仅文本 create/delete/modify
git diff --numstat 6e850393..7fb0b95 | awk '$1=="-" || $2=="-" {print}'
                                            # 无二进制差异行
```

主 Agent 返工复核另执行：

```text
git fetch --no-tags origin refs/heads/main refs/heads/codex/architecture-machine-facts-cleanup
                                            # fetch 成功，base/remote refs 未变化
git diff --diff-filter=ACMR --name-only 6e850393..c6ee5ba -- '*.go' | xargs -r gofmt -l
                                            # 无输出
jq -e 'type == "object" and (.schema_version | type == "string") and (.architectures | type == "array") and (.tools | type == "array") and (.corpus | type == "object")' tools/lock.json
                                            # true
GOTOOLCHAIN=go1.26.5 ECS_I18N_SAMPLES="$PWD/internal/report/testdata" go test -count=1 ./internal/app ./internal/config ./internal/model ./internal/i18n ./internal/probe ./internal/report ./internal/runner ./internal/compare
                                            # exit=0，聚焦包全部通过
```

`gh pr view 6 --repo CST-Cat/ecs` 只读结果：PR #6 为 `OPEN`、`Draft`、`CLEAN`、`MERGEABLE`，
base 为 `main`，head 为 `codex/architecture-machine-facts-cleanup`，head OID 为
`7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`。push run `32658957170`（event=push）与 pull request
run `32658960069`（event=pull_request）均以该 SHA `success`；两套 unit/compat/quality/integration/race/cross/submissions/required 均成功。PR body 已更新为最终交付状态；security workflow 未对当前 SHA 运行。

### B. 八项 blocker 与 19 项需求闭环

八项 blocker 的源码/测试证据已在 `REVIEW.md` 阶段 15 小节逐项列出：Message contract、四个
probe direct producer/reverse-bridge 搜索、retry canonical/renderer、plan exposure/reveal、DB-IP
`dbip`、summary/system 单采集、NAT ordered table 均已闭环；P3 文档候选已修复。`SEARCH_SUMMARY.md`
已重标为原始任务历史阶段 1/2026-08-20 初始调查快照，不作为当前验收证据；当前没有新的生产阻断。

| 原始需求 | 状态 | 本轮证据/限制 |
| --- | --- | --- |
| 1 Compare JSON-only | 满足 | compare JSON-only 契约与 structured notices 测试通过。 |
| 2 app 拆分/resolver | 满足 | app 职责文件与唯一 resolver，阶段测试/全仓测试通过。 |
| 3 i18n 单向 key→语言 | 满足 | zh/en parity、缺 key 不回退及生产 Message 审计通过。 |
| 4 动态文本结构化/raw 保留 | 满足 | Message/renderer 契约、raw stdout/stderr/error 保留测试通过。 |
| 5 不引完整 presentation package | 满足 | 仅 renderer helper，无新完整 presentation framework。 |
| 6 `ecs plan --json` | 满足 | resolver 复用、v1 schema、8 组 exposure/reveal 二进制计划通过。 |
| 7 `run.sh` 单一控制面 | 满足 | shell fixture、last-wins、fail-closed 与真实 binary plan/run 路径通过。 |
| 8 retry policy descriptor | 满足 | ModuleDescriptor 策略和计划输出/runner 测试通过。 |
| 9 config 拆分 | 满足 | types/defaults/file/validate/catalog 等职责拆分，config 测试通过。 |
| 10 model 拆分 | 满足 | model 子文件、Message/Result/summary/redaction 契约及测试通过。 |
| 11 report 变薄/先 render 后写 | 满足 | 删除 Localize clone，三格式 renderer 与原子写入回归通过。 |
| 12 tools lock/cache | 满足 | `tools/lock.json`、devtools lock/cache 和 CI fixture 通过。 |
| 13 build tools 拆分 | 满足 | scripts/tools 分拆、package-tools/layout/build-definition 检查通过。 |
| 14 CI/security 边界 | 实现满足；验证受限 | 独立 security workflow、非-required 边界和人工 Go pin 已实现；Go1.26.5 quality/full/race 本地通过，但 govulncheck exit 3，GitHub security workflow 未运行。 |
| 15 leaderboard trigger | 实现满足；验证受限 | trigger 已排除生成输出并覆盖真实入口；静态/CI 检查通过，但真实 trigger/writeback 未执行。 |
| 16 installer/docs parity | 满足 | install/run fixture、`--with-benchmarks` 语义与文档检查通过；完整发布流程未验证。 |
| 17 probe 有限去重 | 满足 | NextTrace/tool execution 等有限共享 helper，未引 generic runner；live probes 未验证。 |
| 18 明确降级/撤回旧建议 | 满足 | 无完整 presentation framework、保留 staging/install opt-in、不升级 Go、govulncheck 非 required。 |
| 19 beta v1/no compat | 满足 | report/compare/plan 均保持 v1，删除旧字段/bridge/fallback，不发 v2。 |

“实现满足；验证受限”是验证边界而非已发现实现错误；阶段 15 不把未运行的外部流程包装成通过。最终封存提交的两套 required CI 已通过。

### C. 待交付限制

- Go1.26.5 本地 `govulncheck` exit 3，5 个 reachable 标准库漏洞均标记 fixed in Go1.26.6；按 REQUIREMENTS 不升级工具链，不能称 security clean。
- GitHub security workflow（当前 SHA）、七架构工具源码构建/Release、public live probes、真实第二轮 retry、完整交互下载链和真实 leaderboard writeback 未验证。
- `SEARCH_SUMMARY.md` 已重标为原始任务历史阶段 1/2026-08-20 初始调查快照，明确旧实现只用于追溯，不作为当前验收证据；当前状态以 REQUIREMENTS/PLAN/REVIEW/VALIDATION 为准。

### D. Draft PR body（最终交付版本；PR 仍保持 Draft）

~~~markdown
## Summary

- Remediate the eight review blockers while keeping `ecs.report/v1`, `ecs.compare/v1`, and `ecs.plan/v1` in place.
- Emit machine facts directly from network/media/route/backtrace/system producers; keep ECS-generated dynamic text as structured messages and raw external evidence unchanged.
- Structure retry/interference and NAT STUN-pool facts so one canonical JSON can be rendered in Chinese or English.

## Review mapping

- Message string-argument format contracts: closed; production-callsite zh/en contract tests prevent `%!` diagnostics.
- Probe reverse semantic bridges: closed for network/media/route/backtrace; direct producer tests and source audit cover stable IDs/enums.
- Retry canonical pollution: closed; interference/retry/attempt facts stay structured and are localized only by renderers.
- Plan wizard privacy: closed; v1 plans require `exposure` and boolean `reveal`, and ordinary run appends both authoritatively.
- DB-IP and NAT: closed; source ID is `dbip`, and STUN candidates are an ordered machine table.
- Summary/system: closed; legacy summary strings/fallbacks and duplicate system collection were removed.
- Documentation: schema/PLAN/REVIEW/VALIDATION are synchronized; `SEARCH_SUMMARY.md` is explicitly historical, and this body records the final pushed delivery while the PR remains Draft.

## Requirements 1–19

All 19 implementation requirements are satisfied in code/config/docs. Requirement 14 is implemented but verification is limited: the independent security workflow, non-required boundary, and manual Go pin are present; local Go1.26.5 quality/full/race passed, while local govulncheck reports five reachable standard-library vulnerabilities fixed in Go1.26.6 and the GitHub security workflow is not yet verified. Requirement 15 is implemented but verification is limited: the trigger excludes generated outputs and covers the real algorithm entry points; real leaderboard trigger/writeback was not run. External live probes, real second-round retry, full tool Release, and the complete interactive download chain remain unverified.

## Validation

- Default Go1.22.2 and Go1.26.5: full tests/race passed.
- Go1.26.5 `scripts/ci/check.sh`, shell fixtures, integration, seven-architecture ecs cross, submission corpus, and real local-only bilingual system render passed.
- Canonical JSON hashes remain unchanged across zh/en render; no `%!`, stable-key leakage, or legacy summary fields.

## Delivery state

- Final delivery SHA: `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44`; it is the current local, remote, and PR HEAD, and both push/pull_request CI runs are green.
- PR #6 remains OPEN and Draft, with CLEAN/MERGEABLE state. Keep the PR Draft as required; the security red item and external-validation limits require a later independent scan/run or maintainer decision. This task does not implicitly upgrade Go.
~~~

### 阶段 15 未验证边界

- 仍未完成的只是外部边界：当前 SHA 对应的 GitHub security workflow、七架构工具源码构建/Release、公网 live probes、真实第二轮 retry、完整交互下载链和真实 leaderboard writeback。前两轮返工原因、同一 Worker 执行记录、第三轮预推送主审、最终封存 push/PR body/CI 结果已如上保留。

因此当前最终封存提交 `7fb0b95ce3d4cb80a76998971cc5aa36659d9c44` 已位于 local/remote/PR HEAD，远端 push 和两套 CI 全绿；PR #6 仍为 OPEN、Draft。

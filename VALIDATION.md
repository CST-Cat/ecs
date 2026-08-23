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

所有临时目录均已用 `find ... -delete` 清理并确认不存在；本次 diff 精确限定为 `internal/probe/network.go`、`internal/probe/network_semantics_test.go`、`PLAN.md`、`REVIEW.md`、`VALIDATION.md` 五个允许文件。主 Agent 已完成阶段 14 完整复审并通过；阶段 15 仍为 TODO。本地 govulncheck 仍为 exit 3 的安全限制，不能称 security clean；GitHub security、最终 PR/远端同步和发布未验证，未推送。

## 阶段 15 待验证项

以下项目不能由阶段 14 本地证据替代，留待最终总审查和 PR 更新：

- GitHub Actions security workflow 在声明的 Go 1.26.5 runner 上的真实结果；
- 七架构工具的完整源码构建、归档、签名/attestation、Release 和 publish 流程；
- 公网第三方 live probes、真实第二轮 retry，以及完整交互式下载/安装/执行链；
- Go1.26.5 本地 quality gate 已通过；本地 govulncheck 仍 exit 3，5 个可达标准库漏洞均 fixed in Go 1.26.6，且 GitHub security workflow 尚未验证，因此不能称 security clean；安装器、工具布局 fixture、主程序 cross、integration 和双语二进制证据见上文。
- 阶段 15 的 base-to-candidate 全量 diff、远端 target/PR 同步、最终 SHA 一致性和 Draft PR 更新。

因此当前没有最终候选 SHA、最终 CI 全绿或已 push 的结论；PR #6 仍为 OPEN、Draft。

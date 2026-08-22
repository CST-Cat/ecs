# 执行计划

基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`

工作 Branch：`codex/architecture-machine-facts-cleanup`

状态：`TODO` / `IN PROGRESS` / `DONE` / `BLOCKED` / `REOPENED`。

> 本计划在当前工作区串行执行；按仓库协作规则直接在当前分支工作，不创建 Pull Request。远端分支已通过 fetch 建立并继续，验证证据集中记录在 `VALIDATION.md`，未执行的外部集成项明确列在 `REVIEW.md`。

## 当前状态与阶段记录

- **0 基线与仓库规范 — DONE**。目标：建立安全工作边界；输入：用户规范、main、README/CONTRIBUTING/CI；范围：任务文档；产出：`AGENTS.md`、`REQUIREMENTS.md`、Branch 与基准记录；验收：`AGENTS.md` 只含稳定纪律，基准固定为 `main@6e850393...`。
- **1 仓库搜索与事实固化 — DONE**。目标：把 18 项需求映射到真实实现；输入：仓库文件树/代码；范围：只读搜索；产出：`SEARCH_SUMMARY.md`；验收：已确认 app/config/model 大文件、report Localize、source-text i18n、runner retry 白名单、run.sh plan-file、leaderboard trigger 等真实现状。
- **2 `app.go` 行为保持式拆分 — DONE**。目标：命令职责显式化；输入：原 `internal/app/app.go`；范围：同一 `package app`，禁 Cobra/Command/DI；产出：run/render/list/config/doctor 等职责文件；验收：阶段 diff 基本是等量移动，未触碰其他 package。运行验证：由后续全仓 CI 一并覆盖。
- **3 唯一 `resolveRunConfig` — DONE**。目标：CLI+file+defaults 只有一个 resolver；输入：阶段 2；范围：run 配置解析；产出：`resolveRunConfig`，run 仅负责编排；验收：主审发现并返工 `--version` 早退顺序和 flag stderr 重复输出两项行为差异。运行验证：由后续全仓 CI 一并覆盖。
- **4 Compare JSON-only 契约 — DONE**。目标：输入固定 ECS JSON，`--format` 只表示 comparison output formats；输入：compare app/loader/tests；范围：help/命名/契约测试；产出：`outputFormats` 命名、JSON-only 测试；验收：MD/HTML 输入失败，合法 ECS JSON 不要求 `.json` 扩展，不存在 autodetect/parser。运行验证：当前 CI 中 `internal/compare` 已通过，完整 required 仍被阶段 8 中间态阻塞。
- **5 Structured comparison Notice — DONE（按新 beta 版本纪律修订）**。目标：删除 key+args 字符串编码/`ParseNotice`；输入：compare model/build/report；范围：comparison notice；产出：`Notice{Key,Args}`、renderer 直接渲染；**schema 版本保持 `ecs.compare/v1`，结构直接原地更新，不升 v2、不保留旧兼容层**；验收：结构化实现保留，只撤销此前自行升版决定，文档与测试同步保持 v1。
- **6 i18n 单向 lookup + parity — DONE**。目标：zh/en 并列 catalog；输入：i18n maps/tests；范围：静态 key lookup；产出：英文缺 key 返回 key，不回退中文；compare key 纳入 parity；验收：`lookup` 以 catalog membership 判断存在性，旧 fallback 测试反转。旧 probe source-text 层明确暂留到阶段 9。
- **7 `model.Message` 基础设施 — DONE**。目标：动态 ECS 文本具有语言无关机器语义；输入：model summary/fail、report Localize；范围：先迁 model 自生成 summary/failure，不迁 probe；产出：`Message{Key,Args}`、model message catalog、report `renderMessage`；`model.go` 不再 import i18n；验收：Message JSON round-trip、RedactedCopy 深拷贝及 Args IP 遮盖测试已写，阶段 diff 小而集中。
- **8 Runner 动态文本迁移 — DONE**。目标：runner 自生成 notices、skip/panic 等不再写 canonical Chinese；输入：阶段 7 Message；范围：runner→model 及展示边界；产出：runner notices/skip/panic 使用稳定 `Message{Key,Args}`，外部 panic/error/raw evidence 保持原样，进度标题通过 `TitleKey` 在 app/CLI 展示边界本地化，report markdown/html/txt 直接渲染 structured notices；验收：runner 无 i18n import 或源文本翻译，`PrivacyNoticeKey` 来自 ModuleDescriptor，`ecs.report/v1` 保持不变，相关 tests 已改为 key/args 契约。主审两次发现整文件替换夹带无关删除（`text.go`、`app/run.go`）并立即回退后以最小 diff 重做。真实 CI：PR run #349 的 unit、quality、integration、race、cross（7 架构）、submissions、Go 1.22 compat、required 全部 success。
- **9 Probe 动态文本迁移与删除 source-text translation — DONE**。目标：probe ECS 自生成文案改 stable key/Message；输入：`probe_text.go`、probe results；范围：不改第三方 stdout/stderr/raw evidence；产出：所有 Builtins 的 ECS-owned 标题、方法学、字段、表格、摘要、说明和动态状态使用 stable key/Message；删除 exact/regex/template source-text translation；验收：无 `i18n.Text` 反向识别入口，built-in title/descriptor 回归测试通过，关键 probe 双语从同一 JSON 渲染。
- **10 删除 report Localize clone — DONE**。目标：终结 canonical Chinese→localized copy；输入：阶段 7–9；范围：report localization；产出：renderer 直接读取 machine fields/message，在展示边界按 stable key 查当前语言；删除 `probe_text.go`、`report.Localize` 及旧 source-text catalogs；验收：同一 JSON 可 `render --lang zh|en`，不存在 `report.Localize`/反向文本翻译入口。
- **11 Report render-all-before-write — DONE**。目标：多格式输出原子成组；输入：writer；范围：render/write 顺序；产出：主报告和 comparison 均先生成全部格式 bytes 后写文件；验收：任一 renderer/未知格式失败时不留下部分文件，JSON 保持 canonical。
- **12 `ecs plan --json` — DONE**。目标：Go 成为 run planning 唯一控制面；输入：`resolveRunConfig` + ModuleDescriptor；范围：app plan/schema/tests/docs；产出：`ecs.plan/v1`、modules、required tools、外联语义、Ookla/staging/corpus 机器计划；验收：plan 复用 `resolveRunConfig` 和同一 descriptor 选择顺序，JSON 不含本地化散文，resolver/staging 回归测试通过。
- **13 `run.sh` 消除第二套 planner — DONE**。目标：Shell 仅 bootstrap/checksum/plan consumer/tool staging/run；输入：阶段 12；范围：`run.sh`；产出：删除 shell 对 profile/only/skip/config/exposure/module manifest 的业务解释，保留 staging/Ookla；验收：`sh -n run.sh` 与 `scripts/run_test.sh` 通过，fixture 消费 `ecs.plan/v1`。
- **14 Retry policy 收回 ModuleDescriptor — DONE**。目标：删除 runner 模块 ID 白名单；输入：descriptor+runner；范围：最小 metadata；产出：`RetryOnInterference bool` 并写入 `ecs.plan/v1`；验收：cpu/zstd/npb/memory/crypto/disk 集合行为不变，任意 runner ID 由 descriptor policy 决定，config/runner/plan 回归测试通过。
- **15 拆分 `config.go` — DONE**。目标：职责稳定后物理模块化；输入：阶段 12/14 后 config；范围：同 package；产出：`types.go`、`defaults.go`、`file.go`、`validate.go`、`catalog.go`、`network.go` 与既有 endpoints/exposure/modules 边界；验收：`internal/config`、`app`、`probe`、`runner` 测试通过，ModuleDescriptor 仍为模块元数据唯一事实源。
- **16 拆分 `model.go` — DONE**。目标：Message 模型稳定后物理拆分；输入：阶段 7–10；范围：同 package；产出：`types.go`、`result.go`、`message.go`、`evidence.go`、`failure.go`、`summary.go`、`redact.go`、`format.go`；验收：model 无 i18n，`model`、`report`、`compare`、`probe`、`runner`、`app` 测试通过，JSON/脱敏行为保持。
- **17 `tools/lock.json` — DONE**。目标：统一内部供应链机器事实；输入：common/build/release 固定值；范围：内部构建链；产出：`tools/lock.json`（架构、工具 tag/commit/source、STREAM、NPB、语料摘要/尺寸/拼接顺序）；`common.sh`、工具构建、语料构建和发布校验从锁读取；验收：`scripts/lock_test.sh`、工具 stage/package 回归与全部 shell 语法通过，`run.sh` 不读取仓库 lock。
- **18 Staticcheck cache 绑定 devtools lock — DONE**。目标：go.mod/go.sum 变化自动重建；输入：check/devtools；范围：缓存判定；产出：`.devtools-bin/staticcheck.lock`（由 `devtools/go.mod` + `go.sum` 摘要派生）；验收：`scripts/devtools_cache_test.sh` 证明匹配可复用、lock 变化失效，质量检查接入该门禁。
- **19 拆 `build_tools.sh` — DONE**。目标：按工具拆 shell，不造框架；输入：阶段 17；范围：`scripts/tools/*.sh` + 顶层编排；产出：sysbench/zstd/NPB/OpenSSL/STREAM/fio/iperf3/NextTrace/ping 的独立构建与 smoke 函数，顶层仅保留取源、静态校验、许可和 manifest 编排；验收：无 ToolBuilder/provider/plugin，架构/产物布局保持，全部新增脚本通过 `bash -n`。
- **20 CI/Security 边界 — DONE**。目标：quality 语义准确，security 独立；输入：workflows/check；范围：CI；产出：quality 注释仅描述静态/格式/契约检查，新增独立 `security.yml`，按周或手动运行固定版本 `govulncheck`；验收：未接入 `ci.required`，仍读取人工固定的 `.go-version`，未引入自动升级。
- **21 Leaderboard trigger — DONE**。目标：消除写回 baseline 自触发并覆盖真实算法入口；输入：workflow/score；范围：paths；产出：排除 `submissions/baseline.json` 与 `internal/score/embedded/baseline.json` 两个生成输出，同时显式监听 leaderboard 脚本、baseline/score/submission/tier 算法、模块评分元数据和模型入口；验收：输出文件不自触发，提交目录与真实算法入口仍触发。
- **22 Installer/docs parity — DONE**。目标：保留显式 `--with-benchmarks` 并修陈旧文案；输入：install/README/README_EN/docs；范围：脚本与文档；产出：安装器默认使用 `CST-Cat/ecs`，镜像/派生仓库仍可用 `ECS_REPOSITORY` 覆盖，中英文安装说明与 examples 同步；验收：persistent install 仍仅在显式 `--with-benchmarks` 时安装 sysbench/fio/iperf3，`sh -n install.sh` 与安装行为回归通过。
- **23 Probe 小范围去重 — DONE**。目标：只抽真实重复；输入：route/backtrace/external commands；范围：NextTrace adapter/有限 command helper；产出：保留 route/backtrace 共用的 NextTrace adapter，并将多个探针实际共用的版本读取、binary SHA-256、ANSI/NUL 输出清洗和错误尾部截断集中到 `tool_execution.go`；验收：无 GenericBenchmarkRunner/provider/plugin，`go test ./internal/probe` 通过。
- **24 完整 Branch diff 与总审查 — DONE**。目标：全任务回归；输入：Branch vs baseline、CI/docs；范围：全仓；产出：`REVIEW.md`、`VALIDATION.md`、必要返工与临时文档清理；验收：每项实质 diff 可追溯，无意外框架/兼容层/无关重写，测试证据真实，无法验证明确标注。
- **25 分支交付 — IN PROGRESS（已获用户授权）**。目标：用阶段 24 完全相同状态交付；输入：通过总审查的 HEAD；范围：当前 tracking 分支；产出：准确提交并推送当前分支，不创建或更新 PR；验收：远端分支包含该最终提交，工作区干净，推送前后差异一致。

## 计划调整记录

- 早期计划曾记录 Draft PR #6；按当前仓库规则不创建或更新 PR，阶段 25 改为 N/A。
- 阶段 5 最初因 structured notices 改变 JSON 字段类型而自行将 comparison schema 升到 `ecs.compare/v2`。用户随后明确 beta 阶段不以兼容性驱动版本升级；**结构化 Notice 设计不回滚，只把版本标识恢复并固定为 `ecs.compare/v1`，直接原地更新 v1 契约。**
- 同一规则适用于阶段 8 的主报告 Message/notices 变化：保留新结构，`ecs.report/v1` 不升 v2，不为旧结构保留兼容 schema。
- 阶段 7 采用渐进迁移：新增 `Summary.Messages` / `Result.SummaryMessages`，旧 presentation string 暂作任务内迁移字段；完成结构化迁移后阶段 10 直接删除 Localize/旧 presentation 路线，不以兼容性为理由长期保留。

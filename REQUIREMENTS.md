# 当前任务需求

基准 Branch：`main`

基准 commit：`6e85039301fc817e8c4a290ffb9b111fc37e55a8`

工作 Branch：`codex/architecture-machine-facts-cleanup`

## 1. Compare：JSON 是唯一可比较数据

- `ecs compare` 输入只接受 ECS JSON report；Markdown/HTML 永远只是展示产物，不参与反解析比较。
- 保留 `reporter.LoadJSONForComparison(path)` 或等价 JSON-only 唯一入口，不按扩展名做权威判断。
- `--format=json,md,html` 明确表示 comparison output formats，内部命名避免与输入格式混淆。
- 禁止输入自动识别、Markdown parser、HTML parser、报告类型猜测。
- comparison notice 改为结构化语义（如 `Notice{Key, Args}`），renderer 直接通过 i18n 渲染，不再编码字符串后反解析。

## 2. 拆分 `internal/app/app.go`

- 保持单一 `package app`，不引 Cobra、不造 Command interface、不引 DI framework。
- 让 command 与 run 内部阶段显式化，至少形成 `app.go` dispatch、`run.go` orchestration、`run_config.go` resolver，以及 render/list/doctor/config/submit/leaderboard 等职责文件；已独立的 compare/baseline/interactive 继续保持。
- 抽出可复用的 `resolveRunConfig(args)` 或等价 resolver，供 `run` 与后续 `plan` 共用。
- 首阶段拆分以不改行为为原则。

## 3. 重建 i18n 为单向 key → 当前语言

- 保留 `Main()` 的早期语言选择。
- 静态 UI 文案用 stable key，默认中文直接取 zh，`--lang=en` 直接取 en。
- 删除 source-text translation / exact sentence match / regex template 逆向识别思想。
- 中英文 key set 必须一致；英文缺 key 不得偷偷回退中文，应显式暴露并由测试失败捕获。

## 4. ECS 自生成动态文本结构化

- 动态 ECS 文本保存 `Message{Key, Args}` 或等价机器语义，不保存“canonical Chinese”。
- `model` 不 import i18n；Message 不知道语言。
- 外部程序 stdout/stderr/raw evidence 原样保存，绝不翻译。
- 迁移完成后删除 `probe_text.go`、模板式翻译系统和 `report/localize.go` / copy-localize 路线。
- 保持 `ecs render report.json --lang ...` 能从同一 JSON 直接重渲染目标语言。

## 5. 不强制引入完整 presentation package

- i18n 重做后优先使用 renderer + 小型 shared helpers。
- 不创建大而全的 `presentation.ReportView`，除非出现新的必要证据。

## 6. 新增 `ecs plan --json`

- 必须复用 `resolveRunConfig`，不得另造 planner resolver。
- plan 输出 Shell 准备工具所需的机器事实，包括最终模块选择和工具 staging 决策（例如 Ookla 是否需要）。

## 7. `run.sh` 只保留 bootstrap / staging / execution

- Shell 不再自行解释 profile/only/skip/config/exposure/module manifest 形成第二套控制面。
- `run.sh` 下载并校验 ecs → 调 `ecs plan --json` → 根据 plan 准备工具 → 调 `ecs run`。
- 暂时保留工具 staging 和 Ookla 自动准备；本阶段不强行把所有下载搬进 Go。

## 8. Retry policy 收回 `ModuleDescriptor`

- runner 不再维护 cpu/zstd/npb/memory/crypto/disk 等模块 ID retry 白名单。
- descriptor 用最小必要字段（优先 `RetryOnInterference bool`）表达策略；不引复杂策略对象，除非存在第二种真实策略。

## 9. 拆分 `internal/config/config.go`

- 在职责稳定后按 types/defaults/file/validate/catalog/endpoints/exposure/modules 等实际边界物理拆分。
- 保持现有 `ModuleDescriptor` 作为模块机器元数据事实源。
- 不引新 package 级框架。

## 10. 拆分 `internal/model/model.go`

- 与 Message/i18n 数据模型稳定后再拆。
- 目标边界包括 report/result/message/evidence/failure/summary/redact 等，按实际代码决定。
- `model` 不能依赖 i18n。

## 11. Report 层变薄

- 删除 localize 深拷贝与 source-text translation 路线。
- Report 保留 JSON encode/decode、Markdown、HTML、terminal、atomic writer、sanitize 和必要 shared formatting helpers。
- 多格式写文件改为“全部 render 成 bytes 成功后再写”，避免半套报告落盘。

## 12. Tools 供应链机器事实统一

- 建立 `tools/lock.json` 或等价 JSON，记录 architectures、tools、version/tag/commit、source、corpus SHA、corpus bytes 等供应链固定事实。
- 构建脚本可依赖 jq；公网 bootstrap `run.sh`/`install.sh`/`compare.sh` 不依赖该 lock，保留少量自包含 `uname → arch` 重复。
- `devtools/go.mod` / `go.sum` hash 进入 staticcheck binary cache 判定，lock 变化才重编。

## 13. 拆 `build_tools.sh`，但不造工具构建框架

- 建议 `scripts/tools/*.sh` 按工具拆实现，顶层脚本只读取 lock、顺序调用、组装 stage。
- 禁止 `ToolBuilder interface`、generic provider/plugin framework 等过度抽象。

## 14. CI / Security 重新定位

- `ci.yml` quality 注释去掉不准确的“源码安全”。
- `govulncheck` 如加入，先放独立 `security.yml`，支持 schedule + workflow_dispatch；不立刻接 `ci.required`。
- `.go-version` 保持人工固定，不引自动升级/同步门禁。

## 15. 修 Leaderboard 自触发与 trigger 覆盖

- 修复 workflow 写回 `internal/score/embedded/baseline.json` 后再次触发自己的问题。
- trigger 不只按目录猜输入，应覆盖 baseline algorithm 的真实入口文件。

## 16. 保留 `install.sh --with-benchmarks`

- 该 opt-in persistent install 与 `run.sh` ephemeral staging 可以共存，不因本次重构删除。
- 修正文档/脚本中的陈旧 repository 文案或相关不一致。

## 17. Probe 不做大改

- 只做高收益小去重，例如 route/backtrace 共享 NextTrace adapter、有限 external command helper（binary lookup/timeout/version/SHA/stdout/stderr）。
- 禁止 GenericBenchmarkRunner 等把不同探针语义强行抽象统一。

## 18. 明确降级/撤回的旧建议

- 不强上完整 presentation package。
- 不立刻删除 Ookla 自动 staging。
- 不删除 `install --with-benchmarks`。
- 暂不拆 termcolor visual package。
- docs 只生成机械事实，不追求全自动生成。
- 不在本任务内立即升级 Go 1.26.7。
- `govulncheck` 不立即成为 required gate。

## 19. Beta 阶段版本纪律

- 本项目当前处于 beta 阶段，代码、JSON schema、内部 API 或其他契约发生 breaking change 时，允许直接修改当前 `v1` 契约。
- 不因为代码结构、字段类型或语义发生 breaking change 就把 `ecs.report/v1`、`ecs.compare/v1` 等版本号升级为 `v2`。
- 不为旧结构保留兼容层、双 schema、迁移适配器或 fallback；本任务优先直接删除旧设计并让当前 `v1` 表示最新 beta 契约。
- 除非用户以后明确要求，Agent 不得自行升级这些版本标识。

## 目标架构原则

- JSON 是机器事实；compare 只相信 JSON。
- app 是功能编排，不是巨型函数。
- i18n 是 key → 当前语言的单向生成，不存在中文 → 英文反翻。
- Shell 不拥有业务决策权。
- 继续 Less is more：删除补偿层，不用新框架替代旧复杂度。

## 环境限制

当前可用 GitHub 连接器没有 `exec`/Sol High Worker Subagent 接口，因此无法真实执行“每阶段由唯一 Sol High Worker Subagent 通过 exec 完成”的要求。主 Agent 不得冒充该能力；其余阶段仍严格串行、实际查看仓库状态和 diff，并通过 GitHub Actions 等可获得的真实证据进行验证。

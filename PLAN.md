# 执行计划

基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`

工作 Branch：`codex/architecture-machine-facts-cleanup`

状态标记：`TODO` / `IN PROGRESS` / `DONE` / `BLOCKED` / `REOPENED`。

> 环境说明：当前无 `exec`/Sol High Worker Subagent 能力，无法满足对应 Worker 分发要求；不得冒充。其余阶段按本计划严格串行，由主 Agent 实际读取仓库、修改、审查 diff，并尽可能以 GitHub Actions 取得真实测试证据。

## 0. 基线与仓库规范建立 — DONE

- 目标：确认基准、读取仓库约束、建立工作 Branch 与任务文档。
- 输入：用户规范、`main`、README、CONTRIBUTING、CI。
- 范围：仅工作流程和任务 Markdown。
- 产出：`AGENTS.md`、`REQUIREMENTS.md`、本 `PLAN.md`；记录基准 commit。
- 验收：不修改业务代码；`AGENTS.md` 不包含具体业务需求；任务需求独立保存。

## 1. 仓库搜索与现状证据固化 — IN PROGRESS

- 目标：定位 app/i18n/model/report/config/runner/scripts/workflows 的真实实现与依赖，避免按设想盲改。
- 输入：当前 Branch 文件树与代码搜索结果。
- 范围：只读搜索；维护 `SEARCH_SUMMARY.md`。
- 产出：关键文件、符号、重复事实源、删除候选和风险清单。
- 验收：18 项需求均能映射到真实文件；不凭记忆填写供应链或 workflow 事实。

## 2. `app.go` 行为保持式拆分

- 目标：让 command 与 run orchestration 显式化，不改变 CLI 行为。
- 输入：`internal/app/app.go` 与现有 app tests。
- 范围：`internal/app`；保持单 package，不引 Cobra/Command/DI。
- 产出：`run.go`、`run_config.go`、render/list/doctor/config/submit/leaderboard 等职责文件；`app.go` 只保留 dispatch/bootstrap/help 等合理内容。
- 验收：现有 app 测试通过；无 CLI 行为变化；diff 主要为移动/重排和最小必要抽取。

## 3. 唯一 `resolveRunConfig` 边界

- 目标：把 CLI + file + defaults → final runtime config 固定为可复用 resolver。
- 输入：阶段 2 结果。
- 范围：run config 解析与调用边界。
- 产出：`resolveRunConfig(args)` 或等价稳定 API；run 调用该 resolver。
- 验收：无第二套配置解析；测试覆盖 CLI override、config file、defaults、selection/exposure 关键路径。

## 4. Compare JSON-only 契约与 outputFormats 命名

- 目标：明确 compare 只读 ECS JSON，同时消除输入/输出 format 歧义。
- 输入：`internal/app/compare.go`、report comparison loader、docs/tests。
- 范围：compare CLI/help/tests/docs 和必要内部命名；不按扩展名硬判。
- 产出：JSON-only 输入契约；`outputFormats` 或等价命名；禁止 MD/HTML 输入识别的测试。
- 验收：合法 ECS JSON 可比较；非法/非 JSON 失败；MD/HTML 不进入解析路线；现有 output formats 仍可用。

## 5. Structured comparison Notice

- 目标：消除 notice 的字符串编码→ParseNotice→再拆解翻译路径。
- 输入：compare model/report renderer/i18n。
- 范围：comparison notice 数据模型与渲染。
- 产出：结构化 `Notice{Key, Args}` 或等价；renderer 直接渲染 key+args。
- 验收：无反向解析 notice；JSON/MD/HTML compare 输出语义保持；测试覆盖参数化 notice。

## 6. i18n 单向 key lookup 与 key parity

- 目标：静态 UI 文案彻底变成 key → 当前语言。
- 输入：`internal/i18n`、Main language bootstrap、相关测试。
- 范围：i18n 核心，不迁移全部动态 model 文本。
- 产出：zh/en key parity 检查；英文缺失显式暴露，不偷偷中文 fallback；保留早期 Set language。
- 验收：zh/en key set 相等；缺 key 测试失败或返回 key；无 source-text 翻译参与静态 UI。

## 7. `Message{Key,Args}` 数据模型

- 目标：为 ECS 自生成动态文本建立语言无关机器语义。
- 输入：model、runner、report schema 与 render 需求。
- 范围：model 新 message 类型及 JSON contract；model 不 import i18n。
- 产出：Message 类型与渲染 helper；必要 schema/testdata 更新。
- 验收：Message 可 JSON round-trip；同一 JSON 可按 zh/en 渲染；外部 raw evidence 不变化。

## 8. 迁移 runner 动态文本

- 目标：先迁 runner 生成的 retry/failure/summary 等动态 ECS 文本。
- 输入：阶段 7 Message。
- 范围：runner → model 消息；不改 probe raw output。
- 产出：runner 不写死 canonical Chinese 作为可翻译语义。
- 验收：runner tests、report render tests 覆盖两种语言；外部证据原样保留。

## 9. 迁移 probe 动态文本并删除 source-text translation

- 目标：完成 probe 自生成文本迁移，删除逆向文本识别系统。
- 输入：`probe_text.go`、probe/result 建模、阶段 7/8。
- 范围：ECS 自生成文本；不翻译第三方 stdout/stderr/raw evidence。
- 产出：删除 `probe_text.go`、regex/source-text template translation 及相关死代码/tests。
- 验收：搜索无 source-text translation 入口；关键 probe 渲染 zh/en 正确；raw evidence bytes/strings 语义不变。

## 10. 删除 report Localize clone 路线并收薄 report

- 目标：终结 canonical Chinese → localized copy。
- 输入：`report/localize.go`、copy tests、renderers。
- 范围：report localization pipeline。
- 产出：删除 localize 深拷贝；renderer 在需要人类文案时直接从 Message/i18n 渲染。
- 验收：`ecs render report.json --lang zh|en` 都从同一 JSON 工作；无 report.Localize 依赖。

## 11. Report render-all-before-write

- 目标：避免多格式输出部分成功后留下半套文件。
- 输入：report writer。
- 范围：多格式 render/write 顺序。
- 产出：先全部 render bytes，全部成功后才 atomic write。
- 验收：任一 renderer 失败时不产生本批次部分文件；成功路径不回归。

## 12. 新增 `ecs plan --json`

- 目标：让 Go 成为 run planning 唯一控制面。
- 输入：阶段 3 resolver、ModuleDescriptor/tool metadata。
- 范围：app plan command、JSON plan schema/tests/docs。
- 产出：最终 modules、tool requirements、Ookla/staging 决策等机器计划；复用 resolver。
- 验收：plan 与 run 对同一参数产生一致选择；无第二 resolver；JSON 稳定可由 shell 消费。

## 13. `run.sh` 消除第二套 CLI/planner

- 目标：Shell 只做 bootstrap、checksum、plan 消费、tool staging、run。
- 输入：阶段 12 `ecs plan --json`。
- 范围：`run.sh` 及 shell contract tests/docs。
- 产出：删除 shell 对 profile/only/skip/config/exposure/module manifest 的业务解释；保留 staging/Ookla。
- 验收：shell 不再拥有模块选择事实；常用参数透传；`sh -n run.sh` 与相关 contract tests 通过。

## 14. Retry policy 收回 `ModuleDescriptor`

- 目标：消除 runner 模块 ID retry 白名单。
- 输入：config ModuleDescriptor、runner retry logic。
- 范围：最小 metadata 字段与 runner 读取。
- 产出：优先 `RetryOnInterference bool`；删除 runner 白名单。
- 验收：retry 模块集合与现有行为一致；新增 descriptor test 防止副本回归。

## 15. 拆分 `config.go`

- 目标：职责稳定后物理模块化 config。
- 输入：阶段 12/14 稳定后的 config API。
- 范围：`internal/config` 文件拆分；不新建 package。
- 产出：types/defaults/file/validate/catalog/endpoints/exposure/modules 等按真实职责整理。
- 验收：公开/包内行为不变；ModuleDescriptor 仍为单一模块事实源；config tests 通过。

## 16. 拆分 `model.go`

- 目标：Message 模型稳定后按领域物理拆分。
- 输入：阶段 7–10 后 model。
- 范围：`internal/model` 文件组织；不改变 schema 除前述 Message 必要变化。
- 产出：report/result/message/evidence/failure/summary/redact 等合理文件。
- 验收：model 不依赖 i18n；JSON round-trip 与 report tests 通过；无机械重复定义。

## 17. 建立 `tools/lock.json`

- 目标：统一构建链供应链机器事实。
- 输入：`common.sh`、build scripts、release metadata、上游固定信息。
- 范围：内部构建链；公网 bootstrap 不依赖 lock。
- 产出：architectures/tools/version/source/corpus SHA/bytes 等 JSON lock；消费方改为读 lock。
- 验收：所有 lock 值能追溯现有仓库/上游证据；不存在新增猜测值；bootstrap 自包含 arch mapping 保留。

## 18. Staticcheck cache 绑定 devtools lock

- 目标：`devtools/go.mod/go.sum` 变化时自动重建 staticcheck。
- 输入：`scripts/ci/check.sh` 或相关 helper。
- 范围：devtool cache key/build decision。
- 产出：module lock hash 参与缓存判定。
- 验收：binary 存在但 lock 变化会重建；lock 不变可复用；quality contract test/脚本静态检查通过。

## 19. 拆 `build_tools.sh`，不造框架

- 目标：按工具拆 shell 实现，顶层只编排。
- 输入：阶段 17 lock 与现有 `build_tools.sh`。
- 范围：`scripts/tools/*.sh` 与顶层 builder。
- 产出：每工具独立脚本，顶层顺序调用与 stage assemble。
- 验收：无 ToolBuilder/provider/plugin 抽象；七架构定义与产物布局保持；shell syntax/check tests 通过。

## 20. CI / Security 边界清理

- 目标：修正 quality 语义并建立非 required 的独立安全扫描。
- 输入：ci/release/check.sh、Go toolchain约束。
- 范围：workflow 与必要 docs。
- 产出：quality 注释不再称“源码安全”；`security.yml` schedule + workflow_dispatch；不接 `ci.required`；不自动改 `.go-version`。
- 验收：workflow YAML 有效；security 独立；普通 CI DAG 不增加安全 required 依赖。

## 21. Leaderboard trigger 修复

- 目标：消除 workflow 写回 baseline 后的自触发，并覆盖真实 baseline algorithm 入口。
- 输入：`leaderboard.yml`、score/baseline 入口文件。
- 范围：workflow paths/paths-ignore 或等价 trigger。
- 产出：baseline 输出路径不触发自身；算法入口变化能触发。
- 验收：静态 trigger 逻辑与真实写回/入口一一对应；不误删 submissions 触发。

## 22. Installer/docs parity 与陈旧文案

- 目标：保留 `--with-benchmarks`，修正与 repository/行为不一致的文案。
- 输入：install.sh、README/README_EN/docs。
- 范围：不删 persistent benchmark install。
- 产出：中英文文档与脚本 help 一致。
- 验收：`--with-benchmarks` 仍可见且 opt-in；无本任务引入的旧架构描述。

## 23. Probe 小范围高收益去重

- 目标：只处理明确重复的 NextTrace adapter / external command helper，不扩大架构。
- 输入：route/backtrace 与外部命令适配代码。
- 范围：仅真实重复；禁止 GenericBenchmarkRunner。
- 产出：最小共享 helper，保持各 probe 语义独立。
- 验收：diff 小且可解释；probe tests/集成 contract 不回归；无泛型构建框架。

## 24. 全仓一致性、完整 diff 与回归总审查

- 目标：从完整任务视角验收所有需求和非目标。
- 输入：工作 Branch 相对基准的全部 diff、CI、docs。
- 范围：全仓只读审查；必要返工则重新打开对应阶段。
- 产出：`REVIEW.md`、`VALIDATION.md`；清理无长期价值临时 Markdown。
- 验收：每个实质 diff 可追溯到 REQUIREMENTS；无意外兼容层/框架/无关重写；能运行的测试有真实证据，不能运行的明确未验证。

## 25. PR 交付

- 目标：以经总审查的完全相同状态创建 PR。
- 输入：阶段 24 通过的 Branch HEAD。
- 范围：PR metadata，不再做代码修改。
- 产出：PR，描述需求映射、阶段结果、测试、限制。
- 验收：PR diff 与总审查状态一致；若 PR 后再改代码则重新打开阶段 24。

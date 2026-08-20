# 执行计划

基准：`main@6e85039301fc817e8c4a290ffb9b111fc37e55a8`

工作 Branch：`codex/architecture-machine-facts-cleanup`

状态：`TODO` / `IN PROGRESS` / `DONE` / `BLOCKED` / `REOPENED`。

> 环境限制：当前没有 `exec`/Sol High Worker Subagent 接口，不能真实执行该项要求，也不得用其他能力冒充。当前容器无法解析 `github.com`，不能 clone 仓库；Draft PR #6 暂未通过连接器取得 Actions run。因此已完成阶段若无外部 CI 证据，运行测试统一标记为“未验证（执行环境限制）”。所有阶段仍串行推进，并由主 Agent 查看实际文件与阶段 diff。

## 当前状态与阶段记录

- **0 基线与仓库规范 — DONE**。目标：建立安全工作边界；输入：用户规范、main、README/CONTRIBUTING/CI；范围：任务文档；产出：`AGENTS.md`、`REQUIREMENTS.md`、Branch 与基准记录；验收：`AGENTS.md` 只含稳定纪律，基准固定为 `main@6e850393...`。
- **1 仓库搜索与事实固化 — DONE**。目标：把 18 项需求映射到真实实现；输入：仓库文件树/代码；范围：只读搜索；产出：`SEARCH_SUMMARY.md`；验收：已确认 app/config/model 大文件、report Localize、source-text i18n、runner retry 白名单、run.sh plan-file、leaderboard trigger 等真实现状。
- **2 `app.go` 行为保持式拆分 — DONE**。目标：命令职责显式化；输入：原 `internal/app/app.go`；范围：同一 `package app`，禁 Cobra/Command/DI；产出：run/render/list/config/doctor 等职责文件；验收：阶段 diff 基本是等量移动，未触碰其他 package。运行验证：未验证。
- **3 唯一 `resolveRunConfig` — DONE**。目标：CLI+file+defaults 只有一个 resolver；输入：阶段 2；范围：run 配置解析；产出：`resolveRunConfig`，run 仅负责编排；验收：主审发现并返工 `--version` 早退顺序和 flag stderr 重复输出两项行为差异。运行验证：未验证。
- **4 Compare JSON-only 契约 — DONE**。目标：输入固定 ECS JSON，`--format` 只表示 comparison output formats；输入：compare app/loader/tests；范围：help/命名/契约测试；产出：`outputFormats` 命名、JSON-only 测试；验收：MD/HTML 输入失败，合法 ECS JSON 不要求 `.json` 扩展，不存在 autodetect/parser。运行验证：未验证。
- **5 Structured comparison Notice — DONE**。目标：删除 key+args 字符串编码/`ParseNotice`；输入：compare model/build/report；范围：comparison notice；产出：`Notice{Key,Args}`、renderer 直接渲染、schema 升 `ecs.compare/v2`、schema 文档同步；验收：阶段仅 8 个相关文件，`build.go` 仅 3 增 3 删，无无关重写。运行验证：未验证。
- **6 i18n 单向 lookup + parity — DONE**。目标：zh/en 并列 catalog；输入：i18n maps/tests；范围：静态 key lookup；产出：英文缺 key 返回 key，不回退中文；compare key 纳入 parity；验收：`lookup` 以 catalog membership 判断存在性，旧 fallback 测试反转。旧 probe source-text 层明确暂留到阶段 9。运行验证：未验证。
- **7 `model.Message` 基础设施 — DONE**。目标：动态 ECS 文本具有语言无关机器语义；输入：model summary/fail、report Localize；范围：先迁 model 自生成 summary/failure，不迁 probe；产出：`Message{Key,Args}`、model message catalog、report `renderMessage`；`model.go` 不再 import i18n；验收：Message JSON round-trip、RedactedCopy 深拷贝及 Args IP 遮盖测试已写，阶段 diff 小而集中。运行验证：未验证。
- **8 Runner 动态文本迁移 — IN PROGRESS**。目标：runner 自生成 notices、skip/panic/retry 等不再写 canonical Chinese；输入：阶段 7 Message；范围：runner→model；产出：结构化 messages，外部错误/raw evidence 保持原样；验收：runner 无 i18n import 或源文本生成，相关 tests 更新。
- **9 Probe 动态文本迁移与删除 source-text translation — TODO**。目标：probe ECS 自生成文案改 stable key/Message；输入：`probe_text.go`、probe results；范围：不改第三方 stdout/stderr/raw evidence；产出：删除 exact/regex/template source-text translation；验收：无 `i18n.Text` 反向识别入口，关键 probe 双语从同一 JSON 渲染。
- **10 删除 report Localize clone — TODO**。目标：终结 canonical Chinese→localized copy；输入：阶段 7–9；范围：report localization；产出：renderer 直接读 machine fields/message；验收：同一 JSON 可 `render --lang zh|en`，不存在 `report.Localize`。
- **11 Report render-all-before-write — TODO**。目标：多格式输出原子成组；输入：writer；范围：render/write 顺序；产出：先全部 render bytes 后写文件；验收：任一 renderer 失败时不留下部分文件。
- **12 `ecs plan --json` — TODO**。目标：Go 成为 run planning 唯一控制面；输入：`resolveRunConfig` + ModuleDescriptor；范围：app plan/schema/tests/docs；产出：modules、required tools、Ookla/staging 等机器计划；验收：与 run 同 resolver、同选择结果，无第二 planner。
- **13 `run.sh` 消除第二套 planner — TODO**。目标：Shell 仅 bootstrap/checksum/plan consumer/tool staging/run；输入：阶段 12；范围：`run.sh`；产出：删除 shell 对 profile/only/skip/config/exposure/module manifest 的业务解释，保留 staging/Ookla；验收：相关 shell contract/syntax 覆盖。
- **14 Retry policy 收回 ModuleDescriptor — TODO**。目标：删除 runner 模块 ID 白名单；输入：descriptor+runner；范围：最小 metadata；产出：优先 `RetryOnInterference bool`；验收：retry 模块集合行为不变，descriptor 成为唯一事实源。
- **15 拆分 `config.go` — TODO**。目标：职责稳定后物理模块化；输入：阶段 12/14 后 config；范围：同 package；产出：types/defaults/file/validate/catalog/endpoints/exposure/modules 等真实边界；验收：行为不变、ModuleDescriptor 单一事实源。
- **16 拆分 `model.go` — TODO**。目标：Message 模型稳定后物理拆分；输入：阶段 7–10；范围：同 package；产出：report/result/message/evidence/failure/summary/redact 等文件；验收：model 无 i18n，JSON round-trip/现有行为保持。
- **17 `tools/lock.json` — TODO**。目标：统一内部供应链机器事实；输入：common/build/release 固定值；范围：内部构建链；产出：architectures/tools/version/source/corpus SHA/bytes；验收：值均可追溯，不让公网 bootstrap 依赖 lock。
- **18 Staticcheck cache 绑定 devtools lock — TODO**。目标：go.mod/go.sum 变化自动重建；输入：check/devtools；范围：缓存判定；产出：module lock hash；验收：binary 存在但 lock 变化仍重建，未变化可复用。
- **19 拆 `build_tools.sh` — TODO**。目标：按工具拆 shell，不造框架；输入：阶段 17；范围：`scripts/tools/*.sh` + 顶层编排；产出：工具独立脚本；验收：无 ToolBuilder/provider/plugin，架构/产物布局保持。
- **20 CI/Security 边界 — TODO**。目标：quality 语义准确，security 独立；输入：workflows/check；范围：CI；产出：移除“源码安全”错误注释，`security.yml` schedule+manual，非 required；验收：不接 `ci.required`，不自动升级 `.go-version`。
- **21 Leaderboard trigger — TODO**。目标：消除写回 baseline 自触发并覆盖真实算法入口；输入：workflow/score；范围：paths；产出：精确 trigger；验收：输出文件不自触发，submissions 与算法入口仍触发。
- **22 Installer/docs parity — TODO**。目标：保留显式 `--with-benchmarks` 并修陈旧文案；输入：install/README/README_EN/docs；范围：脚本与文档；产出：中英文行为一致；验收：persistent install 仍 opt-in，不误删功能。
- **23 Probe 小范围去重 — TODO**。目标：只抽真实重复；输入：route/backtrace/external commands；范围：NextTrace adapter/有限 command helper；产出：小型 helper；验收：无 GenericBenchmarkRunner、无过度抽象。
- **24 完整 Branch diff 与总审查 — TODO**。目标：全任务回归；输入：Branch vs baseline、CI/docs；范围：全仓；产出：`REVIEW.md`、`VALIDATION.md`、必要返工与临时文档清理；验收：每项实质 diff 可追溯，无意外框架/兼容层/无关重写，测试证据真实，无法验证明确标注。
- **25 PR 最终交付 — TODO**。目标：用阶段 24 完全相同状态交付；输入：通过总审查的 HEAD；范围：PR metadata；产出：更新 Draft PR #6 或最终 PR；验收：PR diff 与总审查一致，后续若再改代码必须重开阶段 24。

## 计划调整记录

- 为取得可能的 PR CI 证据，提前创建 Draft PR #6；它不是“已交付”状态，最终仍由阶段 24/25 验收。
- Compare notices 从 JSON string 改 object 属于不兼容 schema 变化，因此阶段 5 将 comparison schema 从 `ecs.compare/v1` 升至 `ecs.compare/v2`，而不是在同版本下偷偷变更字段类型。
- 阶段 7 采用渐进迁移：新增 `Summary.Messages` / `Result.SummaryMessages`，旧 presentation string 暂作迁移兼容字段；只有结构化字段完成后才在阶段 10 删除 Localize，避免一次把全部 probe 与 renderer 混成不可审查的大 diff。

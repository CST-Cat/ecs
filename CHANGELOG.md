# Changelog

本文件依据 Git tag 及其之间的实际提交历史整理，记录 `ecs` 从首个公开版本
`v0.1.0` 到 `v0.7.9` 及后续 `Unreleased` 的主要变化。

- 每个版本以对应 Git tag 的日期为准；版本区间内的功能提交、修复提交和必要的合并提交一并归纳。
- 重复的“按最新提交重建评分基线”CI 提交不逐条重复罗列，但其对基线、排行榜参考和发布校验的影响会记录在对应版本中。
- `Unreleased` 用于后续维护；发布新版本时，应先补充该节，再移动为带日期的版本节。

## Unreleased

- 将固定 Release 工具链升级到 Go 1.26.6，修复 Go 标准库安全扫描发现的可达漏洞，并确认 govulncheck 无漏洞结果。
- 收敛排行榜提交的 artifact 指纹为单一当前规范 JSON 算法：投影排除 `id`、`sample_id` 和 `ran_at`，保留其余允许公开字段及精确浮点值；提交 JSON 不再接受 `fingerprint_version`，旧格式需按当前 v1 contract 重新导出。

## 0.7.10 — 2026-08-27

- 收敛 ECS machine model：`Field.Value`、`Measurement.Display` 和表格单元使用严格的 raw/key tagged value，表格元数据集中到 typed columns，JSON、脱敏、compare 与多格式 renderer 共享同一稳定边界。
- 各内建 probe 直接生成 stable `Result`，删除 semantic adapter 与 runner 对具体字段/表格 schema 的反解析；comparison parameters 回到 producer，Probe interface 只保留 `ID` 与 `Run`，模块事实统一由 descriptor 管理。
- 修复短格式 early flags、Exposure 严格校验和非法 terminal color fallback，补充 CLI/model/probe/renderer 回归覆盖；整理 probe、app、i18n 文件并清理临时任务 Markdown。

## 0.7.9 — 2026-08-26

- Probe 与报告边界改为稳定机器语义：内建探针的标题、字段、表格、状态和摘要以 stable key/Message 保存，JSON 不再依赖中文原文；`render`、Markdown、HTML 和终端展示在输出边界直接按当前语言渲染，中英文重渲染不改变机器数据。
- `ecs plan` 成为运行计划的唯一 JSON 机器接口，`run.sh` 只负责下载校验、按计划 staging 工具并执行；模块的干扰重试策略写入 descriptor，报告多格式改为全部成功渲染后再写文件，避免留下半套输出。
- 新增 `tools/lock.json` 统一架构、工具版本/tag/commit、STREAM、NPB 与 Silesia corpus 的固定事实；工具构建按工具拆分到 `scripts/tools/`，staticcheck 缓存绑定 `devtools/go.mod` 与 `go.sum` 摘要，发布/语料校验复用同一锁定事实。
- 安装器默认使用 `CST-Cat/ecs`，仍可用 `ECS_REPOSITORY` 覆盖；`--with-benchmarks` 继续是安装 sysbench、fio、iperf3 的显式 opt-in。新增独立的定时/手动 `govulncheck` workflow，安全扫描不接入 `ci.required`；排行榜 workflow 排除自身生成的 baseline，改为监听实际聚合算法入口。

## 0.7.8 — 2026-08-20

- 精简一次性 Shell 入口：`compare.sh` 成为唯一 Shell 报告对比入口，`run.sh` 删除 `--compare` 转发；`compare.sh` 删除二进制缓存、`--no-cache`、`--install` 以及 `install.sh` 调用，每次仅下载并校验当前 Release 主程序，在临时目录执行后清理。
- 删除 TXT 报告文件格式：`run`、`render`、`compare` 持久化输出统一为 JSON、Markdown、HTML；终端文本显示继续保留，且不属于 TXT 文件格式。
- 工具链策略调整：根 `go.mod` 继续声明 Go 1.22 最低源码兼容线；CI 仅 `compat` job 显式使用 Go 1.22.x；新增根 `.go-version`，作为普通 CI、`leaderboard` 和 Release 共用的人工固定工具链；删除 workflow 中重复的 `ECS_GO`/`ECS_RELEASE_GO` pin；不引入自动升级或版本同步门禁。
- 收紧 compare 参数边界：Shell wrapper 与 Go compare 尊重精确 `--`，边界后的 option-like 报告路径按 positional 透传；显式空 `--output=` 在写文件前失败，不再回退到 `./reports`。

## 0.7.7 — 2026-08-19

- 简化维护流程：移除发布前与发布后的 `govulncheck` 漏洞扫描，以及围绕扫描结果自动提出 Go 升级 PR 的状态机；同时移除 Dependabot 自动维护 PR。Go 工具链改由维护者人工固定：CI 与 Release 当前使用 Go 1.26.5，最低源码兼容线仍为 Go 1.22。
- Release 主链收缩为 `preflight → tools × 7 → assemble → verify → attest → publish`，其中 `verify` 针对实际解包产物；继续保留 checksum 校验、冻结候选提交 SHA、干净工作区、VCS metadata 校验、工具 digest 校验、GitHub Action 完整 SHA 固定和 artifact attestation 等供应链不变量。
- 测试布局同步收缩并聚焦高价值行为：由 `integration` build tag 下的真实工具覆盖承接集成验证，删除没有测试内容的定时 `live` 层，落实 submission corpus 校验，并增加 `run`、`compare` 与安装流程的行为回归。

## 0.7.6 — 2026-08-19

- 修复 Release 后扫描回归测试对 Go 版本间非 Go 制品诊断差异的兼容性：接受 `no valid Go build metadata` 与 `go version -m failed` 两种合理错误；正式 Release pin 保持为 `1.26.5`。

## 0.7.5 — 2026-08-19

- 修复 TikTok 公开页返回 2xx 但缺少地区信号时的判定：现在保守标为“未知”并保留缺失信号诊断，不再误报为“不可达”。
- 修复反向 DNS 诊断：NXDOMAIN（无 PTR）与 PTR/正向查询故障现在分开记录，FCrDNS 失败会保留可排查的查询错误而不再伪装成“无 PTR”。
- 为表格报告增加可选的机器 schema 字段 `key`、`column_keys` 与 `row_identity`；旧 JSON 缺少这些字段时仍可读取，不会从显示标题、列名或当前数据猜测身份。`compare` 现在按稳定 table/column key 匹配，并且只有显式声明的行身份才逐行比较；无稳定行身份的 legacy 表改用位置式或整表快照的保守比较，避免数据变化伪造行重排。
- 补齐全部生产 probe 表的稳定 schema，并让 apps/media 的分类按独立 machine key 分组；本地化、脱敏和文本渲染的裁剪会保留表格机器字段，中文/英文标题或列名不会再改变机器比较 contract。

- 修复报告本地化边界：采集阶段的模块标题、汇总和 NAT 清单保持 canonical 源文本，`run`、`render` 与 `compare` 始终以 canonical Report 进行输入、评分和比较；JSON 保留采集后的机器数据，TXT/Markdown/HTML 才按 `--lang` 生成独立的展示副本。因此同一事实以中文或英文渲染时 JSON 字节保持一致，字段值、表格列和行内容不会因界面语言改变，英文渲染后的 JSON 也可再次按中文渲染。
- `compare.Build` 的提示与选项校验错误改为稳定的 message key/canonical 参数表达，JSON 与比较计算不再读取当前 UI 语言；TXT/Markdown/HTML 和 CLI 错误边界才将这些值本地化，避免比较结果在中英文之间漂移或把内部 key 泄漏给用户。
- 这是 JSON 持久化语义的兼容修复：此前已生成的英文本地化 JSON 不包含可靠的原文标识，当前实现不会尝试英文→中文逆翻译；需要恢复完整 canonical 数据时，应从原始中文/canonical 报告重新生成。旧 JSON 仍可读取，但其中已被本地化的可见字符串只能按其现有文本展示。
- 收紧 Go Release 与安全升级职责：根 `go.mod` 仅声明最低源码兼容版本（1.22），`devtools/go.mod` 仅管理分析工具环境，正式 Release 编译器唯一由 `release.yml` 的 `ECS_RELEASE_GO` pin 选择（当前 1.26.5），Release 与最低兼容 CI 均强制 `GOTOOLCHAIN=local`；compat 只保留 1.22.x，stable 由 unit 覆盖。
- 安全自动升级现在只修改 Release compiler pin，不改根 `go.mod`；发布后安全复审从下载的七架构 `ecs` 二进制 `go version -m` 提取并校验一致的实际构建 Go 版本，再将该 metadata 传给 triage，避免误用 security runner 自身的 Go 版本。
- 删除运行时 module factory/estimate 注册表、`sync.RWMutex`、`any` 构造和 `init()` 反向注册：config 只保留静态模块描述，probe 提供强类型内建列表，runner 在执行边界显式绑定描述与探针，估算也由 probe 的显式调用负责。该内部重构不新增或改变用户配置字段；既有配置仍按相同模块 ID 解析，绑定完整性在执行前显式校验。
- 工具 manifest 收敛为 `internal/toolsmanifest` 的 Go parser/validator 唯一合同；删除经仓内搜索未发现独立消费者、且未被定位为公开权威合同的重复手工 JSON Schema 文件，CI 与发布阶段改为调用同一个 Go 入口校验示例和实际 manifest，`run.sh` 仅依赖 Release `checksums.txt` 并检查实际请求的可执行文件，同时移除旧 JSON Schema 门禁的 `jsonschema` 依赖；删除仅检查 workflow YAML 能否被 PyYAML 解析的浅层门禁，workflow wiring 由现有 shell policy 检查负责，其它 workflow 语义不由该检查覆盖。构建与 stage 校验脚本共享唯一架构/工具清单。
- 第一批迁移探针工具缺失/版本提示为稳定 `probe.*` message key；canonical JSON 保留 key，TXT/Markdown/HTML 按当前语言显示中英文，外部工具原始输出与尚未迁移的探针说明继续原样保留。
- 删除 Linux-only 项目的非 Linux TTY fallback；CONTRIBUTING/SECURITY 改为说明根 `go.mod` 最低源码兼容、`ECS_RELEASE_GO` 正式编译器、`devtools/go.mod` 分析工具环境及安全升级只改 Release pin；移除无活跃引用的 HANDOFF，并将 research 标为历史调研快照。README、examples 和 submissions 只保留用户行为、命令和目录特有格式，公共政策统一链接到根文档。

## 0.7.3 — 2026-08-18

- 修复 integration CI 的 apt 安装可靠性：`apt-get update/install` 均设置有限 HTTP/HTTPS 超时、重试次数和单操作硬超时；首次 `update` 失败只在检测到 `azure.archive.ubuntu.com` 时通过临时源文件将该 Ubuntu 源替换为 `archive.ubuntu.com` 再试，第三方源不变；普通 `install`、备用 `update` 或备用 `install` 失败均保留诊断并 hard fail，不静默跳过；新增确定性回归测试。

## 0.7.2 — 2026-08-18

- 将主模块的最低源码兼容版本改为 Go 1.22；`devtools` 工具链独立管理，不再与根 `go.mod` 绑定。正式 Release 使用 workflow 独立的 `ECS_RELEASE_GO`（当前为 1.26.5）固定编译器，并以构建实测版本校验发布二进制。
- 安全升级候选现在必须先通过 Go 官方稳定 Release 列表的精确版本门禁；未确认正式发布时不自动提出升级 PR。
- 修复实际测量与网络语义：自定义 iperf3 节点仅对 IPv4/IPv6 字面量固定对应协议族；主机名不固定协议族，协议族留空并交由模块选择；cnspeed 仅在 EOF、达到时限或达到字节上限时成功，非 EOF 读取错误不生成吞吐；回程静态线路签名不伪造 ASN，BGP 改为报告 AS_PATH 中观测到的 ASN 并保留旧机器键；DNS `best_dns_median_ms` 使用 `udp-a-query-warm-v1` method。
- 修复安装器：Arch Linux 的 benchmark 依赖安装使用 `pacman -S --needed --noconfirm`，不刷新 package DB；二进制替换改为在目标目录内用 `mktemp` 创建临时文件，再通过 `cp`、`chmod`、`mv` 完成。
- Commit 4 文档收口：中英文说明统一报告隐私（只遮本机 IP，主机名、远端 IP、路由逐跳和 BGP 前缀不自动遮盖）、按主机与模块协议能力选择协议族、`make test`/`make check` 与 CI 的 race/integration/cross 分工、默认 installer 与 `--with-benchmarks` 的系统包安装边界、`run.sh` 临时 staging，以及 `render` 当前支持 schema 与 `compare` 的 `ecs.report/*` 跨 schema 部分可比规则。

- CI 新增 `compat` 矩阵，在 `1.22.x` 与 `stable` 上执行源码和解析器测试；其余 CI job 统一使用 `stable`。最低源码兼容版本、日常构建工具链和正式 Release 编译器因此各自有清晰边界。
- 普通 `govulncheck` finding 不再让普通质量检查或 security 日常监控 hard fail；扫描器、网络、输入、命令和 JSON 处理错误仍然阻断。Release 安全门禁只在实际 ECS 二进制中的 finding 被明确标为 `HIGH`/`CRITICAL`（或等价的明确高危分数）且 trace 证明可达时阻断发布，普通、严重性不明或不可达的 finding 只告警。
- 发布后扫描以机器可解析的 `release_security_result=clean|blocked|fatal` 标记结果：工具、输入、JSON 与输出错误为 `fatal` 并 hard fail；明确高危且可达的 finding 为 `blocked`，仅告警并继续 triage；普通或严重性不明的 finding 为 `clean` 并保留告警记录。
- `security` workflow 继续扫描当前源码和已发布二进制，保存 JSON、运行 triage，并仅在 Go stdlib/toolchain 漏洞具备官方稳定 Release 的正式修复版时提出升级 PR；PR 仍由人工合并和发布，不自动合并。
- 中英文 README、安全说明与项目协作规则同步更新，明确 Go 最低源码兼容、CI/Release 工具链分工、漏洞 finding 的告警与阻断边界，以及安全扫描、triage 和正式修复版升级流程。

- 后续版本的变更记录写在这里；release workflow 会按 tag 从本文件提取对应版本章节，作为 GitHub Release 正文，并附上该 tag 提交中的 `CHANGELOG.md` 链接。

## 0.7.0 — 2026-08-18

本版重构 CI/CD 并完善报告对比。对普通用户可见的变化有两项：多了一条一行命令的报告对比入口，以及跨版本的旧报告重新变得可比。发布物的构建与校验方式全面收紧，但测量行为一字未改——本版与 0.6.16 的同口径成绩仍可直接比较。

### 报告对比 — 2026-08-18

- 新增 `compare.sh`：`curl … | sh -s -- 昨天.json 今天.json` 即可对比本地报告，不必先安装 ecs。对比是纯本地计算，因此不下载任何基准工具与语料；退出时删掉二进制，**保留**对比结果并打印路径，当前目录不新增任何东西。
- 校验过的二进制缓存在 `${XDG_CACHE_HOME:-~/.cache}/ecs/<tag>` 下供反复对比复用，按具体 Release tag 分目录，每次使用前重新核对摘要；`--no-cache` 关闭，`--install` 委托 `install.sh` 装进 PATH。
- `run.sh --compare` 转发给 `compare.sh`，两个入口等价而实现只有一份。
- 跨 schema 版本的报告重新可比：`run`、`submit`、`render` 仍要求 schema 完全一致，只有 `compare` 放宽为尽力比较。保证比较安全的一直是指标签名（`key` + `method` + 单位 + 优劣方向 + 逐个参数）而非顶层版本号，因此跨版本的语义变化必然落进 `metric_issues` 而不会被误比。跨版本时可比性封顶为 `partially_comparable` 并显式提示；空版本与非 `ecs.report` 家族仍然拒绝。
- 判定“口径不一致”时不再只给一个原因码，而是逐分量列出究竟什么变了（method 从哪一版到哪一版、哪个参数从多少变成多少、单位是否换了），并标注各值出自哪个 ecs 版本。差异相同的指标合并成组，避免模块级参数把真正只影响单个指标的那一项淹掉。

### CI/CD — 2026-08-18

- 两个职责混杂的 workflow 拆成五个，每个只回答一个问题：`ci.yml` 能否合入、`security.yml` 是否仍安全、`live.yml` 外部服务是否正常、`leaderboard.yml` 数据是否需要重建、`release.yml` 能否发布。第三方接口限流不再让普通代码检查变红，写权限也不再出现在任意分支的 push 上。
- 发布 preflight 现在在七架构工具构建前校验对应版本的 CHANGELOG 章节且要求有正文；`ci/required` 直接打印 `unit=success` 等带 job 名的结果，失败时可立即定位具体红灯。
- 明确 CI、live、security、release 红灯的处理边界；quality 检查改为表述为“版本固定、无 live 外部服务依赖”，承认分析工具首次构建与 Python venv/pip 可能联网；分析工具子模块同步使用 Go 1.26.6，避免用旧工具链扫描当前源码。
- integration 现在只下载并编译 19.5 KiB 的官方 STREAM 源文件，复用 Release 的固定 SHA 与编译参数，不再因 STREAM 缺失而让真实集成门禁在 runner 上失败，也不触发完整十工具构建。
- 发布流水线按权限与制品交接拆成 `preflight → tools×7 → assemble → verify/security → attest → publish`，**只有 `publish` 持有 `contents: write`**；编译器、Docker、`govulncheck` 与全部构建脚本都运行在无写权限的 job 中。发布入口冻结候选 SHA，后续阶段不再与移动中的 `main` 比较。
- 新增 artifact attestation：全部发布资产可用 `gh attestation verify` 核对来源仓库、workflow 与提交。
- 每次发布完整构建七架构工具包，删除按路径差异推断、复用上一个 Release 工具包的机制——那种推断错一次就是发出一个没人验证过的二进制。
- 构建实现从 YAML 下沉到仓库脚本：Docker 镜像、apt 包、交叉三元组、QEMU 运行器全部由 `scripts/build_tools_container.sh` 的架构表决定，`--print-params` 可秒级比对构建定义。只要有一台带 Docker 的 Linux 主机，就能按与 CI 完全相同的定义构建。
- 新增发布后安全监控：每日复审已发布的七架构二进制。命中全部来自 Go stdlib/toolchain 且官方已有同系列修复版时，自动开一个只改 `go.mod` 的 PR；其余情况交给人判断。
- 全部 GitHub Action 固定到 40 位 commit SHA，并交由 Dependabot 管理升级。

### 构建工具链 — 2026-08-18

- Go 工具链版本收敛为 `go.mod` 单点定义（`go 1.26.6`），删除 1.22/stable 兼容矩阵与各处硬编码；发布校验改用本次构建实测的版本比对二进制元数据，升级 Go 只需改一行。
- 从源码构建 ecs 需要 Go 1.26.6；**运行 Release 二进制不需要任何 Go 环境**，文档相应改写。
- 测试按“需要什么”分类写进源码 build tag：单元测试无外部依赖，`integration` 需要真实基准工具，`live` 需要公网。删除 CI 配置里逐个列举测试函数名的巨型 `-skip` 正则。
- `staticcheck` 与 `govulncheck` 的版本锁进独立的 `devtools/go.mod`（含 `go.sum` 哈希），主模块 `go.mod` 保持零依赖——从源码构建 ecs 仍然不下载任何模块。

## 0.6.16 — 2026-08-17

本版只动文档与报告标识，不改任何测量行为：schema 标识定为 `ecs.report/v1`，基准工具版本全部冻结并公开对照表，六份 README 按当前源码重写。beta 阶段不保证跨版本报告兼容，请以当前版本重新生成报告。

### 报告 schema — 2026-08-17

- 报告 schema 标识定为 `ecs.report/v1`，字段结构与全部功能保持不变——只改标识，不改内容。

### 工具版本冻结 — 2026-08-17

- 冻结全部基准工具版本并在 README 增设对照表（`sysbench` 1.0.20、`zstd` 1.5.7、NPB-OMP 3.4.4、`openssl` 3.5.7、STREAM 5.10、`fio` 3.42、`iperf3` 3.21、iputils 20250605、`nexttrace-tiny` 1.7.1）；`scripts/build_tools.sh` 对每个上游同时锁定 tag 与该 tag 解析出的 commit。
- 除 `nexttrace-tiny` 落后上游一个补丁版（v1.7.2 仅新增 DNS 客户端模式，不影响路由追踪）外，均为各自上游当前最新稳定版。OpenSSL 取 3.5 LTS 分支（支持至 2030-04-08）而非主线 4.0.x，避免大版本重构改变密码学实现的性能特征。
- README 表格标注每个工具是否在运行时校验版本：`zstd`、`openssl`、NPB 三项版本不符即按缺失处理；其余工具的版本与二进制 SHA-256 随报告记录，并进入排行榜提交的 `tools` 字段供同口径分层。

### 文档 — 2026-08-17

- 重写全部 README：根目录、`examples/` 与 `submissions/` 各提供中英文两份，共六份，内容按当前源代码重新核对（模块表对照 `ecs list`，参数表对照 `ecs run --help`，双语措辞取自 `internal/i18n` 现有键值）。
- 首页承诺段改写为「项目坚持」：不展示任何广告、赞助或返利内容；报告默认写入 `/tmp`，不提供自动上传链路。`ookla` 调用外部官方客户端这一例外单独标注。
- 报告目录说明区分两种入口：`run.sh` 一键运行写入 `${TMPDIR:-/tmp}`，直接运行二进制写入 `./reports`。
- 补充 `--only` 的语义：它取代档位的模块集合而非在其中筛选，因此任何模块都能从任何档位到达。

### CI — 2026-08-14

- 修复定时与手动 `live` CI：将 iperf3 节点可达性检查适配到新的生产吞吐接口，保留按端口范围执行有界 TCP 预检的实网功能，同时不把额外连接重新接回生产吞吐路径；普通 stable CI 增加不发网络请求的 live 标签编译门禁。

## 0.6.15 — 2026-08-13

本版完成一次覆盖隐私、终端输出、网络探测、数据正确性、基准运行时与发布链路的审计整改。安全校验保留在信任边界和网络边界，单核环境则去除已确认的重复工作。

### 安全与隐私 — 2026-08-12

- 脱敏不再只覆盖手写字段清单：`RedactedCopy` 现在遍历整个公开报告 schema 的所有字符串字段以及 map 值，并保持原对象不变。IPv4、IPv6、带端口地址和嵌入自由文本中的地址统一按现有策略处理；原始采集报告的 `run.redacted` 保持 `false`，只有真正脱敏后的副本标为 `true`。
- 终端和彩色 TXT 输出在渲染边界统一把来自报告数据的 C0、DEL、C1、CSI 与 OSC 控制序列替换为普通空格，阻止第三方响应通过改标题、写剪贴板、清屏或移动光标伪造终端内容；渲染器自身生成的 SGR 颜色序列不受影响。
- `cnspeed` 节点清单固定到审计过的上游 commit，不再跟随可变的 `main`。节点 URL 只接受绝对 HTTP(S) 公网目标，拒绝 userinfo、fragment、非法端口与特殊用途地址；专用客户端忽略环境代理，并在 DNS 解析、实际拨号和每次重定向处重新校验目标，封堵恶意 CSV、DNS rebinding 与重定向 SSRF。
- 发布 job 固定 Go 1.26.5，并在七个归档解包后逐一校验 Go build info：版本、完整 Git revision 和 `vcs.modified=false` 必须与发布提交一致。发布前还要求工作区（含未跟踪文件）完全干净，并用固定版本 `govulncheck` 扫描源码和每个发行二进制。

### 数据正确性与兼容性 — 2026-08-12

- 工具 manifest 的 Go 校验器与 JSON Schema 同步收紧：在顶层、`build`/`validation` 和每个 tool 对象拒绝未知字段，要求构建合同完整，且十个标准工具必须各出现且只出现一次；开放扩展的 `parameters` 仍允许工具专属键，示例 manifest 也进入 schema 门禁。
- 排行榜提交采用规范 JSON 指纹，覆盖除 artifact ID、benchmark sample identity 与运行时间外的全部可接受公开字段并保留浮点数精度，避免同一测量因时间不同无法去重，也避免按三位小数舍入造成碰撞。
- Tier 基线记录每个指标自己的样本数；指标只有在自身达到五份有效样本时才能参与门槛计算。旧基线缺少逐指标计数时保守回落到全局基线，畸形、负数或不完整的新计数会被拒绝。
- 三网回程结论只从目标运营商的探测记录中选取最佳命中；仅有其他运营商命中时返回“未识别”，不再把跨运营商线路误报为目标线路。

### 性能与运行时 — 2026-08-12

- 单核配额下，sysbench、zstd、NPB、STREAM 与 OpenSSL speed 各只执行一次真实物理基准，再把同一证据映射到兼容的 `1T`/`NT` 逻辑字段。报告明确标注多线程扩展“不适用”，不伪造 scaling，也不再为相同的单线程工作负载重复消耗时间与 CPU；运行时估算同步减半。
- 清理无调用的 iperf3 端口预检与报告排序辅助函数，修复静态分析发现的无效赋值和冗余布尔表达式；不改变实际工具失败必须如实报告的约束。

### CI、文档与回归验证 — 2026-08-12

- CI 新增固定版本 `staticcheck` 与 `govulncheck` 门禁；发行临时目录全部加入 `.gitignore`，同时由源码契约测试锁定忽略项、扫描命令和发布元数据检查，防止工作流后续静默弱化。
- 中英文 README、schema、隐私与安全说明、第三方来源及贡献指南同步记录全 schema 脱敏、终端边界、固定 cnspeed 来源、单核复用、指纹版本、逐指标样本和当前 Go 支持策略。
- 增加覆盖恶意终端序列、全报告 IP 泄漏、恶意节点清单/私网 DNS/重定向、单核真实执行次数、artifact 指纹内容与严格字段校验、稀疏 Tier 指标、跨运营商回程以及严格 manifest 合同的回归测试。

## 0.6.14 — 2026-08-12

本版是一次审计驱动的整改：修补供应链校验缺口，把磁盘矩阵的固定 10 秒窗口
改为按组分级，并清理长期无人调用的代码。beta 阶段不保留向后兼容，磁盘矩阵
与 IPv6 遮盖的口径都直接改到位。

### 安全 — 2026-08-12

- **`stream.c` 现在按固定 SHA-256 校验**。此前只把下载结果的哈希写进 manifest 而从不比对，唯一的检查是正则匹配 RCS `$Id:` 注释行——攻击者保留那一行即可绕过。官方 STREAM 是单文件、无 release feed、无上游校验和的分发方式，因此必须在本仓库钉住哈希。
- sysbench、fio、iperf3、iputils、NextTrace 全部从"取上游 latest release"改为**固定 tag + 固定 commit 校验**，与既有的 zstd、OpenSSL 一致。tag 被移动或重新打都会让构建失败，而不是静默换掉发行内容。升级工具版本现在是一次显式的两行编辑。
- NextTrace 预编译资产的 digest 校验由"有则校验"改为**必须存在**。它是工具包里唯一的预编译二进制，缺 digest 时跳过校验等于让整条路由链路失去校验。
- IPv6 遮盖从保留前四组（/64）收紧到**保留前两组（/32）**。VPS 提供商普遍按 /64 给单台机器分配整个子网，遮到 /64 仍能唯一定位实例，与 IPv4 的 /16 保护强度不对等。
- Markdown 报告转义补上 `[`、`]`、`(`、`)`：第三方 IP 情报返回的公司名、ISP 名可以构造出可点击链接。
- CI 的 `submissions` job 降为只读；重建排行榜参考的写权限拆到独立 job，并在 job 级限制为 main 分支的 push。此前所有分支的 push 与同仓 PR 都以 `contents: write` 运行 `go test` 与 `go run`。

### 磁盘矩阵计时 — 2026-08-12

- 53 个 fio 作业不再统一使用 10 秒窗口。逐秒采样显示矩阵各档在 2–3 秒内进入吞吐平台，更长的窗口只是重复采样同一个稳定值，却持续消耗云盘突发额度——靠后的档位因此测到的是额度耗尽后的性能，矩阵内部前后不可比。
- 新的分档：基础与混合保持 **10 秒**（磁盘评分口径不变），Crystal **5 秒**，ATTO **3 秒**，其中 64K、32M、64M 为 **5 秒**。64K 的吞吐平台恰好在第 3 秒出现；32M/64M 单次 I/O 时间长，3 秒内样本过少（实测出现 32768/65601 KiB/s 交替）。
- 磁盘模块串行执行下限从 **8 分 50 秒降到 4 分 10 秒**，53 个作业与完整块大小谱系一个不少。
- 每个矩阵单元记录它在模块内的**起始偏移**（来自 fio 的 `job_start`）。曲线在某个偏移之后整体下移通常是突发额度耗尽而非介质特性，保留偏移让读者能自行判断。
- 报告的 `job_duration` 单值字段拆为 `job_duration_base` / `job_duration_crystal` / `job_duration_atto`，另加 `plan_duration` 与 `matrix_mode`；比较签名同步区分三档时长。
- 新增 `--disk-matrix-mode fixed` 复核模式：ATTO 每档传输固定 256 MiB 而不按时长收尾。它与默认口径不是同一件事，数值不可混比，且小块档位耗时极长（实测 512B 读 237 秒、写 348 秒，整轮超过 20 分钟），只用于突发额度与长尾复核。
- 删除恒为真的 `matrixJobsEnabled()`：矩阵降到 2 分钟后不需要开关，留一个永真分支只会让人以为它可配置。

### 运行时长估算 — 2026-08-12

- 磁盘估算改为**按实际作业计划求和**。旧公式 `5s + 2×random + DiskMiB/50` 是矩阵引入之前的口径，从未跟着 53 个作业更新，在默认配置下报 51 秒而实测 551 秒（低估 10.4 倍）。config 无法 import probe，因此新增 `RegisterModuleEstimate` late-binding 注册表，由拥有工作负载的包提供估算。
- speed 估算计入**协议族与 UDP**。旧公式按 `节点数 × 2 × 时长` 算，漏掉了每节点对每个可用协议族都跑上传、下载与一轮 UDP，双栈机器实测低估一倍以上。
- standard 档展示的预估从"约 7–23 分钟"修正为"约 12–39 分钟"，与实际耗时同量级。

### 基准干扰复测 — 2026-08-12

- 自动复测的 CPU steal 阈值从 **1% 提高到 5%**，与 system 模块的累计 steal 告警一致。同一台机器连续三轮的实测波动就有 ±5–8%（randread 29.12 / 26.90 / 27.01 MiB/s），1% steal 的影响小于测量噪声；而超卖 VPS 上 1% steal 是常态，按它复测等于把六个重型基准都跑两遍，收益却被噪声淹没。
- 报告里标注 steal 的阈值保持 1%，并单独命名为 `stealNoticeThreshold`：标注只多一行说明、代价为零，值得对轻微争抢也如实提示；复测要重跑整个基准，需要更高门槛。两者不是同一个判断。

### 修复 — 2026-08-12

- `/proc/uptime` 存在但为空时不再 panic。容器与沙箱里会出现这种情况，此前 `strings.Fields(...)[0]` 越界会让整个 system 模块变成"探针发生 panic"。
- 评分指标解析不再依赖 map 遍历顺序取展示信息。前缀匹配的指标此前取"最后一个匹配项"的 label/unit，导致同一份报告每次渲染得到不同字段。
- `ecs compare` 的比较签名改为引用 `probe` 的跳数与查询域名常量，不再各写一份 `"12"`、`"20"` 与 `"www.cloudflare.com"`。
- HTML 报告的表格语义着色补齐英文取值，此前只匹配中文，英文报告的整张表退化成无色。
- IPQS 与 DB-IP 的公开页通道在报告中标注为"官方公开查询页（浏览器 UA）"，把 UA 这一环也纳入既有的通道披露。

### 清理 — 2026-08-12

- 删除 15 个生产代码中零调用的函数（`overview`、`centered`、`comparisonValueDisplay`、`dnsQuery`、`outboundLocalIP`、`ooklaSummary`、`appendOoklaMeasurements`、`extractNextTraceHops`、`appendToolVersion`、`SortedInterferenceReasons`、`KnownModules`、`ModuleExposureName`、`Keys`、`ErrorKeys`、`ProbeTextKeys`）。
- 合并三处独立的 CJK 显示宽度实现：`ui.displayWidth` 与 `app.padDisplay` 删除，统一使用 `textwidth`。同时删除 `textwidth.VisibleWidth` 与 `PadVisible`——它们与 `Width`、`Pad` 逐字相同。
- 合并两处 STREAM 二进制识别，导出 `probe.IsOfficialStreamBinary` 供 doctor 与 memory 探针共用，并加上 64 MiB 大小上限防止读入超大同名文件。
- 报告渲染不再重复本地化。`WriteFiles` 已统一本地化一次，html/markdown/text 三个渲染器此前各自又调用一次 `Localize`；英文模式下每个字符串要走多级正则模板匹配，三遍是纯浪费。
- `run.sh` 删除 `prepare_ookla_rpm` 中生成 `.repo` 文件后直接返回失败的 20 行无用功，以及随之失去调用方的 `select_ookla_rpm_distribution` 与四个 RPM 变量。
- `toolsmanifest` 删除两处冗余校验：正则确认过的 SHA-256 不再二次 `hex.DecodeString`，`Validate` 末尾的缺失检查已被前面的数量、白名单与去重三项蕴含。
- CI 增加 `gofmt` 门禁，并顺手修正两个既有的格式不合规文件。

## 0.6.9 — 2026-08-11

### CI 与验证 — 2026-08-11

- 修复 v0.6.9 发布说明缺少对应版本章节导致 release workflow 无法生成 GitHub Release 的问题。

### 网络能力 — 2026-08-11

- 新增每轮一次的纯本地 `NetworkCapabilities` 快照：识别 IPv4-only、IPv6-only、双栈与无可用出站族，接受 RFC1918 IPv4 NAT 源地址并排除 IPv6 ULA、链路本地和回环地址；网络探针共享该快照，不再重复做 IPv6 系统检测。
- runner 按原始 IP 族请求形成 effective family；`auto` 只收窄到实际可用族，显式 IPv4/IPv6 不跨族回退，不可用请求统一跳过网络模块而继续运行本地模块；报告保留原始 `run.ip_version`，比较参数记录 effective family；纯本地选择不进入出口发现阶段。
- 同步中英文网络能力跳过文案；报告 schema 与机器可解析字段保持兼容。

## 0.6.8 — 2026-08-11

### CI 与验证 — 2026-08-11

- 修复 Docker 以 root 生成七架构 stage 后，GitHub runner 无法清理 corpus 文件导致发布 job 中止的问题；清理步骤只针对明确的 corpus 路径使用 runner 的授权删除。
- 延续 v0.6.7 的 corpus 拆分方案：工具包保持 gzip 且不含 corpus，独立 Silesia 资产仍按固定长度、顺序和 SHA-256 发布。
## 0.6.7 — 2026-08-11

### 变更 — 2026-08-11

- `ecs-tools_linux_<arch>.tar.gz` 继续使用 gzip，但只包含 `bin/`、`LICENSES/`、`LICENSE`、`NOTICE` 和 `manifest.json`；固定 Silesia corpus 从七个架构包中移除。
- 新增架构无关的 `ecs-corpus_silesia-v1.tar.gz` Release 资产，按固定 ZIP、文件顺序、211938580 bytes 和最终 SHA-256 构建并校验。
- `run.sh` 只有在选中 zstd 时才从 Release checksums 校验并解压独立 corpus，随后再次校验长度和 SHA-256；其他 benchmark 不下载 corpus。

### CI 与验证 — 2026-08-11

- 七架构 tools smoke 仍验证 10 个工具，构建阶段可使用 corpus，但上传前明确移除；CI 同时拒绝 tools 归档中的 `share/`/corpus，并验证独立 corpus 资产内容。

## 0.6.6 — 2026-08-11

### 新增 — 2026-08-11

- 增加固定 `zstd 1.5.7` 的真实压缩基准：使用 SHA-256 固定的 Silesia corpus、等级 3、5 秒评估窗口，分别运行 1 线程和可用全线程，输出压缩/解压吞吐、扩展倍率与每线程效率。
- 增加 NASA NPB 3.4.4 OpenMP Class A 的 EP 和 FT；固定 `gfortran` 编译参数与随机数实现，分别记录 1 线程/全线程原始 `Mop/s` 和扩展倍率，不引入自定义综合分。
- 增加独立的 OpenSSL 3.5.7 密码学模块；固定 16 KiB block、5 秒时长，测量 AES-256-GCM、ChaCha20-Poly1305 和 SHA-256 的 1 worker/全 worker 吞吐与扩展倍率，不并入 CPU 综合结果。
- 三类新基准均记录 method version、完整参数、工具 binary SHA-256 和原始输出；zstd 额外记录 corpus SHA-256、来源归档 SHA-256、字节数和拼接顺序。

### 变更 — 2026-08-11

- 本地性能骨架扩展为 sysbench、zstd、NPB EP/FT、STREAM、OpenSSL speed、fio 和 iperf3；模块总数由 18 增至 21，标准工具由 6 增至 10。
- 发行工具包加入固定版本的 zstd、NPB EP/FT、OpenSSL 及固定 corpus；`run.sh` 仅在相关模块启用时，将校验过的二进制和 corpus 暂存到本次运行目录，不改动宿主机系统包。
- `ecs-tools` 发行归档改用项目已有的 `tar.gz` 格式，消除最小系统“需要先安装 zstd 才能解出固定 zstd”的启动闭环；归档、manifest、binary 与 corpus SHA-256 校验保持不变。
- 资源干扰复测覆盖 CPU、zstd、NPB、STREAM、crypto 和 fio；报告 schema 以新增字段方式保持兼容，并补充三类基准的参数签名，避免不同口径结果被直接比较。
- 终端与 TXT 报告按实际列宽在 20–160 列间自适应表格、柱状图和长行；无色、基础色、256 色与 truecolor 终端保持一致的语义层次，中英文文案同步更新。

### CI 与验证 — 2026-08-11

- 七架构工具构建矩阵补齐 `gfortran`、OpenMP、Perl 和 unzip 依赖；zstd 裁掉字典/trace/兼容格式，NPB 只发布 EP/FT，OpenSSL 只构建官方 CLI 必需依赖并关闭 TLS/网络/动态组件与无关算法族。
- 四个交叉架构先由宿主工具链完成编译，再由 QEMU 执行短功能 smoke；NPB 用同工具链的临时 Class S 跑完整 Verification，发行 Class A 只做静态 ELF/摘要/manifest 校验，避免在模拟器中做无意义的重型性能运行。
- 工具 manifest 与发布资产检查由 6 个二进制更新为 10 个二进制加固定 corpus，同时保留 binary/source SHA-256、编译参数、启用能力和 smoke runner 证据。
- 固定摘要的 NPB 与 Silesia 下载增加内容校验级重试和实际摘要诊断，仍拒绝任何不匹配数据；GitHub token 仅发送给 GitHub API，不再随外部源码请求传出。
- 七架构产物统一规范许可证文件读权限；`ppc64le` 使用同时提供目标 LuaJIT、交叉 gfortran 与配套 libc sysroot 的 Debian forky，并显式调用对应 static QEMU runner，避免 sid 的 libc 版本冲突。
- OpenSSL 继续关闭 TLS、网络、动态组件和无关算法，但保留上游 RISC-V 加速源编译所需的 deprecated SHA context 类型声明；该调整不改变三项固定 EVP benchmark 口径。
- 修复 `ppc64le` 在 Debian forky 中的 QEMU runner 名称，改用实际存在的 `qemu-ppc64le`。
- 将 Silesia ZIP 下载源切换到可用镜像，保留最终 corpus 的 `211938580` 字节数和 `8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0` SHA-256，并增加 ZIP 完整性检查。

## 版本路线概览

| 版本线 | 主要主题 |
| --- | --- |
| `v0.1.0` | Linux-only 基础、真实工具、实网探针、双语输出和一键运行 |
| `v0.2.x` | 外联同意、评分基线、TXT 报告和排行榜提交模型 |
| `v0.3.x` | 统一报告模型、全档位磁盘矩阵和报告渲染基础 |
| `v0.4.x` | 终端/纯文本报告持续重构，以及进度和 CI 调度收敛 |
| `v0.5.x` | 安全提交链路、临时工具暂存、配置档和可见性边界 |
| `v0.6.0–v0.6.4` | 六个标准工具、七个 Linux 架构和可验证发布资产 |
| `v0.6.5` | 结构化失败、资源压力诊断、条件复测和多报告对比 |
| `v0.6.6` | 七架构发布链路修复和 Silesia 镜像切换 |
| `v0.6.7` | Silesia corpus 独立发布与架构 tools 体积收敛 |
| `v0.6.8` | 发布 stage 权限清理修复 |

## 0.1.0 — 2026-08-01

首个公开版本。发布 tag 之前的基础建设最终随该版本成为可运行的 `ecs`。

### 新增

- 确立 Linux-only 边界，删除 Windows 等多平台代码路径，让测试直接断言真实 Linux 行为。
- 以结构化报告为核心：探针先生成统一 JSON 数据，再由本地渲染器输出终端、纯文本、Markdown、HTML 等格式。
- 接入真实 `fio`、`sysbench`、`iperf3`，删除脚本替身和假工具适配，测试不再用模拟输出掩盖外部工具失败。
- 建立系统、容器、硬件、CPU、内存、磁盘、DNS、延迟、网络、端口和应用可达性等基础探针。
- 增加三网回程、流媒体规则引擎、ICMP/UDP 质量、IPv6 判定、NAT/STUN、BGP、FCrDNS、内核参数和模块级并行调度。
- 吸收并实现多挂载盘 I/O、`mbw` 内存带宽、`ioping` 延迟、`smartctl` 介质信息、DNS 黑名单、Telegram DC、应用服务可达性、中国三网就近测速和回程城市选择等能力。
- 将 IP 质量数据源从 11 个扩展到 17 个，再移除纯密钥或不可稳定使用的来源，收敛到 13 个可披露来源。
- 增加中英文 i18n、全局 `--lang`、所有配置项的命令行调节、`curl` 一键运行和交互向导；非交互场景仍可直接运行。
- 增加脱敏完整报告样例、`run.sh` 临时依赖准备、CI 的 `run.sh` 语法检查和一键运行器发布资产。

### 隐私与工程约束

- 默认不上传报告、不植入广告、赞助、返利或在线报告站路径；报告默认只写本地。
- 外部数据源、STUN 服务、第三方组件和 NAT 所需服务均在文档中披露。
- 标准工具适配器测试优先调用真实 Linux 工具，并为实网测试、路由节点池和 IPv6 选择补回归覆盖。

## 0.2.0 — 2026-08-03

把“能测”推进到“能解释外联、能复核评分、能安全提交”。这是一次包含 CLI 和配置兼容变化的功能版本。

### 外联控制与配置

- 将外联控制改为分级权限：`local`、`public`、`thirdparty`、`any`，由 `--exposure` 选择最高允许级别。
- 以 `--accept` 统一承载显式同意，替代 `--offline`、`--ookla` 和 `--accept-ookla-terms` 的旧组合；Ookla 等特殊外联模块不再隐式运行。
- 出口 IP 发现收敛为一次共享步骤：`public` 使用公共 STUN，`thirdparty` 及以上才访问 `ipapi.is`；`network`、`blacklist` 和 `bgp` 复用同一结果。
- 报告 `run` 增加 `exposure` 和 `accepted_modules`，`offline` 保留为 `exposure == local` 的派生字段，维持旧消费方的基本兼容。
- 未知配置字段、外联级别和校验输入明确报错，并补齐此前英文模式下仍输出中文的错误翻译。

### 测试、报告与评分

- 增加可复核的 TXT 输出；终端能力允许时使用比例和颜色柱，不支持颜色时使用密度字符保留层次。
- 增加可替换的 `ecs.baseline/v1` 评分基线，评分只在渲染阶段计算，不把某份基线下的分数写死进报告 JSON。
- 基线按 vCPU 档位选择，样本不足时回退全局基线；使用 MAD modified z-score 做同档离群提醒，不把离群直接当作造假。
- 增加白名单式 `ecs.submission/v1` 排行榜提交格式，完整报告和排行榜数据分离，避免将出口 IP、主机名和逐跳路由带入提交库。
- 增加提交指纹、重复检查、嵌入基线和 CI 校验；补充 Oracle 云主机样本，修正样本标签和基线说明。
- 基线目录改为递归遍历并跳过隐藏目录，修复按月份分目录后 CI 找不到报告的问题。

### 观测能力

- 增加可审计的 VPS 硬件、BGP、公共测速和 IPv6 回程观测，报告记录数据来源、方法和限制。
- 统一报告中的评分说明、外联说明和基线样本数，避免把开发机单次样本误写成 VPS 典型值。

## 0.2.1 — 2026-08-03

- 完善多块大小的内存和磁盘评测，补充更细的工作负载口径和基线数据。
- 增加 Oracle 云主机完整跑分样本，并重建内嵌评分基线和提交库基线。
- 补充 Balloon、KSM 等系统证据的英文翻译，继续保持中英文报告字段对齐。

## 0.3.0 — 2026-08-03

### 报告模型与渲染

- 重构终端统一报告输出，统一模块状态、标题、方法信息、表格和完成反馈。
- 扩展报告 schema、方法学和系统/资源证据，增强 CPU、内存、磁盘矩阵结果的可复核性。
- 为终端、Markdown、HTML 和纯文本渲染增加契约测试，避免同一份 JSON 在不同格式中语义漂移。
- 精简默认报告输出路径，修复 `run.sh` 默认报告目录错误，避免结果意外写入临时目录。

### 测试口径

- 完善内存统计和磁盘矩阵测试，覆盖容器资源信息、矩阵字段和基准渲染边界。
- 继续按最新提交重建评分基线，确保发版报告和排行榜参考使用同一套样本。

## 0.3.1 — 2026-08-03

- 补全所有测试档位的 ATTO 磁盘矩阵，不再只在部分档位展示扩展块大小。
- 同步 `README`、研究文档、`fio` 参数和矩阵回归测试，固定各档位的测试口径。

## 0.3.2 — 2026-08-03

- 修复 ATTO 扩展提示的英文翻译。
- 增加 i18n 回归测试，防止该提示在英文报告中回退为中文。

## 0.4.0 — 2026-08-03

### 档位与测试口径

- 解耦 profile 和模块测试口径：档位只决定模块覆盖范围，共享模块使用相同的 full 深度测试，不再暗示“full 更长所以更准”。
- 清理旧配置档行为文案，文档明确共享资源和测试深度的关系。
- 对网络、磁盘、三网测速和 egress 的协议族、参数和结果处理做一致化修正。

### 修复

- 修正 `iperf3` UDP 零丢包场景的错误断言，避免把合理的零丢包结果判成异常。

## 0.4.1 — 2026-08-03

- 修复 `run.sh` 默认报告输出逻辑，默认结果写入用户指定/当前工作目录，而不是临时工作目录。
- 增加脚本行为回归测试，覆盖输出目录创建、报告保留和一次性运行流程。

## 0.4.2 — 2026-08-03

- 增加实时进度条和耗时计时，终端可看到模块执行状态和累计耗时。
- 重构 runner 与 terminal 的状态传递，补充窄终端和不同终端能力下的测试。
- 调整 CI 基准测试调度，降低常规提交被长耗时实测阻塞的概率。

## 0.4.3 — 2026-08-03

- 精简报告说明和参数展示，删除终端中不适合长期保留的原始全文输出。
- 保留方法、参数和状态等必要的可复核信息，减少重复解释和报告噪声。
- 修复 CI 中 i18n 基准依赖与报告样本之间的耦合问题。

## 0.4.4 — 2026-08-03

- 保留终端进度的完成历史，让已经完成的模块在后续运行过程中仍可追溯。
- 增加完成行、重绘和取消场景的终端回归测试。

## 0.4.5 — 2026-08-03

- 重构纯文本报告版式，重新组织模块标题、状态、数值表、说明列和空状态。
- 让纯文本输出在无 ANSI、管道和窄终端场景下仍保持可读和可解析。

## 0.4.6 — 2026-08-04

- 去重纯文本磁盘矩阵和方法标识，避免同一测试口径重复展示。
- 增加基准渲染回归测试，固定矩阵顺序和方法名称。

## 0.4.7 — 2026-08-04

- 将终端报告重构为模板化排版，统一不同模块的标题、状态、表格和说明结构。
- 重整纯文本渲染测试，覆盖不同模块组合和空结果。

## 0.4.8 — 2026-08-04

- 精简终端报告实现字段和解释列，减少重复的内部实现信息。
- 保留足以解释结果的指标、方法和参数，并补充对应快照式断言。

## 0.4.9 — 2026-08-04

- 收敛终端矩阵和解释性内容，统一表格列、模块说明和数据密度。
- 删除无法帮助判断结果的重复展示，继续保持结构化字段可复核。

## 0.4.10 — 2026-08-04

- 同步终端报告状态显示，确保模块状态与结果内容一致。
- 隐藏不适合直接展示的原始内容，避免终端报告重新膨胀为调试日志。

## 0.4.11 — 2026-08-04

- 为终端报告增加语义颜色层次，用颜色区分标题、状态、警告、错误和重点数值。
- 抽出可测试的终端颜色组件；无色终端仍保留文本语义。

## 0.4.12 — 2026-08-04

- 弱化报告横幅和分隔线，降低长报告中的视觉噪声。
- 保持模块边界和状态层次，不改变机器可解析字段。

## 0.4.13 — 2026-08-04

- 精简终端模块头部元数据，改善紧凑报告的纵向空间。
- 保留后续复核所需的模块身份和关键执行信息。

## 0.4.14 — 2026-08-04

- 汇总模块来源项目，统一展示各测试模块依赖的上游项目或公共数据源。
- 同步报告测试，避免来源说明在模块之间出现不同布局。

## 0.4.15 — 2026-08-04

- 对齐终端数值表中的柱状图，使柱长、数值和列边界保持一致。
- 增加不同宽度和不同数值量级下的对齐测试。

## 0.4.16 — 2026-08-04

- 为回程报告增加逐跳详情，展示目标、路径、命中线路和中间跳点等复核信息。
- 在增加路径证据的同时保持模块头紧凑，并补充回程解析和渲染测试。

## 0.4.17 — 2026-08-04

- 恢复模块头中的来源、执行命令、时间和版本信息布局。
- 修正报告紧凑化过程中丢失的执行元数据，确保结果可追溯到工具和运行口径。

## 0.4.18 — 2026-08-04

- 修正风险柱、跳过详情和相关终端状态的展示。
- 修复 BGP 最长匹配逻辑，补充前缀匹配边界测试。
- 加强 Ookla/路由结果的状态与失败渲染，使外部服务不可用时不被误写成成功。

## 0.4.19 — 2026-08-04

- 按资源拆分容量柱状图刻度，避免不同容量量级共用刻度造成误读。
- 增加各资源刻度和边界值的渲染测试。

## 0.4.20 — 2026-08-04

- 进度历史行只在模块完成时追加，减少运行中的重复刷新。
- 修正完成、跳过和失败状态的历史行时机，并增加终端回归覆盖。

## 0.4.21 — 2026-08-04

- 将进度完成行改为事件驱动输出，移除不必要的轮询式刷新。
- 简化终端进度实现，改善并行模块完成时的稳定性和可读性。

## 0.4.22 — 2026-08-04

- 将 CI 长时间测试调整为 nightly 或手动触发，普通提交只保留快速检查。
- 明确实网/长测与常规单元测试的触发边界，减少第三方限流导致的误红。

## 0.4.23 — 2026-08-04

- 从 CI 移除独立 benchmark job，避免与后续实网调度重复运行。
- 保留必要的测试、race、交叉构建和基线校验，缩短常规 CI 时间。

## 0.5.0 — 2026-08-06

这是提交、排行榜和一键运行链路的集中增强版本。

### 提交与排行榜

- 增加 `curl` 包装提交模式，并贯通 JSON、TXT、Markdown、HTML 报告格式的参数传递。
- 加固提交输出路径：预检查所有父目录，拒绝符号链接父目录，拒绝非有限数值，并省略未设置的元数据参数。
- 增加严格基线输入验证、带统一前缀的错误信息、排行榜命令别名和排行榜参考排序。
- 自动发现安全的云主机元数据，避免提交时把不应暴露的云实例信息随意带出。

### 外部工具与配置档

- 将配置档整理为 `standard` 与 `full`，并统一模块选择、输出格式和外联参数的透传。
- Ookla CLI 改为显式同意后才准备和运行：优先使用临时、可验证来源，信任包来自发行版来源，不把客户端永久装入主机。
- 扩展 `run.sh` 的临时依赖流程和错误降级，工具准备失败时报告缺失/跳过，而不是伪造成绩。

### 可靠性与 CI

- 修复翻译终端上的实时进度计时，并将 Linux TTY 与其他终端能力分开处理。
- 增加运行脚本、清单、交互向导、模块描述和报告格式的回归测试。
- 持续按最新提交重建排行榜参考，保证评分基线与提交库同步。

## 0.5.1 — 2026-08-06

- 修复临时基准工具暂存流程，确保依赖只进入本次运行的临时前缀并在退出时清理。
- 报告记录基准工具版本，补齐 CPU、内存、磁盘、网络、路由和回程模块的工具元数据。
- 为工具缺失、临时路径、报告版本和本地/临时工具优先级增加测试。

## 0.5.2 — 2026-08-06

- 合并实时进度修复，改善窄终端中的光标重绘、历史行和耗时显示。
- 统一 Linux TTY 和非 TTY 的降级路径，避免管道输出混入进度控制字符。

## 0.5.3 — 2026-08-07

- 修复 `fio` 临时工具暂存和全局进度输出，避免工具准备失败或进度行错乱。
- 增加运行脚本测试，覆盖暂存工具选择、报告输出和终端进度清理。

## 0.5.4 — 2026-08-07

- 修复基准柱状图比例，避免不同资源量级被错误拉伸或用错共享刻度。
- 增加比例、颜色、无色字符和边界值测试。
- 提交脱敏的本机样本，补充排行榜/评分基线参考；样本用途和非 VPS 代表性在文档中明确说明。

## 0.5.5 — 2026-08-08

### 测试范围

- 移除 VPS 通常无法可靠观察到的硬件健康检查，清理相关探针、安装需求、文案、第三方披露和样例字段。
- 不再把虚拟磁盘、容器或 VPS 缺失的硬件可见性误报为性能或健康故障。
- 强化保留的 `iperf3` 网络测速路径和真实服务器测试覆盖，修正网络模块边界行为。

### 报告与安全

- 同步 README、`SECURITY.md`、研究文档、安装脚本和完整样例，明确可见硬件与不可见硬件的边界。
- 保留本地报告、工具版本和方法说明，继续拒绝用硬件缺失替换成猜测性结论。

## 0.6.0 — 2026-08-09

这是标准基准工具供应链和发布资产的重构版本。

### 标准工具链

- 新增 `scripts/build_tools.sh`，从官方源构建六个工具：`sysbench`、STREAM、`fio`、`iperf3`、NextTrace Tiny 和 `ping`。
- 工具构建使用临时工作目录和临时暂存目录，记录上游版本、tag/commit、来源、构建参数和 SHA-256。
- 构建后检查 ELF 静态链接、运行时依赖和禁止的可选引擎，再运行功能 smoke；不使用发行版 benchmark 二进制或测试替身。
- 新增 `ecs-tools.manifest/v1`、manifest 示例和 `cmd/tools-manifest-check`，让每个架构的工具资产可被机器校验。

### 架构与发布

- 建立 Linux 七架构工具资产链路：`amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、`ppc64le`。
- 修复七架构 `sysbench` 构建，稳健化 `iperf3` 工具 smoke，统一工具阶段目录和权限归一化。
- 重构 `run.sh` 依赖准备：按架构下载并校验工具包和 manifest，缺失时安全降级，不修改主机系统工具。
- 重构 release/CI 的工具构建、打包、校验和资产上传，清理旧基线和过期样本，保证发布包中的六个工具完整可执行。

### 工程保障

- 增加 doctor、manifest、脚本输出、渲染器契约和工具构建测试。
- 明确构建工具、报告生成和排行榜基线各自的职责，避免开发机样本或构建 smoke 数值混入正式性能结论。

## 0.6.1 — 2026-08-09

### 隐私模型

- 修复本机 IP 脱敏边界：IPv4 保留前两段、隐藏后两段；IPv6 保留前四段、隐藏后四段；`IP:port` 保留端口。
- 改为按本机网卡地址和本次发现的本机出口 IP 精确匹配，只遮盖本机地址，不遮盖远端目标、BGP 前缀和路由逐跳 IP，保留线路复核价值。
- 将脱敏应用到 fields、tables、text blocks、摘要、说明和原始命令输出；JSON、TXT、Markdown、HTML 使用同一份脱敏结果。
- `--reveal` 只关闭本机 IP 脱敏，不改变远端线路信息；本机 IP allow-list 只存在内存中，不写入报告 JSON。

### 网络与报告模型

- 改进 IPv4/IPv6 egress、BGP、IP 质量、DNS、路由、回程和 `iperf3` 的边界处理及真实网络测试。
- 扩展报告 schema 和模型测试，明确敏感字段只代表本机 IP，避免把整个远端路径错误标成敏感。
- 为出口、BGP、网络测速和回程失败补充更清晰的状态、协议族和可复核信息。

### 发布流程

- 优化工具构建、打包和 release CI，校验 tag、主分支、工具来源和架构资产的一致性。
- 改善七架构工具阶段复用和发布前检查，减少因阶段目录、权限或缺失资产造成的发布失败。

## 0.6.2 — 2026-08-09

- 修复 QEMU 环境下对静态工具的误判，避免把合法的静态 ELF 当作动态依赖缺失。
- 调整工具构建验证顺序和架构安全的 ELF 检查，保持 QEMU 只负责最终目标架构的 smoke 执行。

## 0.6.3 — 2026-08-09

- 补齐其余标准工具的多架构交叉编译和 CI 发布支持，不再只交叉构建 `sysbench`。
- 明确原生 ARM、原生兼容、交叉编译和 QEMU smoke 的构建模式，并将目标 triplet、smoke runner 写入发布流程。
- 同步 `README`、`build_tools.sh` 和 CI 矩阵，覆盖七个 Linux 架构的工具阶段。

## 0.6.4 — 2026-08-09

### Manifest 契约

- 收紧 `ecs-tools` manifest 解析：`schema_version`、架构、`build`、`tools` 及嵌套字段必须存在且类型正确。
- 新增并校验 `toolchain_mode`、build/target triplet、`smoke_runner` 和 `build.validation` 元数据。
- 强制 CI 验证范围为 `functional`，`performance_valid` 必须为 `false`，明确构建 smoke 数值不是正式 benchmark 成绩。
- 继续校验六个工具的架构、版本、上游 tag/commit、源码来源、SHA-256、静态链接和精简状态。

### 测试与发布

- 增加 manifest 解析和验证回归测试，覆盖缺字段、错误类型、架构、工具数量、哈希和验证范围。
- 同步工具清单 schema、示例清单、README 和 CI 发布校验，使构建元数据不会与实际命令漂移。

## 0.6.5 — 2026-08-09

这是当前历史中的可靠性诊断和多报告对比版本，包含此前未单独发布的发布流程拆分提交。

### 结构化证据与失败

- 为结果增加有效样本/计划样本和稳定证据等级：`complete`、`partial`、`insufficient`、`not_planned`。
- 新增 `failures[]` 结构化失败字段，使用稳定的 `category`、`stage`、`target`、`retryable`、`count` 和原始 `message`，不要求下游解析人类可读文本。
- 分类覆盖超时、DNS 错误、连接拒绝、网络不可达、限流、HTTP 拒绝、TLS、解析失败、工具缺失、权限不足、不支持和取消等情况。
- 失败会合并重复样本，但不把语义性 warning 伪造成操作失败；TXT、Markdown、HTML 报告会展示结构化失败原因。

### 资源压力与条件复测

- 新增只读的 CPU、内存和 I/O PSI 采样，以及 cgroup CPU 限额、cpuset、throttle、内存事件、swap 和 OOM 诊断。
- CPU、STREAM 和 `fio` 只在测试前负载、steal、PSI、cgroup throttle 或 OOM 造成可识别干扰时自动复测一次；干净环境不会无条件延长测试。
- 复测选择先排除无有效证据的尝试，再采用干扰较低的一轮；干扰相同则保留首次结果，绝不按哪个 benchmark 数值更高来挑成绩。
- JSON 保留两次尝试的指标、证据、干扰测量和触发原因，报告读者可以复核为什么发生复测以及选中了哪一轮。

### 多报告对比

- 新增 `ecs compare`，支持两份到任意多份 JSON 报告，并使用 `ecs.compare/v1` 生成机器可读的比较模型。
- 只有模块 ID、指标 key、`method`、单位、优劣方向和机器参数口径完全一致的数值才会合并比较；不同工具版本、线程、时长、文件大小、目标集合或缺少参数口径的数值会进入不可比较原因。
- 比较结果同时保留模块状态、证据等级、缺失报告、事实字段变化、状态变化、证据变化和不可合并的口径问题，不把 IP、ASN、线路或平台状态的变化擅自判断成好坏。
- 以第 1 份报告为默认基准，可通过 `--reference` 选择其他报告；同组内提供排名、最佳/末位、相对变化和可比性摘要。
- 一次比较可输出 JSON、TXT、Markdown、HTML；2 份、3–5 份和 6 份以上报告使用不同的自适应布局，避免横向表格挤压。
- 比较完全在本地进行，不上传输入报告；增加 compare 构建、模型、渲染、参数口径和多报告边界测试。

### 报告、网络和文档

- 为 benchmark、DNS、网络、路由、BGP、黑名单、流媒体、NAT 和测速结果补充稳定指标 key、方法、参数和证据覆盖，供安全对比使用。
- 增强四种报告格式对失败、证据、重试和对比摘要的渲染契约，保持 JSON 为唯一事实来源。
- 拆分并新增 `README_EN.md`，同步中英文 CLI、报告、隐私、外联、评分和对比说明。
- 更新 `docs/schema.md`、研究文档和发布脚本说明，补充 `ecs.report/v1` 可选字段及 `ecs.compare/v1` 结构。

### CI 与发布流程

- 将工具构建/发布从普通 CI 中拆出，缩短本地和普通提交测试；普通 CI 不因发布工具七架构全量构建而延长。
- 普通 CI 继续运行单元、race、解析、渲染和必要的实网检查；工具发布流程单独负责资产计划、复用、构建、manifest 校验、打包和上传。
- 收紧代表性 `fio` 测试和发布前验证边界，明确 CI 验证功能正确性，真实 VPS 运行结果才是性能测量。

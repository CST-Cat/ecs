# Security

## 运行模型

`ecs` 只面向 Linux，不提供其他操作系统的代码路径或发布二进制。原生探针不需要 root。默认不会安装软件、修改内核参数、更新主机包管理器、写系统目录或上传报告。

`--ip-version 4`/`--ip-version 6`（也可用 `-4`/`-6`）会把可约束的网络探针限制在指定协议族；报告的 `run.ip_version` 与各模块的协议列记录实际口径。默认 `auto` 根据主机能力和模块自身的协议能力选择可用协议族；支持独立双栈测量的模块分别记录 IPv4/IPv6，并在没有真实全球 IPv6 路由时跳过 IPv6 压力探测。

磁盘模块只在用户指定目录创建名称随机的 `.ecs-fio-*` 临时文件，并在完成、错误或取消时清理。它会把文件限制在测试前可用空间的 20% 以内。

路由模块只调用官方 NextTrace Tiny，参数以数组传递，不经过 shell，并在报告中记录参数与程序 SHA-256。通过 `run.sh` 运行时，缺失的 Tiny 从当前架构的 `ecs-tools` 发行包取得：该包整体校验 `checksums.txt`，包内 `manifest.json` 再逐个校验每个 binary 的 SHA-256，通过后才放入本次 `$WORK/bin`；脚本退出时随工作目录清理。发行包本身在构建期从官方 NextTrace Release 取得对应 Linux Tiny asset，并强制比对 GitHub API 提供的 SHA-256 digest——digest 缺失即构建失败，不会以"无 digest"为由跳过校验。`ECS_AUTO_DEPS=0` 或任一环节校验失败会明确跳过路由模块，不安装其他路由程序。NextTrace Tiny 只使用无启动横幅的 JSON 输出模式。

磁盘模块只会调用 `PATH` 中现有的 `fio`，解析其本地 JSON 输出并记录参数、版本与程序 SHA-256。运行 `ecs` 本身不会调用包管理器；只有用户显式执行 `install.sh --with-benchmarks` 才会安装依赖。fio 临时文件仍受 20% 可用空间上限约束。

系统硬件清单只读 `/sys`、`/proc` 和 DMI，采集主板/BIOS、GPU、网卡与块设备等事实；不读取 MAC 地址、序列号，也不把 DMI 缺失当成性能失败。

`nat` 模块用标准库自行实现 STUN（RFC 5389/5780），只向公共 STUN 服务器发送 UDP Binding 请求。
请求内容是固定的协议头加一个随机事务 ID，不携带主机名、系统信息或任何本机数据；服务器能看到的
只有 UDP 包的源地址（本机公网出口）。响应必须通过事务 ID 与 magic cookie 校验才被采信——
UDP 上任何人都能往该端口发包，不校验就等于让第三方决定检测结果。报告中的映射地址按敏感字段遮盖。

路由报告会保留目标地址与中间跳点；分享报告前应按自身威胁模型检查这些原始路径信息。

`bgp` 只向 RouteViews 当前 RIB API 查询出口前缀的公共观测结果；它不上传 ecs 报告，也不声称能看到
私有互联或完整历史。`ookla` 是单独的外部适配器：standard 普通档不默认运行，full 或显式选中后，
官方客户端才会执行真实测速。若通过 `run.sh` 运行且本机缺少 `speedtest`，
脚本只会在 Debian/Ubuntu 从 Ookla 官方 Packagecloud HTTPS 源准备一次性依赖：先校验固定 GPG 指纹，再由 apt
验证签名并下载/解包到 `$WORK`；源文件、key、索引和缓存均位于 `$WORK`，不会写入 `/etc`，退出时随工作目录清理。脚本
不会执行供应商提供的 `curl | sh` 安装脚本；`ECS_AUTO_DEPS=0` 可关闭该准备步骤并让报告标记缺失。
Ookla 可能接收测量所需的出口 IP、客户端和服务器元数据，ecs 只提取选择后的字段，不能把该模式表述为零上传。

`cnspeed` 的社区节点清单不跟随上游 `main` 浮动，而是固定到每个 ecs 版本审计过的 commit。清单行只接受绝对 HTTP(S) URL，拒绝 userinfo、fragment、非法端口和特殊用途 IP。专用客户端不使用环境代理；它在实际拨号边界自行解析主机名，只对通过检查的公网字面地址拨号，并对每一次重定向重复校验，因此 DNS rebinding、内网/回环目标和重定向跳转不能把它变成 SSRF 通道。部分社区节点只提供 HTTP；这意味着测速流量可被链路上观察或篡改，结果是性能参考而不是机密性/完整性证明。

## 安装与供应链

建议从 Release 下载后核对 `checksums.txt`，或从源码自行构建。`install.sh`：

- 只接受 HTTPS；
- 强制验证 Release 资产的 SHA-256；
- 不使用 `--no-check-certificate`；
- 默认二进制安装不调用 `sudo` 或系统包管理器；
- 只有用户显式指定 `--with-benchmarks` 时，才使用检测到的系统包管理器安装 `sysbench`、`fio`、`iperf3`，并可能通过 `sudo` 执行安装；
- 不执行下载到的其他脚本。

这与 `run.sh` 的运行期临时 staging 是两回事：缺失工具时，`run.sh` 会把受校验的
`ecs-tools` 中本次需要的固定版本工具放入本次 `$WORK/bin` 的临时 PATH，退出时清理，
不安装到系统；这不等同于 `install.sh --with-benchmarks` 的显式系统包安装。

在最终 GitHub 仓库确定前，远程安装必须显式设置 `ECS_REPOSITORY=owner/ecs`，避免脚本指向虚假的默认仓库。

## 发布前门禁

Release 从一个**冻结的提交 SHA** 开始：入口处确认候选提交等于当时的远端 `main`，此后所有阶段只认这个 SHA，不再与移动中的 `main` 比较。

流水线按权限和制品交接分阶段，只有最后一步持有仓库写权限：

```
preflight → tools × 7 → assemble → verify / security → attest → publish
  read        read        read         read              OIDC     write
```

编译器、Docker、tar、`govulncheck`、构建脚本和全部第三方工具都运行在没有仓库写权限的阶段中。

- 打包前要求 Git 工作区洁净，CI 下载和中间目录全部显式忽略；
- 每个主程序归档**解包后**用 `go version -m` 校验 `vcs.revision` 等于冻结 SHA、`vcs.modified=false`，以及 Go 工具链等于本次构建的**实测**版本（取自 `go env GOVERSION`，不是写死在配置里的期望值）；
- 对解包出来的实际二进制运行 `govulncheck -mode binary`，这是发布前安全门禁；
- 为全部发布资产生成 GitHub artifact attestation，可用 `gh attestation verify` 核对来源仓库、workflow 与提交。

Go 工具链版本由 `go.mod` 单点定义，CI、发布和安全复审都读它。`staticcheck` 与 `govulncheck` 的版本锁在 `devtools/go.mod`（含 `go.sum` 哈希）。所有 GitHub Action 引用固定到 40 位 commit SHA，升级由 Dependabot 以可审阅的 PR 提出。

## 发布后持续监控

发布完成不代表安全生命周期结束：漏洞库每天都在更新，昨天干净的二进制今天可能已经不干净了。用户手上跑的是 Release 上的那些文件，因此每日复审的对象就是它们。

`security.yml` 每天做两件事：对当前 `main` 的源码跑 `govulncheck`，以及下载最新正式 Release 的七个主程序归档、校验摘要后逐个跑 `govulncheck -mode binary`。发现问题时分两类处理：

OSV 记录中的 `fixed` 版本只作为候选版本，不能单独触发自动升级。`security.yml` 的 release gate 只有在 Go 官方稳定 Release 列表中找到完全匹配且 `stable=true` 的正式版本（例如 `go1.26.7`）时才会置 `released=true`；未发布、查询失败或解析失败时仅输出告警、保持 `released=false`，因此不自动开升级 PR。

- **Go stdlib/runtime 漏洞，且官方已在同一小版本系列内给出修复版，并通过上述正式版本校验**（例如 1.26.6 → 1.26.7）：自动开一个只改 `go.mod` 一行的 PR。这类修复是可以机械确认的等式——同一份 ecs 源码 + 官方修好的工具链，程序自身不需要任何改动。
- **其余全部情况**：ecs 自身源码或第三方依赖的漏洞、只有跨小版本才有修复、漏洞库尚未给出修复版——只让 workflow 失败，交给人判断。

自动开出的 PR **不会自动合并**。合并与打 tag 由人决定，随后走与常规发布完全相同的那一条流水线，制品验证标准不因为是安全重建而降低。判定逻辑在 `scripts/security/triage.py`，其判定表由 `scripts/security/triage_test.sh` 覆盖。

注意：由 `GITHUB_TOKEN` 创建的 PR，其 CI 会停在 approval-required 状态，需要有写权限的人在 PR 页面点一次 **Approve workflows to run**。这是 GitHub 防止递归触发的既定行为。

## 报告隐私

默认在写 JSON 之前只遮盖本机 IP：IPv4 隐藏后两段（保留 /16），IPv6 隐藏后六组（保留 /32），端口号保留。IPv6 刻意比 IPv4 遮得更多：VPS 提供商普遍按 /64 给每台机器分配一整个子网，只隐藏后四组等于把实例本身指认出来。主机名、远端目标、BGP 前缀与路由跳 IP 保持完整。ecs 会遍历当前报告 schema 的所有导出字符串值（包括 `failures[].message`、复测诊断、表格、说明和原始输出），精确查找已知本机 IP；不会因为同一段文本含有远端 IP 就一并遮盖。原始运行对象的 `run.redacted` 是 `false`，只有完成遮盖的副本才是 `true`。`--reveal` 会把完整本机 IP 写入所有选中的报告格式，请只在可信目录中使用。报告文件权限默认为 `0600`，输出目录由 ecs 创建时权限为 `0700`。

在线模块仍会让目标服务看到发起请求的出口 IP；完整列表见 README 的“隐私与网络请求”章节。使用 `--exposure local` 可禁止这些模块运行，`--exposure public` 保留联网但不接触第三方情报服务。

IP 质量模块会把待查出口 IP 发送给启用的数据源。默认的 `all` 模式在没有用户密钥时会使用 IPQuality/check.place 社区通道查询部分上游，因此该服务会看到查询方出口和待查 IP。IPQS 的最后一级免密兜底会把包含待查 IP 的官方公开页 URL 发送给 Jina Reader，并允许读取一小时内缓存；进程存在系统 HTTP(S) 代理时，这个显式 IP 查询可以经代理发出，报告会披露对应通道。若威胁模型不接受中转、网页转换服务或系统代理，请配置官方 API 密钥，并用 `--ip-quality-sources` 只启用相应直连来源；`none` 会关闭附加质量查询，但出口发现仍会访问 ipapi.is。

API 密钥仅从进程环境读取，不支持写入 JSON 配置，也不会加入命令参数、报告来源 URL、错误消息或调试输出。报告只标记“用户密钥直连”，不记录密钥值。运行服务的环境、进程转储和 shell 历史仍由用户负责保护。

各 IP 风险服务可能记录查询、执行限流或按套餐隐藏字段。`ecs` 对单源失败只做已披露的逐级降级，不会把其他供应商的分值冒充失败供应商的结果，也不会把互不等价的分值平均为总分。DB-IP 官方 free API 不含威胁等级时，报告只保留其实际返回的字段并标记“部分”，不会推导或填充分数。

## 终端渲染边界

`ecs render` 可以渲染外部提供的 JSON，因此不把报告中的文本当作可信终端指令。txt 报告和 `ecs compare` 的终端输出会先复制数据，将 C0、DEL 和 C1 控制字符替换为普通空格（连续控制符合并），再排版和上色。这会阻断 OSC 0 改标题、OSC 52 写剪贴板、CSI 清屏/移动光标以及换行/回车伪造布局；唯一保留的 ANSI 序列是净化之后由 ecs 自己生成的 SGR 颜色。净化不修改输入对象或原始 JSON。

## 报告漏洞

请通过最终仓库的私有安全报告渠道提交以下问题：

- 命令注入、路径穿越或任意文件覆盖；
- 未经显式请求的结果上传或遥测；
- 遮盖模式泄露完整本机 IP，或错误遮盖远端 IP；
- 临时文件未清理或突破资源上限；
- Release 校验可绕过；
- HTML 报告中的脚本注入或主动外链加载。
- txt/终端报告中的 OSC、CSI 或其他控制序列注入；
- 由远端清单、重定向或 DNS rebinding 造成的内网/本机请求。

报告时请提供 ecs 版本、操作系统/架构、最小复现和预期影响，不要附带真实凭据或未经遮盖的生产报告。

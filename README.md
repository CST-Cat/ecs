# ecs

无广告、默认零上传、可审计的 VPS 综合测试工具。

`ecs` 不是把一批远程 Shell 脚本重新串起来。它以结构化结果为核心：每个探针先产生同一份带版本 JSON 数据，再由本地渲染器一次导出 JSON、txt、Markdown 和独立 HTML 报告。CPU 使用官方 sysbench CPU 工作负载；内存使用官方 STREAM；磁盘矩阵和 4KiB QD1 延迟统一来自 fio；网络吞吐使用 iperf3。综合评分单独按显式排行榜参考计算，不伪造替代工具成绩、不生成并行效率或跨节点平均值。IP 质量模块在遵守 AGPL 的前提下吸收 IPQuality 的多源覆盖与字段映射，并移除广告、运行计数和在线报告。

> 当前重构目标为 v0.6.0，仅支持 Linux。系统与资源、标准 CPU/内存/磁盘/网络基准、网络/IP、DNS、延迟、端口、服务可达性、公共 BGP 观测、路由、三网回程、full 默认包含的 Ookla 三网测速，以及 JSON/txt/md/html 四种输出已按当前实现维护。standard 可用 `--only ookla` 显式启用它。仍需更多真实 Linux VPS 样本校准公共节点、骨干特征表和平台规则。

## 快速开始

`ecs` 只支持 Linux。下面三条命令均为非交互模式：自动下载并校验 Release 二进制，准备缺失的
测试组件到一次性的 `/tmp/ecs-run.*` 临时前缀，生成本地报告，并在结束时清理临时目录。

```bash
# 完整测试：所有模块
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --yes

# 标准测试：日常综合测试
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile standard --yes

```

通过 `run.sh` 运行时，报告默认直接写入 `${TMPDIR:-/tmp}`，不会创建新的报告目录；如需固定目录，显式传入 `--output PATH`。默认文件名前缀带有本次运行的唯一后缀，也可以用 `--name PREFIX` 自定义。启用全部模块可能运行数分钟，`iperf3` 流量不封顶。

## 为什么再做一个

市面上的优秀脚本已经证明了融合测试的价值，但常见体验仍有几个问题：

- 测试过程或结果里混入赞助商广告；
- 默认把完整结果传到 Pastebin 或私有报告站；
- 终端文本很好看，却难以稳定解析或重新渲染；
- 运行时下载多个脚本/二进制，版本、摘要和真实执行内容不透明；
- IP 风险、流媒体“解锁”和磁盘成绩给出一个结论，却不保留方法与证据。

`ecs` 的约束很简单：

1. 运行时、安装器和报告永远不展示广告、返利链接或赞助内容。
2. 所有报告默认只写本地，没有自动上传代码路径。
3. 默认只遮盖本机 IP：IPv4 隐藏后两段，IPv6 隐藏后四段；主机名和远端 IP 保持完整。
4. 每项指标记录方法、参数、耗时、数据源、警告和原始证据。
5. 管道与 JSON 输出不混入 ANSI、进度条或交互提示。

## 一键运行说明

`ecs` **只支持 Linux**。VPS 几乎全是 Linux 发行版，因此项目不保留 macOS、Windows 或
BSD 的代码路径：多平台分支会迫使测试断言放宽到"哪个平台都成立"，真实生产路径反而
测不到。发布目标只覆盖 Linux 的七个架构：`amd64`、`arm64`、`armv7`、`386`、`s390x`、
`riscv64`、`ppc64le`。`armv7` 对应 `GOARCH=arm`、`GOARM=7`；不提供其他操作系统目标。
工具包的 `ecs-tools.manifest/v1`、每架构 `manifest.json` 与 CI 生成的 `checksums.txt`
是版本、来源、许可证和 SHA-256 的核对依据。源码中的 manifest 示例允许 `unknown`/
`unavailable`，实际版本、tag、摘要、许可证文件和 Release 资产在 CI 产出前不作任何猜测；
工作区里的本地 `dist/` 也不代表已发布资产。

脚本会下载并校验 `ecs`，并优先复用系统中已有的标准工具。本机缺少 `sysbench`、官方
`stream`、`fio`、`iperf3`、`nexttrace-tiny` 或 `ping` 时，才从与当前 Linux 架构匹配的
`ecs-tools` `tar.zst` 包临时提供；下载和使用前按 `checksums.txt`、`manifest.json` 及必要的
二进制 digest 核对架构、来源、版本和 SHA-256。工具只解包到本次运行的 `$WORK`，通过临时
`PATH`/库路径调用，退出时清理。APT/Packagecloud 不用于这些通用缺失工具；它们只在用户显式
选择 `--only ookla` 时作为 Ookla 的独立路径使用。没有可核对的资产时明确跳过相关模块，
不虚构版本或摘要。`WORK` 默认在 `/tmp` 下，显式 `TMPDIR=/absolute/path` 可作为高级覆盖；
`ECS_KEEP=1` 可保留现场排障。

如果不指定档位并且当前有终端，`run.sh` 会进入交互向导；没有终端时按 `standard` 档直接运行。

向导只问真正有代价的四件事，其余用推荐值，直接回车即可：

```
选择配置档
→ 1) standard  标准配置：16 个常规模块（推荐）
  2) full      完整配置：全部 18 个默认模块（含 Ookla；缺失时走独立官方路径）
请选择 [1]

检测 IP 质量与黑名单？会把出口 IP 发给 13 个数据源 [Y/n]
测试网络吞吐？iperf3 会跑满带宽，流量不封顶 [Y/n]
检测流媒体解锁？会访问 33 个平台的公开页 [Y/n]
检测路由与三网回程？耗时较长 [Y/n]
在报告中保留完整本机 IP？ [y/N]

即将运行
  配置档 standard
  模块 16 — system, network, bgp, cpu, memory, disk, dns, latency, ports, nat, blacklist, apps, media, route, backtrace
  预计 约 2–5 分钟
开始测试？ [Y/n]
```

CPU、内存、磁盘这类本地基准不做成开关——它们没有隐私或流量代价，关掉只会让报告残缺。
没有终端时（cron、CI、容器）向导自动跳过，按默认配置直接跑，不会卡在等输入。

`standard` 默认不包含 Ookla，`full` 默认包含 Ookla；full 缺失 `speedtest` 时会走独立的
Ookla 官方签名源路径。显式给出 `--only ookla` 仍可从任意配置档单独选择 Ookla。

`run.sh` 的依赖准备有明确边界：预先存在的程序保持原路径；缺失 `sysbench`、官方 `stream`、
`fio`、`iperf3`、`nexttrace-tiny` 或 `ping` 时，从当前 Linux 架构匹配的 `ecs-tools` `tar.zst`
包临时提供，并核对 `checksums.txt`、`manifest.json` 及必要的二进制 digest。工具只解包到
本次运行的 `$WORK`，经临时 `PATH`/库路径调用，退出时清理；不从发行版 APT 获取这些通用缺失
工具，也不安装到系统目录。没有可核对的架构资产时明确跳过相关测试。
Ookla (`speedtest`) 在 `full` 默认运行，`standard` 只有显式选择 `--only ookla` 才运行；
`--only ookla` 也可从任意配置档单独选择。本机缺失时脚本使用
Ookla 官方 Packagecloud HTTPS 源，下载并固定校验 GPG 公钥指纹，把临时源、key、索引和缓存
全部放在 `$WORK`，由 apt 验证签名后仅下载/解包，不执行供应商的 `curl | sh` 脚本。
Ookla 永不进入 `ecs-tools`。交互向导会先确定最终档位和模块，再准备对应组件。其他发行版没有安全
临时解包器时明确跳过相关测试；可用 `ECS_AUTO_DEPS=0` 直接接受缺失组件警告继续运行。
下载和解包诊断会收进 `$WORK/package-manager.log`；设置 `ECS_KEEP=1` 可保留现场。

```bash
# 禁用自动依赖准备，保留缺失组件警告
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | ECS_AUTO_DEPS=0 sh

# 排障时保留临时工作目录（不会安装或清理系统包）
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | ECS_KEEP=1 sh -s -- --profile standard
```

`curl | sh` 把信任完全交给下载源，这一点无法靠脚本自身解决——能做的是让下载的 `ecs-tools`
`tar.zst` 通过 `checksums.txt`、`manifest.json` 和必要 digest 校验，并且只在本次 WORK 内
解包使用。想长期安装二进制和基准工具，使用 `install.sh`；它的 `--with-benchmarks` 当前只安装
`sysbench`、`fio`、`iperf3`；官方 STREAM 不写入系统，由
`ecs-tools`/`run.sh` 临时提供，或由用户自行提供；CI 已对七架构构建、manifest、依赖审查和真实
smoke 做硬校验，运行时若缺少已核对资产则明确标记不可用，不生成替代成绩。路由模块只使用
官方 NextTrace Tiny；`run.sh` 在选中 route/backtrace 且本机缺失时，
按已核对的发布 manifest 选择对应 Linux 资产并校验 SHA-256 后放入本次临时 WORK，退出时清理；
没有已核对的资产就明确跳过，不虚构版本或摘要。

工具 CI 中，`amd64` 与 `arm64` 使用对应的 GitHub 原生 hosted runner，`386` 由 x86-64
硬件直接执行 32 位兼容 userspace；这三条路径不注册 QEMU。`armv7` 在 ARM64 runner 上用
`arm-linux-gnueabihf` 交叉工具链编译，只在最终真实 smoke 时显式调用 `qemu-arm-static`；
`s390x`、`riscv64`、`ppc64le` 同样由宿主原生运行对应交叉工具链，只让 QEMU 执行最终 smoke。
工具 CI 只验证构建与功能正确性，不验证性能；即使是原生 runner，smoke 日志里的 benchmark 数值
也不能作为正式性能结果。Manifest 会明确记录 `validation.scope=functional` 和
`performance_valid=false`：**CI = correctness，真实用户 VPS = measurement**。

**界面语言**：`--lang zh|en` 对**每个命令**都生效——`run`、`list`、`doctor`、`render`、
`config`、帮助文本与全部 29 个参数说明，以及 `run.sh` 自身的下载提示。未指定时按
`ECS_LANG`/`LC_ALL`/`LANG` 推断，都没有则用中文。

```bash
ecs --lang en doctor
ecs list --lang en
curl -fsSL .../run.sh | sh -s -- --lang en --profile full
```
**选定的语言适用于全部输出**：终端、JSON、txt、Markdown 与 HTML 一致。机器标识符不参与翻译
（模块 `id`、`measurement.key`/`method`/`unit`、`status`、`methodology.kind`），
因此下游按这些字段解析不受语言影响。外部工具的原始输出（sysbench/fio 的 stdout、
NextTrace JSON 路径）本身就是英文，原样保留——那是证据。

从源码构建需要 Go 1.22 或更高版本：

```bash
go build -trimpath -o ecs ./cmd/ecs
./ecs
```

直接运行编译好的 `ecs` 时，默认执行 `standard` 配置，并在 `./reports` 同时生成所选的四种格式：

```text
ecs-report-YYYYMMDD-HHMMSS.json
ecs-report-YYYYMMDD-HHMMSS.txt
ecs-report-YYYYMMDD-HHMMSS.md
ecs-report-YYYYMMDD-HHMMSS.html
```

安装刚刚编译的本地二进制：

```bash
./install.sh --from ./ecs
```

一次安装二进制和标准基准工具：

```bash
./install.sh --from ./ecs --with-benchmarks
```

连接到最终 GitHub 仓库并发布 Release 后，可使用带 SHA-256 校验的远程安装：

```bash
ECS_REPOSITORY=owner/ecs ./install.sh --with-benchmarks
```

默认安装器不会调用 `sudo`、不会修改包管理器，也不会关闭 TLS 校验。`--with-benchmarks` 当前只安装
`sysbench`、`fio`、`iperf3`，也不把 STREAM 交给系统包管理器；STREAM 由架构匹配的
`ecs-tools`/`run.sh` 临时提供，或由用户自行提供；缺少系统工具或已核对资产时明确标记不可用，不
生成替代成绩。`ping` 优先使用
系统已有实现，缺失时按上述 `ecs-tools` 临时路径处理。路由依赖不由安装器持久安装；`run.sh`
负责临时准备已校验的 NextTrace Tiny。Ookla 不属于 `ecs-tools`；full 缺失时由 run.sh 走独立官方签名源，standard 仅在显式选择时走该路径。

## 常用命令

```bash
# 默认综合测试
ecs

# 完整测试，但跳过服务可达性
ecs --profile full --skip media

# 标准性能主链路：官方 STREAM（内存）+ sysbench CPU + fio + iperf3
ecs --only system,cpu,memory,disk,speed

# 标准档只运行本地性能主链路
ecs --profile standard --only system,cpu,memory,disk --exposure local

# 显式写法与默认四格式一致，可用于覆盖输出目录
ecs --format json,txt,md,html --output ./my-report

# 报告中保留完整本机 IP（默认会按协议族遮盖后半段）
ecs --reveal

# 默认查询全部 IP 质量源；也可只启用指定来源
ecs --only network --ip-quality-sources ipapi,ipinfo,abuseipdb,ipqs

# 双栈默认分别探测；也可复用 NodeQuality 的 -4/-6 习惯只测一个协议族
ecs --ip-version 4 --only network,latency,ports
ecs -6 --only network,latency,speed,route

# 当前公共 RIB 观测：出口前缀、起源 ASN、RPKI、报告 peer 与 AS 路径样本
ecs --only bgp

# 四城三网 IPv4/IPv6 回程目标
ecs --only backtrace -6 --backtrace-city all

# standard 默认不启用；显式选择可在任意档位启用官方 Ookla 客户端
ecs --profile full --only ookla --ookla-servers "telecom=123,unicom=456,mobile=789"

# 仅显式选中 Ookla 时，从官方签名包源临时解包 speedtest
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --only ookla --yes

# 终端友好的纯文本报告（彩色柱状图，自适应终端能力）
ecs --format txt
ecs --format txt --color always      # 把颜色一并写进文件

# 从多台机器的报告聚合排行榜参考，再用它算分
ecs leaderboard --source "我的 VPS 集群" --output baseline.json ./reports
ecs --score-baseline baseline.json

# 从已有 JSON 重新导出
ecs render --input ./reports/ecs-report-20260731-120000.json --format json,txt,md,html

# 查看全部模块
ecs list

# 检查 sysbench、STREAM、fio、iperf3、ping、NextTrace Tiny 与 Ookla（若已安装）
ecs doctor

# 输出可修改的 JSON 配置样例
ecs config example
```

运行 `ecs run --help` 查看全部参数。

## 配置档与统一资源口径

| 配置档 | 默认模块数 | 性能主引擎 | CPU/内存每轮 | 临时磁盘上限 | 选中 `speed` 时的网络口径 |
| --- | --- | --- | ---: | ---: | --- |
| `standard` | 16（常规模块） | STREAM + sysbench CPU + fio + iperf3 | 15 秒 | 2048 MiB | 7 节点、双方向、每方向 15 秒，含 UDP 丢包/抖动 |
| `full` | 全部 18 个默认模块（含 Ookla） | STREAM + sysbench CPU + fio + iperf3 + Ookla | 15 秒 | 2048 MiB | 与 standard 相同；额外包含 cnspeed 与 Ookla |

两档只改变默认模块数量，不改变已选模块的测试深度：CPU/内存 15 秒，fio 使用完整
混合/Crystal/ATTO 矩阵，iperf3 使用 7 个节点、双方向 15 秒并附带 UDP 50 Mbps/5 秒；
显式 `--only` 可以从任意档位选中任意模块（例如 standard + `--only cnspeed,disk`）。

fio 文件最多使用测试前可用空间的 20%。iperf3 是按时长尽力跑满链路，实际流量随 VPS 带宽变化，无法用 MiB 预封顶；启动前会显示节点数、时长和并发流。

## 外联级别

配置档决定跑多少测试，外联级别决定**允许多少信息离开这台机器**。两者独立：
`--profile full --exposure public` 是"全套测试，但不碰商业 API"。

| `--exposure` | 含义 | 对方看到什么 |
| --- | --- | --- |
| `local` | 不联网 | 什么都看不到 |
| `public` | 只连公共基础设施 | 你的出口 IP——任何联网都免不了这一层 |
| `thirdparty`（默认） | 加上第三方情报服务 | 出口 IP，外加**被查询的 IP** 交给十余家商业 API |
| `any` | 放开上限到所有已登记的第三方服务 | 与 `thirdparty` 相同（最高级别） |

级别是**上限过滤器**，作用在 `--profile`/`--only`/`--skip` 选出的模块集上。
`ecs list` 会列出每个模块的级别。

```bash
# 只要本地基准，一个包都不发
ecs --exposure local

# 全套测试，但不把出口 IP 交给商业风控 API
ecs --profile full --exposure public

# 显式启用 Ookla；其客户端条款与数据处理独立于 ecs，且不进入 ecs-tools
ecs --exposure thirdparty --only ookla
```

`--exposure any` 是最高外联级别；当前所有模块均按 `local`、`public` 或
`thirdparty` 归类，Ookla 也属于 `thirdparty`，并在报告中保留其独立隐私说明。

被档位带进来的模块超出级别时静默过滤；被你用 `--only` 亲手点名的模块超出级别时
报错并给出该用哪个开关——显式的选择不该被悄悄丢掉。

**出口 IP 发现**：`network`、`blacklist`、`bgp` 都需要先知道本机公网地址。
这一步每次运行只做一次，结果三个模块共用，并按级别选择来源：`thirdparty`
及以上走 ipapi.is（带 ASN、地理、公司字段），`public` 走 STUN（只回一个映射
地址，判定逻辑全在协议里）。因此 `--exposure public` 下 `blacklist` 与 `bgp`
仍然可用，而待查 IP 不必交给第三方。STUN 拿不到 IPv6 地址时按查询失败处理，
不伪造数据。

## 模块

| ID | 报告口径 | 默认实现 | 关键限制 |
| --- | --- | --- | --- |
| `system` | 事实采集 | OS/runtime inspection，含 DMI/主板/BIOS、GPU/网卡/块设备、CPU 缓存、VT-x/AMD-V | 某些容器会隐藏 DMI/内核字段；不是基准 |
| `network` | 第三方评估 | 官方 API + IPQuality 社区兼容通道 | 各库口径不同且可能冲突，不能平均成总分 |
| `bgp` | 第三方评估 | RouteViews 当前 RIB：出口前缀、起源 ASN、RPKI、报告 peer 与 AS 路径样本 | 轻量公共观测；不是私有互联全图，也不含历史 MRT；每个协议族约 1 次查询 |
| `cpu` | 标准基准 | sysbench CPU，prime=20000，单/多线程 | 只与相同版本、参数、线程和时长比较；不派生并行效率 |
| `memory` | 标准基准 | 官方 STREAM 5.10 的 10,000,000 elements/10 iterations，`Copy`/`Scale`/`Add`/`Triad` 分别 1T/NT；缺失时模块明确告警且不生成替代成绩 | STREAM 原始单位、线程和版本必须留在报告证据中 |
| `disk` | 标准基准 | fio Direct I/O 基础/YABS 口径 4K/64K/512K/1M 混合矩阵 + Crystal RND4K/SEQ1M + ATTO 512B–64M + 4KiB QD1 latency + 磁盘库存 | 矩阵和 4KiB QD1 均由同一 fio JSON 产出；只与相同 fio/ecs 参数和文件系统比较 |
| `dns` | 协议测量 | 原生 DNS/UDP | 2–5 个样本的 P95 只作现场诊断，不是标准分 |
| `latency` | 协议测量 | 预解析后的 TCP 建连，并列系统 ping 的 ICMP 往返 | 解析耗时单列；TCP 明显快于 ICMP 时会警告握手可能被本地代理代答；受 Anycast/CDN 调度影响 |
| `speed` | 标准基准 | iperf3 TCP 多流正向/反向 + UDP 50 Mbps/5 秒、多节点 | 公共节点可能繁忙；按时长测试不封顶流量；所有档位同一节点/时长口径 |
| `ports` | 协议测量 | 原生 TCP 握手 | 单目标失败不能独立证明端口被封 |
| `blacklist` | 协议测量 | 17 个 DNSBL 查询 + 反向解析 FCrDNS 校验 | 各名单收录标准差异大，不可合并计分；127.255.255.x 是查询被拒而非命中 |
| `nat` | 协议测量 | 自实现 STUN（RFC 5389/5780）映射与过滤行为发现 | 只反映 UDP 路径，不代表 TCP；服务器不支持 CHANGE-REQUEST 时过滤行为报"未知"而不硬判 |
| `apps` | 协议测量 | Telegram 五个 DC 与代码/镜像/软件源/证书服务的 TCP 握手 | 可达不等于可用；CDN 会让握手在边缘节点完成 |
| `cnspeed` | 协议测量 | 三网就近节点 HTTP 下载（显式选中即可，8 秒/100 MiB） | 到具体节点的带宽，不代表到该运营商全网；清单来自社区且实时抓取 |
| `ookla` | 协议测量 | 本机官方 Ookla Speedtest CLI，可按用户提供的电信/联通/移动服务器 ID 串行测试；full 默认运行，缺失时 run.sh 可通过临时签名官方源准备 | standard 默认不运行；full 默认运行；`--only ookla` 可从任意档位显式选择；不进入 `ecs-tools`；Ookla 独立处理测量数据，不能称为零上传；会产生实际流量 |
| `media` | 启发式判断 | 33 个平台的分平台规则，含 Netflix 自制剧判定，可按 `--media-region` 筛选 | 不等同账号权益、注册、支付或实际播放；规则分强/弱证据标注 |
| `route` | 协议诊断 | 官方 NextTrace Tiny；正向路径快照 | 不等同回程，也不是性能基准 |
| `backtrace` | 启发式判断 | 官方 NextTrace Tiny，追踪四城三网 IPv4/IPv6 参考目标 + 骨干网段特征表，可按 `--backtrace-city` 选北京/广州/上海/成都 | 主动探测推断，非反向抓包；IPv6 目标依赖 DNS/IPv6 出口；未命中特征返回未识别 |

模块按干扰特性分组调度：性能基准（cpu/memory/disk）、大流量测速（speed/cnspeed/显式 ookla）、
路由类（route/backtrace）各自独占运行，避免互相污染——NextTrace 探测对并发较敏感，实测并发
会让关键跳全部无响应。轻量探测（dns/latency/ports/nat/blacklist/apps/media/network/bgp）
并行执行，它们等的是网络往返，并行只是把等待时间叠起来。
本机实测这组模块串行需 37.7 秒、并行 12.1 秒，省 67%。

任意配置档缺少标准工具时，对应模块显示黄色“标准基准未运行”警告，不产生替代成绩。所有终端、JSON、txt、Markdown、HTML 都保留 `methodology.kind`、引擎、工作负载和可比范围。

CPU、内存、磁盘和网络吞吐仍展示标准工具直接返回或按其公开单位换算的原始指标；网络吞吐逐节点、逐方向保存，不跨节点求平均或中位数。综合评分是单独的、相对所选排行榜参考均值的可解释视图，不会改写这些原始测量。

## IP 质量与欺诈值

`network` 默认执行 `--ip-quality-sources all`，覆盖 13 个数据源。`--ip-version 4`、`--ip-version 6` 或快捷方式 `-4`、`-6` 可把一次运行限制到指定协议族；`auto`（默认）在没有真实 IPv6 路由的主机上跳过 IPv6 压力探测，不把 ULA 地址当成公网 IPv6。每个 IPv4/IPv6 出口分别展示：

- MaxMind 使用地与注册地一致性，给出“原生 IP / 广播 IP”线索；
- IPinfo、ipregistry、ipapi、IP2Location、AbuseIPDB 的使用类型和公司类型；
- IP2Location、Scamalytics、ipapi、AbuseIPDB、IPQS、DB-IP 的原始或明确标注的等效风险值；
- IP2Location、ipapi、ipregistry、IPQS、Scamalytics、ipdata、IPinfo、IPWHOIS、DB-IP 的国家、代理、Tor、VPN、机房、滥用、机器人矩阵；
- 每个数据源的成功/失败、查询通道和耗时。缺密钥、限流或接口变化不会让对应行消失。

**默认运行不要求任何 API Key。** 免密模式会依次使用官方免密/tryout 接口、官方公开查询页和 IPQuality 的 `check.place` 社区通道；社区通道请求会串行并留出间隔，避免一次运行制造请求突发。IPQS 在社区共享额度或公开页出口配额耗尽时，可使用 Jina Reader 读取同一官方公开页的一小时内缓存；DB-IP 的风险网页不可访问时，会降级到官方 free API。

> **社区通道现状（2026-07 实测，两个出口）**：`ipinfo.check.place` 的可用性**取决于出口 IP**。
> 从 GitHub Actions（Azure AS8075）出口访问，四条路全部可用，11 个数据源 8 成功 3 部分 0 失败；
> 从一个 DigitalOcean 出口访问，四条路全部返回 403 与 Cloudflare 挑战页，
> MaxMind、AbuseIPDB、Scamalytics、ipdata 随之失败，`原生/广播 IP` 显示"无法判定"。
> 也就是说它没有下线，而是部分数据中心 IP 段被拦。落到被拦的出口时，
> 报告会如实标记"失败"，配置下表的官方密钥即可绕开社区通道。
> `ecs` 不会拿其他数据源的分值顶替失败的数据源。
> 可用 `go test -tags=live -run TestLiveCommunityGateway` 复核你自己出口的情况。

下列环境变量只是可选的“官方实时直连增强项”，不是运行前提。配置后能减少社区服务依赖，并可能获得免费公开页没有的付费字段；密钥不会写进命令行、日志或任何报告：

| 数据源 | 环境变量 |
| --- | --- |
| IPinfo | `IPINFO_TOKEN` |
| ipregistry | `IPREGISTRY_API_KEY` |
| IP2Location | `IP2LOCATION_API_KEY` |
| AbuseIPDB | `ABUSEIPDB_API_KEY` |
| Scamalytics | `SCAMALYTICS_USER` + `SCAMALYTICS_API_KEY` |
| IPQualityScore | `IPQS_API_KEY` |
| DB-IP Extended | `DBIP_API_KEY` |
| IPWHOIS Pro | `IPWHOIS_API_KEY` |

可选示例：

```bash
ABUSEIPDB_API_KEY='...' IPQS_API_KEY='...' ecs --only network
```

各家指标不是同一件事：AbuseIPDB 是指定窗口的滥用置信度，IPQS/IP2Location/Scamalytics 是各自模型的欺诈或风险分，ipapi 是公司/ASN 滥用概率。`ecs` 不把它们硬平均成一个“真相总分”。DB-IP 只有 `low/medium/high` 时，报告中的 `0/50/100*` 只用于画条形图，并带星号披露映射。原生/广播也只是使用地与注册地的一致性启发式，不等于住宅 IP 或低风险。

商业数据库的“实时完整字段”和“永久免密”不能同时保证。IPQS 官方 API 要求凭据，但官方公开查询页可以免密读取；DB-IP 官方 free API 只提供地理字段，`threatLevel` 属于 Extended 数据。`ecs` 会尽力走公开链路，但绝不会拿别家的分数冒充 IPQS/DB-IP 分数。报告中的“部分”表示数据库确实响应了、但该通道没有返回所有截图字段。

不想把出口 IP 交给社区中转或 Jina Reader 时，显式选择已配置的官方来源；完全关闭附加质量查询可用 `--ip-quality-sources none`。要连出口发现也不走商业接口，用 `--exposure public`（改由 STUN 取地址）或 `--skip network`。

## 报告

JSON 是事实来源，schema 当前为 `ecs.report/v1`。四种格式由同一份数据生成：

- **`txt`** 面向终端：分区标题、中文数字章节、按比例的彩色柱状图，适合 `cat` 看完顺手贴进论坛或聊天窗；
- **`md`** 适合 GitHub、论坛和工单；
- **`html`** 是单文件、无 JavaScript、无外部字体/图片/统计脚本，支持深色模式和打印；
- **`json`** 保留精确数值、显示值、测试方法、状态、来源、警告和路由原文；
- 每个模块明确标注“标准基准、协议测量、第三方评估、启发式判断或事实采集”，避免把所有数字都包装成标准成绩；
- `Ctrl+C` 会取消正在运行的探针、清理磁盘临时文件，并导出已经完成的部分。

导出不依赖 Pandoc、Node.js 或浏览器：

```bash
ecs --format json,txt,md,html
ecs render --input report.json --format json,txt,md,html --output ./exported
```

详细字段见 [报告 schema](docs/schema.md)。

### 颜色自适应

终端之间的差距不只是“有没有颜色”，而是**能不能寻址到某个颜色**：8/16 色只能说“给我红色”，
256 色可以说“第 196 号”，真彩色才能直接指定 RGB。链路上还会掉档——SSH 到缺 terminfo 的机器、
穿过配置不全的 tmux，真彩色都可能退回 256 甚至 16 色。

ecs 按 `COLORTERM`、`TERM` 与 `NO_COLOR` 逐级判定，并且**层次不单独依赖颜色**：
柱状图同时用 `░▒▓█` 四级密度字符表达高低，因此单色终端、纯文本文件、被 `grep`
过的输出里层次照样在。

```bash
ecs --format txt                  # 写进文件默认无色（可 diff、可粘贴）
ecs --format txt --color always   # 把颜色一并写进文件
ecs --color 256                   # 显式指定档位：none|basic|256|truecolor
NO_COLOR=1 ecs                    # 遵循跨工具约定，一律关闭
```

写入文件的 txt **默认不带转义序列**：报告会被 diff、贴进不解析 ANSI 的地方，
转义码在那里就是可见垃圾。需要彩色文件时用 `--color always`。

### 综合评分

分项分是**一步除法**：实测值 ÷ 排行榜参考均值 × 1000（延迟类为排行榜参考均值 ÷ 实测值 × 1000），读者可以手算复核。四个维度（CPU、内存、磁盘、带宽）均等权重，总分是已覆盖维度的算术平均。磁盘内部按基础项、`fio_mixed_*`、Crystal、ATTO 四个等权子组平均；内存按 STREAM 的 Copy、Scale、Add、Triad 四个等权子组平均，每个 kernel 的 1T/NT 取中位数。缺失的 STREAM 基线或指标会显式列出且不补零。混合矩阵的 8 个单元、ATTO 的 72 个读写单元都不会按数量放大。

v0.6.0 不携带旧工具口径的内嵌基线。没有显式 `--score-baseline` 或新的社区参考时，报告不生成综合分；这比把旧样本当成当前结果更可靠。

三条规则让分数可解释：

- **只累加真正跑过的维度**，覆盖度与分数并排显示。缺的维度既不按 0 也不按满分，缺失原因会直接列出；
- **分数不封顶**，跑赢排行榜参考均值一倍就是两倍分，截断会抹掉真实差距；
- **只在有分数分布时显示排行榜排名**。排行榜参考会保存不含主机标识的样本分数；少于 5 台或参考未保存分布时明确显示样本不足/暂不排名，不凭空捏造百分位。

**排行榜参考决定分数的含义**，因此它是可替换的数据而不是算法里的常数。当前发行包不预置
历史排行榜参考；横向比较应使用同一测试口径下的多台真实 VPS 重建：

```bash
# 跑完多台机器后，从报告聚合排行榜参考（每项取算术平均；离群值由 CI 另行提醒）
ecs leaderboard --source "我的 VPS 集群" --output baseline.json ./reports

# 之后用这份排行榜参考算分
ecs --score-baseline baseline.json
ecs render --input report.json --score-baseline baseline.json --format txt
```

### 提交进排行榜

完整报告不适合入库：三千多行，含出口 IP、主机名与逐跳路由，那些对排行榜没用却能定位机器。
提交走另一种格式 `ecs.submission/v1`，只带机器规格与跑分数值，每份约 3 KB：

```bash
ecs submit --input ./reports/ecs-report-*.json \
  --output ./submissions/2026-08/
```

导出时会优先从报告中安全白名单的本机元数据自动识别云厂商和地区（例如
cloud-init 或明确的 DMI 签名）；不会读取公网 IP、ASN、地理定位、主机名或原始
元数据。无法识别时对应字段留空；如需手动分组，可用 `--provider`/`--region` 显式覆盖。
字段是白名单——加字段要显式改 `internal/score/submission.go`，不会因为报告新增了
什么就悄悄多带出去。文件名前 12 位是内容指纹，手改数值而不重算会被 CI 发现。

把文件放进 `submissions/YYYY-MM/`，CI 校验格式、指纹与重复；进入主分支后自动重建
`submissions/baseline.json` 并同步进内嵌副本，后续发行包才会携带新的排行榜参考。
样本为空时不生成基线，运行也不会为评分额外联网。

详见 [submissions/README.md](submissions/README.md)。

### 按机型分档

全局平均值得到的是「平均机器」，而 4 核和 32 核混在一起比对两端都不公平——
多线程分数几乎正比于核数。举例说明量级：若样本里 2 核机器的多线程成绩约 1500、
16 核约 12000，全局平均值会落在两者之间，那台 2 核机器只能拿到两三百分，而它
其实完全正常——它只是被 16 核机器拉低了。按档比较才能得到接近 1000 的分数。

排行榜参考因此按 vCPU 分档（1/2/4/8/16/32/64+，云厂商的常见规格），评分时自动选
对应档位，报告里写明「本机 N 核，与 X–Y vCPU 档的机器比较」。

**某档样本少于 5 台就回落到全局排行榜参考均值**并在报告里说明：三台机器算出来的平均值
没有代表性，用它评判别人还不如老实说这档样本不够。

### 离群提醒

跑分库迟早会收到不合常理的数值：空载物理机冒充 VPS、开了写缓存的磁盘测试、
手改过的数字。判据不用预设阈值（没有先验分布，编一个「超过 X MB/s 就是假的」
只会在新硬件面前出丑），而是用 **MAD 的 modified z-score**——完全从样本自身
推出尺度，中位数与 MAD 对极端值都稳健，不会出现「离群值把判定标准本身撑大」
的循环。

```
离群提醒：
  e17c5d3b4627 的 disk_seq_write 比同档（8–15 vCPU，11 个样本）中位数高 12.6 倍（z=118.5）
  离群不等于造假，可能是新硬件或特殊配置；请人工判断是否收录。
```

比较在**同档内**进行——32 核的多线程分数天然远高于 2 核，混在一起会把大机器
全标成离群。同档样本少于 8 个时明确报「样本不足无法判定」而不是硬判：读者需要
知道「没报离群」是因为确实没有，还是因为压根没查。

CI 用 `--annotate` 把结果转成 GitHub 检查注解，**只标记不阻断**。

`ecs leaderboard` 会逐项列出样本数，并指出这批报告没覆盖到的指标——某个指标只有一两台
机器测到时，它对排行榜参考的代表性远低于其他项，这件事必须看得见。样本数不足时报告会明确提示
分数仅供自查。`ecs leaderboard`/`ecs baseline` 都可生成当前 `ecs.baseline/v1` 基线，
输出文件名使用 `baseline.json`，评分通过 `--score-baseline` 指定。

## 隐私与网络请求

默认只遮盖本机网卡地址和本次发现的本机出口 IP：IPv4 显示为 `A.B.x.x`，IPv6 展开后保留前四段、后四段显示为 `x:x:x:x`。端口号保留。主机名、BGP 前缀、远端目标和路由跳 IP 不脱敏，便于复核线路。

遮盖在写入 JSON 之前完成，并按精确 IP 匹配覆盖字段、表格和原始命令输出；即使本机 IP 出现在 NextTrace 原文中也会被遮盖，同一文本里的远端 IP 仍保留完整。`--reveal` 只关闭这项本机 IP 遮盖。默认生成的 JSON/txt/md/html 都使用同一处理结果。

在线配置可能连接以下第三方：

- 出口 IP 发现：`thirdparty` 及以上走 ipapi.is，`public` 走公共 STUN 服务器。每次运行只做一次，`network`、`blacklist`、`bgp` 共用同一结果；
- `network`：按 `--ip-quality-sources` 选择的 IPinfo、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQualityScore、DB-IP、ipdata、IPWHOIS。无用户密钥时，MaxMind 和部分风险源会经 `ipinfo.check.place` 查询；IPQS 最后一级免密兜底会把目标公开页 URL（其中含待查 IP）交给 `r.jina.ai`，并可能读取一小时内缓存；若进程配置了系统 HTTP(S) 代理，该只读兜底可经代理访问；
- `dns`：Cloudflare、Google、Quad9、AliDNS、DNSPod 的 UDP/53；
- `latency`：Cloudflare、Google、阿里云、腾讯、Amazon 的公开 TCP/443；
- `speed`：使用 YABS 维护清单中的 Clouvider/Leaseweb 公共 iperf3 节点；
- `ports`：Example、GitHub、Cloudflare DNS、Gmail 的公开端口；
- `media`：被检测服务自己的公开网页；
- `route`：配置中的路由目标；NextTrace Tiny 的在线 GeoIP 行为由 NextTrace 自身决定；
- `backtrace`：电信、联通、移动的公开参考 IP，用于识别路径上的骨干线路；
- `bgp`：RouteViews 当前公共 RIB API，查询本机 IPv4/IPv6 出口的匹配前缀、起源 ASN、RPKI、报告 peer 和 AS 路径样本；只做当前公共观测，不上传 ecs 报告；
- `cnspeed`：从 GitHub 抓取社区维护的中国测速节点清单，并对选中的三网节点做 HTTP 下载；
- `ookla`：`full` 默认或显式 `--only ookla` 选中后运行本机官方 speedtest 客户端；`run.sh` 缺失时仅从临时的 Ookla 官方签名源下载并解包到 WORK，退出时删除临时目录，客户端会连接 Ookla 测量服务并处理测速所需元数据，ecs 不保留原始 JSON；该客户端不进入 `ecs-tools`；
- `apps`：Telegram 官方 DC 域名，以及 GitHub、Docker Hub、npm、PyPI、Debian/Ubuntu/Alpine 源、Let's Encrypt、Cloudflare 的 TCP 端口；
- `blacklist`：17 个 DNS 黑名单的解析服务，只把反转后的出口 IP 作为域名查询；
- `nat`：公共 STUN 服务器（小米、1&1、Hoiio、Google、Cloudflare），只发送 STUN Binding 请求；
- `latency`：优先调用系统 `ping` 对同一目标发 ICMP；支持精简 ping（如 busybox）的三段统计行；系统 ping 完全不可用时只保留 TCP 建连，并明确标记，不用 TCP 数字冒充 ICMP。

显式选择协议族时，HTTP、TCP、UDP、iperf3、路由工具和能按族解析的目标都会使用对应的 `tcp4`/`tcp6`、`udp4`/`udp6` 或 `-4`/`-6` 参数；无法支持该协议族的固定目标会如实显示失败/跳过，不会用另一族的结果替代。

`--exposure local` 会跳过所有声明需要网络的模块，`--exposure public` 保留联网但不接触第三方情报服务。项目不包含 ecs 遥测、运行次数统计、Pastebin、报告站或隐藏的报告上传请求；显式启用 Ookla 后，外部客户端自身的数据处理规则另行适用。

VPS 测试默认忽略 `HTTP_PROXY`、`HTTPS_PROXY` 和 `ALL_PROXY`，避免把代理出口误当成服务器自身出口；若检测到这些变量，报告会留下说明。

IP 质量请求只发送待查询的出口 IP 和普通 HTTP 元数据，不发送 CPU、内存、磁盘、路由或其他报告内容。`check.place` 是第三方社区服务，会同时看到查询方出口和待查 IP；如不接受该信任边界，请使用官方密钥并通过 `--ip-quality-sources` 只选择直连来源。

## 配置文件

```bash
ecs config example > ecs.json
ecs --config ecs.json
```

配置文件支持覆盖资源参数，以及 DNS、延迟和路由目标。例如：

下面的示例显式写出 JSON/txt/Markdown/HTML 四种输出，与默认四格式一致，也可用于覆盖配置文件中的格式。

```json
{
  "profile": "standard",
  "exposure": "thirdparty",
  "ip_version": "auto",
  "skip": ["media"],
  "ip_quality_sources": ["all"],
  "formats": ["json", "txt", "md", "html"],
  "output": "./reports",
  "disk_path": "/var/tmp",
  "iperf_duration": "5s",
  "http_timeout": "10s",
  "latency_targets": [
    {"name": "My endpoint", "address": "example.com:443", "kind": "custom"}
  ],
  "backtrace_targets": [
    {"name": "自定义 IPv6", "address": "2001:db8::1", "kind": "自定义", "family": "6"}
  ],
  "ookla_servers": [
    {"carrier": "电信", "id": 123}
  ]
}
```

`exposure` 对应命令行的 `--exposure`。Ookla 的独立许可与隐私条款只在该模块实际运行
时生效，报告会保留相应说明。

未知字段会直接报错，避免拼写错误被静默忽略。命令行参数优先于配置文件。

**每一项配置都能只用命令行调节**，不必写配置文件——容器与一次性排查场景下这很重要：

```bash
# 换 DNS 解析器与采样次数
ecs --only dns --dns-resolvers "Ali=223.5.5.5:53,DNSPod=119.29.29.29:53" --dns-attempts 8

# 指定自建 iperf3 节点
ecs --only speed --iperf-targets "Home=my.iperf.example:5201-5210"

# 换延迟目标、路由目标与 STUN 服务器
ecs --latency-targets "example.com:443" --route-targets "1.1.1.1,8.8.8.8" \
    --stun-servers "stun.miwifi.com:3478"

# IPv6 主机名可在 JSON 中用 family: 6 固定；命令行的字面量和 -v6 主机名会自动识别
ecs --only backtrace -6 --backtrace-targets "北京电信 IPv6=bj-ct-v6.ip.zstaticcdn.com"
```

## 退出码

| 退出码 | 含义 |
| ---: | --- |
| `0` | 报告已成功生成；单个探针可能有跳过或降级 |
| `1` | 参数、配置、读取或写入错误 |
| `2` | 使用 `--strict` 且报告存在警告/探针错误 |
| `130` | 用户中断；已尽量生成部分报告 |

## 构建与验证

项目核心没有第三方 Go 依赖：

```bash
go test ./...
go vet ./...
go test -race ./...
make build
make cross
```

`scripts/package.sh VERSION` 的发布契约是 Linux 七个架构；`ecs-tools` 每个架构 `tar.zst` 包的
`manifest.json` 记录 sysbench、官方 STREAM、fio、iperf3、NextTrace Tiny、ping 六项，
并随包携带 `LICENSE`、`NOTICE` 和 CI 填充的 `LICENSES/`。Ookla 不进入 `ecs-tools`，只在
显式启用时调用本机官方客户端。CI 只对实际生成的资产写入 `checksums.txt`，并填充 manifest
中的版本、来源、许可证和 SHA-256；源码和本地 `dist/` 不承诺任何已发布版本或资产。报告
仍只写本地，输出格式固定保持 JSON/txt/md/html，schema 仍为 `ecs.report/v1`；新增字段
保持可选，工作负载变化只升级 `measurement.method`，破坏性变更才升级 schema。

## 调研与取舍

设计前横向阅读了 12 个项目，包括用户指定的 [oneclickvirt/ecs](https://github.com/oneclickvirt/ecs)、[spiritLHLS/ecs](https://github.com/spiritLHLS/ecs)，以及 YABS、LemonBench、bench.sh、SuperBench、nench、IPQuality、RegionRestrictionCheck、NextTrace、backtrace、ecsspeed。

逐项目调研提交、许可证、可取点、风险和由此形成的直接约束见 [竞品与上游能力调研](docs/research.md)。IP 质量模块明确采用 IPQuality 的多源覆盖、字段语义和风险分段思路，因此整个项目按 AGPL-3.0-only 发布并保留上游归属；其他 GPL 项目仍只通过独立进程适配，不把代码链接进核心。

实际运行时可选程序、许可证边界和在线服务见 [第三方组件清单](THIRD_PARTY.md)。

## 接下来

- 增加 MaxMind、DB-IP、IP2Location 等离线数据库适配器，降低在线 API 依赖；
- 增加 Phoronix/UnixBench 可选长测套件；
- 建立 Linux KVM、LXC、OpenVZ、低内存 NAT VPS 的回归样本库；
- 用真实海外 VPS 样本校准三网骨干特征表，并补齐 CN2 GIA/GT 的入口段判定；
- 把流媒体规则包拆成可独立更新、带过期时间的外部文件；
- 在不破坏 schema 的前提下增加中英文终端与报告；
- 发布稳定排行榜参考后冻结性能工作负载版本和比较规则。

## 许可证

[GNU AGPL v3.0](LICENSE)。IPQuality 相关归属与改动说明见 [NOTICE](NOTICE)。

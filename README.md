# ecs

无广告、默认零上传、可审计的 VPS 综合测试工具。

`ecs` 不是把一批远程 Shell 脚本重新串起来。它以结构化结果为核心：每个探针先产生同一份带版本 JSON 数据，再由本地渲染器一次导出终端、Markdown 和独立 HTML 报告。所有性能原始成绩只读取 sysbench、fio、mbw、iperf3 等明确记录版本与口径的工具；综合评分单独按显式排行榜参考计算，不伪造替代工具成绩、不生成并行效率或跨节点平均值。IP 质量模块在遵守 AGPL 的前提下吸收 IPQuality 的多源覆盖与字段映射，并移除广告、运行计数和在线报告。

> 当前处于首个开发版本，仅支持 Linux。系统与资源、SMART/温度、标准 CPU/内存/磁盘/网络基准、网络/IP、DNS、延迟、端口、服务可达性、公共 BGP 观测、路由、三网回程、可选 Ookla 三网测速、JSON/Markdown/HTML 已可运行。仍需更多真实 Linux VPS 样本校准公共节点、骨干特征表和平台规则。

## 快速开始

`ecs` 只支持 Linux。下面三条命令均为非交互模式：自动下载并校验 Release 二进制，准备缺失的
测试组件，生成本地报告，并在结束时清理本次新增组件。

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
3. 默认遮盖主机名与公网 IP；只有显式传入 `--reveal` 才保留完整值。
4. 每项指标记录方法、参数、耗时、数据源、警告和原始证据。
5. 管道与 JSON 输出不混入 ANSI、进度条或交互提示。

## 一键运行说明

`ecs` **只支持 Linux**。VPS 几乎全是 Linux 发行版，因此项目不保留 macOS、Windows 或
BSD 的代码路径：多平台分支会迫使测试断言放宽到"哪个平台都成立"，真实生产路径反而
测不到。发布二进制覆盖 Linux 的 `amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、
`ppc64le`。

脚本会下载并校验 `ecs`，按所选配置识别缺失的标准工具，从系统包管理器准备临时组件，
生成本地报告后清理本次新增的包。

如果不指定档位并且当前有终端，`run.sh` 会进入交互向导；没有终端时按 `standard` 档直接运行。

向导只问真正有代价的四件事，其余用推荐值，直接回车即可：

```
选择配置档
→ 1) standard  标准配置：16 个常规模块（推荐）
  2) full      完整配置：全部 18 个模块（含 Ookla）
请选择 [1]

检测 IP 质量与黑名单？会把出口 IP 发给 13 个数据源 [Y/n]
测试网络吞吐？iperf3 会跑满带宽，流量不封顶 [Y/n]
检测流媒体解锁？会访问 33 个平台的公开页 [Y/n]
检测路由与三网回程？耗时较长 [Y/n]
在报告中保留完整 IP 与主机名？ [y/N]

即将运行
  配置档 standard
  模块 16 — system, network, bgp, cpu, memory, disk, dns, latency, ports, nat, blacklist, apps, media, route, backtrace
  预计 约 2–5 分钟
开始测试？ [Y/n]
```

CPU、内存、磁盘这类本地基准不做成开关——它们没有隐私或流量代价，关掉只会让报告残缺。
没有终端时（cron、CI、容器）向导自动跳过，按默认配置直接跑，不会卡在等输入。

`run.sh` 的依赖准备有明确边界：开始前记录已安装包集合，只在缺失时调用系统包管理器，
结束时只清理本次新增的包；不执行 `autoremove` 或全局缓存清理，也不碰开始前已有的包。
Ookla (`speedtest`) 属于 `full` 档和显式 `--only ookla` 的模块；本机缺失时脚本使用
Ookla 官方 Packagecloud HTTPS 源，下载并固定校验 GPG 公钥指纹，把临时源、key、索引和缓存
全部放在 `$WORK`，由 apt/dnf/yum 验证签名后安装，不执行供应商的 `curl | sh` 脚本。`standard`
档不默认选择 Ookla。交互向导会先确定最终档位和模块，再准备对应组件。测试期间不要并行运行其他包管理器操作；若安装完成后的包状态被外部改变，脚本会
复用 `packages.before`/`packages.after` 快照并跳过清理，保留临时目录避免把外部新增包当成测试依赖删除。
包管理器的正常安装、更新和清理输出会收进临时日志，只在失败时显示末尾诊断；设置 `ECS_KEEP=1`
或发生清理失败时会保留现场日志。
无法获得 root/`sudo` 或没有受支持的包管理器时，脚本会停止并提示原因；可用 `ECS_AUTO_DEPS=0`
接受缺失组件警告继续运行。

```bash
# 禁止自动安装，保留 ecs 的降级行为
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | ECS_AUTO_DEPS=0 sh

# 排障时保留临时工作目录；本次新增的系统包仍会清理
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | ECS_KEEP=1 sh -s -- --profile standard
```

`curl | sh` 把信任完全交给下载源，这一点无法靠脚本自身解决——能做的是让下载的二进制
必须通过 SHA-256 校验，并让依赖只来自系统包管理器配置的仓库。想长期安装二进制和基准
工具，使用 `install.sh`；它仍然只在显式 `--with-benchmarks` 时持久安装组件。

**界面语言**：`--lang zh|en` 对**每个命令**都生效——`run`、`list`、`doctor`、`render`、
`config`、帮助文本与全部 29 个参数说明，以及 `run.sh` 自身的下载提示。未指定时按
`ECS_LANG`/`LC_ALL`/`LANG` 推断，都没有则用中文。

```bash
ecs --lang en doctor
ecs list --lang en
curl -fsSL .../run.sh | sh -s -- --lang en --profile full
```
**选定的语言适用于全部输出**：终端、Markdown、HTML 与 JSON 一致。机器标识符不参与翻译
（模块 `id`、`measurement.key`/`method`/`unit`、`status`、`methodology.kind`），
因此下游按这些字段解析不受语言影响。外部工具的原始输出（sysbench/fio 的 stdout、
traceroute 路径）本身就是英文，原样保留——那是证据。

从源码构建需要 Go 1.22 或更高版本：

```bash
go build -trimpath -o ecs ./cmd/ecs
./ecs
```

直接运行编译好的 `ecs` 时，默认执行 `standard` 配置，并在 `./reports` 同时生成：

```text
ecs-report-YYYYMMDD-HHMMSS.json
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

默认安装器不会调用 `sudo`、不会修改包管理器，也不会关闭 TLS 校验。只有显式给出 `--with-benchmarks` 时，才会通过检测到的 apt/dnf/yum/apk/pacman 安装 sysbench、fio、iperf3 和 traceroute。远程仓库尚未确定，因此项目没有硬编码一个虚假的下载地址。

## 常用命令

```bash
# 默认综合测试
ecs

# 完整测试，但跳过服务可达性
ecs --profile full --skip media

# 标准性能主链路：固定使用 sysbench + fio + iperf3
ecs --only system,cpu,memory,disk,speed

# 标准档只运行本地性能主链路
ecs --profile standard --only system,cpu,memory,disk --exposure local

# 只保留 JSON 和 HTML
ecs --format json,html --output ./my-report

# 报告中保留完整主机名与 IP（默认不会）
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

# 官方 Ookla 客户端（完整档自动包含；也可单独选择）
ecs --profile full --only ookla --ookla-servers "telecom=123,unicom=456,mobile=789"

# 一键脚本会从官方签名包源临时准备缺失的 speedtest
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --yes

# 终端友好的纯文本报告（彩色柱状图，自适应终端能力）
ecs --format txt
ecs --format txt --color always      # 把颜色一并写进文件

# 从多台机器的报告聚合排行榜参考，再用它算分
ecs leaderboard --source "我的 VPS 集群" --output baseline.json ./reports
ecs --score-baseline baseline.json

# 从已有 JSON 重新导出
ecs render --input ./reports/ecs-report-20260731-120000.json --format txt,md,html

# 查看全部模块
ecs list

# 检查 sysbench、fio、iperf3 与路由工具
ecs doctor

# 输出可修改的 JSON 配置样例
ecs config example
```

运行 `ecs run --help` 查看全部参数。

## 配置档与统一资源口径

| 配置档 | 默认模块数 | 性能主引擎 | CPU/内存每轮 | 临时磁盘上限 | 选中 `speed` 时的网络口径 |
| --- | --- | --- | ---: | ---: | --- |
| `standard` | 16（常规模块） | sysbench + fio + iperf3 | 15 秒 | 2048 MiB | 7 节点、双方向、每方向 15 秒，含 UDP 丢包/抖动 |
| `full` | 18（全部模块，含 Ookla） | sysbench + fio + iperf3 | 15 秒 | 2048 MiB | 与 standard 相同；额外包含 cnspeed 和 Ookla |

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
| `any` | 放开上限到所有已登记的第三方服务 | 与 `thirdparty` 相同（为兼容保留的最高级别） |

级别是**上限过滤器**，作用在 `--profile`/`--only`/`--skip` 选出的模块集上。
`ecs list` 会列出每个模块的级别。

```bash
# 只要本地基准，一个包都不发
ecs --exposure local

# 全套测试，但不把出口 IP 交给商业风控 API
ecs --profile full --exposure public

# 完整测试包含 Ookla；其客户端条款与数据处理独立于 ecs
ecs --profile full --only ookla
```

`--exposure any` 是为旧配置保留的最高外联级别；当前所有模块均按 `local`、`public` 或
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
| `system` | 事实采集 | OS/runtime inspection，含 DMI/主板/BIOS、GPU/网卡/块设备/RAID、CPU 缓存、VT-x/AMD-V、温度与只读 SMART 摘要 | 某些容器会隐藏 DMI/内核字段；SMART 需 smartctl/root，虚拟盘通常不透传；不是基准 |
| `network` | 第三方评估 | 官方 API + IPQuality 社区兼容通道 | 各库口径不同且可能冲突，不能平均成总分 |
| `bgp` | 第三方评估 | RouteViews 当前 RIB：出口前缀、起源 ASN、RPKI、报告 peer 与 AS 路径样本 | 轻量公共观测；不是私有互联全图，也不含历史 MRT；每个协议族约 1 次查询 |
| `cpu` | 标准基准 | sysbench CPU prime=20000，单/多线程 | 只与相同版本、参数、线程和时长比较 |
| `memory` | 标准基准 | sysbench memory 单/多线程读写与实际/明确派生时延 + mbw memcpy 带宽；并报告内存使用与可选 Balloon/KSM 证据 | sysbench 反复读写同一缓冲区会命中缓存，mbw 在两个大数组间搬运；可选内核接口缺失时明确 unavailable |
| `disk` | 标准基准 | fio Direct I/O 基础/YABS 4K/64K/512K/1M 混合矩阵 + Crystal RND4K/SEQ1M + ATTO 512B–64M + 磁盘容量/使用率/设备库存 + ioping 空载延迟 + smartctl 介质健康 | 所有档位都使用相同 10 秒完整 mixed/Crystal/ATTO 口径；ATTO 不含 5M；只与相同 fio/ecs 参数和文件系统比较 |
| `dns` | 协议测量 | 原生 DNS/UDP | 2–5 个样本的 P95 只作现场诊断，不是标准分 |
| `latency` | 协议测量 | 预解析后的 TCP 建连，并列系统 ping 的 ICMP 往返 | 解析耗时单列；TCP 明显快于 ICMP 时会警告握手可能被本地代理代答；受 Anycast/CDN 调度影响 |
| `speed` | 标准基准 | iperf3 TCP 多流正向/反向 + UDP 50 Mbps/5 秒、多节点 | 公共节点可能繁忙；按时长测试不封顶流量；所有档位同一节点/时长口径 |
| `ports` | 协议测量 | 原生 TCP 握手 | 单目标失败不能独立证明端口被封 |
| `blacklist` | 协议测量 | 17 个 DNSBL 查询 + 反向解析 FCrDNS 校验 | 各名单收录标准差异大，不可合并计分；127.255.255.x 是查询被拒而非命中 |
| `nat` | 协议测量 | 自实现 STUN（RFC 5389/5780）映射与过滤行为发现 | 只反映 UDP 路径，不代表 TCP；服务器不支持 CHANGE-REQUEST 时过滤行为报"未知"而不硬判 |
| `apps` | 协议测量 | Telegram 五个 DC 与代码/镜像/软件源/证书服务的 TCP 握手 | 可达不等于可用；CDN 会让握手在边缘节点完成 |
| `cnspeed` | 协议测量 | 三网就近节点 HTTP 下载（显式选中即可，8 秒/100 MiB） | 到具体节点的带宽，不代表到该运营商全网；清单来自社区且实时抓取 |
| `ookla` | 协议测量 | 本机官方 Ookla Speedtest CLI，可按用户提供的电信/联通/移动服务器 ID 串行测试；缺失时 run.sh 可通过临时签名官方源准备 | standard 默认不启用，full 或显式 `--only ookla` 可运行；Ookla 独立处理测量数据，不能称为零上传；会产生实际流量 |
| `media` | 启发式判断 | 33 个平台的分平台规则，含 Netflix 自制剧判定，可按 `--media-region` 筛选 | 不等同账号权益、注册、支付或实际播放；规则分强/弱证据标注 |
| `route` | 协议诊断 | NextTrace/traceroute/tracepath | 正向路径快照不等同回程，也不是性能基准 |
| `backtrace` | 启发式判断 | 四城三网 IPv4/IPv6 参考目标路径 + 骨干网段特征表，可按 `--backtrace-city` 选北京/广州/上海/成都 | 主动探测推断，非反向抓包；IPv6 目标依赖 DNS/IPv6 出口；未命中特征返回未识别 |

模块按干扰特性分组调度：性能基准（cpu/memory/disk）、大流量测速（speed/cnspeed/ookla）、
路由类（route/backtrace）各自独占运行，避免互相污染——traceroute 尤其敏感，实测并发
会让关键跳全部无响应。轻量探测（dns/latency/ports/nat/blacklist/apps/media/network/bgp）
并行执行，它们等的是网络往返，并行只是把等待时间叠起来。
本机实测这组模块串行需 37.7 秒、并行 12.1 秒，省 67%。

任意配置档缺少标准工具时，对应模块显示黄色“标准基准未运行”警告，不产生替代成绩。所有终端、JSON、Markdown、HTML 都保留 `methodology.kind`、引擎、工作负载和可比范围。

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
ecs --format txt,md,html,json
ecs render --input report.json --format txt,md,html --output ./exported
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

分项分是**一步除法**：实测值 ÷ 排行榜参考均值 × 1000（延迟类为排行榜参考均值 ÷ 实测值 × 1000），读者可以手算复核。四个维度（CPU、内存、磁盘、带宽）均等权重，总分是已覆盖维度的算术平均。磁盘内部按 legacy、`fio_mixed_*`、Crystal、ATTO 四个等权子组平均；内存按 memcpy、写、读、时延四个等权子组平均。混合矩阵的 8 个单元、ATTO 的 36 个读写单元都不会按数量放大，缺失项会显式列出且不补零。当前内嵌排行榜参考已包含本机完整模块测试的新矩阵，但只有 1 台 Oracle VPS 样本；收集更多提交后应重建 baseline。

```
总分              570   基于 3/4 个维度
CPU           ░░░░░░░·················      286
    单线程事件率                  785.9 events/s  排行榜参考均值的 29%
内存          ▒▒▒▒▒▒▒▒▒▒▒▒▒···········      556
磁盘          █████████████████████···      870
带宽          未测（未计入）
```

三条规则让分数可解释：

- **只累加真正跑过的维度**，覆盖度与分数并排显示。缺的维度既不按 0 也不按满分——
  参考实现里出现过“总分 3867 = CPU N/A + GPU N/A + 内存 2850 + 磁盘 1017”，两项缺失却照给总分；
- **分数不封顶**，跑赢排行榜参考均值一倍就是两倍分，截断会抹掉真实差距；
- **只在有分数分布时显示排行榜排名**。排行榜参考会保存不含主机标识的样本分数；少于 5 台或旧参考未保存分布时明确显示样本不足/暂不排名，不凭空捏造百分位。

**排行榜参考决定分数的含义**，因此它是可替换的数据而不是算法里的常数。当前内嵌排行榜参考是
Oracle Cloud Classic Free Tier 的单台 4 vCPU/约 24 GiB VPS 参考快照，样本数为 1，
只能用于自查；横向比较仍应使用多台真实 VPS 重建：

```bash
# 跑完多台机器后，从报告聚合排行榜参考（每项取算术平均；离群值由 CI 另行提醒）
ecs leaderboard --source "我的 VPS 集群" --output baseline.json ./reports

# 之后用这份排行榜参考算分
ecs --score-baseline baseline.json
ecs render --input report.json --score-baseline baseline.json --format txt
```

### 提交进排行榜

完整报告不适合入库：三千多行、含出口 IP 主机名与逐跳路由，那些对排行榜没用却能定位机器。
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

fork 仓库把文件放进 `submissions/YYYY-MM/` 开 PR，CI 校验格式、指纹与重复；
合并后自动重建 `submissions/baseline.json` 并同步进内嵌副本，下次发版随二进制发出去。
新用户 `curl … | sh` 拿到的就自带最新排行榜参考，**不需要额外联网**。

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

CI 用 `--annotate` 把结果转成 GitHub 注解显示在 PR 页面上，**只标记不阻断**。

`ecs leaderboard` 会逐项列出样本数，并指出这批报告没覆盖到的指标——某个指标只有一两台
机器测到时，它对排行榜参考的代表性远低于其他项，这件事必须看得见。样本数为 1 时报告会明确提示
分数仅供自查。旧命令 `ecs baseline` 仍是兼容别名；输出文件名 `baseline.json`、`--score-baseline`
参数和 `ecs.baseline/v1` schema 均保持不变。

## 隐私与网络请求

默认情况下，主机名显示为 `hidden`，IPv4 显示为 `A.B.C.x`，IPv6 只保留 `/48` 前缀。遮盖在写入 JSON 之前完成，因此默认生成的三个文件都不保存完整值。

遮盖同时覆盖表格与原始命令输出：`route` 与 `backtrace` 保存的 traceroute 原文会逐个 IP 按段遮盖，既不泄露完整地址，又保留 `59.43`、`202.97` 这类前缀供你复核线路判定是否成立。

在线配置可能连接以下第三方：

- 出口 IP 发现：`thirdparty` 及以上走 ipapi.is，`public` 走公共 STUN 服务器。每次运行只做一次，`network`、`blacklist`、`bgp` 共用同一结果；
- `network`：按 `--ip-quality-sources` 选择的 IPinfo、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQualityScore、DB-IP、ipdata、IPWHOIS。无用户密钥时，MaxMind 和部分风险源会经 `ipinfo.check.place` 查询；IPQS 最后一级免密兜底会把目标公开页 URL（其中含待查 IP）交给 `r.jina.ai`，并可能读取一小时内缓存；若进程配置了系统 HTTP(S) 代理，该只读兜底可经代理访问；
- `dns`：Cloudflare、Google、Quad9、AliDNS、DNSPod 的 UDP/53；
- `latency`：Cloudflare、Google、阿里云、腾讯、Amazon 的公开 TCP/443；
- `speed`：使用 YABS 维护清单中的 Clouvider/Leaseweb 公共 iperf3 节点；
- `ports`：Example、GitHub、Cloudflare DNS、Gmail 的公开端口；
- `media`：被检测服务自己的公开网页；
- `route`：配置中的路由目标；若使用 NextTrace，其在线 GeoIP 行为由 NextTrace 自身决定；
- `backtrace`：电信、联通、移动的公开参考 IP，用于识别路径上的骨干线路；
- `bgp`：RouteViews 当前公共 RIB API，查询本机 IPv4/IPv6 出口的匹配前缀、起源 ASN、RPKI、报告 peer 和 AS 路径样本；只做当前公共观测，不上传 ecs 报告；
- `cnspeed`：从 GitHub 抓取社区维护的中国测速节点清单，并对选中的三网节点做 HTTP 下载；
- `ookla`：选中后运行本机官方 speedtest 客户端；`run.sh` 缺失时仅从临时的 Ookla 官方签名源准备并在退出时清理新增包，客户端会连接 Ookla 测量服务并处理测速所需元数据，ecs 不保留原始 JSON；
- `apps`：Telegram 官方 DC 域名，以及 GitHub、Docker Hub、npm、PyPI、Debian/Ubuntu/Alpine 源、Let's Encrypt、Cloudflare 的 TCP 端口；
- `blacklist`：17 个 DNS 黑名单的解析服务，只把反转后的出口 IP 作为域名查询；
- `nat`：公共 STUN 服务器（小米、1&1、Hoiio、Google、Cloudflare），只发送 STUN Binding 请求；
- `latency`：除 TCP 建连外，还会调用系统 `ping` 对同一目标发 ICMP。

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

```json
{
  "profile": "standard",
  "exposure": "thirdparty",
  "ip_version": "auto",
  "skip": ["media"],
  "ip_quality_sources": ["all"],
  "formats": ["json", "md", "html"],
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

`scripts/package.sh VERSION` 会生成 Linux 七个架构的压缩包和 `checksums.txt`。GitHub Actions 同时在最低 Go 1.22 和当前稳定版测试，并在 `v*` 标签上生成 Release。

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

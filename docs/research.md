# ecs 竞品与上游能力调研

> 调研快照：2026-07-31  
> 方法：阅读公开 README、入口代码、输出格式、依赖下载逻辑和许可证。IPQuality 的多源覆盖、字段语义与分段规则按 AGPL 合规吸收并明确归属；其他项目只吸收功能思想与工程经验。
>
> 文中早期研究记录可能出现已弃用的基础路由工具名；当前实现和依赖策略已收敛为
> NextTrace-only，路由模块不再安装或调用其他实现。

## 样本选择

最先对照的是用户指定的两个融合测试项目：

| 项目 | 定位 | 值得吸收 | 需要避免或改进 | 许可证 |
| --- | --- | --- | --- | --- |
| [oneclickvirt/ecs](https://github.com/oneclickvirt/ecs) | Go 版综合测试套件 | 跨平台单二进制、组件化、非 root 降级、测试中断后保留结果 | 依赖树较大；默认上传；报告仍偏终端文本 | GPL-3.0 |
| [spiritLHLS/ecs](https://github.com/spiritLHLS/ecs) | Shell 版融合测试脚本 | 功能覆盖广、快速/完整/单项模式、国内三网场景完整 | 运行链会下载多个脚本与二进制；默认上传；运行和文档包含赞助内容 | MIT |

除上述两个项目外，选择了十个在不同细分方向有代表性的优秀项目：

| # | 项目（调研提交） | 主要方向 | 值得吸收 | 需要避免或改进 | 许可证 |
| ---: | --- | --- | --- | --- | --- |
| 1 | [YABS](https://github.com/masonr/yet-another-bench-script) `f8c6a48cd6ff` | Geekbench、fio、iperf3 基准 | 4K/64K/512K/1M 磁盘矩阵；JSON 输出；明确带宽警告；本地工具优先 | Geekbench 闭源且版本间不可直接比较；运行时下载预编译程序；完整网络测试流量高 | WTFPL-2.0 |
| 2 | [LemonBench](https://github.com/LemonBench/LemonBench) `659c366e2e2a` | 综合性能、流媒体、路由 | 快速/完整资源预算；单/半/全线程；FIO Direct；对测试误差与 Abuse 风险说明充分 | 需要 root；外部工具较多；旧版本结果不可直接横比 | MIT |
| 3 | [bench.sh](https://github.com/teddysun/across/blob/master/bench.sh) `fdb40962837b` | 轻量基础信息与测速 | 极低认知成本；I/O 多次取平均；虚拟化识别覆盖广；空间不足时安全跳过 | 结果缺少结构化 schema；固定测速节点易老化；运行时下载 Ookla CLI | Apache-2.0 |
| 4 | [SuperBench](https://github.com/oooldking/script) `7557507773ac` | 面向中国网络的快速测试 | 三网节点分组；快速与完整测速；终端表格紧凑 | 根目录未发现明确许可证；固定节点会失效；存在 `--no-check-certificate` 和外部文件链 | 未明确 |
| 5 | [nench](https://github.com/n-st/nench) `7a061485badb` | 一分钟级快速筛查 | 每个网络请求有硬超时；IPv4/IPv6 默认并列；ioping 延迟与吞吐兼顾；建议重复运行 | 项目已较久未更新；下载静态 ioping；100 MB × 多节点流量较高 | Apache-2.0 |
| 6 | [IPQuality](https://github.com/xykt/IPQuality) `44a55baec6cd` | IP 风险、流媒体、邮件端口 | 多数据源交叉验证；默认遮盖 IP；双栈、JSON、代理/网卡选择；单屏信息密度高 | 风险 API 的口径和可用性不一致；在线报告需主动关闭；运行/文档有赞助内容 | AGPL-3.0 |
| 7 | [RegionRestrictionCheck](https://github.com/lmc999/RegionRestrictionCheck) `b6d4a6f9a87f` | 流媒体区域可用性 | 平台规则覆盖广；免 root；尽量不引入额外依赖；维护清晰的平台支持矩阵 | 平台页面变化会使规则失效；终端文本不适合稳定解析；有赞助展示 | AGPL-3.0 |
| 8 | [NextTrace](https://github.com/nxtrace/NTrace-core) `d1a721aeacd6` | 路由、MTR、PMTU、节点标注 | TTY 与管道输出分离；严格 JSON 模式；ICMP/TCP/UDP；快速三网路由；Full/Tiny/NTR 构建变体 | 完整功能依赖在线 GeoIP/地图服务；GPL 代码不能直接纳入宽松许可证核心；运行展示赞助信息 | GPL-3.0 |
| 9 | [backtrace](https://github.com/zhanghanyun/backtrace) `55956a8447c0` | 三网回程识别 | 单一职责、跨架构小型 Go 二进制、输出简洁 | 数据与线路规则需要持续维护；缺少稳定的机器可读报告接口 | MIT |
| 10 | [ecsspeed](https://github.com/spiritLHLS/ecsspeed) `ee36867d2cf1` | 动态测速节点 | 节点列表自动更新；按运营商选低延迟节点；下载组件校验 SHA-256；JSON 导出 | 依赖 Speedtest 服务与动态节点质量；私有节点不可复现；多节点测试可能消耗大量流量 | MIT |

## 对 ecs 的直接设计约束

1. **运行时零广告**：二进制、终端、Markdown、HTML、JSON 和安装脚本均不展示赞助商、返利链接、二维码或推广语。
2. **默认零上传**：所有报告只写本地。首个稳定版不提供隐式上传路径；未来即使加入分享，也必须由用户显式指定目标。
3. **结构化数据优先**：探针先产生带 schema 版本的 JSON 数据，终端、Markdown 和独立 HTML 都由同一份数据渲染，避免三套结果互相漂移。
4. **标准性能工具唯一**：所有配置档的 CPU、内存、磁盘和网络吞吐原始成绩分别调用 sysbench、fio、mbw、iperf3（mbw 仅作内存补充口径）。项目不保留自研替代基准、自动回退、并行效率或跨节点均值；综合评分是独立的、基于可替换基线的相对视图。
5. **外部引擎必须可审计**：sysbench、fio、iperf3、NextTrace 等只作为可关闭的本地适配器；调用时记录可安全读取的版本、命令参数、程序摘要和数据来源。`run.sh` 只把 Debian/Ubuntu 签名源中的缺失工具下载并解包到临时 WORK，不调用系统安装器；需要持久安装时才显式使用 `install.sh --with-benchmarks`。
6. **资源预算可见**：`standard`、`full` 是 16/18 个模块的配置预设；运行前按实际选中的模块给出预计耗时、临时磁盘占用和网络流量，所有选中模块沿用统一深度口径；任何网络压力测试都可单独关闭。
7. **结果必须可比较**：每个性能结果记录引擎版本、块大小、队列深度、线程数、测试时长、样本数和时间戳。不同方法不混成一个总分。
8. **双栈是一等公民**：IPv4/IPv6 分开探测、分开记录失败原因，不能用“有地址”代替“可联网”。
9. **IP 质量不迷信单一分数**：保存每个数据源的原始判定、查询时间和错误；聚合结论必须显示置信度与冲突项。
10. **流媒体结论带证据**：规则按版本管理，输出 HTTP 状态、重定向地区或命中的页面信号；反爬、登录墙和网络错误返回“未知”，不能误报为不解锁。
11. **终端和文件输出分离**：TTY 可以有颜色与动态进度；管道、日志和 JSON 永远无 ANSI、无交互噪音。
12. **中断也有报告**：收到 `Ctrl+C` 后立即停止当前压力测试、清理临时文件，并导出已经完成的部分。
13. **只面向 Linux**：不保留 macOS、Windows 或 BSD 的代码路径与发布目标。VPS 几乎全是
    Linux 发行版，多平台分支的代价是测试断言被迫放宽到"哪个平台都成立"，真实生产
    路径反而失去覆盖（见下方第 4 条实测教训）。架构维度保留：Linux VPS 有 ARM 与
    RISC-V，发布覆盖 `amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、`ppc64le`。

## 首版功能矩阵

| 模块 | 非基准诊断 | 标准基准工具 | standard | full |
| --- | :---: | --- | :---: | :---: |
| 系统、虚拟化、资源、温度、SMART 与内核网络栈 | ✓ | smartctl + `/proc`/`/sys` 只读采集 | ✓ | ✓ |
| IPv4/IPv6、ASN、原生/广播、五库类型、六库评分、九库因子 | ✓ | 官方 API 密钥直连；IPQuality 社区通道；离线 GeoIP（规划） | ✓ | ✓ |
| CPU 单线程/多线程固定工作负载（cgroup 配额感知） | — | sysbench CPU（唯一） | 15s | 15s |
| 内存顺序读写、事件时延与 memcpy 补充带宽 | — | sysbench memory + 可选 mbw；Balloon/KSM 只读 sysfs/proc 证据 | 15s | 15s |
| 磁盘 legacy、Crystal、ATTO 与 50/50 混合矩阵 | — | fio JSON，Direct I/O，引擎探测回退 | 52 作业 | 52 作业 |
| DNS 延迟、失败率与抖动 | ✓ | — | ✓ | ✓ |
| TCP 延迟与可达率 | ✓ | 系统 ping 的 ICMP 往返 | ✓ | ✓ |
| 多节点上传/下载吞吐 | — | iperf3 JSON（唯一，逐节点原值） | 7 节点 × 15s | 7 节点 × 15s |
| UDP 丢包与抖动 | — | iperf3 UDP JSON | 与 speed 同时执行 | 与 speed 同时执行 |
| CPU steal 与容器资源真值 | ✓ | /proc/stat + cgroup v1/v2 | ✓ | ✓ |
| 常用及邮件端口出站能力 | ✓ | — | ✓ | ✓ |
| NAT 类型与 UDP 映射/过滤行为 | ✓ | 自实现 STUN（RFC 5389/5780） | ✓ | ✓ |
| 流媒体与 AI 服务区域检测（33 平台，强/弱证据分级） | ✓ | 内置规则包 v2 | ✓ | ✓ |
| 多目标正向路由 | NextTrace JSON | NextTrace full release（run.sh 临时校验） | ✓ | ✓ |
| 三网回程线路识别 | 骨干网段特征表 | NextTrace JSON | ✓ | ✓ |
| 当前公共 BGP/互联观测 | RouteViews 当前 RIB | HTTPS JSON API | ✓ | ✓ |
| 中国三网 HTTP 下载带宽（显式选中） | — | speedtest.cn 节点 HTTP | 8s/100 MiB | 8s/100 MiB |
| Ookla 三网测速 | 外部官方客户端 | 本机 speedtest CLI（full 或显式 `--only ookla` 时运行；run.sh 可按需从官方签名源下载并临时解包） | — | ✓ |
| JSON、Markdown、独立 HTML | ✓ | — | ✓ | ✓ |

两档配置只改变默认模块集合（standard 16、full 18）；表中标注“选中时”的模块
可以用 `--only` 从任意档位启用，并始终采用同一 full 深度参数。

## 许可证与复用边界

这些项目横跨 MIT、Apache-2.0、WTFPL、AGPL-3.0、GPL-3.0 和未明确许可证。`ecs` 不再为了维持宽松许可证而回避有价值的 IP 质量能力：

- IPQuality 的多源清单、字段对应关系、类型归类和供应商风险分段构成明确的实现输入；`ecs` 因此整体改用 AGPL-3.0-only，并在 `NOTICE`、报告来源和文档中保留项目、提交与许可证归属；
- 没有移植 IPQuality 的广告、赞助素材、运行计数、在线报告上传、依赖安装、流媒体或邮件检测代码；
- 网络实现、并发、密钥路由、结构化模型、终端/Markdown/HTML 渲染和错误隔离由 `ecs` 重新实现；
- sysbench、fio、iperf3、NextTrace 等程序仍作为独立进程调用，发行包不捆绑它们；`run.sh` 的缺失依赖只在 WORK 内解包，显式持久依赖安装才走操作系统包管理器。

## 首版之后的实测校准

功能矩阵完成后用真实网络与真实基准工具复跑，实测暴露并修正了以下只看代码发现不了的问题：

1. **回程跳数上限**：路径快照的 12 跳沿用到回程识别时，骨干特征段来不及出现——
   海外到中国通常在第 10–15 跳才进入 `202.97` / `59.43` / `219.158`。两个模块的跳数
   已拆开，回程用 20 跳。
2. **探测并发导致误判**：并发 6 个 NextTrace 时关键跳全部变成 `*`，同一目标单独跑
   却能稳定命中。运营商对 ICMP/UDP 探测普遍限速，因此回程追踪限制为 2 并发，并在
   多数跳无响应时明确标注"可能被限速"，而不是报告"未识别"。
3. **区域信号误报**：从最终 URL 提取两字母地区码会把 `youku.com/ku/` 读成 `KU`、
   把 `/eu/` 读成 `EU`。现在所有区域信号都必须通过 ISO 3166-1 alpha-2 校验。

   同时把通用规则的 404 从"不解锁"降级为"未知"：只请求首页时，404 说明入口 URL 变了，
   不能证明该地区不可用；实测中 Disney+ 与 Hulu 都因此被误判过。

4. **跨平台分支侵蚀了测试价值**：为了让 CI 在多平台上同时变绿，fio 适配器的假脚本
   被改成只报告 `psync`，于是测到的是同步引擎 QD1 路径；sysbench 的 steal 断言也被
   放宽成"允许缺失"。而真实 VPS 上 fio 用的是 libaio/io_uring，`/proc/stat` 必然可读
   ——被测的恰好不是生产路径。修法不是把断言写得更巧，而是删掉多平台代码：项目改为
   只面向 Linux，测试随之断言真实的队列深度与 steal 指标必须存在。

   顺带补上的一个真实差异：Alpine 等精简镜像默认是 busybox ping，统计行只有
   `min/avg/max` 三段而没有 iputils 的 `mdev`。原先的四段正则在这些镜像上解析不出
   RTT，现已增加三段回退，并区分"标准差为 0"与"该实现不报告标准差"。

5. **脚本替身证明不了任何事**：标准基准适配器的测试原先用 `#!/bin/sh` 假脚本冒充
   fio / sysbench / iperf3。这类替身只能证明"解析器认得它自己造出来的输出"，证明不了
   它认得工具的真实输出——真实 `fio --enghelp` 每行带 TAB 缩进、首行是
   `Available IO engines:`，真实 iperf3 的 JSON 字段名也随版本变过。三个替身已全部删除，
   改用真实工具：fio 与 sysbench 直接跑，iperf3 在回环起一个真实服务端，
   ping 与 NextTrace 打 `127.0.0.1`。全部不依赖公网。
   CI 因此会安装 fio/sysbench/iperf3，并在工具缺失时直接失败而不是静默跳过。

6. **TCP 与 ICMP 背离说明握手被代答**：一次真实运行里 `latency` 报告到 Cloudflare 的
   TCP 建连 0.11 ms、状态 `ok`，而同一张表的 ICMP 列是 221 ms——相差两千倍。
   核查发现网关上有透明代理代答 TCP 而不处理 ICMP，TCP 数字量的是到代理的距离。
   模块此前对这种矛盾毫无察觉，看起来一切正常。现增加交叉校验：TCP 中位数不到同目标
   ICMP 平均的 1/5 时判定为被截获，降级为 `warning` 并说明 TCP 列不能当作链路延迟。
   缺证据（没有 ICMP、ICMP 全丢、目标在本地网络）一律不下结论。
   这条恰好印证了当初并列 ICMP 的设计意图：两个独立测量互相矛盾时，
   至少要让读者知道有矛盾。

7. **社区中转的可用性取决于出口**：`ipinfo.check.place` 从一个 DigitalOcean 出口访问时，
   全部查询返回 403 与 Cloudflare 挑战页，依赖它的 MaxMind、AbuseIPDB、Scamalytics、ipdata
   四个免密数据源全部失败，`原生/广播 IP` 变成"无法判定"。
   **但这个结论只有一个数据点时是错的**：同期从 GitHub Actions（Azure AS8075）出口访问，
   四条路全部可用，11 个数据源 8 成功 3 部分 0 失败。它没有下线，是部分数据中心 IP 段被拦。
   代码行为在两种情况下都正确——如实标记失败、不拿别家分数顶替。
   教训是单一出口的观测不足以下"服务不可用"这种结论；实网测试进 CI 之后立刻提供了
   第二个出口，才推翻了第一版判断。

8. **节点清单不能凭记忆写**：`iperfNodePool` 里的 `ams.speedtest.eranium.net` 与
   `speedtest.tyo1.jp.leaseweb.net` 在三个独立 DNS 上都无 A 记录——它们从来就不存在，
   是照着印象编出来的；端口范围也有错（Eranium 实际是 5201-5210 而非 5200-5209）。
   现已逐条抄自 YABS `v2026-07-24` 的 `IPERF_LOCS` 数组并实测 7/7 可达。
   凡是外部清单（节点、端口、数据源地址）一律照抄上游并注明版本，改动后用实网测试复核。

9. **有 IPv6 地址不等于能用 IPv6**：`hostHasUsableIPv6()` 只排除了回环与链路本地，
   把 Tailscale 的 ULA（`fd7a::/48`）当成了可用的公网 IPv6，于是每个 iperf3 节点都白跑
   一轮 IPv6 测试并全部失败，在报告里留下一堆并非链路问题的"失败"行。
   注意 `net.IP.IsGlobalUnicast()` 对 ULA 同样返回 true，不能用它判断，得用 `IsPrivate()`。
   现在先要求存在全球可路由的单播地址，再用一次 UDP dial 确认内核确实有到公网 IPv6 的
   路由（UDP dial 只做路由查找、不发包）。这一条正是设计约束第 8 条
   "不能用'有地址'代替'可联网'"被自己违反的实例。

## 与同类项目的功能对比（2026-08-01 逐个抓源码核对）

不凭印象比较，逐个下载源码后对照实际实现：

| 能力 | bench.sh | YABS | superbench | nench | spiritLHLS | oneclickvirt | ecs |
| --- | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| CPU 基准 | — | Geekbench | — | 自研 AES/bzip2 | 串联 | cputest | sysbench |
| 内存基准 | — | — | — | — | 串联 | memorytest | sysbench |
| 磁盘基准 | dd | fio | dd | ioping+dd | 串联 | disktest | fio |
| 网络吞吐 | speedtest | iperf3 | speedtest | curl | 串联 | speedtest | iperf3 |
| IP 质量 | — | 基础 | 基础 | — | 串联 | securityCheck | 11 源 |
| 流媒体解锁 | — | — | — | — | 串联 | UnlockTests | 33 平台 |
| 路由 / 三网回程 | — | — | — | — | 串联 | backtrace/nt3 | ✓ |
| 出站端口 | — | — | — | — | 串联 | portchecker | ✓ |
| **NAT 类型** | — | — | — | — | gostun | **gostun** | **自实现 STUN** |
| DNS 质量 | — | — | — | — | — | — | **✓（同类中独有）** |
| CPU 缓存 | ✓ | — | ✓ | — | ✓ | basics | ✓ |
| VT-x / AMD-V | ✓ | ✓ | — | — | ✓ | basics | ✓ |
| 结构化 JSON | — | ✓ | — | — | — | ✓ | ✓ |
| 零广告 / 零上传 | ✓ | 可选上传 | ✗ | ✓ | ✗ | — | ✓ |

补齐的差距：

- **NAT 类型**是唯一的实质功能缺口，只有 oneclickvirt 与 spiritLHLS（均调用 gostun）具备。
  已按 RFC 5389/5780 自行实现，无第三方依赖。与 gostun 一类实现的区别在于对
  "测不出来"的处理：多数公共 STUN 服务器禁用或**忽略** CHANGE-REQUEST，
  实测 `stun.l.google.com` 与 `stun.cloudflare.com` 会照常从原地址回包——
  只看"有没有回包"就会把对称型 NAT 后的机器误报成全锥型 NAT1。
  因此判定过滤行为时必须核对响应的源地址，核不上就报"未知"。
- **CPU 缓存**与 **VT-x/AMD-V**（决定能否跑嵌套虚拟化）几乎所有竞品都采集，已补入 `system`。

保留边界的能力：

- **Geekbench**：闭源，结果处理受其客户端与条款控制，且不同大版本之间不可直接比较；已有 sysbench 时不值得作为默认依赖。
- **自研 AES/bzip2 压缩跑分**（nench 的做法）：违反"性能成绩只能来自标准工具"的约束。
- **串联他人脚本**（spiritLHLS 的做法）：运行时下载多个脚本与二进制，版本、摘要与真实执行内容都不透明。
- **完整私有互联拓扑**：公共 RouteViews 只能提供当前可见 RIB、报告 peer 和 AS path 样本，不能冒充运营商内部 peer graph 或完整历史 MRT。

## 与 oneclickvirt/ecs、spiritLHLS/ecs 的逐项差距（2026-08-01 按 README 参数面核对）

用户指定的这两个项目是功能覆盖最全的同类。逐条对照它们的命令行参数与 README 声明，
`ecs` **并未覆盖全部功能**。如实记录差距，不夸大；目前新增的 Ookla 与 BGP 能力仍保留各自的外部服务边界。

### 已覆盖（能力相当或更强）

| 能力 | 对方实现 | ecs |
| --- | --- | --- |
| 系统基础信息 | basics | `system`，含 cgroup 配额、steal、CPU 缓存、VT-x/AMD-V |
| NAT 类型 | gostun | `nat`，自实现 STUN，且区分"服务器不支持"与"被过滤" |
| CPU / 内存 / 磁盘 | cputest / memorytest / disktest | sysbench + fio |
| IP 质量 | securityCheck / IPQuality | `network`，11 源，保留通道与失败状态 |
| 流媒体解锁 | UnlockTests | `media`，33 平台，强/弱证据分级 |
| 邮件端口 | portchecker | `ports` |
| 三网回程 | backtrace | `backtrace` |
| 路由 | nt3 | `route` |

### 尚未覆盖（真实缺口，按对 VPS 评测的价值排序）

1. **完整私有 BGP 互联图与历史事件**。`bgp` 已能查询 RouteViews 当前公共 RIB 的匹配前缀、起源、RPKI、报告 peer 与 AS path 样本，
   但公共观测无法看到供应商内部 peer graph、完整 MRT 历史或所有未公开会话。
2. **三网就近 Ping**。oneclickvirt 的 `-ping`（源自 ecsspeed）按运营商选低延迟节点。
   `ecs` 的 `latency` 是固定 5 个全球站点，不是三网就近。
3. **多挂载盘 IO**。oneclickvirt 的 `-diskmc`、spiritLHLS 的 `-mdisk` 都支持测试系统盘
   之外的挂载盘。`ecs` 只测 `--disk-path` 指定的单个路径。
4. **流媒体地区选择**。oneclickvirt 的 `-utregion` 有 20 种地区组合，`ecs` 目前只保留
   global/jp/tw/hk/cn 五组，协议族可用 `--ip-version`/`-4`/`-6` 选择。
5. **Telegram DC 测试**（oneclickvirt `-tgdc`）与**热门网站可达性**（`-web`）。
6. **STREAM 内存带宽**。oneclickvirt 的 `-memorym` 默认就是 stream，`ecs` 只有 sysbench。
   STREAM 是内存带宽的行业标准口径，与 sysbench 的微基准不是一回事。

### 有意不做（与项目约束冲突，不属于"缺口"）

- **Geekbench**（两个项目都支持）：闭源，免费版强制上传结果，大版本间不可直接比较。
- **结果自动上传**（spiritLHLS 默认传 pastebin、oneclickvirt 的 `-upload` 默认开）：
  与"默认零上传"直接冲突。
- **dd 作为磁盘测试**：dd 测的是缓存与顺序写的混合，误差大且不可比；fio 已覆盖。
- **菜单交互模式**：与"管道/JSON 输出不含交互噪音"的约束冲突，命令行参数已足够。

### ecs 相对这两个项目的独有部分

- `dns` 模块（公共解析器延迟、失败率、抖动）：两个项目都没有。
- 17 个 DNSBL 区域与 FCrDNS 组合检查：不把公共解析器拒绝码误报成黑名单命中。
- 带版本的结构化 JSON schema，Markdown/HTML 由同一份数据渲染，可 `ecs render` 重放。
- 每项指标标注 `methodology.kind`（标准基准/协议测量/第三方评估/启发式/事实采集）与可比范围。
- 默认遮盖主机名与 IP，覆盖字段、表格与 NextTrace 原文。
- 零广告、零上传、不串联下载他人脚本。

## 两个项目的技术栈与开源替代评估（2026-08-01 实测）

用户指定核对 oneclickvirt/ecs 与 spiritLHLS/ecs 的技术栈，判断各组件是否有可靠的开源替代。
逐项核实上游许可证、Debian 包可用性，并实跑关键候选。

| 能力 | 两个项目使用 | 该技术是否开源可靠 | 可用的开源替代 | ecs 现状 |
| --- | --- | --- | --- | --- |
| 系统信息 | basics（GPL-3.0）/ bench.sh 等拼装 | ✅ | 标准库读 `/proc`、`/sys` | 已自实现，另有 cgroup、steal |
| NAT 类型 | gostun（GPL-3.0） | ✅ | 自实现 STUN RFC 5389/5780 | 已自实现 |
| CPU | sysbench / **geekbench** | sysbench ✅ GPL-2.0；geekbench ❌ 闭源且强制上传 | sysbench | 已用 sysbench |
| 内存 | sysbench / dd / **mbw** / **stream** | mbw ✅ Debian 有包；STREAM ✅ 但无 Debian 包需自编译 | **mbw**（Debian `mbw` 1.2.2） | 只有 sysbench，可补 mbw |
| 磁盘 | fio / dd | fio ✅ GPL-2.0 | fio + **ioping**（延迟）+ **smartmontools**（SMART） | 已用 fio + ioping；system/disk 只读接入 smartctl |
| 流媒体 | UnlockTests（GPL-3.0）、RegionRestrictionCheck（AGPL-3.0） | ✅ | 自实现规则引擎 | 已自实现 |
| 邮件端口 | portchecker（GPL-3.0） | ✅ | 标准库 TCP | 已自实现 |
| 回程 / 路由 | backtrace（MIT 衍生）、nt3（GPL-3.0，基于 NTrace-core） | ✅ | 自实现特征表 + NextTrace 适配器 | 已自实现 |
| DNSBL 黑名单 | IPQuality 的"400+ 数据库" | 协议是标准 DNS A 查询 | **自实现**（`dns.go` 的查询栈已具备） | 缺，零依赖可补 |
| IP 质量数据库 | securityCheck + 20 余家商业 API | 代码 ✅ / **API 本身闭源黑盒** | **无替代**——只能如实标注来源与失败 | 已用 11 源并披露通道 |
| **三网测速** | ecsspeed / oneclickvirt-speedtest，基于 speedtest.net + speedtest.cn | **Ookla 官方 CLI 闭源 + EULA + 外部数据处理**；服务器目录与出口策略会变化 | librespeed-cli ✅ LGPL-3.0（Debian 有包）；showwin/speedtest-go ✅ MIT（Ookla 协议实现） | `ookla` 作为第三方模块适配本机 CLI；full 默认包含，直接运行需预装，run.sh 可从官方签名源按需准备，三网需配置服务器 ID |
| 三网 Ping | pingtest（借鉴 ecsspeed） | ✅ | ICMP 已具备，缺的是节点数据而非技术 | `latency` 为固定全球站点 |

### librespeed-cli 实跑结论（开源替代，但不是中国三网等价物）

装 Debian 官方包 `librespeed-cli 1.0.11`（上游 librespeed/speedtest-cli，LGPL-3.0）实测：

```json
{"server":{"name":"Amsterdam, Netherlands (Clouvider)"},
 "bytes_sent":83525632,"bytes_received":124925326,
 "ping":231,"jitter":1.35,"upload":42.83,"download":64.06}
```

可用之处：JSON 输出字段完整（ping/jitter/上下行/字节数），支持 `--server` 指定、
`--exclude` 排除、`--local-json` 自带节点列表，完全开源可审计。

但有三个必须知道的限制：

1. **没有中国大陆节点**。44 个公共节点里亚洲只有印度、新加坡、日本各一个，
   因此**替代不了"三网测速"**——那正是中文 VPS 圈最看重的一项。
2. **静默失败**：节点不响应时输出 `null` 且退出码仍为 `0`。若接入必须显式检测，
   否则会把失败当成"测到了 0"。实测 5 个节点中 Tokyo、Frankfurt 均不可用，第三个才成功。
3. 单次约消耗 208 MB 流量，需计入资源预算。

### 结论

两个项目的技术栈里，**除中国三网测速、商业 IP 数据库和完整私有 BGP 图外，其余都有可靠开源替代，且 ecs 大多
已用更彻底的方式实现**（自实现协议、零第三方 Go 依赖，不下载他人二进制）。

- 可立即采用的 Debian 官方开源包：`mbw`（内存带宽）、`ioping`（I/O 延迟）、
  `smartmontools`（SMART 与通电时间）、`librespeed-cli`（通用 HTTP 测速）。
- **三网测速没有零外部服务的开源解**：`librespeed-cli` 可审计，但公共节点不覆盖中国三网的
  同等服务器集合；`ookla` 适配器因此只支持显式调用官方客户端，并把条款、实际流量和外部数据处理写进报告。
- 商业 IP 数据库无开源替代，这是行业事实；ecs 能做的是保留原值、通道与失败状态，
  不平均、不顶替——这一点已经做到。

## 两档配置的模块入口（2026-08-03）

两档不是质量等级，而是预先列好模块集合的入口。所有被选中的模块都使用同一套
full 深度参数，保证直接运行与 `--only` 运行的结果可以比较。

| | standard | full |
| --- | ---: | ---: |
| 默认模块数 | 16 | 18 |
| CPU/内存每轮 | 15 s | 15 s |
| fio 临时文件与作业 | 2048 MiB，上限安全检查；52 项完整 mixed/Crystal/ATTO | 同左 |
| iperf3（选中 `speed` 时） | 7 节点 × 双方向 × 15 s，附 UDP 50 Mbps/5 s | 同左 |
| `cnspeed`（选中时） | 8 s/100 MiB 每运营商 | 同左 |

`standard`、`full` 仅分别预选 16、18 个模块。`SelectModules` 先处理
`--only` 再处理 `--skip`，因此 standard 也可以直接运行 `--only cnspeed,disk`，而
`--skip` 仍可从任何预设中移除模块。Ookla 属于普通 thirdparty 模块，不需要额外确认。

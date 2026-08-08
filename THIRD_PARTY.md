# Third-party components and services

`ecs` 只面向 Linux，Go 依赖仍只有标准库。IP 质量模块采用 [xykt/IPQuality](https://github.com/xykt/IPQuality) 的多源覆盖、字段映射与风险分段思路，项目因此整体按 AGPL-3.0-only 发布；归属和差异见 [NOTICE](NOTICE)。

## 可选本地程序

这些程序可以由用户预先安装，也可以由 `run.sh` 在一次性测试期间从 Debian/Ubuntu 已配置且
签名的软件源下载并解包到本次 `$WORK/root` 临时前缀（通过 `$WORK/bin` 调用），或通过安装器的
显式 `--with-benchmarks` 选项持久安装。`ecs` 仅以独立进程调用，
不随发行包分发：

| 程序 | 用途 | 上游 | 许可证 | ecs 行为 |
| --- | --- | --- | --- | --- |
| sysbench | CPU 素数计算与内存顺序读写 | [akopytov/sysbench](https://github.com/akopytov/sysbench) | GPL-2.0 | CPU、内存模块唯一基准引擎；解析文本统计；记录参数、版本与 SHA-256 |
| fio | Direct I/O 磁盘测试 | [axboe/fio](https://github.com/axboe/fio) | GPL-2.0 | 磁盘模块唯一基准引擎；解析 JSON；记录参数、版本与 SHA-256 |
| iperf3 | TCP 多流上传/反向下载 | [ESnet/iperf](https://github.com/esnet/iperf) | BSD-3-Clause | standard/full 的网络吞吐唯一基准；解析逐节点 JSON 原值；记录节点、参数、版本与 SHA-256 |
| NextTrace | 带节点信息的路由追踪 | [nxtrace/NTrace-core](https://github.com/nxtrace/NTrace-core) | GPL-3.0 | 路由模块唯一引擎；本机缺失时 `run.sh` 可从官方 GitHub Release 临时下载并校验 API digest；强制 JSON、无启动横幅；记录参数与 SHA-256 |
| mbw | memcpy 口径的内存带宽 | [ahorvath/mbw](http://ahorvath.web.cern.ch/ahorvath/mbw/) | GPL-2.0 | `memory` 的补充口径，与 sysbench 并列保留不合并；数组大小按可用内存收敛，避免小内存机器 OOM |
| ioping | 单请求 Direct I/O 延迟 | [koct9i/ioping](https://github.com/koct9i/ioping) | GPL-3.0 | `disk` 的补充口径；用 `-D` 与 fio 的 direct=1 同口径 |
| ping | ICMP 往返与丢包 | 操作系统发行方 | 随发行版而异 | `latency` 模块的 ICMP 列；兼容 iputils 与 busybox 两种统计行格式；参数以数组传入、不经过 shell；不可用时只保留 TCP 结果 |
| speedtest | Ookla 外部测速 | [Ookla Speedtest CLI](https://www.speedtest.net/apps/cli) | 闭源，独立条款 | `full` 或显式选择 `ookla` 时运行；缺失时 `run.sh` 只从 Ookla 官方 Packagecloud 签名源下载并解包到本次 `$WORK`，不写入系统包数据库；不保留原始 JSON，客户端仍会向 Ookla 测量服务发送其所需数据 |

`nat` 模块不调用任何外部程序：STUN（RFC 5389/5780）由 `ecs` 用标准库自行实现，
只发送 Binding 请求，不含 TURN、ICE、认证或消息完整性。

`run.sh` 不调用任何系统包安装/卸载命令：缺失组件只下载到 `$WORK/packages`，用
`dpkg-deb -x` 解包到 `$WORK/root`，并通过临时 `PATH`/库路径使用；预先存在的程序保持原路径，
测试结束时整个 `$WORK` 一并删除。Debian/Ubuntu 之外若没有安全的临时解包器，脚本明确跳过相关
测试，不偷偷全局安装。Ookla 缺失且模块被选中时，脚本把官方 Packagecloud 源、固定指纹的 GPG
公钥、索引和缓存全部放在 `$WORK`，由 apt 验证签名后仅下载/解包，不执行
供应商的 `curl | sh` 安装脚本；退出时工作目录一并消失。`ECS_AUTO_DEPS=0` 会跳过所有安装，
让报告如实标记缺失模块。`install.sh --with-benchmarks` 是持久安装的明确入口；两条路径都不替
用户接受闭源软件许可证。Geekbench 因闭源和免费版结果处理边界不作为默认依赖；Ookla 只提供显式、可审计的本机客户端适配器。

## 在线服务

在线配置会直接连接：

- [ipapi.is](https://ipapi.is/)：出口 IP、ASN、地理、类型、风险因子与公司/ASN 滥用概率；出口发现每次运行只做一次，由 `network`、`blacklist`、`bgp` 共用，`--exposure public` 时改用公共 STUN 服务器取地址、不访问本接口；
- [IPinfo](https://ipinfo.io/developers)：网络/公司类型与隐私信号；
- [ipregistry](https://ipregistry.co/docs/)：网络类型与代理、Tor、VPN、机房、滥用信号；
- [IP2Location.io](https://www.ip2location.io/ip2location-documentation)：类型、代理因子与 IP2Proxy 欺诈分；
- [AbuseIPDB](https://docs.abuseipdb.com/)：使用类型与滥用置信度；
- [Scamalytics](https://scamalytics.com/)：Web 流量欺诈分与匿名化信号；
- [IPQualityScore](https://www.ipqualityscore.com/documentation/proxy-detection-api/overview)：欺诈分与代理、VPN、Tor、近期滥用、机器人信号；
- [DB-IP](https://db-ip.com/api/doc.php)：威胁等级、代理与爬虫信号；
- [ipdata](https://ipdata.co/)：代理、Tor、机房和已知威胁信号；
- [IPWHOIS](https://ipwhois.io/documentation)：国家、代理、VPN、Tor 与托管信号；
- [ip-api.com](https://ip-api.com/)：免密，提供 proxy / hosting / mobile 布尔信号；免费端点仅 HTTP 且有频率限制；
- [ip.sb](https://ip.sb/)：免密，仅地理与运营商，用作国家归属的交叉验证；
- [IPQuality/check.place](https://check.place/)：无用户密钥时，为 MaxMind、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQS、ipdata 提供社区兼容中转；
- [Jina Reader](https://github.com/jina-ai/reader)（Apache-2.0 服务实现）：当 IPQS 社区额度和官方公开页出口配额同时不可用时，读取同一公开查询页的一小时内缓存；不提供或代算供应商分数；
- YABS 当前公共 iperf3 节点清单中的 Clouvider/Leaseweb 测速服务：standard/full 的多节点 TCP 吞吐；
- 电信、联通、移动的公开参考 IP：`backtrace` 模块用于识别路径上的骨干线路，只发送路由探测包；
- [RouteViews API](https://api.routeviews.org/docs/)：`bgp` 模块查询当前公共 RIB 的匹配前缀、起源 ASN、RPKI、报告 peer 和 AS path 样本；不下载历史 MRT、不上传 ecs 报告；
- 四城三网 IPv4/IPv6 回程目标：IPv4 使用公开参考地址，IPv6 使用地区节点域名并强制 `family: "6"`；目标服务会看到路由探测的源地址；
- [Ookla Speedtest](https://www.speedtest.net/about/privacy)：仅在用户显式启用并确认条款后由本机官方客户端连接；Ookla 的数据处理不属于 ecs 本地报告保证范围；
- DNS 黑名单服务（Spamhaus、SpamCop、Barracuda、CBL、PSBL、blocklist.de、UCEPROTECT、DroneBL、s5h、SpamRats、GBUdb、Mailspike、Backscatterer）：`blacklist` 模块把出口 IP 反转后作为域名查 A 记录，不发送其他信息。各名单有各自的收录标准、解除流程与查询配额；部分名单（如 Spamhaus）拒绝来自公共解析器的查询并返回 127.255.255.x，ecs 将其判为“查询被拒”而非命中；
- [spiritLHLS/speedtest.cn-CN-ID](https://github.com/spiritLHLS/speedtest.cn-CN-ID)（MIT，每日更新）：`cnspeed` 模块的中国测速节点清单，含运营商标注。清单实时抓取而不内置快照——节点会下线，用过期清单去测已消失的节点比不测更糟。ecs 只读取清单，不复制其内容入库；测速本身直接连清单里的节点，不经过第三方；
- 公共 STUN 服务器（`stun.miwifi.com`、`stun.1und1.de`、`stun.hoiio.com`、`stun.l.google.com`、`stun.cloudflare.com`）：`nat` 模块的 UDP Binding 请求。STUN 请求本身不携带任何本机信息——服务器看到的只是 UDP 包的源地址，也就是本机公网出口；返回的映射地址是判定 NAT 类型的依据；
- README 中列出的公共 DNS、延迟、端口以及服务自身公开网页。

第三方服务会看到请求出口 IP，并可能有各自的日志、速率限制、套餐字段差异与隐私政策。社区中转和 Jina Reader 还会看到被查询 IP；报告的数据源状态表会区分“官方 API”“官方免密/公开接口”“IPQuality/check.place 中转”和网页读取兜底。`--exposure local` 会跳过全部在线探针；`--exposure public` 保留联网但排除第三方情报服务，此时出口 IP 改由 STUN 发现；`--ip-quality-sources none` 只关闭附加质量源。

官方 API 凭据只从 `IPINFO_TOKEN`、`IPREGISTRY_API_KEY`、`IP2LOCATION_API_KEY`、`ABUSEIPDB_API_KEY`、`SCAMALYTICS_USER`、`SCAMALYTICS_API_KEY`、`IPQS_API_KEY`、`DBIP_API_KEY`、`IPWHOIS_API_KEY` 读取。它们不会进入配置文件示例、进程参数、错误文本或报告。

## 调研项目

`backtrace` 的三网回程识别思路与参考目标选择来自 [zhanghanyun/backtrace](https://github.com/zhanghanyun/backtrace)（MIT）；骨干网段与 AS 对应关系属于运营商公开事实，未复制上游代码或数据文件。磁盘的 50/50 混合矩阵沿用 [YABS](https://github.com/masonr/yet-another-bench-script)（WTFPL-2.0）的块大小与队列深度口径。

oneclickvirt/ecs、spiritLHLS/ecs 与另外十个项目构成功能、交互和工程风险调研样本。逐项提交快照、许可证与取舍见 [docs/research.md](docs/research.md)。其中 IPQuality 是已归属的 AGPL 上游；其他项目的源码、规则文件和报告模板未复制进本项目。

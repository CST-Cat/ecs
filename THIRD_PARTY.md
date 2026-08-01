# Third-party components and services

`ecs` 只面向 Linux，Go 依赖仍只有标准库。IP 质量模块采用 [xykt/IPQuality](https://github.com/xykt/IPQuality) 的多源覆盖、字段映射与风险分段思路，项目因此整体按 AGPL-3.0-only 发布；归属和差异见 [NOTICE](NOTICE)。

## 可选本地程序

这些程序由用户预先安装，或通过安装器的显式 `--with-benchmarks` 选项交给 Linux 发行版的包管理器安装。`ecs` 仅以独立进程调用，不随发行包分发：

| 程序 | 用途 | 上游 | 许可证 | ecs 行为 |
| --- | --- | --- | --- | --- |
| sysbench | CPU 素数计算与内存顺序读写 | [akopytov/sysbench](https://github.com/akopytov/sysbench) | GPL-2.0 | CPU、内存模块唯一基准引擎；解析文本统计；记录参数、版本与 SHA-256 |
| fio | Direct I/O 磁盘测试 | [axboe/fio](https://github.com/axboe/fio) | GPL-2.0 | 磁盘模块唯一基准引擎；解析 JSON；记录参数、版本与 SHA-256 |
| iperf3 | TCP 多流上传/反向下载 | [ESnet/iperf](https://github.com/esnet/iperf) | BSD-3-Clause | standard/full 的网络吞吐唯一基准；解析逐节点 JSON 原值；记录节点、参数、版本与 SHA-256 |
| NextTrace | 带节点信息的路由追踪 | [nxtrace/NTrace-core](https://github.com/nxtrace/NTrace-core) | GPL-3.0 | 仅在路由模块启用且本机存在时调用；强制 JSON、无启动横幅；记录参数与 SHA-256 |
| traceroute / tracepath | 基础路由追踪 | 操作系统发行方 | 随发行版而异 | NextTrace 不存在时使用；路径快照 12 跳、三网回程 20 跳；不经过 shell |
| ping | ICMP 往返与丢包 | 操作系统发行方 | 随发行版而异 | `latency` 模块的 ICMP 列；兼容 iputils 与 busybox 两种统计行格式；参数以数组传入、不经过 shell；不可用时只保留 TCP 结果 |

`nat` 模块不调用任何外部程序：STUN（RFC 5389/5780）由 `ecs` 用标准库自行实现，
只发送 Binding 请求，不含 TURN、ICE、认证或消息完整性。

默认安装不会触碰包管理器。`install.sh --with-benchmarks` 是用户明确授权后的便捷入口；它安装发行版提供的软件包，不从随机镜像下载裸二进制，也不替用户接受闭源软件许可证。Geekbench 因闭源和免费版上传行为不作为默认依赖。

## 在线服务

在线配置会直接连接：

- [ipapi.is](https://ipapi.is/)：出口 IP、ASN、地理、类型、风险因子与公司/ASN 滥用概率；
- [IPinfo](https://ipinfo.io/developers)：网络/公司类型与隐私信号；
- [ipregistry](https://ipregistry.co/docs/)：网络类型与代理、Tor、VPN、机房、滥用信号；
- [IP2Location.io](https://www.ip2location.io/ip2location-documentation)：类型、代理因子与 IP2Proxy 欺诈分；
- [AbuseIPDB](https://docs.abuseipdb.com/)：使用类型与滥用置信度；
- [Scamalytics](https://scamalytics.com/)：Web 流量欺诈分与匿名化信号；
- [IPQualityScore](https://www.ipqualityscore.com/documentation/proxy-detection-api/overview)：欺诈分与代理、VPN、Tor、近期滥用、机器人信号；
- [DB-IP](https://db-ip.com/api/doc.php)：威胁等级、代理与爬虫信号；
- [ipdata](https://ipdata.co/)：代理、Tor、机房和已知威胁信号；
- [IPWHOIS](https://ipwhois.io/documentation)：国家、代理、VPN、Tor 与托管信号；
- [IPQuality/check.place](https://check.place/)：无用户密钥时，为 MaxMind、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQS、ipdata 提供社区兼容中转；
- [Jina Reader](https://github.com/jina-ai/reader)（Apache-2.0 服务实现）：当 IPQS 社区额度和官方公开页出口配额同时不可用时，读取同一公开查询页的一小时内缓存；不提供或代算供应商分数；
- YABS 当前公共 iperf3 节点清单中的 Clouvider/Leaseweb 测速服务：standard/full 的多节点 TCP 吞吐；
- 电信、联通、移动的公开参考 IP：`backtrace` 模块用于识别路径上的骨干线路，只发送路由探测包；
- 公共 STUN 服务器（`stun.miwifi.com`、`stun.1und1.de`、`stun.hoiio.com`、`stun.l.google.com`、`stun.cloudflare.com`）：`nat` 模块的 UDP Binding 请求。STUN 请求本身不携带任何本机信息——服务器看到的只是 UDP 包的源地址，也就是本机公网出口；返回的映射地址是判定 NAT 类型的依据；
- README 中列出的公共 DNS、延迟、端口以及服务自身公开网页。

第三方服务会看到请求出口 IP，并可能有各自的日志、速率限制、套餐字段差异与隐私政策。社区中转和 Jina Reader 还会看到被查询 IP；报告的数据源状态表会区分“官方 API”“官方免密/公开接口”“IPQuality/check.place 中转”和网页读取兜底。`--offline` 会跳过全部在线探针，`--ip-quality-sources none` 只关闭附加质量源。

官方 API 凭据只从 `IPINFO_TOKEN`、`IPREGISTRY_API_KEY`、`IP2LOCATION_API_KEY`、`ABUSEIPDB_API_KEY`、`SCAMALYTICS_USER`、`SCAMALYTICS_API_KEY`、`IPQS_API_KEY`、`DBIP_API_KEY`、`IPWHOIS_API_KEY` 读取。它们不会进入配置文件示例、进程参数、错误文本或报告。

## 调研项目

`backtrace` 的三网回程识别思路与参考目标选择来自 [zhanghanyun/backtrace](https://github.com/zhanghanyun/backtrace)（MIT）；骨干网段与 AS 对应关系属于运营商公开事实，未复制上游代码或数据文件。磁盘的 50/50 混合矩阵沿用 [YABS](https://github.com/masonr/yet-another-bench-script)（WTFPL-2.0）的块大小与队列深度口径。

oneclickvirt/ecs、spiritLHLS/ecs 与另外十个项目构成功能、交互和工程风险调研样本。逐项提交快照、许可证与取舍见 [docs/research.md](docs/research.md)。其中 IPQuality 是已归属的 AGPL 上游；其他项目的源码、规则文件和报告模板未复制进本项目。

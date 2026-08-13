# Third-party components and services

`ecs` 只面向 Linux，Go 依赖仍只有标准库。IP 质量模块采用 [xykt/IPQuality](https://github.com/xykt/IPQuality) 的多源覆盖、字段映射与风险分段思路，项目因此整体按 AGPL-3.0-only 发布；归属和差异见 [NOTICE](NOTICE)。

版本、tag、二进制 SHA-256 和随包许可证文件以 CI 生成的 `manifest.json`、`checksums.txt`
和 `LICENSES/` 为准。源码中的示例 manifest 允许 `unknown`/`unavailable`，这里不填写尚未由
CI 产出的工具版本、发布资产或许可证正文。

`tools/ecs-tools-manifest.schema.json` 与 Go 解析器使用同一严格合同：顶层必须包含 `build`，`build.validation` 必须明确为功能校验且不具备性能有效性，`tools` 必须恰好各含一个下文十个工具，未知字段会同时被 schema 和解析器拒绝。CI 会先检查 Draft 2020-12 schema 本身，再校验示例与实际生成的 manifest。

## 可选本地程序

这些程序可由系统或受校验的临时依赖路径提供。zstd、NPB 与 OpenSSL 只接受下表的固定版本/参数；任意系统版本不会生成可比较成绩。缺少 `sysbench`、`zstd`、`npb-ep`、`npb-ft`、`openssl`、官方 `stream`、`fio`、`iperf3`、`nexttrace-tiny` 或 `ping` 时，`run.sh` 从当前 Linux 架构匹配的 `ecs-tools` `tar.gz` 包临时提供二进制；选中 zstd 时，固定 corpus 由独立 Release 资产临时提供。`ecs` 仅以独立进程调用。
`ecs-tools` 的工具包边界由每个架构的 `manifest.json` 和 `LICENSES/` 决定，不把下表之外
的版本或资产默认为已发布：

| 程序 | 用途 | 可核对来源/许可证 | ecs 行为 |
| --- | --- | --- | --- |
| `sysbench` | CPU 标准基准 | [上游仓库](https://github.com/akopytov/sysbench) · [LICENSE](https://github.com/akopytov/sysbench/blob/master/LICENSE) · GPL-2.0-only | 只运行 CPU 单线程/多线程工作负载；记录版本和 SHA-256 |
| `zstd` | 固定 Silesia corpus 的 level 3 压缩/解压吞吐；5s，1/全 worker | [Zstandard v1.5.7](https://github.com/facebook/zstd/tree/v1.5.7) · [LICENSE](https://github.com/facebook/zstd/blob/v1.5.7/LICENSE) · BSD-3-Clause/GPL-2.0-only 双许可 | 只构建含 benchmark/压缩/解压/多线程的 CLI，裁掉字典训练、trace、legacy 与 zlib/lzma/lz4 格式；校验 binary SHA-256，固定 corpus 由独立 Release 资产按长度与 SHA-256 校验；保留原始输出 |
| `npb-ep` / `npb-ft` | NPB-OMP EP + FT Class A，1T/全线程 Mop/s | [NASA NPB 3.4.4](https://www.nas.nasa.gov/software/npb.html) · 上游源文件的 NASA NPB permissive notice | 发布包只编译 EP/FT Class A，裁掉其余 kernel/class/MPI；固定 `-O3 -fopenmp -static`、`randi8` 和 OpenMP 环境；Verification 失败不采纳 Mop/s |
| `openssl` | AES-256-GCM、ChaCha20-Poly1305、SHA-256；16 KiB、5s、1/全 worker | [OpenSSL 3.5.7](https://github.com/openssl/openssl/tree/openssl-3.5.7) · [LICENSE](https://github.com/openssl/openssl/blob/openssl-3.5.7/LICENSE.txt) · Apache-2.0 | 只构建官方 `apps/openssl` 及依赖，关闭 TLS/网络、动态组件和无关算法族；记录 binary SHA-256、完整 `speed` 参数、`-mr` 原始输出和扩展倍率 |
| `stream` | 官方 STREAM 内存带宽：10,000,000 elements、10 iterations；`1T`/`NT` × `Copy`/`Scale`/`Add`/`Triad` | [官方来源与 Run Rules](https://www.cs.virginia.edu/stream/ref.html) · 具体许可证文本/版本待 CI 产物填充 | 只调用官方二进制并保留四 kernel、线程和原始单位；缺失时内存基准明确未运行 |
| `fio` | Direct I/O 磁盘基础项、混合/Crystal/ATTO 矩阵和 4KiB QD1 latency | [上游仓库](https://github.com/axboe/fio) · [COPYING](https://github.com/axboe/fio/blob/master/COPYING) · GPL-2.0-only | 磁盘结果统一来自 fio JSON；记录版本和 SHA-256；QD1 延迟也由 fio 产生 |
| `iperf3` | TCP 多流双方向与 UDP 丢包/抖动 | [上游仓库](https://github.com/esnet/iperf) · [LICENSE](https://github.com/esnet/iperf/blob/master/LICENSE) · BSD-3-Clause | 网络吞吐唯一标准工具；逐节点、逐方向保留 JSON 原值，不跨节点求平均 |
| `nexttrace-tiny` | 路由和回程追踪 | [上游仓库](https://github.com/nxtrace/NTrace-core) · [LICENSE](https://github.com/nxtrace/NTrace-core/blob/main/LICENSE) · GPL-3.0-only | 只使用官方 Tiny 资产；实际版本和 SHA-256 由 manifest/报告记录 |
| `ping` | 系统 ICMP 往返与丢包 | [iputils](https://github.com/iputils/iputils) 或发行版提供；许可证随实际发行版包 | 系统 ping 优先；兼容 busybox 等精简 ping 的三段统计行；完全不可用时只保留 TCP 并明确说明 |

上表中带 1T/NT 或 1/全 worker 口径的五个本地基准（sysbench、zstd、NPB、STREAM、OpenSSL）在有效 CPU allowance 为 1 时只执行一次参数相同的官方工具命令。报告保留 1T/NT 逻辑原始指标以兼容现有 schema，但不伪造第二个独立样本，也不生成扩展倍率。

Ookla 官方 `speedtest` 客户端是闭源、适用其自身条款和隐私政策的外部适配器。`full` 默认
运行它，`standard` 默认不运行；`--only ookla` 仍可从任意配置档显式单独选择。它不进入 `ecs-tools`；`ecs` 不在本文
中复制其许可证文本，具体条款请核对 [官方 CLI 页面](https://www.speedtest.net/apps/cli)
和 [隐私政策](https://www.speedtest.net/about/privacy)。

`mbw` 和 `ioping` 不属于当前测试链路、`ecs-tools` 清单或报告 schema。

`nat` 模块不调用任何外部程序：STUN（RFC 5389/5780）由 `ecs` 用标准库自行实现，
只发送 Binding 请求，不含 TURN、ICE、认证或消息完整性。

`run.sh` 优先使用符合口径的系统程序；需要临时工具时选择当前 Linux 架构匹配的 `ecs-tools`
`tar.gz`，并核对 `checksums.txt`、`manifest.json` 和 10 个二进制 digest 后，才解包到本次运行的 `$WORK`；选中 zstd 时，另从独立 Release 资产核对 checksum、长度和 SHA-256 后解包 corpus。
APT/Packagecloud 不用于这些通用缺失工具。Ookla 缺失且模块被 profile 选中或被 `--only` 显式选中时，才走独立的官方 Packagecloud 源、固定指纹的 GPG
公钥、索引和缓存路径；由 apt 验证签名后仅下载/解包，不执行供应商的 `curl | sh` 安装脚本。
`full` 缺失 `speedtest` 时走该独立官方签名源，`standard` 只有显式 `--only ookla` 时走该路径；Ookla 永不进入
`ecs-tools`。`ECS_AUTO_DEPS=0` 会跳过临时依赖准备，让报告如实标记缺失模块。
`install.sh --with-benchmarks` 当前只安装 `sysbench`、`fio`、`iperf3`，也不安装
`mbw`/`ioping`、zstd corpus、NPB、固定 OpenSSL 或官方 STREAM。工具由 `ecs-tools` 临时提供，固定 corpus 由独立 Release 资产经 `run.sh`
临时提供。两条路径都不替用户接受
闭源软件许可证。Geekbench 因闭源和免费版结果处理边界不作为依赖；Ookla 只提供可审计的
本机客户端适配器。

七架构工具链先在宿主架构上完成原生或交叉编译，之后才直接运行或交给 QEMU 做短功能
smoke，绝不在 QEMU 内编译。交叉架构的 NPB 用同一源码、编译器和参数额外生成不入包的
Class S EP/FT 并在 QEMU 中跑到 Verification；发布的 Class A ELF 仍逐个做静态链接、架构、
摘要和 manifest 校验。这样验证目标运行时/OpenMP，又不把模拟器中的 Class A 重负载误当性能测试。

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
- [Ookla Speedtest](https://www.speedtest.net/about/privacy)：full 默认或用户用 `--only ookla` 显式选择后由本机官方客户端连接；Ookla 的数据处理不属于 ecs 本地报告保证范围；
- DNS 黑名单服务（Spamhaus、SpamCop、Barracuda、CBL、PSBL、blocklist.de、UCEPROTECT、DroneBL、s5h、SpamRats、GBUdb、Mailspike、Backscatterer）：`blacklist` 模块把出口 IP 反转后作为域名查 A 记录，不发送其他信息。各名单有各自的收录标准、解除流程与查询配额；部分名单（如 Spamhaus）拒绝来自公共解析器的查询并返回 127.255.255.x，ecs 将其判为“查询被拒”而非命中；
- [spiritLHLS/speedtest.cn-CN-ID](https://github.com/spiritLHLS/speedtest.cn-CN-ID/tree/fbc05248d2e106f7ef14f3ce7e037bc9976b58bb)（MIT）：`cnspeed` 模块的中国测速节点清单，含运营商标注。当前固定提交为 `fbc05248d2e106f7ef14f3ce7e037bc9976b58bb`；上游 `main` 虽每日更新，ecs 发行版不会在未经代码评审时自动更换访问目标。ecs 只读取该 commit 的清单，不复制内容入库；专用客户端只连接通过 scheme、DNS、拨号与重定向公网地址校验的 HTTP(S) 节点，不使用环境代理。部分节点仅有 HTTP，中间网络可观察或篡改测速流量；
- 公共 STUN 服务器（`stun.miwifi.com`、`stun.1und1.de`、`stun.hoiio.com`、`stun.l.google.com`、`stun.cloudflare.com`）：`nat` 模块的 UDP Binding 请求。STUN 请求本身不携带任何本机信息——服务器看到的只是 UDP 包的源地址，也就是本机公网出口；返回的映射地址是判定 NAT 类型的依据；
- README 中列出的公共 DNS、延迟、端口以及服务自身公开网页。

第三方服务会看到请求出口 IP，并可能有各自的日志、速率限制、套餐字段差异与隐私政策。社区中转和 Jina Reader 还会看到被查询 IP；报告的数据源状态表会区分“官方 API”“官方免密/公开接口”“IPQuality/check.place 中转”和网页读取兜底。`--exposure local` 会跳过全部在线探针；`--exposure public` 保留联网但排除第三方情报服务，此时出口 IP 改由 STUN 发现；`--ip-quality-sources none` 只关闭附加质量源。

官方 API 凭据只从 `IPINFO_TOKEN`、`IPREGISTRY_API_KEY`、`IP2LOCATION_API_KEY`、`ABUSEIPDB_API_KEY`、`SCAMALYTICS_USER`、`SCAMALYTICS_API_KEY`、`IPQS_API_KEY`、`DBIP_API_KEY`、`IPWHOIS_API_KEY` 读取。它们不会进入配置文件示例、进程参数、错误文本或报告。

## 调研项目

`backtrace` 的三网回程识别思路与参考目标选择来自 [zhanghanyun/backtrace](https://github.com/zhanghanyun/backtrace)（MIT）；骨干网段与 AS 对应关系属于运营商公开事实，未复制上游代码或数据文件。磁盘的 50/50 混合矩阵沿用 [YABS](https://github.com/masonr/yet-another-bench-script)（WTFPL-2.0）的块大小与队列深度口径。

oneclickvirt/ecs、spiritLHLS/ecs 与另外十个项目构成功能、交互和工程风险调研样本。逐项提交快照、许可证与取舍见 [docs/research.md](docs/research.md)。其中 IPQuality 是已归属的 AGPL 上游；其他项目的源码、规则文件和报告模板未复制进本项目。

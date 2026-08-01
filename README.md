# ecs

无广告、默认零上传、可审计的 VPS 综合测试工具。

`ecs` 不是把一批远程 Shell 脚本重新串起来。它以结构化结果为核心：每个探针先产生同一份带版本 JSON 数据，再由本地渲染器一次导出终端、Markdown 和独立 HTML 报告。所有性能成绩只读取 sysbench、fio、iperf3 的结果；项目不实现 CPU、内存、磁盘或网络吞吐替代分数，也不生成并行效率、跨节点平均值或综合跑分等自算成绩。IP 质量模块在遵守 AGPL 的前提下吸收 IPQuality 的多源覆盖与字段映射，并移除广告、运行计数和在线报告。

> 当前处于首个开发版本，仅支持 Linux。系统与资源、标准 CPU/内存/磁盘/网络基准、网络/IP、DNS、延迟、端口、服务可达性、路由、三网回程、JSON/Markdown/HTML 已可运行。仍需更多真实 Linux VPS 样本校准公共节点、骨干特征表和平台规则。

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

## 快速开始

`ecs` **只支持 Linux**。VPS 几乎全是 Linux 发行版，因此项目不保留 macOS、Windows 或
BSD 的代码路径：多平台分支会迫使测试断言放宽到"哪个平台都成立"，真实生产路径反而
测不到。发布二进制覆盖 Linux 的 `amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、
`ppc64le`。

从源码构建需要 Go 1.22 或更高版本：

```bash
go build -trimpath -o ecs ./cmd/ecs
./ecs
```

默认执行 `standard` 配置，并在 `./reports` 同时生成：

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

# 低资源快速筛查
ecs --profile quick

# 完整测试，但跳过服务可达性
ecs --profile full --skip media

# 标准性能主链路：固定使用 sysbench + fio + iperf3
ecs --only system,cpu,memory,disk,speed

# 快速档仍只使用标准工具，只缩短时长和磁盘文件
ecs --profile quick --only system,cpu,memory,disk --offline

# 只保留 JSON 和 HTML
ecs --format json,html --output ./my-report

# 报告中保留完整主机名与 IP（默认不会）
ecs --reveal

# 默认查询全部 IP 质量源；也可只启用指定来源
ecs --only network --ip-quality-sources ipapi,ipinfo,abuseipdb,ipqs

# 从已有 JSON 重新导出 Markdown/HTML
ecs render --input ./reports/ecs-report-20260731-120000.json

# 查看全部模块
ecs list

# 检查 sysbench、fio、iperf3 与路由工具
ecs doctor

# 输出可修改的 JSON 配置样例
ecs config example
```

运行 `ecs run --help` 查看全部参数。

## 配置档与资源上限

| 配置档 | 目标 | 性能主引擎 | CPU/内存每轮 | 临时磁盘上限 | 网络负载 |
| --- | --- | --- | ---: | ---: | --- |
| `quick` | 低资源快速筛查 | sysbench + fio | 5 秒 | 256 MiB | 默认不跑吞吐 |
| `standard` | 日常综合验机 | sysbench + fio + iperf3 | 10 秒 | 1024 MiB | 3 节点、双方向、每方向 10 秒 |
| `full` | 更稳定的长样本 | sysbench + fio + iperf3 | 15 秒 | 2048 MiB | 7 节点、双方向、每方向 15 秒，含 UDP 丢包 |

采样窗口对齐 sysbench 的通行时长：低于 10 秒的窗口在突发性能机型（AWS t 系列、
GCP e2、阿里突发实例）上测到的是 burst credit 而不是稳态性能，且方差极大。

fio 文件最多使用测试前可用空间的 20%。iperf3 是按时长尽力跑满链路，实际流量随 VPS 带宽变化，无法用 MiB 预封顶；启动前会显示节点数、时长和并发流。

## 模块

| ID | 报告口径 | 默认实现 | 关键限制 |
| --- | --- | --- | --- |
| `system` | 事实采集 | OS/runtime inspection，含 CPU 缓存与 VT-x/AMD-V | 某些容器会隐藏 DMI/内核字段；不是基准 |
| `network` | 第三方评估 | 官方 API + IPQuality 社区兼容通道 | 各库口径不同且可能冲突，不能平均成总分 |
| `cpu` | 标准基准 | sysbench CPU prime=20000，单/多线程 | 只与相同版本、参数、线程和时长比较 |
| `memory` | 标准基准 | sysbench memory 顺序读写 + mbw memcpy 带宽 | 两者口径不同并列保留；sysbench 反复读写同一缓冲区会命中缓存，mbw 在两个大数组间搬运 |
| `disk` | 标准基准 | fio Direct I/O 矩阵 + ioping 空载延迟 + smartctl 介质健康 | 只与相同 fio/ecs 参数和文件系统比较；SMART 需 root 且虚拟磁盘通常不透传 |
| `dns` | 协议测量 | 原生 DNS/UDP | 2–5 个样本的 P95 只作现场诊断，不是标准分 |
| `latency` | 协议测量 | 预解析后的 TCP 建连，并列系统 ping 的 ICMP 往返 | 解析耗时单列；TCP 明显快于 ICMP 时会警告握手可能被本地代理代答；受 Anycast/CDN 调度影响 |
| `speed` | 标准基准 | iperf3 TCP 多流正向/反向、多节点 | 公共节点可能繁忙；按时长测试不封顶流量 |
| `ports` | 协议测量 | 原生 TCP 握手 | 单目标失败不能独立证明端口被封 |
| `blacklist` | 协议测量 | 17 个实测可用的 DNSBL 查询 | 各名单收录标准差异大，不可合并计分；127.255.255.x 是查询被拒而非命中 |
| `nat` | 协议测量 | 自实现 STUN（RFC 5389/5780）映射与过滤行为发现 | 只反映 UDP 路径，不代表 TCP；服务器不支持 CHANGE-REQUEST 时过滤行为报"未知"而不硬判 |
| `apps` | 协议测量 | Telegram 五个 DC 与代码/镜像/软件源/证书服务的 TCP 握手 | 可达不等于可用；CDN 会让握手在边缘节点完成 |
| `media` | 启发式判断 | 33 个平台的分平台规则，含 Netflix 自制剧判定 | 不等同账号权益、注册、支付或实际播放；规则分强/弱证据标注 |
| `route` | 协议诊断 | NextTrace/traceroute/tracepath | 正向路径快照不等同回程，也不是性能基准 |
| `backtrace` | 启发式判断 | 三网参考 IP 路径 + 骨干网段特征表 | 主动探测推断，非反向抓包；未命中特征返回未识别 |

探针串行运行，避免 CPU、内存、磁盘和网络压力互相污染。DNS、端口等同类目标在模块内部并发。

任意配置档缺少标准工具时，对应模块显示黄色“标准基准未运行”警告，不产生替代成绩。所有终端、JSON、Markdown、HTML 都保留 `methodology.kind`、引擎、工作负载和可比范围。

CPU、内存、磁盘和网络吞吐只展示标准工具直接返回或按其公开单位换算的原始指标。`ecs` 不对这些指标二次打分；网络吞吐逐节点、逐方向保存，不跨节点求平均或中位数。

## IP 质量与欺诈值

`network` 默认执行 `--ip-quality-sources all`，每个 IPv4/IPv6 出口分别展示：

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

不想把出口 IP 交给社区中转或 Jina Reader 时，显式选择已配置的官方来源；完全关闭附加质量查询可用 `--ip-quality-sources none`。无论如何，`network` 仍需访问 ipapi.is 来发现服务器的 IPv4/IPv6 出口。

## 报告

JSON 是事实来源，schema 当前为 `ecs.report/v1`。Markdown 和 HTML 由同一份数据生成：

- Markdown 适合 GitHub、论坛和工单；
- HTML 是单文件、无 JavaScript、无外部字体/图片/统计脚本，支持深色模式和打印；
- JSON 保留精确数值、显示值、测试方法、状态、来源、警告和路由原文；
- 每个模块明确标注“标准基准、协议测量、第三方评估、启发式判断或事实采集”，避免把所有数字都包装成标准成绩；
- `Ctrl+C` 会取消正在运行的探针、清理磁盘临时文件，并导出已经完成的部分。

HTML/Markdown 一键导出不依赖 Pandoc、Node.js 或浏览器：

```bash
ecs render --input report.json --format md,html --output ./exported
```

详细字段见 [报告 schema](docs/schema.md)。

## 隐私与网络请求

默认情况下，主机名显示为 `hidden`，IPv4 显示为 `A.B.C.x`，IPv6 只保留 `/48` 前缀。遮盖在写入 JSON 之前完成，因此默认生成的三个文件都不保存完整值。

遮盖同时覆盖表格与原始命令输出：`route` 与 `backtrace` 保存的 traceroute 原文会逐个 IP 按段遮盖，既不泄露完整地址，又保留 `59.43`、`202.97` 这类前缀供你复核线路判定是否成立。

在线配置可能连接以下第三方：

- `network`：ipapi.is；以及按 `--ip-quality-sources` 选择的 IPinfo、ipregistry、IP2Location、AbuseIPDB、Scamalytics、IPQualityScore、DB-IP、ipdata、IPWHOIS。无用户密钥时，MaxMind 和部分风险源会经 `ipinfo.check.place` 查询；IPQS 最后一级免密兜底会把目标公开页 URL（其中含待查 IP）交给 `r.jina.ai`，并可能读取一小时内缓存；若进程配置了系统 HTTP(S) 代理，该只读兜底可经代理访问；
- `dns`：Cloudflare、Google、Quad9、AliDNS、DNSPod 的 UDP/53；
- `latency`：Cloudflare、Google、阿里云、腾讯、Amazon 的公开 TCP/443；
- `speed`：使用 YABS 维护清单中的 Clouvider/Leaseweb 公共 iperf3 节点；
- `ports`：Example、GitHub、Cloudflare DNS、Gmail 的公开端口；
- `media`：被检测服务自己的公开网页；
- `route`：配置中的路由目标；若使用 NextTrace，其在线 GeoIP 行为由 NextTrace 自身决定；
- `backtrace`：电信、联通、移动的公开参考 IP，用于识别路径上的骨干线路；
- `apps`：Telegram 官方 DC 域名，以及 GitHub、Docker Hub、npm、PyPI、Debian/Ubuntu/Alpine 源、Let's Encrypt、Cloudflare 的 TCP 端口；
- `blacklist`：17 个 DNS 黑名单的解析服务，只把反转后的出口 IP 作为域名查询；
- `nat`：公共 STUN 服务器（小米、1&1、Hoiio、Google、Cloudflare），只发送 STUN Binding 请求；
- `latency`：除 TCP 建连外，还会调用系统 `ping` 对同一目标发 ICMP。

`--offline` 会跳过所有声明需要网络的模块。项目不包含遥测、运行次数统计、Pastebin、报告站或隐藏的上传请求。

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
  "skip": ["media"],
  "ip_quality_sources": ["all"],
  "formats": ["json", "md", "html"],
  "output": "./reports",
  "disk_path": "/var/tmp",
  "iperf_duration": "5s",
  "http_timeout": "10s",
  "latency_targets": [
    {"name": "My endpoint", "address": "example.com:443", "kind": "custom"}
  ]
}
```

未知字段会直接报错，避免拼写错误被静默忽略。命令行参数优先于配置文件。

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
- 发布稳定基线后冻结性能工作负载版本和比较规则。

## 许可证

[GNU AGPL v3.0](LICENSE)。IPQuality 相关归属与改动说明见 [NOTICE](NOTICE)。

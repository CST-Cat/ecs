# ecs

语言 / Language：[简体中文](README.md) | [English](README_EN.md)

无广告、默认不上传报告、结果可审计的 Linux VPS 综合测试工具。

`ecs` 将系统信息、CPU、内存、磁盘、网络、IP 质量、流媒体、路由与三网回程等测试统一为结构化结果，再从同一份数据生成 JSON、纯文本、Markdown 和 HTML 报告。每项结果都会保留测试方法、参数、工具、耗时、警告、有效样本/计划样本和原始证据，方便复核，也方便后续重新渲染。

项目坚持三个原则：

- 不展示广告、赞助或返利内容；
- 报告只写入用户指定的本地路径，不提供自动上传链路；
- 不用替代工具伪造缺失成绩，不把不同口径的数据强行合并成一个结论。

## 快速开始

一键脚本仅支持 Linux。它会下载并校验最新 Release，优先复用系统中已有的工具，并把缺失组件临时放入本次运行目录；测试结束后自动清理，不向系统目录安装文件。

`ecs` 一共提供 18 个测试模块：Standard 默认运行其中 16 个，Full 运行全部 18 个。

### Standard：日常综合测试

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile standard --yes --output ./reports
```

测试项目：系统与硬件、BGP、CPU、内存、磁盘、DNS、网络延迟、iperf3 吞吐、端口、NAT、黑名单、常用服务、中国三网 HTTP 测速、流媒体、路由和三网回程。

对应模块：`system`、`bgp`、`cpu`、`memory`、`disk`、`dns`、`latency`、`speed`、`ports`、`nat`、`blacklist`、`apps`、`cnspeed`、`media`、`route`、`backtrace`。

### Full：完整测试

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile full --yes --output ./reports
```

在 Standard 的 16 个项目上增加多源 IP 质量（`network`）和官方 Ookla Speedtest（`ookla`），合计运行全部 18 个模块。这两项依赖外部商业服务；免费或公开通道可能受额度、缓存和数据库更新影响，仅作为扩展参考。Ookla 还会产生较大流量，并按其自身条款处理测速所需的数据；它不属于 `ecs` 的本地零上传保证。

> `speed`、`cnspeed` 和 `ookla` 都可能跑满带宽。流量按测试时长产生，不设固定上限。

### Only：自选测试

`--only` 不是第三个预设档位，而是用逗号分隔的模块 ID 替换 Standard 或 Full 的默认模块集合，只运行明确选中的项目。一键脚本与本地 `ecs` 命令都支持它。

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --only system,cpu,memory,disk \
  --exposure local --yes --format json,txt --output ./reports
```

常用组合：

| 目标 | `--only` 的值 | 测试项目 |
| --- | --- | --- |
| 系统概览 | `system` | 操作系统、虚拟化与硬件信息 |
| 本地性能 | `system,cpu,memory,disk` | 系统信息、CPU、内存和磁盘，不产生网络请求 |
| BGP 与信誉 | `bgp,blacklist` | 公共 BGP 观测与 DNS 黑名单 |
| 多源 IP 质量 | `network` | 商业与第三方 IP 情报；Full 默认项目 |
| 网络质量 | `dns,latency,speed,ports,nat` | DNS、延迟、iperf3、端口与 NAT |
| 服务可达性 | `apps,media` | 常用服务与流媒体检测 |
| 路由诊断 | `route,backtrace` | 正向路由与三网回程 |
| 中国三网测速 | `cnspeed` | 社区 HTTP 节点；Standard 默认项目 |
| Ookla 测速 | `ookla` | 官方专有客户端；Full 默认项目 |

18 个模块都可以任意组合，完整 ID、测试内容和对应工具见下方的[测试项目与工具](#测试项目与工具)表。

不带测试参数运行一键脚本时，如果当前有终端，脚本会进入交互向导；带 `--profile`、`--only` 等测试参数或处于 cron、CI 环境时会直接运行。

## 报告输出

四种报告由同一份测试结果生成。使用 `--format` 选择一种或多种格式，并用 `--output` 显式指定输出目录：

```bash
ecs --profile standard \
  --format json,txt,md,html \
  --output ./reports
```

一键脚本接受相同参数：

```bash
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | \
  sh -s -- --profile standard --yes \
  --format json,txt,md,html --output ./reports
```

| 格式 | 参数 | 适合场景 |
| --- | --- | --- |
| JSON | `--format json` | 自动化处理、归档、二次分析和重新渲染 |
| 纯文本 | `--format txt` | 终端查看、聊天窗口和论坛粘贴 |
| Markdown | `--format md` | GitHub、工单和支持 Markdown 的论坛 |
| HTML | `--format html` | 浏览器查看、打印和离线分享；单文件且不依赖外部资源 |

四种格式都会显示每个模块的“有效样本 / 计划样本”和比例柱。它反映采集与解析是否完整，不代表商业 IP 情报或流媒体平台的判断一定准确。

证据同时带有稳定等级：`complete`（完整）、`partial`（部分）、`insufficient`（证据不足）和 `not_planned`（本轮无计划样本）。超时、DNS 错误、连接被拒、限流、HTTP 拒绝、解析失败、工具缺失等不会只埋在说明文字里，而会写入 `failures[]` 的稳定机器字段，并在 txt、Markdown 和 HTML 中显示。

`system` 会报告真实 cgroup CPU/内存限额、cpuset、CPU throttle、PSI CPU/内存/I/O 压力和 OOM 事件。CPU、STREAM 与 fio 测试只在检测到测试前高负载、steal、PSI、cgroup throttle 或 OOM 干扰时自动复测一次；干净环境不会无条件延长测试。选择结果时先排除无有效证据的轮次，再采用干扰较低的一轮，同分保留首次结果，绝不按跑分高低挑成绩。两轮指标、证据和干扰原因都会保留在 JSON 中。

`--output` 指向目录，目录不存在时会自动创建。使用 `--name` 可以固定文件名前缀：

```bash
ecs --profile standard \
  --format json,txt,md,html \
  --output ./reports \
  --name my-vps
```

上面的命令会生成：

```text
./reports/my-vps.json
./reports/my-vps.txt
./reports/my-vps.md
./reports/my-vps.html
```

已有 JSON 报告也可以重新导出，测试无需重跑：

```bash
ecs render \
  --input ./reports/my-vps.json \
  --format json,txt,md,html \
  --output ./exported
```

报告字段与稳定标识见 [报告结构说明](docs/schema.md)。

## 多报告对比

`ecs compare` 可以安全比较 2 份到任意多份 JSON 报告，并从一次比较同时生成 JSON、纯文本、Markdown 和 HTML。`--reference` 使用从 1 开始的报告序号；默认以第 1 份为基准。

```bash
# 两台机器或同一台机器的前后对比
ecs compare ./reports/old.json ./reports/new.json \
  --reference 1 \
  --format json,txt,md,html \
  --output ./compare \
  --name old-vs-new

# 目录中的 2 到多台机器；shell 会展开全部 JSON
ecs compare ./fleet/*.json \
  --reference 1 \
  --format json,txt,md,html \
  --output ./compare \
  --name fleet
```

报告会按输入数量自动改变版式：

| 报告数 | 终端 / Markdown | HTML |
| ---: | --- | --- |
| 2 | 基准与候选并排，直接显示相对变化 | 双列卡片，突出前后差异 |
| 3–5 | 每个指标一行的紧凑矩阵 | 自适应多列卡片 |
| 6 及以上 | 每个指标改为纵向排名，避免横向挤压 | 排名列表；窄屏自动折成单列 |

它不是把多份完整报告简单拼在一起：同一组内自动用 `★`、加粗、颜色和数据比例柱高亮最佳数值，用 `▲` / `▼` 标出相对基准的提升或回退；无色终端仍保留符号、排名和密度柱。摘要只保留状态、证据、关键性能指标、事实变化和口径问题，比逐份报告更紧凑。

安全比较只发生在模块 ID、指标 key、`method`、单位、优劣方向和机器参数口径完全相同的数值之间。新报告会直接记录线程数、时长、工具版本/哈希、文件大小和目标集合指纹；不同 sysbench/fio/iperf3 参数、不同方法版本，或缺少 `higher_is_better` / 参数口径的数字不会被强行排名，而会进入“不可合并的口径”。旧格式报告不做参数猜测，请重新运行测试生成报告。IP、ASN、线路、平台状态等离散变化只并排展示事实，不自动判断哪个更好。比较完全在本地完成，不上传输入报告。

## 测试项目与工具

“内置”表示由 `ecs` 自己实现协议或读取 Linux 系统接口，不需要额外的可执行文件。

| 模块 | 测试内容 | 使用的工具或数据源 | 默认档位 |
| --- | --- | --- | --- |
| `system` | 操作系统、虚拟化、CPU、内存、磁盘、cgroup 限额、PSI 与 OOM | 内置采集器，读取 `/proc`、`/sys`、cgroup、DMI 等系统接口 | Standard / Full |
| `network` | 出口 IP、ASN、地理位置与多源 IP 质量 | 内置 HTTP 客户端 + 多家 IP 情报数据源 | Full |
| `bgp` | 出口前缀、起源 ASN、RPKI 与 AS 路径样本 | RouteViews 公共 RIB API | Standard / Full |
| `cpu` | 单/多线程事件率与 P95、扩展倍率、每线程效率、压力诊断及条件复测 | `sysbench` + 内置统计 | Standard / Full |
| `memory` | Copy、Scale、Add、Triad 带宽、扩展倍率、稳定性、压力诊断及条件复测 | 官方 STREAM + 内置统计 | Standard / Full |
| `disk` | Direct I/O、混合读写、Crystal/ATTO 矩阵、延迟、压力诊断及条件复测 | `fio` + 内置统计 | Standard / Full |
| `dns` | 各公共解析器的 P50、P95、抖动与成功率 | 内置 DNS/UDP 客户端 | Standard / Full |
| `latency` | 各目标 TCP P50/P95/抖动/成功率与 ICMP 往返 | 内置 TCP 客户端 + `ping` | Standard / Full |
| `speed` | 多节点 TCP 双向吞吐、分秒稳定性、重传与 UDP 丢包/抖动 | `iperf3` + 内置统计 | Standard / Full |
| `ports` | 常见服务端口可达性 | 内置 TCP 客户端 | Standard / Full |
| `nat` | UDP 映射与过滤行为 | 内置 STUN 客户端 | Standard / Full |
| `blacklist` | DNSBL 收录/干净/拒绝/失败计数与反向解析一致性 | 内置 DNS 客户端 + 公共 DNSBL | Standard / Full |
| `apps` | Telegram、代码托管、镜像站与软件源可达性 | 内置 TCP 客户端 | Standard / Full |
| `cnspeed` | 电信、联通、移动节点 HTTP 下载速度 | 内置 HTTP 客户端 + 社区节点清单 | Standard / Full |
| `ookla` | 官方测速服务的延迟与吞吐 | 官方 Ookla Speedtest CLI（`speedtest`） | Full |
| `media` | 多个平台的地区与可用性线索 | 内置 HTTP 客户端 + 分平台规则 | Standard / Full |
| `route` | 正向路径、探测/可见/超时跳点与逐目标耗时 | 官方 NextTrace Tiny + 内置统计 | Standard / Full |
| `backtrace` | 四城三网回程路径与骨干特征 | 官方 NextTrace Tiny + 内置特征表 | Standard / Full |

一键脚本会为缺失的 `sysbench`、STREAM、`fio`、`iperf3`、`ping` 和 NextTrace Tiny 准备与当前架构匹配的临时工具，并校验清单与摘要。Ookla 不进入该工具包；仅在选中 `ookla` 且本机缺少 `speedtest` 时，脚本才会从独立的官方签名软件源临时下载并解包。无法取得可核验工具时，对应项目会明确标记为跳过，不生成替代成绩。

## 选择测试项目

配置档只决定默认选择哪些模块；Standard 和 Full 对共同模块使用相同的测试深度。也可以通过 `--only` 精确选择模块，或用 `--skip` 从配置档中排除模块。

```bash
# 只运行本地资源与性能测试
ecs --only system,cpu,memory,disk \
  --exposure local \
  --format json,txt \
  --output ./reports

# 完整测试，但跳过流媒体
ecs --profile full \
  --skip media \
  --format json,txt,md,html \
  --output ./reports

# 只检查 IPv6 三网回程
ecs --only backtrace -6 \
  --backtrace-city all \
  --format txt,html \
  --output ./reports

# 在任意配置档中单独运行 Ookla
ecs --only ookla \
  --ookla-servers "telecom=123,unicom=456,mobile=789" \
  --format json,txt \
  --output ./reports
```

常用参数：

| 参数 | 作用 |
| --- | --- |
| `--profile standard\|full` | 选择默认模块集合 |
| `--only MODULES` | 只运行逗号分隔的模块 |
| `--skip MODULES` | 跳过逗号分隔的模块 |
| `--exposure LEVEL` | 限制允许的外联级别 |
| `-4` / `-6` | 只测试 IPv4 或 IPv6 |
| `--lang zh\|en` | 设置命令行和报告语言 |
| `--reveal` | 在报告中保留完整本机 IP |
| `--config FILE` | 从 JSON 配置文件读取参数 |
| `--strict` | 报告存在警告或探针错误时返回非零状态 |

运行 `ecs run --help` 查看全部参数，运行 `ecs list` 查看当前模块与外联级别。

## 隐私与外联

配置档控制“测试什么”，`--exposure` 控制“允许连接到哪里”。两者彼此独立。

| 级别 | 行为 |
| --- | --- |
| `local` | 只运行本机采集和基准，不连接网络 |
| `public` | 允许连接公共基础设施，但不查询商业 IP 情报服务 |
| `thirdparty` | 允许使用第三方情报与测速服务；这是默认上限 |
| `any` | 允许所有已登记的外联模块 |

```bash
# 全套本地性能测试，不产生网络请求
ecs --only system,cpu,memory,disk \
  --exposure local \
  --format json,txt \
  --output ./reports

# 使用 Full 的模块集合，但不接触商业 IP 情报和 Ookla
ecs --profile full \
  --exposure public \
  --format json,txt,md,html \
  --output ./reports
```

默认情况下，本机 IPv4 会遮盖后两段，IPv6 会遮盖后四段；遮盖在写入 JSON 之前完成，因此四种格式保持一致。`--reveal` 只关闭本机 IP 遮盖，远端目标、BGP 前缀和路由跳地址仍会保留，便于复核线路。

`ecs` 本身不包含遥测、运行次数统计、Pastebin、在线报告站或隐藏上传逻辑。联网模块仍会让目标服务看到出口 IP；`network` 会把待查询 IP 发送给所选情报源，Ookla 则适用自己的条款与数据处理规则。详细清单见 [第三方组件与在线服务](THIRD_PARTY.md)。

## 安装与构建

一键脚本适合临时测试。如果需要长期使用，可以从源码构建并安装本地二进制：

```bash
go build -trimpath -o ecs ./cmd/ecs
./install.sh --from ./ecs
```

同时安装标准基准工具：

```bash
./install.sh --from ./ecs --with-benchmarks
```

安装器不会默认调用 `sudo`、修改包管理器或关闭 TLS 校验。STREAM、NextTrace Tiny 与 Ookla 仍由一键脚本按需临时准备，不会由 `--with-benchmarks` 持久安装。

项目支持 Linux 的 `amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64` 和 `ppc64le` 架构。

## 配置文件

生成配置样例并运行：

```bash
ecs config example > ecs.json
ecs --config ecs.json \
  --format json,txt,md,html \
  --output ./reports
```

配置文件可以保存模块选择、外联级别、协议族、报告格式、基准参数以及自定义 DNS、延迟、iperf3、路由、STUN 和 Ookla 节点。未知字段会直接报错，命令行参数优先于配置文件。

## 排行榜与评分

原始测量永远保留。综合评分只在存在有效排行榜参考时生成；可以用 `--score-baseline` 显式提供自己的参考。缺失维度不会补零，也不会用内置常数伪造排名。

```bash
# 从多台机器的 JSON 报告生成同口径参考
ecs leaderboard \
  --source "我的 VPS 集群" \
  --output baseline.json \
  ./reports

# 使用同一份参考运行并输出报告
ecs --score-baseline baseline.json \
  --format json,txt,md,html \
  --output ./scored-reports
```

如需提交精简且不包含公网 IP、主机名和逐跳路由的排行榜数据，请阅读 [提交说明](submissions/README.md)。

## 开发与验证

```bash
go test ./...
go vet ./...
go test -race ./...
make build
```

更多资料：

- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)
- [报告结构说明](docs/schema.md)
- [竞品与上游能力调研](docs/research.md)
- [第三方组件与许可证](THIRD_PARTY.md)

## 许可证

[GNU Affero General Public License](LICENSE)。IPQuality 相关归属与改动说明见 [NOTICE](NOTICE)。

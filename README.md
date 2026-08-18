# ecs

无广告、默认零上传的 Linux VPS 综合测试工具。一次运行覆盖 21 个模块：本地性能基准、网络质量、路由与回程、流媒体与 IP 信誉，结果只写到本地的 JSON / txt / Markdown / HTML 四份报告里。

项目坚持：

- 不展示任何广告、赞助或返利内容；
- 报告默认写入 `/tmp`，不提供自动上传链路；
- 每个模块都写明方法（`inventory` / `standard-benchmark` / `protocol-measurement` / `heuristic`）与比较范围，分数是一步除法，读者能手算验证；
- 纯 Go 标准库，`go.mod` 没有任何第三方 require，单文件静态二进制。

唯一的例外是 `ookla` 模块：它调用本机官方 speedtest 客户端，Ookla 独立处理这部分测量数据，报告会为它单独标注独立隐私条款。

## 快速开始

### 一键运行（不安装任何东西）

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
```

脚本会下载并校验 SHA-256，把缺失的固定版本基准工具按当前架构放进本次运行的临时 PATH（**不写系统目录、不通过系统包管理器安装**），跑完即清理。显式选择 `ookla` 且本机缺少 `speedtest` 时，`run.sh` 另走已校验的官方包源临时解包路径，也只写入本次 `$WORK`，不会安装到系统。直接管道运行会进入交互向导；带参数则跳过向导直接开跑：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --lang en
```

报告默认写在 `${TMPDIR:-/tmp}`，不会创建新目录；要指定位置用 `--output PATH`。

### 一键对比（不安装任何东西）

已有两份及以上 JSON 报告时，不必先装 ecs：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- 昨天.json 今天.json
```

脚本只下载主程序（对比是纯本地计算，不需要基准工具和语料），校验 SHA-256 后执行，**退出时删掉二进制、留下对比结果**。结果写在 `/tmp` 下的独立目录里并打印路径，当前目录不会新增任何东西。

校验过的二进制缓存在 `${XDG_CACHE_HOME:-~/.cache}/ecs/<版本>` 下，下次对比直接复用、不再联网；每次使用前都会重新核对摘要，因此缓存被改动过也不会被执行。`--no-cache` 关闭缓存，`--install` 把这份二进制交给 `install.sh` 装进 PATH。其余参数原样透传给 `ecs compare`，`--format txt` 和 `--format=txt` 两种写法都可以。

各输入报告的 schema 版本可以不同：跨版本时只比较双方都存在且签名一致的指标，可比性降为“部分可比”并在概览与说明处标注。

已经在用 `run.sh` 的话，加一个 `--compare` 也能进到同一条路——它转交给 `compare.sh` 执行，两个入口完全等价：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --compare 昨天.json 今天.json
```

### 安装二进制

```sh
ECS_REPOSITORY=CST-Cat/ecs ./install.sh                     # 仅安装 ecs
ECS_REPOSITORY=CST-Cat/ecs ./install.sh --with-benchmarks   # 顺带用系统包管理器装 sysbench / fio / iperf3
./install.sh --from ./bin/ecs                               # 安装本地已构建的二进制
```

安装器只下载一个发行资产并强制校验 `checksums.txt` 中的 SHA-256，不匹配即退出。默认装到 `/usr/local/bin`（不可写时回落 `~/.local/bin`）；默认二进制安装不调用 `sudo` 或系统包管理器，只有显式 `--with-benchmarks` 时才会通过检测到的系统包管理器安装 `sysbench`、`fio`、`iperf3`，并可能需要 `sudo`。

### 从源码构建

```sh
make build          # → bin/ecs
make test           # 默认 Go 测试：go test ./...
make check          # ./scripts/ci/check.sh：静态/安全/schema/工作流 YAML/脚本语法等质量门禁
make cross          # 七个 Linux 架构交叉编译到 dist/
```

`make test` 只运行默认 Go 测试（`go test ./...`）；`make check` 调用 `./scripts/ci/check.sh`，运行静态、源码安全、schema、工作流 YAML、shell 语法及其他质量门禁。CI 在 `.github/workflows/ci.yml` 中将 `unit`、`quality`、`race`、`integration`、`cross` 与提交校验分成独立 job：`race` 执行 `go test -race ./...`，`integration` 使用真实基准工具，`cross` 构建七个 Linux 架构。

从源码构建 ecs 需要 Go 1.26.5（版本由 `go.mod` 单点定义，工具链会自动获取）。运行 Release 二进制不需要 Go：它们是静态链接的，下载解压即可执行。

## 命令

| 命令 | 作用 |
| --- | --- |
| `ecs [run] [选项]` | 运行测试，默认 `standard` 档 |
| `ecs list` | 列出配置档、模块、外联级别与 IP 质量数据源 |
| `ecs render --input FILE` | 从既有 JSON 重新导出四种格式，不重跑探针 |
| `ecs compare 报告...` | 比较 2 份及以上 JSON 报告 |
| `ecs config example` | 打印配置文件示例 |
| `ecs doctor` | 检查标准基准工具是否就绪 |
| `ecs leaderboard 报告...` | 从多份报告/提交聚合排行榜参考 |
| `ecs baseline 报告...` | 同 `leaderboard` |
| `ecs submit --input FILE` | 从完整报告导出可公开入库的瘦身提交 |
| `ecs version` | 显示版本 |

所有子命令都接受 `--lang zh|en`；未指定时按环境变量判断，回落中文。

退出码：`0` 正常，`1` 参数或运行错误，`2`（仅 `--strict`）存在警告或失败探针，`130` 被中断。

## 配置档与模块

| 配置档 | 模块数 | 内容 |
| --- | --- | --- |
| `standard`（默认） | 19 | 本地与公共模块，含 `cnspeed`；不含多源 IP 质量与 Ookla |
| `full` | 21 | 全部模块，额外包含 `network` 与 `ookla` |

档位只是模块数量的快捷方式：**两档使用完全相同的基准深度与节点集**，所以同一个模块在任何档位下含义一致，跑分可直接互比。`--only` 可以在任意档位显式点名任何模块：它直接取代档位集合而非在其中筛选，因此任何模块都能从任何档位到达。

| 模块 | 名称 | 外联 | 说明 |
| --- | --- | --- | --- |
| `system` | 系统与资源 | local | 系统、虚拟化、资源、内核网络栈 |
| `network` | 网络与 IP 质量 | thirdparty | IPv4/IPv6、原生/广播、多库风险分与风险因子 |
| `bgp` | BGP 与互联观测 | public | RouteViews 当前公共 RIB：出口前缀、起源 ASN、RPKI 与互联路径样本 |
| `cpu` | CPU 性能 | local | sysbench 标准 CPU 基准（prime=20000，单线程 + 多线程） |
| `zstd` | zstd 压缩性能 | local | 固定 Silesia corpus 的压缩/解压吞吐（1T/NT） |
| `npb` | NPB EP + FT 计算性能 | local | NASA NPB-OMP 3.4.4 EP + FT Class A 浮点、FFT 与多核吞吐（1T/NT） |
| `memory` | 内存性能 | local | 官方 STREAM 内存带宽（Copy/Scale/Add/Triad × 1T/NT） |
| `crypto` | 服务器密码学性能 | local | OpenSSL speed AES-256-GCM、ChaCha20-Poly1305、SHA-256（1 worker/全 worker） |
| `disk` | 磁盘性能 | local | fio Direct I/O 基准 + QD1 4K 延迟 + Crystal/ATTO/混合矩阵 |
| `dns` | DNS 质量 | public | 公共 DNS 延迟、失败率与抖动 |
| `latency` | 网络延迟 | public | TCP 建连延迟与可达率 |
| `speed` | 网络吞吐 | public | iperf3 多节点标准吞吐基准（TCP 双向多流 + UDP） |
| `ports` | 出站端口 | public | Web、SSH、DNS 与邮件出站端口 |
| `nat` | NAT 类型 | public | STUN（RFC 5389/5780）探测 UDP 映射/过滤行为 |
| `blacklist` | IP 信誉与发信条件 | public | 17 个 DNS 黑名单收录情况 + 反向解析 FCrDNS 校验 |
| `apps` | 应用服务可达性 | public | Telegram DC、代码/镜像/软件源/证书服务可达性 |
| `cnspeed` | 中国三网测速 | public | 电信/联通/移动就近节点 HTTP 下载带宽 |
| `ookla` | Ookla Speedtest | thirdparty | 调用本机官方 speedtest；Ookla 独立处理测量数据，**不属于 ecs 零上传范围** |
| `media` | 流媒体与 AI 服务 | public | 33 个平台的公开页证据（`--media-region` 可筛选） |
| `route` | 路由追踪 | public | NextTrace Tiny 正向路径 |
| `backtrace` | 三网回程 | public | 三网回程线路识别（`--backtrace-city` 选城市） |

```sh
ecs --only system,cpu,memory,disk     # 只跑这四个
ecs --profile full --skip media       # 全量但跳过流媒体
```

## 外联分级与隐私

模块碰外部世界的方式差别很大：跑 sysbench 一个包都不发，查公共 DNS 只让对方看到出口 IP，而把出口 IP 提交给商业风控 API 是把**被查询对象本身**交了出去。`--exposure` 按"对方看到了什么"设一个上限，超限模块自动不跑：

| 级别 | 含义 |
| --- | --- |
| `local` | 不联网，一个包都不发 |
| `public` | 仅公共基础设施，对方只看到出口 IP |
| `thirdparty`（默认） | 含第三方情报服务 |
| `any` | 允许所有已登记的第三方服务 |

```sh
ecs --exposure local     # 完全离线，只跑本地基准与资源采集
ecs --exposure public    # 联网但不把出口 IP 交给商业情报 API
```

档位带进来的越限模块会被静默过滤，但 `--only` 亲手点名的模块越限会**直接报错**——用户写下的东西不该被悄悄丢掉。

**报告脱敏默认开启**：默认只遮盖本机 IP（包括 JSON 和其他输出）；主机名不会遮盖。远端 IP、路由逐跳（route hop）和 BGP 前缀不会自动遮盖。`--reveal` 保留完整本机 IP，启用时向导会明确警告。详见 [SECURITY.md](SECURITY.md) 与 [THIRD_PARTY.md](THIRD_PARTY.md)。

## 综合评分

评分只覆盖四个有标准基准支撑的维度：`cpu`、`memory`、`disk`、`bandwidth`。

```
分项分 = 实测值 / 排行榜参考均值 × 1000
总分   = 已覆盖维度的等权平均
```

规则是为了让分数**可复核**而不是好看：

- 一步除法，读者能手算验证；参考均值随报告一起呈现。
- 只累加真正跑过的维度，缺失的维度既不按 0 也不按满分，覆盖度与分数一起显示。
- 维度内先按 Group 等权聚合，再平均——宽矩阵（如 ATTO 的 18 个块档）不会因为单元多而放大权重。
- 按 vCPU 分档取参考值；档位样本不足时回落全局均值，报告写明用的是哪一档。
- 排名只在参考文件真的保存了分数分布时才显示，绝不从主机数目倒推百分位。

参考均值是**可替换的数据**，不是写死在算法里的常数：

```sh
ecs leaderboard reports/*.json --output my-baseline.json   # 从自己的样本聚合
ecs --score-baseline my-baseline.json                      # 用它来算分
ecs render --input 旧报告.json --score-baseline my-baseline.json   # 换个参考重算同一份数据
```

发行二进制内嵌一份参考。当前内嵌副本为空（`sample_count: 0`），因此在社区提交积累起来之前，分数需要自备参考文件才有意义。提交流程见 [submissions/README.md](submissions/README.md)。

## 报告输出

一次运行同时产出四种格式，JSON 是唯一事实来源，其余三种由它渲染，**渲染器不会重跑探针或推导出与 JSON 不一致的结论**。

```
reports/ecs-report-20260813-075451.json    # schema: ecs.report/v1
reports/ecs-report-20260813-075451.txt
reports/ecs-report-20260813-075451.md
reports/ecs-report-20260813-075451.html
```

通过 `run.sh` 一键运行时，报告写入 `${TMPDIR:-/tmp}`；直接运行二进制时默认写入 `./reports`。两种入口都用 `--output` 改目录，`--name` 改文件名前缀，`--format json,html` 只出部分格式。写进文件的 txt 默认不着色，方便 diff 和粘贴；`--color always` 才把 ANSI 写进文件。

字段定义见 [docs/schema.md](docs/schema.md)。

```sh
ecs compare 昨天.json 今天.json               # 对比两次运行
ecs compare a.json b.json c.json --reference 2   # 以第 2 份为基准
ecs render --input 报告.json --format html    # 从 JSON 重新导出
```

## 常用参数

```
--profile standard|full        配置档
--only / --skip 模块,模块      显式增减模块
--exposure local|public|thirdparty|any   外联上限
--lang zh|en                   界面语言
-4 / -6 / --ip-version auto|4|6   协议族
--format json,txt,md,html      输出格式
--output DIR / --name 前缀      报告位置与命名
--reveal                       保留完整本机 IP
--strict                       有警告或失败即返回退出码 2
--interactive / --yes          启用 / 跳过交互向导
--cpu-time 15s                 每轮 CPU/内存测试时长
--disk-mib 2048 / --disk-path . / --disk-multi   磁盘用量、目录与多挂载点
--iperf-duration 15s / --speed-threads 8         吞吐测试时长与并发流
--dns-attempts 8 / --latency-attempts 10         采样次数
--backtrace-city beijing,guangzhou|all           回程城市
--media-region global,jp,tw,hk,cn                流媒体地区
--ip-quality-sources all|none|名称,...            IP 质量数据源
--score-baseline FILE          评分参考文件
```

`auto` 会根据主机能力和模块自身的协议能力选择可用协议族；支持独立双栈测量的模块会分别记录 IPv4/IPv6。

完整列表：`ecs run --help`。更多可复制的场景组合见 [examples/README.md](examples/README.md)。

## 配置文件

命令行参数之外，也可以用 JSON 配置文件；未知字段会被拒绝，避免拼错的键被静默忽略。

```sh
ecs config example > ecs.json
ecs --config ecs.json
```

命令行参数优先级高于配置文件。可配置项包括档位、模块增减、外联级别、协议族、输出格式与目录、各项时长与采样次数，以及 DNS 解析器、延迟目标、路由目标、STUN 服务器、iperf3 节点等端点清单的完整替换。

## 依赖的外部工具

本地性能模块调用标准基准程序而不是自己实现——自造的算法没有可比性。`ecs doctor` 会逐个检查：

| 工具 | 用途 | 必需 |
| --- | --- | --- |
| `sysbench` | CPU 基准 | 是 |
| `zstd` 1.5.7 | 固定 Silesia corpus 压缩基准 | 是 |
| `npb-ep` / `npb-ft` 3.4.4 | NPB-OMP EP / FT Class A | 是 |
| `openssl` 3.5.7 | 密码学吞吐 | 是 |
| `stream` | 官方 STREAM 内存带宽 | 是 |
| `fio` | 磁盘 Direct I/O | 是 |
| `iperf3` | 网络吞吐 | 是 |
| `nexttrace-tiny` | 路由与回程 | 可选 |
| `ping` | ICMP 往返与丢包 | 可选 |
| `speedtest` | Ookla 官方客户端（外部服务） | 可选 |

`zstd`、`openssl`、NPB 三项**校验版本**：口径固定才谈得上跨机器比较，版本不符按缺失处理。工具缺失时报告会明确写出"该标准成绩未运行"，**不会用替代算法凑一个分数出来**。

通过 `run.sh` 运行时，缺失工具从当前架构的 `ecs-tools` 发行包临时取得：整包校验 `checksums.txt`，包内 `manifest.json` 再逐个校验每个二进制的 SHA-256，通过后才放进本次临时 PATH，退出时随工作目录清理。`ECS_AUTO_DEPS=0` 可关闭这套自动准备。

## 平台支持

仅 Linux，发布七个架构：`amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、`ppc64le`。原生探针不需要 root。不提供其他操作系统的代码路径或二进制。

## 文档

- [SECURITY.md](SECURITY.md) — 运行模型、边界与安全承诺
- [THIRD_PARTY.md](THIRD_PARTY.md) — 第三方组件、服务与会发送的数据
- [CONTRIBUTING.md](CONTRIBUTING.md) — 探针提交要求
- [CHANGELOG.md](CHANGELOG.md) — 版本变更
- [docs/schema.md](docs/schema.md) — 报告 JSON schema
- [docs/research.md](docs/research.md) — 竞品与上游能力调研
- [examples/README.md](examples/README.md) — 用法示例集
- [submissions/README.md](submissions/README.md) — 排行榜提交库

English: [README_EN.md](README_EN.md)

## 冻结的工具版本

跨机器比较只有在测量口径完全相同时才成立，因此以下版本自 v1 起冻结。`scripts/build_tools.sh` 对每个上游同时锁定 tag 与该 tag 当时解析出的 commit——tag 被移动或重新打过，构建会直接失败，而不是悄悄换掉发行包里的内容。

| 工具 | 冻结版本 | 上游锁定方式 | 运行时校验 |
| --- | --- | --- | --- |
| `sysbench` | 1.0.20 | tag `1.0.20` + commit | — |
| `zstd` | 1.5.7 | tag `v1.5.7` + commit | 是 |
| NPB-OMP | 3.4.4 | 官方 `NPB3.4.4.tar.gz` + SHA-256 | 是 |
| `openssl` | 3.5.7 | tag `openssl-3.5.7` + commit | 是 |
| STREAM | 5.10 | 官方单文件源码 + SHA-256 | — |
| `fio` | 3.42 | tag `fio-3.42` + commit | — |
| `iperf3` | 3.21 | tag `3.21` + commit | — |
| `ping`（iputils） | 20250605 | tag `20250605` + commit | — |
| `nexttrace-tiny` | 1.7.1 | tag `v1.7.1` + commit + 官方 asset SHA-256 | — |
| `speedtest` | 不冻结 | Ookla 官方分发，版本由其控制 | — |

「运行时校验」指探针在执行前核对工具自报版本，不符时按缺失处理、不产出成绩——这三项测的是软件实现本身，换一个版本数字必然变。其余工具测的是硬件与链路能力，版本不拦截，但会连同二进制 SHA-256 写入报告，并随提交进入排行榜的 `tools` 字段，供同口径分层。`nexttrace-tiny` 是包内唯一的上游预编译二进制，其余均由本仓库从源码构建。表中任何一行变动都改变测量口径，需要按 [docs/schema.md](docs/schema.md) 的规则升级对应模块的 `measurement.method`。

## 许可证

[AGPL-3.0-only](LICENSE)。IP 质量模块的多源覆盖、字段映射与风险分段思路取自 [xykt/IPQuality](https://github.com/xykt/IPQuality)，项目因此整体按 AGPL-3.0-only 发布；归属与差异见 [NOTICE](NOTICE)。

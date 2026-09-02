# ecs

无广告、默认零上传的 Linux VPS 综合测试工具。一次运行覆盖本地性能、网络质量、路由与回程、流媒体和 IP 信誉，报告只写到本地 JSON、Markdown、HTML 文件。

项目边界：

- 不展示广告、赞助或返利内容；
- 默认不上传报告；`run.sh` 的报告默认写入 `${TMPDIR:-/tmp}`，直接运行二进制时默认写入 `./reports`；
- JSON 是机器事实来源；人类可读展示包括终端文本、Markdown 和 HTML，均由 JSON 渲染；`--lang` 只改变这些人类可读展示；
- 本地性能使用标准基准工具。工具缺失或版本不符合固定口径时明确报告未运行，不合成替代分数；
- `ookla` 调用官方客户端并遵守其独立的数据处理条款，详见 [THIRD_PARTY.md](THIRD_PARTY.md)。

## 快速开始

不安装即可运行：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- --profile full --lang en
```

`run.sh` 会校验下载的文件，并把本次所需的固定工具放入临时 PATH；不会写系统目录，运行结束后清理。报告位置可用 `--output PATH` 指定。

已有报告时可以只下载主程序进行本地比较：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- yesterday.json today.json
```

`compare.sh` 是唯一的 shell compare 入口。通过管道执行时，脚本本身不会落盘，只下载临时 `ecs`；比较完成后删除临时文件并保留结果。

安装发行二进制：

```sh
./install.sh
./install.sh --with-benchmarks
# 使用镜像或派生仓库时覆盖默认仓库
ECS_REPOSITORY=owner/ecs ./install.sh
./install.sh --from ./bin/ecs
```

默认安装只安装 `ecs`；只有显式使用 `--with-benchmarks` 才会通过系统包管理器安装 `sysbench`、`fio`、`iperf3`。

## 命令

| 命令 | 作用 |
| --- | --- |
| `ecs [run]` | 按配置档运行测试，默认 `standard` |
| `ecs list` | 列出配置档、模块和外联级别 |
| `ecs doctor` | 检查外部基准工具 |
| `ecs render --input FILE` | 从 JSON 重新导出报告，不重跑探针 |
| `ecs compare REPORT...` | 比较两份或更多报告 |
| `ecs config example` | 打印 JSON 配置示例 |
| `ecs leaderboard REPORT...` | 聚合排行榜参考 |
| `ecs submit --input FILE` | 从完整报告导出可公开提交的精简 JSON |
| `ecs plan` | 以 JSON 输出解析后的机器执行计划，不执行探针 |
| `ecs version` | 显示版本 |
| `ecs help` | 显示命令总览 |

非运行命令的界面语言放在全局前缀中，例如 `ecs --lang en compare ...`；`run` 和 `plan` 也在自己的选项中接受 `--lang zh|en`。退出码为 `0`（成功）、`1`（参数或运行错误）、`2`（`--strict` 下有警告或失败）、`130`（中断）。

### 全命令参数表

以下为各命令接受的全部参数。默认值以内置默认配置为准；`--config` 提供的文件覆盖内置默认值，命令行显式参数再覆盖文件。

**全局前缀**（放在子命令之前）

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--lang zh\|en` | 环境变量，回落 `zh` | 界面语言，只影响人类可读展示 |

**`ecs [run]` / `ecs plan`**（两者共用同一套参数；`plan` 不接受 `--version`）

选择与范围：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--profile standard\|full` | `standard` | 配置档 |
| `--config FILE` | 无 | JSON 配置文件，未知字段会被拒绝 |
| `--only MODULE,...` | 无 | 只运行这些模块 |
| `--skip MODULE,...` | 无 | 跳过这些模块 |
| `--exposure local\|public\|thirdparty\|any` | `thirdparty` | 外联级别上限 |
| `--ip-version auto\|4\|6` | `auto` | 网络协议族，各网络探针分别记录 |
| `-4` / `-6` | 关 | `--ip-version 4` / `6` 的快捷方式，两者互斥 |
| `--ip-quality-sources all\|none\|NAME,...` | `all` | IP 质量数据源，名称见 `ecs list` |

输出与呈现：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--format json,md,html` | `json,md,html` | 输出格式，逗号分隔 |
| `--output DIR` | `./reports`（`run.sh` 下为临时目录） | 报告输出目录 |
| `--name PREFIX` | 时间戳命名 | 报告文件名前缀 |
| `--lang zh\|en` | 同全局 | 界面语言 |
| `--color auto\|none\|basic\|256\|truecolor\|always` | `auto` | 终端颜色模式 |
| `--no-color` | 关 | 禁用 ANSI 颜色 |
| `--reveal` | 关 | 在本地报告中保留完整本机 IP |
| `--score-baseline FILE` | 内嵌参考 | 评分排行榜参考文件 |

本地基准强度：

| 参数 | 默认 | 取值范围 | 说明 |
| --- | --- | --- | --- |
| `--cpu-time DURATION` | `15s` | 100ms–30s | 每轮 CPU/内存测试时长 |
| `--disk-mib N` | `2048` | 16–16384 | 磁盘临时文件大小（MiB） |
| `--disk-path DIR` | `.` | — | 磁盘测试目录 |
| `--disk-multi` | 关 | — | 额外测试系统盘之外的挂载盘 |
| `--disk-matrix-mode time\|fixed` | `time` | — | 磁盘矩阵测量方式；`fixed` 仅用于复核，耗时可达 20 分钟以上 |

网络探针强度与目标：

| 参数 | 默认 | 取值范围 | 说明 |
| --- | --- | --- | --- |
| `--timeout DURATION` | `10s` | 1s–60s | 单次 HTTP 请求超时 |
| `--dns-attempts N` | `8` | 1–20 | 每个 DNS 解析器的采样次数 |
| `--latency-attempts N` | `10` | 1–20 | 每个延迟目标的采样次数 |
| `--speed-threads N` | `8` | 1–32 | 测速并发流 |
| `--iperf-duration DURATION` | `15s` | 1s–30s | iperf3 每节点每方向时长 |
| `--dns-resolvers [名称=]host:port,...` | 内置列表 | — | 覆盖 DNS 解析器 |
| `--latency-targets [名称=]host:port,...` | 内置列表 | — | 覆盖延迟目标 |
| `--route-targets [名称=]host,...` | 内置列表 | — | 覆盖路由目标 |
| `--stun-servers [名称=]host:port,...` | 内置列表 | — | 覆盖 STUN 服务器 |
| `--iperf-targets [名称=]host:start[-end],...` | 内置列表 | — | 覆盖 iperf3 节点 |
| `--media-region global,jp,tw,hk,cn` | 全部 | — | 流媒体检测地区 |
| `--backtrace-city beijing\|guangzhou\|shanghai\|chengdu\|all` | `beijing,guangzhou` | — | 三网回程测试城市 |
| `--backtrace-targets carrier:名称=IP/域名,...` | 按城市展开 | — | 覆盖三网回程目标，`carrier` 只能是 `telecom`、`unicom`、`mobile` |
| `--ookla-servers telecom=ID,unicom=ID,mobile=ID` | 自动选择 | — | 固定 Ookla 三网服务器，ID 来自官方客户端 |

行为与退出：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--interactive` | 关 | 启动交互向导，无终端时自动跳过 |
| `--yes` | 关 | 跳过交互向导，直接按当前参数运行 |
| `--strict` | 关 | 探针出现警告或错误时返回退出码 `2` |
| `--version` | — | 显示版本后退出（`plan` 不支持） |
| `--help` / `-h` | — | 显示全部运行参数 |

**`ecs render --input FILE`**

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--input FILE` | 必填 | ecs JSON 报告路径 |
| `--format json,md,html` | `json,md,html` | 输出格式 |
| `--output DIR` | 与输入文件同目录 | 输出目录 |
| `--name PREFIX` | 输入文件名（去扩展名） | 报告文件名前缀 |
| `--score-baseline FILE` | 内嵌参考 | 评分排行榜参考文件 |

**`ecs compare REPORT...`**（两份或更多 JSON）

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--reference N` | `1` | 以第 N 份报告为基准（从 1 开始） |
| `--format json,md,html` | `json,md,html` | 输出格式 |
| `--output DIR` | `./reports` | 输出目录 |
| `--name PREFIX` | 时间戳命名 | 输出文件名前缀 |
| `--color auto\|none\|basic\|256\|truecolor\|always` | `auto` | 终端颜色模式 |
| `--no-color` | 关 | 禁用 ANSI 颜色 |

**`ecs leaderboard REPORT...`**

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--output FILE` | `ecs-baseline.json` | 排行榜参考输出路径 |
| `--source TEXT` | 空 | 参考来源说明，写进文件并显示在报告里 |
| `--annotate` | 关 | 输出 GitHub Actions 检查注解，让离群在检查页可见 |
| `--verbose` | 关 | 同时列出因样本不足而无法判定的档位与指标 |
| `--strict` | 关 | 遇到无效输入立即失败，不写出基线文件 |

**`ecs submit --input FILE`**

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--input FILE` | 必填 | ecs JSON 报告路径 |
| `--output PATH` | 按内容自动命名 | 提交文件输出路径或目录 |
| `--provider NAME` | 空 | 自报商家（如 `vultr`、`hetzner`），用于排行榜分组 |
| `--region NAME` | 空 | 自报地区（如 `jp`、`us-west`），用于排行榜分组 |
| `--note TEXT` | 空 | 备注，最多 200 字 |

**`ecs list` / `ecs doctor` / `ecs version` / `ecs help`** 不接受参数；**`ecs config`** 只接受子命令 `example`。

## 报告、渲染与比较

一次运行可以写三种文件格式：

```text
ecs-report-YYYYMMDD-HHMMSS.json    # ecs.report/v1，canonical machine data
ecs-report-YYYYMMDD-HHMMSS.md
ecs-report-YYYYMMDD-HHMMSS.html
```

```sh
ecs --format json,html --output ./reports --name my-run
ecs --lang en render --input ./reports/my-run.json --format html,md
ecs compare yesterday.json today.json --format md,html
ecs compare a.json b.json c.json --reference 2
```

JSON 保存采集后的 canonical 字段、表格和原始证据；中文或英文渲染不会改变机器数据。`render` 可用同一个 JSON 再生成另一种语言；`compare` 只接受通过当前 `ecs.report/v1` exact loader 的 JSON，schema 不一致的报告会被拒绝。字段定义见 [docs/schema.md](docs/schema.md)。

## 配置档与模块

| 配置档 | 内容 |
| --- | --- |
| `standard`（默认） | 本地与公共模块，不含多源 IP 质量与 Ookla |
| `full` | 全部模块，额外包含 `network` 与 `ookla` |

`--only` 会显式选择模块，`--skip` 跳过模块；越过 `--exposure` 上限的默认模块会被过滤，显式点名的越限模块会报错。

| 模块 | 主要内容 | 外联 |
| --- | --- | --- |
| `system` | 系统、虚拟化、资源与内核网络栈 | local |
| `cpu` / `zstd` / `npb` | CPU、压缩、EP/FT 标准基准 | local |
| `memory` / `crypto` / `disk` | STREAM、OpenSSL、fio 磁盘基准 | local |
| `dns` / `latency` / `speed` | DNS、TCP 延迟、iperf3 吞吐 | public |
| `ports` / `nat` / `route` / `backtrace` | 出站端口、STUN、正向与回程路由 | public |
| `bgp` / `apps` / `cnspeed` / `media` | 互联观测、服务可达性、三网测速、流媒体 | public |
| `blacklist` | IP 信誉与反向解析 | public |
| `network` / `ookla` | 多源 IP 质量、官方 Speedtest | thirdparty |

更多场景组合见 [examples/README.md](examples/README.md)。

## 外联与隐私

`--exposure` 按外部服务能看到的内容设置上限：

| 级别 | 行为 |
| --- | --- |
| `local` | 不联网，只跑本地采集和基准 |
| `public` | 只访问公共基础设施，对方可看到出口 IP |
| `thirdparty`（默认） | 允许已登记的第三方情报服务 |
| `any` | 允许所有已登记的外部服务 |

```sh
ecs --exposure local
ecs --exposure public
ecs --only cnspeed,backtrace,route --backtrace-city all
```

报告默认只遮盖本机 IP；主机名、远端 IP、逐跳路由和 BGP 前缀不会自动遮盖。`--reveal` 保留完整本机 IP，分享报告前请确认内容。完整执行模型、数据边界和第三方服务说明见 [SECURITY.md](SECURITY.md) 与 [THIRD_PARTY.md](THIRD_PARTY.md)。

## 评分与提交

评分维度为 `cpu`、`memory`、`disk`、`bandwidth`；分项分为“实测值 / 参考均值 × 1000”，缺失维度不按零计入。参考值是可替换的本地 JSON：

```sh
ecs leaderboard reports/*.json --output my-baseline.json
ecs --score-baseline my-baseline.json
ecs render --input report.json --score-baseline my-baseline.json
ecs submit --input report.json --provider vultr --region jp-tokyo --note "monthly run"
```

发行二进制内嵌参考为空时，评分需要提供自己的 `--score-baseline`。提交格式和目录规则见 [submissions/README.md](submissions/README.md)。
排行榜按完整报告的 `run.id` 计 benchmark sample；提交中的 `sample_id` 是其稳定匿名派生值，原始 `run.id` 不进入提交 JSON。提交 `id` 仍只表示 artifact 内容，因此同一运行的报告与提交只计一次，而不同运行即使指标相同也分别计数。

## 配置文件

全部参数见上文[全命令参数表](#全命令参数表)，运行时以 `ecs run --help` 为准。配置文件由以下命令生成，未知字段会被拒绝，命令行参数优先：

```sh
ecs config example > ecs.json
ecs --config ecs.json
```

## 工具与平台边界

本地性能调用 `sysbench`、固定版本 `zstd`、NPB-OMP、OpenSSL、官方 STREAM、`fio` 和 `iperf3`；路由使用 NextTrace Tiny，Ookla 使用官方客户端。`ecs doctor` 会报告缺失工具。版本校验失败按未运行处理，不使用自研基准替代。

项目只支持 Linux，发布架构包括 `amd64`、`arm64`、`armv7`、`386`、`s390x`、`riscv64`、`ppc64le`；原生探针不需要 root。

从源码构建和维护项目见 [CONTRIBUTING.md](CONTRIBUTING.md)。版本变化见 [CHANGELOG.md](CHANGELOG.md)，许可证和归属见 [NOTICE](NOTICE) 与 [LICENSE](LICENSE)。

## 其他文档

- [README_EN.md](README_EN.md) — English overview
- [docs/schema.md](docs/schema.md) — 报告 JSON schema
- [examples/README.md](examples/README.md) — 场景命令
- [submissions/README.md](submissions/README.md) — 排行榜提交格式
- [SECURITY.md](SECURITY.md) — 安全与隐私边界
- [THIRD_PARTY.md](THIRD_PARTY.md) — 外部工具与服务

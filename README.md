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
ECS_REPOSITORY=CST-Cat/ecs ./install.sh
ECS_REPOSITORY=CST-Cat/ecs ./install.sh --with-benchmarks
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
| `ecs version` | 显示版本 |

所有命令接受 `--lang zh|en`。退出码为 `0`（成功）、`1`（参数或运行错误）、`2`（`--strict` 下有警告或失败）、`130`（中断）。

## 报告、渲染与比较

一次运行可以写三种文件格式：

```text
ecs-report-YYYYMMDD-HHMMSS.json    # ecs.report/v1，canonical machine data
ecs-report-YYYYMMDD-HHMMSS.md
ecs-report-YYYYMMDD-HHMMSS.html
```

```sh
ecs --format json,html --output ./reports --name my-run
ecs render --input ./reports/my-run.json --format html,md --lang en
ecs compare yesterday.json today.json --format md,html
ecs compare a.json b.json c.json --reference 2
```

JSON 保存采集后的 canonical 字段、表格和原始证据；中文或英文渲染不会改变机器数据。`render` 可用同一个 JSON 再生成另一种语言；不同报告 schema 只比较签名一致的指标，并明确标记部分可比。字段定义见 [docs/schema.md](docs/schema.md)。

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

## 常用参数与配置

```text
--profile standard|full          配置档
--only / --skip MODULE,...       选择或跳过模块
--exposure local|public|thirdparty|any
--lang zh|en                     界面语言
--format json,md,html             输出格式
--output DIR / --name PREFIX     报告位置与命名
--reveal                         保留完整本机 IP
--strict                         有警告或失败时返回 2
--interactive / --yes            启用或跳过向导
--score-baseline FILE            评分参考
```

完整选项以 `ecs run --help` 为准。配置文件由以下命令生成，未知字段会被拒绝，命令行参数优先：

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

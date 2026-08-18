# 用法示例

可直接复制的场景化命令集。本目录只有说明文档，不含预生成的报告样例——报告里带有具体机器的实测数据，应当由你自己在目标机器上生成。

所有示例都可换成一次性运行的形式：把 `ecs` 换成

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s --
```

参数完全相同。参数总表见 `ecs run --help`，模块与外联级别见 `ecs list`。

## 第一次使用

```sh
ecs                                   # standard 档，21 个模块里的 19 个
ecs --interactive                     # 交互向导：选档位、外联级别、按组开关模块
ecs doctor                            # 先看看基准工具齐不齐
ecs list                              # 有哪些模块、各自的外联级别
```

向导通过 `/dev/tty` 读输入，所以 `curl … | sh` 也能交互。没有可用终端（cron、容器、CI）时不阻塞，直接按默认值跑。

## 按用途裁剪

### 完全离线

```sh
ecs --exposure local
```

一个网络包都不发，只跑本地基准与资源采集。超过上限的模块自动不跑，报告如实标注。

### 只要本地性能跑分

```sh
ecs --only system,cpu,memory,disk,zstd,npb,crypto
```

七个 `local` 级模块：系统资源 + sysbench CPU + STREAM 内存 + fio 磁盘 + zstd 压缩 + NPB EP/FT + OpenSSL 密码学。

### 只要网络诊断

```sh
ecs --only dns,latency,speed,ports,nat --exposure public
```

`--exposure public` 保证联网但不把出口 IP 交给任何商业情报 API。

### 中国线路专项

```sh
ecs --only cnspeed,backtrace,route --backtrace-city all
```

三网就近节点 HTTP 带宽 + 回程线路识别 + 正向路由。回程默认只测北京、广州；`all` 会加上上海、成都，耗时相应翻倍。

### 邮件服务器体检

```sh
ecs --only blacklist,ports --strict
```

17 个 DNS 黑名单收录情况、反向解析 FCrDNS 校验，以及 25/465/587 等出站端口是否被封。`--strict` 让任何警告或失败变成退出码 2，适合放进巡检脚本。

### 流媒体解锁

```sh
ecs --only media                          # 全部地区
ecs --only media --media-region jp,hk,tw  # 只看日港台
```

判断依据是平台公开页的证据，**不等同于账号能否播放、注册或支付**。

### 多源 IP 质量

```sh
ecs --only network                                                   # 13 个数据源全查
ecs --only network --ip-quality-sources ipinfo,scamalytics,abuseipdb  # 只查这三个
```

这个模块是 `thirdparty` 级别：它会把你的出口 IP 提交给商业风控接口。`standard` 档默认不含它，`full` 档才有。

## 控制耗时与资源

```sh
# 快速冒烟：缩短每轮时长与磁盘用量
ecs --cpu-time 3s --disk-mib 256 --iperf-duration 5s --only cpu,memory,disk,speed

# 深度磁盘测试：更大的文件、指定目录、附加挂载盘
ecs --only disk --disk-mib 8192 --disk-path /mnt/data --disk-multi

# 复核突发额度与长尾（每档固定传输量，可能超过 20 分钟）
ecs --only disk --disk-matrix-mode fixed
```

`--disk-matrix-mode fixed` 与默认的 `time` **不是同一测量口径，数值不可混比**，只用于复核。磁盘模块把临时文件限制在测试前可用空间的 20% 以内，完成、出错或取消时都会清理。

运行前终端会打印本次的预计耗时与磁盘用量；选中 `speed` 时会明确提示 iperf3 按时长跑满带宽，**流量不封顶**。

## 协议族

```sh
ecs -4                       # 仅 IPv4
ecs -6                       # 仅 IPv6
ecs --ip-version auto        # 默认：按主机和模块协议能力选择
```

`auto` 会根据主机能力和模块自身的协议能力选择可用协议族；支持独立双栈测量的模块会分别记录 IPv4/IPv6。没有真实全球 IPv6 路由时会跳过 IPv6 压力探测。

## 自定义测试目标

内置端点清单可以整组替换，格式统一为 `[名称=]地址`，逗号分隔：

```sh
ecs --only speed --iperf-targets "自建=iperf.example.net:5201-5210" --speed-threads 16
ecs --only dns   --dns-resolvers "Cloudflare=1.1.1.1:53,AliDNS=223.5.5.5:53" --dns-attempts 12
ecs --only latency --latency-targets "自家=api.example.com:443" --latency-attempts 20
ecs --only route --route-targets "Google=8.8.8.8,AliDNS=223.5.5.5"
ecs --only nat   --stun-servers "Xiaomi=stun.miwifi.com:3478"
ecs --only backtrace --backtrace-targets "上海电信=202.96.209.133"
```

Ookla 的三网服务器 ID 需要自己从官方客户端查（官方目录变动频繁，ecs 不联网抓取）：

```sh
ecs --only ookla --ookla-servers "电信=1234,联通=5678,移动=9012"
```

## 报告

```sh
ecs --output ./reports --name my-vps        # 目录与文件名前缀
ecs --format json                           # 只出 JSON
ecs --format json,html                      # 只出 JSON 与网页
ecs --reveal                                # 保留完整本机 IP（分享前请三思）
ecs --color always                          # 把 ANSI 颜色也写进 txt 文件
```

产物为 `<前缀>.{json,txt,md,html}`，未指定前缀时是 `ecs-report-YYYYMMDD-HHMMSS`。JSON 是唯一事实来源，可以随时重新导出别的格式而不重跑探针：

```sh
ecs render --input reports/ecs-report-20260813-075451.json --format html,md
ecs render --input 报告.json --output /tmp/out --name 改个名
```

`render` 按当前二进制支持的报告 schema 加载；`compare` 可比较 `ecs.report/*` 家族的报告。跨 schema 时，仅比较双方都存在且签名一致的指标，并标记为部分可比。报告结构只做新增式演进（新字段一律可选），因此当前实现不认识的可选字段会被忽略，已知字段不受影响。

## 对比多次运行

```sh
ecs compare 昨天.json 今天.json
ecs compare a.json b.json c.json --reference 2 --format json,txt,md,html --output ./compare
```

`--reference` 指定以第几份为基准（从 1 开始）。报告路径写在参数前后都行。

## 评分与排行榜参考

分项分 = 实测值 ÷ 参考均值 × 1000。参考是可替换的数据文件，因此**换了参考，分数不可直接互比**。

```sh
# 从自己的一批报告聚合一份参考
ecs leaderboard reports/*.json --source "我的机队 2026-08" --output my-baseline.json

# 用它算分
ecs --score-baseline my-baseline.json

# 同一份数据换个参考重算，不重跑
ecs render --input 报告.json --score-baseline my-baseline.json
```

`leaderboard` 同时接受完整报告和瘦身提交，也接受目录（递归收集 `.json`）。输出会逐项列出每个指标的样本数、按 vCPU 的分档情况，以及样本不足而回落全局均值的档位——这些都是判断分数可信度的前提。

## 提交到排行榜

```sh
ecs submit --input 报告.json --provider vultr --region jp-tokyo --note "常规月度跑分"
```

提交是**另一种东西，不是报告的压缩版**：字段白名单只含机器规格与跑分数值，出口 IP、主机名、路由路径与 ASN 一律不写入。详细规则见 [../submissions/README.md](../submissions/README.md)。

一次完成测试并直接产出提交：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --profile full --yes --provider vultr --region jp-tokyo
```

## CI 与自动化

```sh
ecs --yes --strict --format json --output ./ci-reports --lang en
```

- `--yes` 跳过向导（无终端时本就不会触发，显式写上更稳妥）；
- `--strict` 让警告或失败返回退出码 2，正常是 0，被中断是 130；
- 只出 JSON，后续用 `render` 按需生成人读格式。

配合 `--exposure local` 可以在完全隔离的构建环境里跑本地基准，不产生任何出站流量。

## 配置文件

```sh
ecs config example > ecs.json
ecs --config ecs.json
ecs --config ecs.json --only cpu,memory     # 命令行优先级更高
```

生成的示例：

```json
{
  "profile": "standard",
  "exposure": "thirdparty",
  "reveal": false,
  "ip_version": "auto",
  "ip_quality_sources": ["all"],
  "formats": ["json", "txt", "md", "html"],
  "output": "./reports",
  "disk_path": ".",
  "iperf_duration": "5s",
  "http_timeout": "10s"
}
```

还可以写 `only` / `skip` 数组，以及 `dns_resolvers`、`latency_targets`、`route_targets`、`backtrace_targets`、`stun_servers`、`iperf_targets`、`ookla_servers`、`media_regions` 等端点清单。**未知字段会被拒绝**——拼错的键不会被静默忽略。

## 环境变量

一次性运行脚本 `run.sh`：

| 变量 | 作用 |
| --- | --- |
| `ECS_REPOSITORY` | 发行仓库，默认 `CST-Cat/ecs` |
| `ECS_VERSION` | 版本 tag，默认 `latest` |
| `ECS_AUTO_DEPS=0` | 关闭自动依赖准备，让 ecs 自己报告缺失组件 |
| `ECS_KEEP=1` | 保留临时工作目录用于排障 |
| `ECS_LANG` | 脚本自身提示的语言 |
| `TMPDIR` | 工作目录与默认报告位置，必须是绝对路径 |

安装脚本 `install.sh`：`ECS_REPOSITORY`、`ECS_RELEASE_BASE`、`ECS_INSTALL_DIR`、`ECS_VERSION`。

---

English: [README_EN.md](README_EN.md) · 返回：[项目说明](../README.md)

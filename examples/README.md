# 用法示例

可直接复制的场景命令。本目录只有说明文档，不含预生成报告；报告应在目标机器上生成。通用边界、隐私和参数总表见 [../README.md](../README.md)、[../SECURITY.md](../SECURITY.md) 和 `ecs run --help`。

所有示例都可以改成一次性运行：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s --
```

## 第一次使用

```sh
ecs
ecs --interactive
ecs doctor
ecs list
```

没有可用终端时向导不会阻塞；自动化任务可显式使用 `--yes`。

## 按用途裁剪

完全离线：

```sh
ecs --exposure local
```

本地性能：

```sh
ecs --only system,cpu,memory,disk,zstd,npb,crypto
```

网络诊断：

```sh
ecs --only dns,latency,speed,ports,nat --exposure public
```

中国线路：

```sh
ecs --only cnspeed,backtrace,route --backtrace-city all
```

邮件服务器：

```sh
ecs --only blacklist,ports --strict
```

流媒体：

```sh
ecs --only media
ecs --only media --media-region jp,hk,tw
```

多源 IP 质量（会把出口 IP 发送给商业情报服务）：

```sh
ecs --only network
ecs --only network --ip-quality-sources ipinfo,scamalytics,abuseipdb
```

## 控制耗时与资源

```sh
# 快速冒烟
ecs --cpu-time 3s --disk-mib 256 --iperf-duration 5s --only cpu,memory,disk,speed

# 指定磁盘目录和容量
ecs --only disk --disk-mib 8192 --disk-path /mnt/data --disk-multi

# 固定传输量矩阵（与默认 time 口径不可混比）
ecs --only disk --disk-matrix-mode fixed
```

运行前会打印预计耗时和磁盘用量；`speed` 按配置时长发送流量，流量不封顶。临时磁盘文件会在完成、出错或取消时清理。

## 协议族与自定义目标

```sh
ecs -4
ecs -6
ecs --ip-version auto
```

`auto` 按主机和模块能力选择协议族，支持双栈的模块会分别记录 IPv4/IPv6。

端点格式统一为 `[name=]address`，逗号分隔：

```sh
ecs --only speed --iperf-targets "自建=iperf.example.net:5201-5210" --speed-threads 16
ecs --only dns --dns-resolvers "Cloudflare=1.1.1.1:53,AliDNS=223.5.5.5:53" --dns-attempts 12
ecs --only latency --latency-targets "自家=api.example.com:443" --latency-attempts 20
ecs --only route --route-targets "Google=8.8.8.8,AliDNS=223.5.5.5"
ecs --only nat --stun-servers "Xiaomi=stun.miwifi.com:3478"
ecs --only backtrace --backtrace-targets "上海电信=202.96.209.133"
ecs --only ookla --ookla-servers "电信=1234,联通=5678,移动=9012"
```

Ookla 服务器 ID 请从官方客户端取得；目录会变化，ecs 不代为抓取。外部服务的数据边界见 [../THIRD_PARTY.md](../THIRD_PARTY.md)。

## 报告与比较

```sh
ecs --output ./reports --name my-vps
ecs --format json
ecs --format json,html
ecs --reveal
ecs --color always
```

输出为 `<prefix>.{json,md,html}`。JSON 是 canonical 数据，可在之后重新渲染：

```sh
ecs render --input reports/ecs-report-20260813-075451.json --format html,md
ecs render --input 报告.json --output /tmp/out --name 改个名 --lang en
ecs compare 昨天.json 今天.json
ecs compare a.json b.json c.json --reference 2 --format json,md,html --output ./compare
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- yesterday.json today.json
```

跨 schema 只比较签名一致的指标，并标记部分可比；不会重新运行探针。字段定义见 [../docs/schema.md](../docs/schema.md)。

## 评分与提交

```sh
ecs leaderboard reports/*.json --source "我的机队 2026-08" --output my-baseline.json
ecs --score-baseline my-baseline.json
ecs render --input 报告.json --score-baseline my-baseline.json
ecs submit --input 报告.json --provider vultr --region jp-tokyo --note "常规月度跑分"
```

一次运行直接提交：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --profile full --yes --provider vultr --region jp-tokyo
```

提交 JSON 的字段白名单、目录规则和本地校验见 [../submissions/README.md](../submissions/README.md)；不要在这里重复维护隐私或排行榜政策。

## 自动化与配置

```sh
ecs --yes --strict --format json --output ./ci-reports --lang en
ecs --yes --strict --exposure local --format json --output ./reports
```

`--strict` 在有警告或失败时返回 2，成功为 0，中断为 130；之后可以用 `render` 生成可读格式。

```sh
ecs config example > ecs.json
ecs --config ecs.json
ecs --config ecs.json --only cpu,memory
```

命令行参数优先于配置文件，未知字段会被拒绝。配置中可使用 `only`、`skip`、各项时长和采样次数，以及 `dns_resolvers`、`latency_targets`、`route_targets`、`backtrace_targets`、`stun_servers`、`iperf_targets`、`ookla_servers`、`media_regions` 等列表。

一次性脚本的常用环境变量：

| 变量 | 作用 |
| --- | --- |
| `ECS_REPOSITORY` | 发行仓库覆盖值（默认 `CST-Cat/ecs`） |
| `ECS_VERSION` | 版本 tag |
| `ECS_AUTO_DEPS=0` | 关闭临时工具准备 |
| `ECS_KEEP=1` | 保留临时工作目录排障 |
| `ECS_LANG` | 脚本提示语言 |
| `TMPDIR` | 临时目录与默认报告位置 |

安装脚本另接受 `ECS_RELEASE_BASE`、`ECS_INSTALL_DIR` 和 `ECS_VERSION`。

---

English: [README_EN.md](README_EN.md) · 返回：[项目说明](../README.md)

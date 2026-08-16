# 排行榜提交库

这个目录存放社区跑分提交。CI 从这些文件聚合出排行榜参考（`baseline.json`），评分用的"参考均值"就来自它。

目录当前为空——还没有收录任何提交，所以发行二进制内嵌的参考也是空的（`sample_count: 0`）。在样本积累起来之前，综合评分需要用 `--score-baseline` 自备参考文件才有意义。

## 提交里有什么，没有什么

提交**不是报告的压缩版，而是另一种东西**。一份 `full` 报告有三千多行、上百 KB，里面还有出口 IP、主机名、反向解析、逐跳路由——那些字段对排行榜毫无用处，却足以定位一台具体机器。

所以提交的字段是**白名单**而不是黑名单：加字段必须显式改代码，不会因为报告新增了什么就悄悄多带出去。

```json
{
  "schema": "ecs.submission/v1",
  "id": "ddecbbc0421f",
  "fingerprint_version": "v2",
  "host": {
    "vcpu": 16,
    "memory_gib": 30.69,
    "virtualization": "kvm",
    "cpu_model": "AMD Ryzen 7 PRO 4750U with Radeon Graphics",
    "arch": "amd64",
    "region": "jp-tokyo",
    "provider": "vultr"
  },
  "tool": {
    "ecs": "v0.6.15",
    "sysbench": "sysbench 1.0.20",
    "fio": "fio-3.39",
    "iperf3": "iperf 3.16"
  },
  "ran_at": "2026-08-13T07:54:51Z",
  "profile": "standard",
  "memory_backend": "stream",
  "metrics": {
    "cpu_single": 1234.56,
    "cpu_multi": 9876.54,
    "memory_triad": 12345.67,
    "disk_seq_read": 890.12,
    "bandwidth_download": 945.6
  },
  "note": "常规月度跑分"
}
```

| 收录 | 理由 |
| --- | --- |
| `host.vcpu` / `memory_gib` | 决定机器落在哪个档位 |
| `host.cpu_model` / `arch` / `virtualization` | 让同型号之间可比 |
| `host.provider` / `region` | 分组用，**由提交者自报**——从 IP 推断需要引入 IP 情报数据，而那正是这个格式要避开的东西 |
| `tool.*` | 换了 ecs 或基准工具版本口径可能变；不记版本的跑分库过几个月就是一堆无法解释的数字 |
| `metrics` | 评分需要的原始实测值，键与评分维度一一对应 |

| 不收录 | 理由 |
| --- | --- |
| 出口 IP、主机名、反向解析 | 能把机器指认出来，且对分组毫无帮助 |
| ASN、精确地理位置 | 同上 |
| 逐跳路由路径、磁盘序列号 | 同上 |
| 除四个评分维度以外的任何探针结论 | 排行榜用不到 |

`ecs submit` 完成后会把这条边界打印出来：**提交只含机器规格与跑分数值；出口 IP、主机名、路由路径与 ASN 均不写入。**

## 生成一份提交

先跑测试拿到 JSON 报告，再导出提交：

```sh
ecs --profile full --output ./reports
ecs submit --input ./reports/ecs-report-20260813-075451.json \
  --provider vultr --region jp-tokyo --note "常规月度跑分"
```

或者一步完成：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --profile full --yes --provider vultr --region jp-tokyo
```

`--output` 不给时写到 `${TMPDIR:-/tmp}` 并自动命名；给一个已存在的目录则在目录内自动命名；给一个尚不存在的路径则按该路径写文件。**已存在的文件永远不会被覆盖**，符号链接一律拒绝。

三个自报字段都是可选的：`--provider`（最长 48 字符）、`--region`（最长 32 字符）、`--note`（最长 200 字符）。`system` 模块若识别到了云厂商或区域会自动填入，命令行的显式取值优先。

## 放进哪里

```
submissions/
├── 2026-08/
│   ├── ddecbbc0421f-vultr-jp-tokyo.json
│   └── 3f9a12c4e5b7-hetzner-de-fsn.json
├── 2026-09/
│   └── ...
└── baseline.json        ← CI 生成，不要手工编辑
```

两条硬性要求：

1. **恰好一层子目录。** CI 只收集 `submissions/*/*.json`，放在本目录根下或更深层的文件都不会被看到。按月份分目录是当前约定。
2. **文件名必须与内容自洽**，即 `ecs submit` 生成的那个名字：`<id>[-<provider>][-<region>].json`。`id` 由内容派生，`provider`/`region` 取自报字段并转成小写连字符形式。名字对不上目录列表就会误导人，CI 会直接报错。

`baseline.json` 是 CI 的产物，不是输入——聚合时会自动跳过它。

## 校验规则

CI 用 `go test ./internal/score/ -run TestSubmissionCorpus`（`ECS_SUBMISSION_DIR=submissions`）逐份检查。本地提交前可以先自己跑一遍：

```sh
go test ./internal/score/ -run TestSubmissionCorpus -v
ecs leaderboard --strict --output /tmp/check.json submissions
```

一份提交必须满足：

- `schema` 恰为 `ecs.submission/v1`；`fingerprint_version` 省略（旧算法）或为 `v2`；
- 文件是**单个 JSON 对象**，无未知字段、无尾随内容，不超过 256 KiB；
- `metrics` 非空，每个键都是已登记的评分指标，值为正的有限数；
- `host.vcpu` 与 `host.memory_gib` 为正数；`tool.ecs` 非空；`note` 不超过 200 字；
- 存在 STREAM 内存指标时 `memory_backend` 必须是 `stream`，没有时不得出现这个标记；
- **`id` 必须与内容重新计算的指纹一致**；
- 与库中已有提交不重复（按规范指纹判定）。

最后两条是关键：指纹由内容派生，**手改数值而不重算会被当场发现**。同理，改动 `provider`、`region`、`note` 或工具版本也会改变 `id`，此时文件名必须一起改——正确做法是重新跑一次 `ecs submit`，而不是编辑既有文件。

## 离群检测

聚合时会做离群标记，结果以 GitHub Actions 注解形式显示在检查页面上：

```sh
ecs leaderboard --annotate --verbose --output /tmp/baseline.json submissions
```

**离群只提示、不阻断合并。** 一台机器跑出远超同档的成绩，可能是新硬件、特殊配置，也可能是数据有问题——这需要人来判断，不该由阈值自动否决。`--verbose` 会额外列出样本太少、无法判定的组合。

## 排行榜参考如何重建

`main` 分支收到 push 后，CI 会：

1. 用 `ecs baseline --source "社区提交聚合" --output submissions/baseline.json submissions` 重新聚合；
2. 把结果同步到 `internal/score/embedded/baseline.json`，让发行二进制内嵌最新参考；
3. 构建并跑一遍评分测试；
4. 有变化才提交，无变化跳过。

校验 job 是只读的，写权限只给这一个 job，且只在主分支 push 时运行——检查阶段不会改工作区，PR 上也不会出现写权限。

## 参考值怎么用

- 参考值按 **vCPU 分档**：多线程分数几乎正比于核数，拿全体均值会让小机器永远不及格、大机器永远满分。档位样本不足时自动回落全局均值，报告写明用的是哪一档。
- **排名**只在参考文件真的保存了分数分布、且样本数达到阈值（当前 5）时才显示，绝不从主机数目倒推百分位。
- 每个指标的样本数会在聚合输出里逐项列出。只有一两台机器测到的指标，代表性远低于其他项，使用者应当看得到这件事。

## 刻意不做的事

不上传、不下载、不合并远端基线。样本从哪来、代表什么，由使用者自己掌握——这是分数能被解释的前提。

---

English: [README_EN.md](README_EN.md) · 返回：[项目说明](../README.md)

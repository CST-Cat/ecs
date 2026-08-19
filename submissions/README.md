# 排行榜提交库

这个目录存放社区跑分提交。`ecs leaderboard` 从这些 JSON 聚合排行榜参考；目录当前为空，因此发行二进制内嵌的参考也是空的（`sample_count: 0`）。没有自己的参考文件时，请通过 `--score-baseline` 提供一份。

提交的隐私边界和项目整体数据处理规则见 [../README.md](../README.md)、[../SECURITY.md](../SECURITY.md) 和 [../THIRD_PARTY.md](../THIRD_PARTY.md)。这里仅记录本目录特有的格式、路径与校验规则。

## 文件格式

提交不是完整报告的压缩版，而是由 `ecs submit` 生成的独立 JSON：

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
    "ecs": "v0.7.3",
    "sysbench": "sysbench 1.0.20",
    "fio": "fio-3.42",
    "iperf3": "iperf 3.21"
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

字段是显式白名单：主机规格、工具版本和评分所需的 `metrics` 会保留，完整报告中的探针结论不会因为新增字段自动进入提交。`provider`、`region` 和 `note` 为提交者自报字段；长度限制和可用评分键由 validator 检查。

## 生成提交

先运行测试取得报告，再导出：

```sh
ecs --profile full --output ./reports
ecs submit --input ./reports/ecs-report-20260813-075451.json \
  --provider vultr --region jp-tokyo --note "常规月度跑分"
```

也可以一次完成：

```sh
curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh -s -- \
  --submit --profile full --yes --provider vultr --region jp-tokyo
```

不指定 `--output` 时写入 `${TMPDIR:-/tmp}` 并自动命名；指定已有目录时在目录内命名；指定尚不存在的路径时使用该路径。已有文件不会覆盖，符号链接会拒绝。

## 放置路径

```text
submissions/
├── 2026-08/
│   ├── ddecbbc0421f-vultr-jp-tokyo.json
│   └── 3f9a12c4e5b7-hetzner-de-fsn.json
├── 2026-09/
│   └── ...
└── baseline.json        # CI 生成，不要手工编辑
```

必须满足：

1. JSON 位于恰好一层子目录中；校验只收集 `submissions/*/*.json`，根目录或更深层文件不会进入样本。
2. 文件名与 `ecs submit` 输出一致：`<id>[-<provider>][-<region>].json`。`id` 来自内容指纹，后两个部分来自自报字段并转为小写连字符。
3. `baseline.json` 是聚合产物，不是输入，聚合时会跳过。

## 本地校验

CI 使用同一套 Go 测试检查提交；本地提交前运行：

```sh
ECS_SUBMISSION_DIR=submissions go test ./internal/score/ -run TestSubmissionCorpus -v
ecs leaderboard --strict --output /tmp/check.json submissions
```

提交必须是单个不超过 256 KiB 的 JSON 对象，schema 恰为 `ecs.submission/v1`，没有未知字段或尾随内容；`metrics` 非空且键已登记，数值为正的有限数；主机 vCPU/内存为正，`tool.ecs` 非空；STREAM 指标与 `memory_backend` 一致；`id` 必须与内容重新计算的指纹一致；库中不能存在重复指纹。

修改任意值、provider、region、note 或工具版本都会改变指纹，正确做法是重新运行 `ecs submit`，不要手改既有文件。

## 重建参考

在主分支验证通过后，维护流程会聚合 `submissions/*/*.json` 生成 `baseline.json` 并更新内嵌参考。使用者本地可直接生成自己的参考：

```sh
ecs leaderboard submissions --source "社区提交聚合" --output my-baseline.json
ecs --score-baseline my-baseline.json
```

参考文件按 vCPU 档位选择均值；样本不足时回落全局均值，报告会说明采用的档位。排名只有在参考保存分布且样本足够时才显示。

---

English: [README_EN.md](README_EN.md) · 返回：[项目说明](../README.md)

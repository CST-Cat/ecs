# 跑分提交库

这里存放社区提交的跑分记录，用于聚合出评分基线（`baseline.json`）。
新用户跑 ecs 时看到的分数，就是相对这份基线的倍率。

> 目录名刻意不叫 `reports/`：那是 ecs 默认的报告输出目录（已在 `.gitignore` 里），
> 两者同名会让人把含出口 IP 的完整报告误提交进来。

## 里面存的是什么

**不是完整报告。** 完整报告有三千多行，含出口 IP、主机名、反向解析与逐跳路由——
那些字段对排行榜毫无用处，却足以定位一台具体机器。

提交格式 `ecs.submission/v1` 只带两类信息：

- **机器规格**：vCPU 数、内存、虚拟化类型、CPU 型号、架构，以及从安全白名单元数据自动识别的云厂商与地区（缺失时留空，可用 `--provider`/`--region` 覆盖）；
- **跑分数值**：CPU、内存、磁盘、带宽四个维度的原始实测值，加上产出它们的工具版本。

字段是白名单：加字段要显式改 `internal/score/submission.go`，不会因为报告新增了
什么就悄悄多带出去。每份约 3 KB，一千份也就三兆。

白名单中的磁盘扩展键包括 `fio_mixed_*`（4K/64K/512K/1M 混合读写吞吐）、
`crystal_*`（4 个 Crystal 工作负载 × 读写 × 吞吐/IOPS）和 `atto_*`（18 个 ATTO
块大小 × 读写 × 吞吐/IOPS）；ATTO 清单到 `64m`，不含 5M。
包含 STREAM 内存分数的提交必须写 `memory_backend: "stream"`，并使用
`memory_copy`、`memory_scale`、`memory_add`、`memory_triad` 四个当前聚合键。
提交只允许当前 schema 定义的指标；缺少 STREAM 键的提交只会在其它维度有效，不会伪装成内存基线。评分会把缺失项明确列出，
绝不把缺失当成零或宣称完整覆盖。

## 怎么提交

```bash
# 1. 正常跑一次测试
ecs --profile full

# 2. 从报告导出提交文件（自动识别云厂商与地区；缺失时留空）
ecs submit --input ./reports/ecs-report-*.json \
  --output ./submissions/2026-08/

# 3. fork 本仓库，把生成的文件放进 submissions/YYYY-MM/，开 PR
```

导出时会优先读取报告中的 cloud-init/DMI 明确信号；不会使用公网 IP、ASN、地理定位、
主机名或原始云元数据。无法识别时对应字段留空；需要手动分组时，可用
`--provider`/`--region` 显式覆盖。导出时会打印这份提交都带了什么，请自己过目一遍再提交。

## 目录约定

```
submissions/
  2026-08/
    e8ee186c40c1-oracle-cloud-us-sanjose-1.json
    92d7ce3c401b-oracle-cloud-us-sanjose-1.json
  baseline.json        ← 由 CI 从上面所有提交重建，不要手改
```

文件名前 12 位是内容指纹，由机器规格与测量值派生，用于查重——同一台机器
重复提交同一份结果会得到同一个 ID，CI 会拦下。

## CI 会检查什么

- schema 与字段合法性、指纹与内容是否一致（手改数值而没重算指纹会被发现）；
- 与已有提交是否重复；
- 数值是否离群到不合常理（同档内 MAD 判定，样本不足时明确不判）。

离群不等于造假——可能是新硬件或特殊配置。CI 只标记，由维护者判断。

## 当前库存

当前有 2 份样本，来自 Oracle Cloud Classic Free Tier（4 vCPU、约 24 GiB、
ARM64 KVM）。它们构成当前发布基线，但样本数仍不足以代表公共 VPS 群体；报告会明确
提示分档样本不足。

真正的参考价值要等跨机器样本积累起来。每档至少 5 台才会启用分档，
每档至少 8 台才能做离群判定。

## 分档

基线按 vCPU 分档（1/2/4/8/16/32/64+），评分时自动选对应档位：多线程分数几乎
正比于核数，用全体平均值当基线会让小机器永远不及格、大机器永远满分。

每档至少要 5 台机器才会启用，否则回落到全局基线——三台机器算出来的平均值
没有代表性。所以**提交越多，分档越细、参考价值越高**；同一机型的样本尤其有用。

## 基线怎么重建

合并到主分支后 CI 自动跑：

```bash
ecs baseline --source "社区提交聚合" --output submissions/baseline.json submissions/
```

每个指标取**算术平均**；离群值由独立的 MAD 检查标记，不把离群检测的规则
偷偷混入基线定义。
只有至少一台机器测到的指标才会进入基线，凭空补齐会让缺失伪装成数据。
评分时磁盘的 baseline、混合、Crystal、ATTO 是四个等权子组；每个子组先平均内部单元，
所以混合矩阵的 8 个单元和 ATTO 的 72 个读写表格单元不会按数量放大。内存按
STREAM 的 Copy、Scale、Add、Triad 四个等权子组处理，每个 kernel 的 1T/NT
先取中位数，缺失子组不补零。

重建后的 `baseline.json` 会在下次发版时由 CI 写进
`internal/score/embedded/baseline.json`，随二进制编译进去——新用户
`curl … | sh` 拿到的就自带最新基线，不需要额外联网。

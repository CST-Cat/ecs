# 跑分提交库

这里存放社区提交的跑分记录，用于聚合出评分基线（`baseline.json`）。
新用户跑 ecs 时看到的分数，就是相对这份基线的倍率。

> 目录名刻意不叫 `reports/`：那是 ecs 默认的报告输出目录（已在 `.gitignore` 里），
> 两者同名会让人把含出口 IP 的完整报告误提交进来。

## 里面存的是什么

**不是完整报告。** 完整报告有三千多行，含出口 IP、主机名、反向解析与逐跳路由——
那些字段对排行榜毫无用处，却足以定位一台具体机器。

提交格式 `ecs.submission/v1` 只带两类信息：

- **机器规格**：vCPU 数、内存、虚拟化类型、CPU 型号、架构，以及提交者自报的地区与商家；
- **跑分数值**：CPU、内存、磁盘、带宽四个维度的原始实测值，加上产出它们的工具版本。

字段是白名单：加字段要显式改 `internal/score/submission.go`，不会因为报告新增了
什么就悄悄多带出去。每份约 3 KB，一千份也就三兆。

## 怎么提交

```bash
# 1. 正常跑一次测试
ecs --profile full

# 2. 从报告导出提交文件（自报地区与商家，用于分组）
ecs submit --input ./reports/ecs-report-*.json \
  --region jp --provider vultr --output ./submissions/2026-08/

# 3. fork 本仓库，把生成的文件放进 submissions/YYYY-MM/，开 PR
```

导出时会打印这份提交都带了什么，请自己过目一遍再提交。

## 目录约定

```
submissions/
  2026-08/
    8109db82d028-vultr-jp.json
    b8e4d2f10c93-hetzner-fsn.json
  baseline.json        ← 由 CI 从上面所有提交重建，不要手改
```

文件名前 12 位是内容指纹，由机器规格与测量值派生，用于查重——同一台机器
重复提交同一份结果会得到同一个 ID，CI 会拦下。

## CI 会检查什么

- schema 与字段合法性、指纹与内容是否一致（手改数值而没重算指纹会被发现）；
- 与已有提交是否重复；
- 数值是否离群到不合常理。

离群不等于造假——可能是新硬件或特殊配置。CI 只标记，由维护者判断。

## 基线怎么重建

合并到主分支后 CI 自动跑：

```bash
ecs baseline --source "社区提交聚合" --output submissions/baseline.json submissions/
```

每个指标取**中位数**而不是平均：一台异常快或异常慢的机器不该把基线拽走。
只有至少一台机器测到的指标才会进入基线，凭空补齐会让缺失伪装成数据。

重建后的 `baseline.json` 会在下次发版时由 CI 写进
`internal/score/embedded/baseline.json`，随二进制编译进去——新用户
`curl … | sh` 拿到的就自带最新基线，不需要额外联网。

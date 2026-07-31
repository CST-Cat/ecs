# ecs 报告 schema

当前 schema 标识为 `ecs.report/v1`。JSON 是 Markdown 与 HTML 的唯一事实来源；渲染器不得重新执行探针或推导与 JSON 不一致的结果。

## 顶层

```json
{
  "schema_version": "ecs.report/v1",
  "tool": {},
  "run": {},
  "results": [],
  "summary": {},
  "notices": []
}
```

- `tool`：生成器版本、提交和构建时间。
- `run`：报告 ID、配置档、时间、离线/遮盖/中断状态、请求模块和输出格式。
- `results`：按实际执行顺序排列的探针结果。
- `summary`：`ok`、`warning`、`skipped`、`error` 数量与人类可读摘要。
- `notices`：适用于整份报告的方法或隐私说明。

## Result

每个探针返回一个 `Result`：

```json
{
  "id": "disk",
  "title": "磁盘性能",
  "methodology": {
    "kind": "standard-benchmark",
    "label": "标准基准",
    "engine": "fio",
    "profile": "Direct I/O seq 1 MiB QD1 + random 4 KiB QD32",
    "comparison_scope": "相同 fio/ecs 版本、文件系统、文件大小、ioengine、块大小、队列深度与时长"
  },
  "status": "ok",
  "summary": "写 500 MiB/s · 读 1.2 GiB/s",
  "started_at": "2026-07-31T12:00:00Z",
  "duration_ms": 4200,
  "fields": [],
  "measurements": [],
  "tables": [],
  "text_blocks": [],
  "notes": [],
  "sources": []
}
```

`status` 只有四种：

- `ok`：探针按计划完成；
- `warning`：缺少必需的标准工具、条件降级、部分样本失败或需要复核；此状态不保证存在成绩；
- `skipped`：被配置、离线模式、能力缺失或资源保护跳过；
- `error`：探针自身失败。Runner 会隔离 panic 并继续后续模块。

`methodology.kind` 明确结果证据等级：

- `standard-benchmark`：由 sysbench、fio、iperf3 外部标准工具直接产生；`ecs` 不使用该类别承载自研或替代分数；
- `protocol-measurement`：DNS、TCP、traceroute 等协议级现场测量，不是基准分；
- `provider-assessment`：IP 情报供应商各自的评分或分类；
- `heuristic`：公开页面和规则得出的启发式判断；
- `inventory`：系统事实采集；
`engine`、`profile` 和 `comparison_scope` 必须让读者能判断两个数字是否具有可比性。不能因为结果是数值就标成 `standard-benchmark`。

## Field

`fields` 适合离散信息：

```json
{"key":"ipv4","label":"IPv4 出口","value":"203.0.113.x","sensitive":true}
```

`sensitive` 表示写报告前应进入遮盖流程。默认 JSON 本身已经遮盖，而不是只在 HTML 上隐藏。

## Measurement

`measurements` 是可比较的数值：

```json
{
  "key": "fio_sequential_write_mib_s",
  "label": "fio 顺序写入",
  "value": 512.34,
  "unit": "MiB/s",
  "display": "512.3 MiB/s",
  "method": "fio-direct-1MiB-write-qd1-v1",
  "higher_is_better": true
}
```

- `value` 与 `unit` 用于机器处理；
- `display` 是报告中的稳定显示值；
- `method` 是版本化的工作负载/算法标识；
- `rating` 可选，只有存在公开阈值时才使用；
- `higher_is_better` 可为 `true`、`false` 或缺省，避免对无方向指标做错误排序。

不同 `method` 的数值不能直接混入同一个排名或总分。

性能模块只保存标准工具直接返回或按公开单位换算的指标。CPU 不派生并行效率，网络吞吐不派生跨节点平均值、中位数或综合分；逐节点、逐方向的 iperf3 数值各自保存。

IP 质量指标尤其需要保留 `method`：

- `ipapi` 的值是公司或 ASN 滥用概率；
- `AbuseIPDB` 是指定回溯窗口的滥用置信度；
- `IP2Location`、`Scamalytics`、`IPQS` 是不同模型的风险/欺诈分；
- `DB-IP` 原生只有 `low/medium/high` 时，`value` 使用 0/50/100 方便可视化，但 `method` 和显示值必须标记为派生映射。

这些数值都采用 `/100` 展示不代表可直接求平均。`ecs` 不生成跨供应商总分。

## Table 与 TextBlock

`tables` 保存端点、平台或样本矩阵。`columns` 定义顺序，每一行应与列数一致。

`network` 结果固定保留 IP 类型属性、风险评分、风险因子和数据源状态表。即使供应商未配置、被限流或解析失败，对应行也不能静默删除；使用 `未启用`、`失败`、`未返回` 或 `—` 区分状态。这样 Markdown/HTML 和下游程序能看到证据缺口，而不是把缺失误判成低风险。

`text_blocks` 保存路由等需要原文复核的结果。终端 ANSI 和 NUL 会在写入前移除；HTML 使用转义后的 `<pre>` 展示。

## 兼容策略

- `ecs.report/v1` 内只增加带 `omitempty` 的可选字段，不改变既有字段含义；
- 删除、重命名、改变单位或状态语义时升级 schema 版本；
- `ecs render` 忽略未知的可选字段，允许旧渲染器读取同一主版本的新报告；
- 缺少/不支持 `schema_version`、存在第二个顶层值或 JSON 结构/类型错误时直接报错；
- 工作负载变化通过 `measurement.method` 升级，即使顶层 schema 不变。

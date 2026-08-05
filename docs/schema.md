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
- `run`：报告 ID、配置档、时间、外联级别、离线/遮盖/中断状态、IP 协议族、请求模块和输出格式。
  - `exposure`：本次运行允许的最高外联级别，取值 `local`、`public`、`thirdparty`、`any`；
  - `offline`：`exposure == "local"` 的派生布尔值，保留给既有的报告消费方。
- `results`：按实际执行顺序排列的探针结果。
- `summary`：`ok`、`warning`、`skipped`、`error` 数量与人类可读摘要。
- `notices`：适用于整份报告的方法或隐私说明。

综合评分不写进报告 JSON：它依赖运行时选定的基线，把某一份基线下的分数固化进
数据文件会让同一份 JSON 在换基线后自相矛盾。评分只在 txt/md/html 渲染时计算，
基线来源与样本数随分数一起呈现。基线文件本身是独立的 `ecs.baseline/v1` 格式，
由 `ecs baseline` 生成。

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
    "profile": "Direct I/O seq 1 MiB QD1 + rand 4 KiB QD32 + randrw 50/50 QD64",
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

本次扩展仍只保存工具返回的原始指标。内存标准基准使用
`sysbench_memory_{write,read}_{single,multi}_mib_s`，并为四个操作/线程上下文保存
`sysbench_memory_{write,read}_{single,multi}_latency_ms`，并在表中保留对应的 P95 时延。
Latency 优先使用 sysbench
原生 `Latency (ms)` 的平均值；若该版本没有原生时延，则明确标记为派生，并按
`total time * 1000 / total number of events` 计算每个 1 MiB 事件的平均耗时。它不是
DRAM 单次访问延迟。补充的 `mbw_memcpy_mib_s` 必须同时披露
`mbw_array_size_mib`，以便知道动态数组大小。

磁盘 `disk` 结果保留旧的 fio/YABS 兼容指标；只要选中 `disk`（无论配置档预设还是
`--only`），就增加下面三组完整表。两档配置只预选模块集合，不改变这些表的深度：

- `50/50 混合读写`：4K、64K、512K、1M，读写吞吐保存为
  `fio_mixed_{4k,64k,512k,1m}_{read,write}_mib_s`；提交与基线沿用同一组键名，
  这些单元与其它磁盘工作负载一样进入独立的等权评分子组。

- `Crystal`：`RND4K/Q1`、`RND4K/Q32`、`SEQ1M/Q1`、`SEQ1M/Q8`，读写各保存
  `crystal_{rnd4k_q1,rnd4k_q32,seq1m_q1,seq1m_q8}_{read,write}_{mib_s,iops}`；
- `ATTO`：512B、1K、2K、4K、8K、16K、32K、64K、128K、256K、512K、1M、2M、4M、
  8M、16M、32M、64M，读写各保存
  `atto_{512b,1k,2k,4k,8k,16k,32k,64k,128k,256k,512k,1m,2m,4m,8m,16m,32m,64m}_{read,write}_{mib_s,iops}`。
  5M 不属于本 schema 的 ATTO 清单。缺失单元仍在表中显示为 `—`/`未返回`，不会补零。

内存库存字段 `memory_total`、`memory_used`、`memory_available`、
`memory_usage_percent` 以及 `balloon_reclaim`/`ksm_merging` 的 status、`*_available`
布尔值和 `*_evidence` 都是显式字段。Balloon reclaim 只有 Linux sysfs reclaim 控制项
或 reclaim/migration/deferred 相关 `/proc/vmstat` 证据才会标为可用；KSM 要求 sysfs
`run` 与 `pages_sharing` 同时存在。缺少这些可选接口时报告 `unavailable`，不从虚拟化
类型或 inflate/deflate 活动计数推断能力。

如果 cgroup 暴露 `memory.current`（或 v1 等价文件），内存测评优先用它计算有效配额内的
已用/可用值；否则保留按 `MemAvailable` 的兼容回退，并在 notes 中说明证据边界。

磁盘库存同时保存 `disk_device`、`disk_total`、`disk_used`、`disk_available` 和
`disk_usage_percent`，设备与挂载点来自测试路径的 `df -P` 记录；无法读取时保留
`unavailable`，不猜测底层块设备。

性能模块只保存标准工具直接返回或按公开单位换算的指标。CPU 不派生并行效率，网络吞吐不派生跨节点平均值、中位数或综合分；逐节点、逐方向的 iperf3 数值各自保存。

IP 质量指标尤其需要保留 `method`：

- `ipapi` 的值是公司或 ASN 滥用概率；
- `AbuseIPDB` 是指定回溯窗口的滥用置信度；
- `IP2Location`、`Scamalytics`、`IPQS` 是不同模型的风险/欺诈分；
- `DB-IP` 原生只有 `low/medium/high` 时，`value` 使用 0/50/100 方便可视化，但 `method` 和显示值必须标记为派生映射。

这些数值都采用 `/100` 展示不代表可直接求平均。`ecs` 不生成跨供应商总分。

## Table 与 TextBlock

`tables` 保存端点、平台或样本矩阵。`columns` 定义顺序，每一行应与列数一致。

表格可选 `numeric_columns` 与对应的 `numeric_higher_is_better`。前者是从零开始的
数值列索引，后者注明相对柱的方向；渲染器据此绘制按数据比例变化的柱，不猜测本地化
列名。省略方向时按越大越好处理。该元数据只影响呈现，不改变 JSON 中的原始单元格。

`sensitive_columns` 是可选的列索引数组，列出需要遮盖的列：

```json
{
  "title": "三网回程线路",
  "columns": ["运营商", "参考目标", "线路", "命中跳", "命中 IP", "状态"],
  "rows": [["电信", "北京电信", "电信 163 骨干（AS4134）", "10", "202.97.55.x", "已识别"]],
  "sensitive_columns": [4]
}
```

`network` 结果固定保留 IP 类型属性、风险评分、风险因子和数据源状态表。即使供应商未配置、被限流或解析失败，对应行也不能静默删除；使用 `未启用`、`失败`、`未返回` 或 `—` 区分状态。这样 Markdown/HTML 和下游程序能看到证据缺口，而不是把缺失误判成低风险。

`text_blocks` 保存路由等需要原文复核的结果。终端 ANSI 和 NUL 会在写入前移除；HTML 使用转义后的 `<pre>` 展示。

```json
{"title":"北京电信 (219.141.136.x) 原始路径","language":"text","content":"…","sensitive":true}
```

`sensitive` 为真时，正文里的 IP 会在写入前按段遮盖：IPv4 隐藏最后一段、IPv6 只保留 `/48`。
路由类原文必须置位——路径 IP 会暴露机房位置。遮盖刻意保留前缀，因为 `59.43`、`202.97`
这类网段正是判定线路类型的依据，整段抹掉会让证据失去复核价值。

遮盖对 `fields`、`tables` 和 `text_blocks` 一致生效，`--reveal` 同时关闭这三者。

配置文件中的 `Endpoint` 可选 `family` 字段，值为 `"4"` 或 `"6"`；空值表示自动选择。
IPv6 回程目标会固定使用 `family: "6"`，避免 IPv6-only 主机名被解析成 IPv4。
`ookla_servers` 只控制外部 Ookla 适配器的服务器选择，不代表 Ookla 客户端本身不发送测量数据。

## 兼容策略

- `ecs.report/v1` 内只增加带 `omitempty` 的可选字段，不改变既有字段含义；
- 删除、重命名、改变单位或状态语义时升级 schema 版本；
- `ecs render` 忽略未知的可选字段，允许旧渲染器读取同一主版本的新报告；
- 缺少/不支持 `schema_version`、存在第二个顶层值或 JSON 结构/类型错误时直接报错；
- 工作负载变化通过 `measurement.method` 升级，即使顶层 schema 不变。

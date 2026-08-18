# ecs 报告 schema

当前 schema 标识为 `ecs.report/v1`。JSON 是 txt、Markdown 与 HTML 的唯一事实来源（JSON 自身也可直接发布）；渲染器不得重新执行探针或推导与 JSON 不一致的结果。

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
  - `redacted`：只有在写文件前已完成全 schema 本机 IP 遮盖的副本才为 `true`；Runner 内部的原始对象为 `false`。
- `results`：按实际执行顺序排列的探针结果。
- `summary`：`ok`、`warning`、`skipped`、`error` 数量与人类可读摘要。
- `notices`：适用于整份报告的方法或隐私说明。

`ecs render` 从同一份 JSON 可重新导出 `json`、`txt`、`md`、`html` 四种格式；渲染器
不重新执行探针，也不把某种格式当成唯一输出。

综合评分不写进报告 JSON：它依赖运行时选定的基线，把某一份基线下的分数固化进
数据文件会让同一份 JSON 在换基线后自相矛盾。评分只在 txt/md/html 渲染时计算，
基线来源与样本数随分数一起呈现。基线文件本身是独立的 `ecs.baseline/v1` 格式，
由 `ecs leaderboard` 或 `ecs baseline` 生成。

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
    "comparison_scope": "相同 fio/ecs 版本、文件系统、文件大小、ioengine、块大小、队列深度与时长",
    "parameters": {
      "scope_revision": "1",
      "tool_version": "fio-3.39",
      "tool_sha256": "…",
      "actual_file_size": "2.00 GiB",
      "ioengine": "io_uring",
      "job_duration": "15s"
    }
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
  "sources": [],
  "evidence": {"valid": 53, "expected": 53, "unit": "job", "grade": "complete"},
  "failures": []
}
```

`status` 只有四种：

- `ok`：探针按计划完成；
- `warning`：缺少必需的标准工具、条件降级、部分样本失败或需要复核；此状态不保证存在成绩；
- `skipped`：被配置、离线模式、能力缺失或资源保护跳过；
- `error`：探针自身失败。Runner 会隔离 panic 并继续后续模块。

`methodology.kind` 明确结果证据等级：

- `standard-benchmark`：由 sysbench、zstd、NASA NPB-OMP、官方 STREAM、OpenSSL speed、fio、iperf3 外部标准工具直接产生；`ecs` 不使用该类别承载自研或替代分数；
- `protocol-measurement`：DNS、TCP、NextTrace 等协议级现场测量，不是基准分；
- `provider-assessment`：IP 情报供应商各自的评分或分类；
- `heuristic`：公开页面和规则得出的启发式判断；
- `inventory`：系统事实采集；
`engine`、`profile` 和 `comparison_scope` 面向读者解释口径；`parameters` 面向机器保存实际运行输入，值不参与翻译。`compare` 只信任后者，不解析说明文字。不能因为结果是数值就标成 `standard-benchmark`。

`evidence` 把模块状态与采样覆盖率分开：

- `valid`：取得可用结论或有效统计的样本数；
- `expected`：本轮实际计划的样本数；
- `unit`：稳定的机器单位，当前包括 `module`、`run`、`job`、`sample`、`query`、`target`、`operation` 和 `source`。
- `grade`：由 `valid` / `expected` 规范化得到的稳定等级：
  - `complete`：所有计划证据均有效；
  - `partial`：至少一项有效，但未达到计划数；
  - `insufficient`：有计划样本，但没有取得有效证据；
  - `not_planned`：本轮没有计划样本，不能误写成失败。

`valid` / `expected` 是等级的唯一事实来源；若序列化等级与计数冲突，加载时按计数重新规范化，不信任陈旧标签。

例如 DNS 的分母是解析器数乘每个解析器的正式查询次数，预热查询不计入；DNSBL
只有明确“已收录”或“未收录”才是有效结论，被拒和查询失败不计入 `valid`；端口不可达
仍是一次有效的可达性结论。四种渲染格式都从该字段显示同口径覆盖率与比例柱，不能用
`status == ok` 推导为 100%。当前运行中若异常或自定义探针没有提供内部样本口径，
runner 只补 `module` 级的 0/1 或 1/1。

覆盖率只表示计划观测是否取得并通过协议/解析校验，不表示外部数据源的内容准确；商业
IP 情报返回 13/13 仍可能过期、冲突或误判。

## 结构化失败原因

`failures[]` 记录“哪一步没有取得可用证据”，与模块 `status` 和 `evidence.grade` 分开。一个模块可以完整取得证据但结论需要留意，也可以只有部分证据而没有任何可归类的操作失败；渲染器不能把“未知”自动当成失败。

```json
{
  "category": "timeout",
  "stage": "query",
  "target": "resolver.example:53",
  "retryable": true,
  "count": 3,
  "message": "read udp: i/o timeout"
}
```

- `category` 是供机器分支的稳定枚举：`timeout`、`dns_error`、`connection_refused`、`network_unreachable`、`rate_limited`、`http_rejected`、`tls_error`、`parse_error`、`tool_missing`、`permission_denied`、`unsupported`、`cancelled`、`unknown`；
- `stage` 和 `target` 定位失败步骤与目标；
- `retryable` 只说明该类操作通常值得重试，不代表 ecs 会无条件重跑模块；
- `count` 合并同一目标、阶段、类别和消息的重复失败，避免逐样本膨胀；
- `message` 保留原始诊断文本，消费者不得解析它来替代 `category`。

JSON 保留枚举原值；txt、Markdown 与 HTML 使用当前语言显示类别，同时保留阶段、目标、次数、可重试性和原始信息。

## cgroup、PSI 与条件复测

`system` 的字段和 measurements 显式记录容器的真实 CPU quota、有效 cpuset、内存上限/当前使用、swap 上限、CPU throttle 累计值、PSI CPU/内存/I/O 的 `some` / `full` 值，以及 cgroup OOM/OOM-kill 计数。来源路径同时写入“cgroup 与 PSI 压力诊断”表；接口不存在时显示 unavailable，不按宿主机物理规格猜测。

CPU、zstd、NPB、STREAM（`memory`）、OpenSSL speed（`crypto`）和 fio（`disk`）会在测试窗口前后采样：

- 测试前 load、PSI `avg10` 用于识别已经存在的争抢；
- 窗口内 `/proc/stat` steal、cgroup throttle 和 OOM 增量用于识别实际干扰；
- 工作负载自身产生的 PSI 增量仍被报告，但不会单独触发复测；
- 只有首轮含有效证据且命中干扰条件时才再运行一次，最多两轮。

触发时 `Result.retry` 保存完整审计轨迹：

```json
{
  "triggered": true,
  "selected_attempt": 2,
  "selection_rule": "...",
  "trigger_reasons": ["..."],
  "attempts": [
    {
      "number": 1,
      "status": "ok",
      "duration_ms": 10000,
      "evidence": {"valid": 2, "expected": 2, "unit": "run", "grade": "complete"},
      "interference": {"detected": true, "score": 3, "reasons": [], "measurements": []},
      "measurements": []
    }
  ]
}
```

选择规则先排除没有有效证据的轮次，再选干扰评分较低的一轮；同分保留首轮。性能数值本身不参与选择，避免自动挑选偶然更高的成绩。普通 Fields/Table/Notes 也会显示复测原因、两轮干扰评分和采用轮次，因此四种人类报告无需读取隐藏 JSON 才能发现复测。

## Field

`fields` 适合离散信息：

```json
{"key":"ipv4","label":"IPv4 出口","value":"203.0.x.x","sensitive":true}
```

`sensitive` 表示该值是本机 IP，写报告前应进入遮盖流程。默认 JSON 本身已经遮盖，而不是只在 HTML 上隐藏。主机名、远端 IP 和网络前缀不应设置该标记。

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

本次扩展仍只保存工具返回的原始指标。当前内存标准基准使用官方 STREAM 5.10，分别
运行 1T 与 NT 的 `Copy`、`Scale`、`Add`、`Triad`；结构化带宽字段为
`stream_{copy,scale,add,triad}_{1t,nt}_mib_s`，线程上下文和官方原始输出必须同时保留。
STREAM 不可用时只报告标准内存基准未运行，不使用替代后端。评分若使用内存维度，会对
每个 STREAM kernel 的 1T/NT 取中位数，
四个 kernel 子组等权；缺少匹配 STREAM 基线或指标时透明列为缺失，不补替代分。

`zstd`、`npb` 和 `crypto` 与 CPU 评分保持独立，不生成跨工具综合分：

manifest 的 `corpus_path` 表示运行时路径；选中 zstd 时，`run.sh` 从独立 `ecs-corpus_silesia-v1.tar.gz` Release 资产准备固定 corpus，架构 tools 归档不再携带该文件。
- `zstd` 固定 zstd 1.5.7、level 3、5s、1/全 worker 和 `zstd-silesia-l3-v1`。corpus 是 211,938,580 bytes 的 `ecs-silesia-v1.corpus`，SHA-256 为 `8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0`；其 Silesia ZIP 来源 SHA-256 为 `0626e25f45c0ffb5dc801f13b7c82a3b75743ba07e3a71835a41e3d9f63c77af`。指标为 `zstd_{compress,decompress}_{1t,nt}_mb_s`、`zstd_{compress,decompress}_scaling_ratio` 和 `zstd_{compress,decompress}_per_worker_efficiency_percent`。
- `npb` 只保留 NASA NPB-OMP 3.4.4 的 EP/FT Class A，固定 `-O3 -fopenmp -static`、`randi8`、OpenMP 环境与 1T/全线程。指标为 `npb_{ep,ft}_{1t,nt}_mops` 和 `npb_{ep,ft}_scaling_ratio`；表格同时保留官方 `Mop/s/thread`、耗时与 Verification。只有尺寸/迭代数、线程数、版本、编译参数和 Verification 全部通过才采纳 Mop/s。
- `crypto` 固定 OpenSSL 3.5.7、16 KiB、5s、`-elapsed -mr -multi` 与 1/全 worker，分别测 AES-256-GCM、ChaCha20-Poly1305 和 SHA-256。指标为 `openssl_{aes_256_gcm,chacha20_poly1305,sha_256}_{1w,nw}_mb_s` 和对应 `*_scaling_ratio`；表格保留 `+F` 行的原始 bytes/s，MB/s 是除以 1,000,000 的可复算表示。

三个模块的 `methodology.parameters` 都包含 method version、实际线程/worker、固定时长和工具二进制 SHA-256。zstd 另包含 corpus 长度/两级 SHA-256 与完整参数指纹；NPB 包含 EP/FT 各自 SHA-256、problem class、编译参数和环境指纹；crypto 包含算法、block、计时/输出模式和六组完整参数指纹。任一口径不同时 `compare` 将其分组，不强行排名。

当 CPU allowance 只有 1 核时，sysbench、zstd、NPB、STREAM 与 OpenSSL 的“单线程”和“全线程”命令参数完全相同。这种情况只物理运行一次，然后将同一原始样本写入既有 1T/NT（或 1W/NW）指标键，以保持机器字段兼容。扩展倍率和每 worker/线程效率不生成；表格显示“不适用”，`evidence.expected` 计物理执行数而不是逻辑字段数，原始输出也只保留一份。

磁盘 `disk` 结果使用 fio/YABS 当前指标；只要选中 `disk`（无论配置档预设还是
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

- `fio_random_read_4k_qd1_latency_avg_ms`、`_p95_ms`、`_p99_ms`、`_max_ms`：同一
  Direct I/O、4 KiB、randread、iodepth=1、numjobs=1 的 fio JSON `clat` 统计；四项
  分别表示均值、P95、P99 和最大值，缺失项保持未返回，不以 0 填充。它们是当前 fio
  QD1 字段，也不与 QD32/QD64 吞吐作同一指标比较。

内存库存字段 `memory_total`、`memory_used`、`memory_available`、
`memory_usage_percent` 以及 `balloon_reclaim`/`ksm_merging` 的 status、`*_available`
布尔值和 `*_evidence` 都是显式字段。Balloon reclaim 只有 Linux sysfs reclaim 控制项
或 reclaim/migration/deferred 相关 `/proc/vmstat` 证据才会标为可用；KSM 要求 sysfs
`run` 与 `pages_sharing` 同时存在。缺少这些可选接口时报告 `unavailable`，不从虚拟化
类型或 inflate/deflate 活动计数推断能力。

如果 cgroup 暴露 `memory.current`（或 v1 等价文件），内存测评优先用它计算有效配额内的
已用/可用值；否则按 `MemAvailable` 回退，并在 notes 中说明证据边界。

磁盘库存同时保存 `disk_device`、`disk_total`、`disk_used`、`disk_available` 和
`disk_usage_percent`，设备与挂载点来自测试路径的 `df -P` 记录；无法读取时保留
`unavailable`，不猜测底层块设备。

性能模块保存标准工具直接返回的指标，以及由同一轮原始样本可复算的诊断量，不生成跨
工具或跨节点综合分：

- CPU 保留 sysbench 单/多线程 `events/s` 与各自 P95 延迟，并由两项事件率计算
  `sysbench_cpu_scaling_ratio` 和 `sysbench_cpu_per_thread_efficiency_percent`；
  有效性字段同时披露 CPU 配额、测试前负载与测试窗口内 steal 的干扰。
- STREAM 保留四个 kernel 的 1T/NT 带宽，另存 NT/1T 扩展倍率；Avg/Min/Max 时间和
  `(Max-Min)/Min` 波动率进入稳定性表，均可从同一份官方输出复核。
- iperf3 仍逐节点、逐协议族、逐方向保存最终吞吐，不派生跨节点平均值；同时保存上传/
  下载各自的 retransmits，并从已有 `intervals[].sum` 计算分秒最低、P50 与变异系数，
  不为稳定性统计增加第二轮流量。

DNS 与 TCP 延迟分别为每个解析器/目标保存 P50、P95、标准差和成功率；DNSBL 分开保存
已收录、确认干净、查询被拒、查询失败四种计数；route 为每个目标保存探测跳位、可见
跳点、超时跳点和耗时。汇总字段只用于阅读，逐项结构化数据才是机器处理依据。

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
  "title": "STUN 探测明细",
  "columns": ["协议", "服务器", "映射地址", "状态"],
  "rows": [["IPv4", "example-stun", "203.0.x.x:54321", "完成"]],
  "sensitive_columns": [2]
}
```

`network` 结果固定保留 IP 类型属性、风险评分、风险因子和数据源状态表。即使供应商未配置、被限流或解析失败，对应行也不能静默删除；使用 `未启用`、`失败`、`未返回` 或 `—` 区分状态。这样 Markdown/HTML 和下游程序能看到证据缺口，而不是把缺失误判成低风险。

`text_blocks` 保存路由等需要原文复核的结果。JSON 按 JSON 规则编码控制字符，HTML 使用转义后的 `<pre>` 展示；txt/终端渲染在排版前对整份报告的字符串值做副本净化，将 C0、DEL、C1（包括 OSC/CSI）替换为普通空格并合并连续控制符。净化之后只允许渲染器自己生成 SGR 颜色序列，输入 JSON 不会被改写。

```json
{"title":"本机绑定信息","language":"text","content":"local 203.0.x.x:443","sensitive":true}
```

本机 IP 的遮盖规则为：IPv4 隐藏后两段（保留 /16），IPv6 隐藏后六组（保留 /32），`IP:port` 中的端口号保留。
生产报告还会携带一份不写入 JSON 的本机 IP 列表，在报告 schema 的所有导出字符串值中只替换与该列表精确匹配的地址，包括失败消息、复测尝试、方法参数、表格、说明和原始输出。`route` 和 `backtrace` 的远端逐跳 IP 因此保持完整，原始路径不应整块标记为敏感。

遮盖不维护人工字段白名单；新增的导出字符串字段会自动进入同一 visitor。映射的稳定机器 key 保持不变，字符串 value 会遮盖。`--reveal` 关闭这次本机 IP 遮盖，且 `run.redacted` 保持 `false`。

配置文件中的 `Endpoint` 可选 `family` 字段，值为 `"4"` 或 `"6"`；空值表示自动选择。
IPv6 回程目标会固定使用 `family: "6"`，避免 IPv6-only 主机名被解析成 IPv4。
`ookla_servers` 只控制外部 Ookla 适配器的服务器选择，不代表 Ookla 客户端本身不发送测量数据。

`backtrace` 只会从与当前参考目标同运营商的骨干特征中选择结论；异网骨干命中仍保留在路径证据中，但表格结论为“未识别”，不用其代替目标运营商的线路类型。

## 排行榜提交与基线 schema

`ecs.submission/v1` 的新提交增加 `fingerprint_version: "v2"`。v2 ID 是除 `id` 和 `ran_at` 以外所有允许公开字段的规范 JSON SHA-256 前 12 位：同一内容在不同导出时间得到同一 ID，而主机规格、工具、profile、note 或任何精确浮点值的改动都会改变 ID。未携带 `fingerprint_version` 的历史文件仍按冻结的旧算法校验，不重写旧 ID；其它版本值直接拒绝。

`ecs.baseline/v1` 的每个 `tiers[]` 保留总机器数 `sample_count`，并增加 `metric_sample_counts`记录每个平均值的独立样本数。某个分档指标只在自身样本数至少为 5 时覆盖全局值；“该档有 5 台机器”不再让只有 1 个磁盘样本的指标冒充可用分档。为保守兼容，缺少 `metric_sample_counts` 的旧分档不被信任，评分时回落全局指标。加载器会拒绝重复/非法档位、非正有限指标、样本数越界和指标/计数 key 不一致。

## 多报告比较 schema

`ecs compare` 接受 2 份到任意多份 `ecs.report/v1` JSON，生成独立的 `ecs.compare/v1` 对象；它不会把比较结果伪装成一次探针运行。

```json
{
  "schema_version": "ecs.compare/v1",
  "tool": {},
  "generated_at": "2026-08-09T12:00:00Z",
  "reference_report": 0,
  "inputs": [],
  "summary": {
    "comparability": "partially_comparable",
    "reports": 3,
    "modules": 21,
    "comparable_metrics": 120,
    "improved": 20,
    "regressed": 12,
    "unchanged": 88,
    "metric_issues": 2,
    "observed_changes": 6,
    "status_changes": 1,
    "evidence_changes": 3
  },
  "modules": [],
  "notices": []
}
```

CLI 的 `--reference` 从 1 开始，JSON 中的 `reference_report` 与各处 `report` 索引从 0 开始。`inputs[]` 按命令行输入顺序保留标签、原报告 ID、ecs 版本、配置档、开始时间、协议族与遮盖状态。

### 数值比较

只有以下机器口径全部一致的 measurement 才进入同一 `metrics[]` 项：

- 相同模块 `id` 与 measurement `key`；
- 相同 `method`、`unit` 和 `higher_is_better`；
- 相同 `methodology.kind` 与 `methodology.parameters` 机器参数口径。

`methodology.parameters` 是新报告直接写入的稳定键值表，记录线程数、时长、工具版本/哈希、文件大小、节点集哈希等实际运行输入；`compare` 不解析 `profile` 或 `comparison_scope` 的说明文字，也不猜测旧报告参数。显示用 `label`、`engine` 与 `profile` 不参与签名，因此中英文报告不会因翻译不同而失配。

缺少稳定模块/指标 key、有限数值、`method`、`higher_is_better` 或机器参数口径，同一报告内模块 ID/指标 key 重复，只有一份报告含某项，或方法/参数分裂的指标都会进入 `metric_issues[]`，不会被静默丢弃或强行排名。若同一 key 形成两组各自可比的方法，两组数值分别保留，同时明确报告口径分裂。

每个 `MetricValue` 保存：

- `available`、原始 `value` 与 `display`；
- 组内 `rank`、`best`、`worst` 和供比例柱使用的 `quality_ratio`；
- 相对基准的 `outcome`：`improved`、`regressed`、`unchanged` 或 `no_reference`；
- `performance_change_percent`：已按优劣方向规范化，正数始终表示性能提升，低延迟等“越低越好”的指标不会反号误导。

排名与高亮只在本次输入且同口径的报告子集内有效，不是绝对质量评级。并列最佳保持相同名次；缺失报告显示 unavailable，不补零。

### 事实变化

唯一 `Field.key` 的变化进入 `observed_changes[]`。表格只有在标题与列 schema 相同、且能找到每行唯一身份列时，才会展开改变的单元格；无法安全对齐的表不会靠行号猜测。IP、ASN、平台状态、路由等离散值只展示每份报告的原值，不设置 best/worst，也不推断哪个更优。

### 自适应渲染

比较对象同样可输出 `json`、`txt`、`md`、`html` 四种格式：2 份报告使用成对并排视图，3–5 份使用紧凑矩阵，6 份及以上使用逐指标纵向排名。txt 沿用项目的终端颜色能力检测与密度柱；无色时仍保留 `★`、`▲`、`▼`、排名和柱长。HTML 使用同一色阶的比例柱、最佳值高亮、深浅色主题和窄屏单列布局，不依赖脚本或外部资源。

## 当前 schema 规则

- `ecs.report/v1` 只接收当前字段与当前单位；旧版报告不参与新参数口径比较；
- 删除、重命名、改变单位或状态语义时升级 schema 版本；
- `ecs render` 忽略当前实现不认识的可选字段，但不会因此改变已知字段；
- 缺少/不支持 `schema_version`、存在第二个顶层值或 JSON 结构/类型错误时直接报错；
- 工作负载变化通过 `measurement.method` 升级，即使顶层 schema 不变。
- `ecs.compare/v1` 消费 `ecs.report/` 家族的输入，**允许各输入的 schema 版本不同**；比较 schema 的字段语义发生不兼容变化时独立升级版本。

### 为什么只有 `compare` 放宽 schema 版本

`run`、`submit`、`render` 都要求输入的 `schema_version` 与本二进制完全一致：它们把报告当作当前 schema 的实例去解释，版本不符意味着字段语义可能已经变了，继续下去只会得到看似合理的错误结论。

`compare` 不同。真正防止「拿不可比的数字作比较」的是**指标签名**——`key` + `method` + `unit` + 优劣方向 + 逐个 `methodology.parameters`——而不是顶层版本号。由于本项目约定工作负载语义变了就升 `measurement.method`（见上一条），跨版本的语义变化**必然**表现为签名不一致，会落进 `metric_issues` 并由 `differences` 逐分量指出是 method 变了、某个参数变了还是单位换了。这比拒绝加载信息量严格更大。

硬拒绝的代价则是实打实的：schema 每升一次版，用户手里所有旧报告立刻永久不可比，而「比较不同时期的报告」正是 `compare` 存在的理由。

放宽的是版本，不是格式：`schema_version` 为空或不属于 `ecs.report/` 家族仍然直接拒绝。

**残留风险**：签名覆盖不到的字段——`status` 枚举语义、`evidence` 的 `grade` 与比例口径——理论上可以在升版时改变含义而没有任何信号。因此各输入 schema 版本不一致时：

- `summary.comparability` 封顶为 `partially_comparable`，不会给出 `comparable`；
- `notices` 首条列出涉及的全部 schema 版本；
- 每份输入的 `inputs[].schema_version` 如实记录，四种输出格式都在概览处显示版本集合。

输入报告来自不同 ecs 版本（但 schema 相同）时不降级，只在 `notices` 首条给出说明：下方的模块缺失与 method 不一致多为版本差异的正常结果，而不是故障。

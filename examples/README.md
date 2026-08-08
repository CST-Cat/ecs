# 报告样例

仓库不再保存旧版报告样例。当前实现以 README、`docs/schema.md` 和 CI 生成的真实报告
为准：内存是 STREAM 1T/NT 四 kernel，磁盘是 fio QD1 avg/P95/P99/max，默认输出
JSON/txt/md/html。

报告中的主机名、出口 IP、ASN、网段和本机地理信息已替换为示例值；测试指标、模块结构、方法、告警和公共测速节点信息保留，用于验证 JSON 消费端与报告渲染器。

生成当前报告（会使用现行 full 语义，含 Ookla；缺失时由 run.sh 走独立官方路径）：

```bash
ecs --profile full --format json,txt,md,html --output ./reports --name sample
```

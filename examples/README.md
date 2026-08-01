# 报告样例

`full-report-sample.json` 是 `full` 档的一份真实运行报告样例。

报告中的主机名、出口 IP、ASN、网段和本机地理信息已替换为示例值；测试指标、模块结构、方法、告警和公共测速节点信息保留，用于验证 JSON 消费端与报告渲染器。

生成同类报告：

```bash
ecs --profile full --format json --output ./reports --name sample
```

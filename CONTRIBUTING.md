# Contributing

`ecs` 的目标是让结果可复核，而不是单纯增加更多“检测项”。

提交探针时请同时满足：

1. 给出测试目的、方法、资源上限和失败语义；
2. 产出结构化 `Field`、`Measurement`、`Table` 或 `TextBlock`，不要只打印终端文本；
3. 网络规则要保留 HTTP 状态、重定向或页面信号，未知情况不得硬判；
4. 新增第三方服务时在 README 列出会发送的数据及其用途；
5. 外部二进制只能做可关闭的本地适配器，记录参数、可安全读取的版本与程序摘要，自动下载必须校验来源；
6. 不加入赞助商、推广、返利、遥测或默认上传；
7. 解析、遮盖、报告与协议逻辑要有**不依赖公网的确定性测试**。这条是为了让默认 `go test` 快、稳、可重复，**不是**禁止联网测试——曾经有人（包括本项目自己）拿它当理由不做真实调用，那是误读。
8. IP 风险源必须保留供应商原始语义、通道、失败状态和耗时；不得把不同模型的分值直接平均。
9. 从 GPL/AGPL 上游适配代码或规则时，必须在 `NOTICE` 和 `THIRD_PARTY.md` 标明项目、版本、许可证和改动；不要移除上游归属。
10. 性能成绩必须直接来自公开维护、可审计的标准工具；不得加入自研 CPU、内存、磁盘或网络吞吐工作负载、替代分数、跨样本聚合分或综合跑分。
11. 项目只面向 Linux。不要引入 `runtime.GOOS` 分支、其他操作系统的采集函数或发布目标；测试断言真实 Linux 行为，不放宽到"哪个平台都成立"。架构维度（`GOARCH`）仍需保留。
12. 外部工具的适配器测试必须调用**真实工具**，不得用脚本替身冒充 fio、sysbench、iperf3、ping 或 NextTrace：替身只能证明解析器认得自己造出来的输出。需要隔离时用回环（iperf3 起本地服务端、ping/NextTrace 打 `127.0.0.1`）。
13. 凡是依赖第三方服务或公共节点的能力，除确定性测试外**还必须有真实调用的实网测试**，放在 `//go:build live` 文件里。固定样本只能证明解析器认得历史格式，证明不了上游没变——`ipinfo.check.place` 全线 403、iperf3 节点池里两个域名根本不存在，都是实网测试才发现的。
    实网测试不挂在普通主分支检查上：第三方限流或改版不应让每次代码检查都变红。由定时任务与手动触发运行，并遵守"个别源失败只记录、全部失败才判失败"。
14. 数据源清单、节点地址、端口范围一律照抄上游并注明版本，**不得凭记忆填写**；改动后用实网测试复核每一条。

本地检查：

Go 工具链版本由 `go.mod` 单点定义（当前 `go 1.26.6`），CI、发布和安全复审都读它，任何地方都不再重复写版本号。升级 Go 只改那一行。本机装的是更低的补丁版时，`GOTOOLCHAIN=auto`（默认）会自动获取所需版本。运行 Release 二进制不需要 Go。

`staticcheck` 与 `govulncheck` 的版本锁在 `devtools/go.mod`（含 `go.sum` 哈希）。主模块 `go.mod` 保持零依赖：从源码构建 ecs 不下载任何模块，这是发布物的一项属性，不为了跑分析工具而放弃。

```bash
gofmt -w $(git ls-files '*.go')
make test            # go test ./...，不需要任何外部工具
make check           # 格式、vet、staticcheck、源码漏洞、schema、脚本与 workflow 语法
make integration     # 需要真实 fio / sysbench / iperf3
go test -race ./...
```

测试按"需要什么"分三类，分类写在源码的 build tag 里，不写在 CI 配置里：

| 类别 | 命令 | 需要 |
|---|---|---|
| 单元与解析 | `go test ./...` | 无 |
| 集成 | `go test -tags=integration ./...` | 宿主装有真实基准工具 |
| 实网 | `go test -tags=live ./...` | 公网，会把出口 IP 发给表内数据源 |

实网测试（按需运行）：

```bash
make live
```

性能工作负载一旦进入稳定版本，不应原地改变语义。需要调整块大小、并发、算法或计时方式时，请升级 `measurement.method`。

## 模块扩展边界

模块的跨切面元数据集中在 [`internal/config/modules.go`](internal/config/modules.go)：
ID、配置档归属、外联级别、并发分类、方法学、依赖、文案键、估算和可选评分维度都从
`ModuleDescriptor` 派生。探针包只负责把具体构造器注册到 descriptor 的
`ProbeFactory`，不再维护执行顺序、曝光或方法学副本。

新增或删除模块后，先运行：

```sh
go run ./cmd/ecs list --machine
go test ./internal/config ./internal/probe ./internal/runner ./internal/i18n ./internal/app ./internal/score
sh -n run.sh
```

`run.sh` 下载二进制后会读取并校验同一份机器可读 manifest；manifest 缺失或非法会直接停止，
不会使用另一套过期模块列表。只有显式设置 `ScoreEnabled`/`ScoreKey` 的 descriptor 才能进入
排行榜；指标定义仍由 `internal/score` 单独维护。

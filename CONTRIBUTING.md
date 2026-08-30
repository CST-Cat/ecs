# Contributing

`ecs` 的目标是让结果可复核，而不是单纯增加更多“检测项”。

提交探针时请同时满足：

1. 给出测试目的、方法、资源上限和失败语义；
2. 产出结构化 `Field`、`Measurement`、`Table` 或 `TextBlock`，不要只打印终端文本；
3. 网络规则要保留 HTTP 状态、重定向或页面信号，未知情况不得硬判；
4. 新增第三方服务时在 README 列出会发送的数据及其用途；
5. 外部二进制只能做可关闭的本地适配器，记录参数、可安全读取的版本与程序摘要，自动下载必须校验来源；
6. 不加入赞助商、推广、返利、遥测或默认上传；
7. 解析、遮盖、报告与协议逻辑要有**不依赖公网的确定性测试**，让默认 `go test` 快、稳、可重复。
8. IP 风险源必须保留供应商原始语义、通道、失败状态和耗时；不得把不同模型的分值直接平均。
9. 从 GPL/AGPL 上游适配代码或规则时，必须在 `NOTICE` 和 `THIRD_PARTY.md` 标明项目、版本、许可证和改动；不要移除上游归属。
10. 性能成绩必须直接来自公开维护、可审计的标准工具；不得加入自研 CPU、内存、磁盘或网络吞吐工作负载、替代分数、跨样本聚合分或综合跑分。
11. 项目只面向 Linux。不要引入 `runtime.GOOS` 分支、其他操作系统的采集函数或发布目标；测试断言真实 Linux 行为，不放宽到"哪个平台都成立"。架构维度（`GOARCH`）仍需保留。
12. 外部工具的适配器测试必须调用**真实工具**，不得用脚本替身冒充 fio、sysbench、iperf3、ping、STREAM 或 NextTrace：替身只能证明解析器认得自己造出来的输出。需要隔离时用回环（iperf3 起本地服务端、ping/NextTrace 打 `127.0.0.1`）。
13. 数据源清单、节点地址、端口范围一律照抄上游并注明版本，**不得凭记忆填写**；第三方服务和公共节点可能限流、改版或下线，不得从固定样本推断当前可用性，无法确认时要明确记录。

本地检查：

Go 版本按三层职责管理：

1. 根 `go.mod` 的 `go 1.22` 是源码最低兼容版本，不代表当前开发工具链版本。
2. `.github/workflows/ci.yml` 的 `compat` job 显式使用 Go `1.22.x` 并设置 `GOTOOLCHAIN=local`；它是唯一的最低版本验证入口。
3. 普通 CI、leaderboard、security 和正式 Release workflow 直接通过 `actions/setup-go@v7` 请求 `stable` 并启用 `check-latest`；Release 的 assemble 会记录实际 `GOVERSION`，再由 verify 校验。

`devtools/go.mod` 是工具 module 的最低 Go 版本要求与工具依赖清单（用于构建 `staticcheck`/`govulncheck`），不是 compiler selector。
主模块 `go.mod` 保持零依赖，因此从源码构建 ecs 不下载任何模块。运行 Release binary 不需要 Go。

GitHub Actions、`staticcheck` 及其他工具的升级均由维护者手工审查后决定；普通工作流的 Go 编译器版本由
setup-go `stable` 跟随当前官方稳定版本，仓库不会为版本升级生成拉取请求。

```bash
gofmt -w $(git ls-files '*.go')
make test            # go test ./...，不需要任何外部工具
make check           # 普通 quality 门禁；具体范围见下文
make integration     # 需要真实 fio / sysbench / iperf3 / ping / STREAM
go test -race ./...
```

`make check` 与 CI 的 `quality` job、Release 的 preflight 共用 `scripts/ci/check.sh`。它检查 Go 格式，
在默认、`integration` 两种 build tag 组合下运行 `go vet` 与固定版本的 `staticcheck`，并检查工具 manifest 示例、
shell 语法、发布中间目录忽略规则、工具包布局回归和各架构构建定义。首次构建 `staticcheck` 时可能下载
`devtools/go.mod` 与 `devtools/go.sum` 固定的依赖。

测试按"需要什么"分两类；集成测试的分类写在源码的 build tag 里，不写在 CI 配置里：

| 类别 | 命令 | 需要 |
|---|---|---|
| 单元与解析 | `go test ./...` | 无 |
| 集成 | `go test -tags=integration ./...` | 宿主装有真实 fio / sysbench / iperf3 / ping / STREAM |

性能工作负载一旦进入稳定版本，不应原地改变语义。需要调整块大小、并发、算法或计时方式时，请升级 `measurement.method`。

## 模块扩展边界

模块的跨切面元数据集中在 [`internal/config/modules.go`](internal/config/modules.go)：
ID、配置档归属、外联级别、并发分类、方法学、依赖、文案键、估算和可选评分维度都从
`ModuleDescriptor` 派生。探针包通过 `internal/probe.Builtins` 提供强类型内建探针，
`runner` 在执行边界按 ID 显式绑定；不再维护运行时 factory、执行顺序、曝光或方法学副本。

新增或删除模块后，先运行：

```sh
go run ./cmd/ecs list
go test ./internal/config ./internal/probe ./internal/runner ./internal/i18n ./internal/app ./internal/score
sh -n run.sh
```

`run.sh` 下载 `ecs` 二进制后会调用 `plan`，读取稳定的模块、配置档、暴露级别、reveal 和工具 ID，
按该结果准备依赖并运行；该输出缺失或非法会直接停止，不会使用另一套过期模块列表。只有
`internal/score.Dimensions()` 中定义的模块才能进入排行榜；评分成员资格与指标定义均由
`internal/score` 单独维护。

# 工作交接与后续计划

> 更新于 2026-07-31。仓库 <https://github.com/CST-Cat/ecs>，默认分支 `main`。
> 后续一律直接在 `main` 上开发，不再新建分支。

## 一、当前状态

`main` 已包含 5 个提交，CI（`test` × Go 1.22/stable、`race`、`cross`）在合并前为绿。

| 提交 | 内容 |
| --- | --- |
| `11259b1` | 初始提交 |
| `73a2445` | 标准化：采样窗口、cgroup、steal、fio 引擎探测与混合矩阵、延迟与 DNS 采样 |
| `fd003b0` | 功能补齐：三网回程、流媒体规则引擎、ICMP、UDP 丢包、脱敏边界、节点池 |
| `4457bda` | CI 修复（临时方案，见下方 P0） |
| `668eb7d` | 合并 `standardize-benchmarks`（分支保留未删） |

### 已解决的问题

- **采样窗口过短**：sysbench 从 0.75/2/4s 提到 5/10/15s。低于 10 秒的窗口在突发性能机型上测的是 burst credit。
- **不感知 cgroup**：新增 `internal/probe/container.go`，读 cgroup v1/v2 的 CPU 配额与内存上限，线程数取 `min(NumCPU, ceil(quota))`。
- **无超售指标**：CPU 探针记录压测窗口内的 steal 增量，`system` 记录自开机累计值，累计超 5% 告警。
- **fio 硬选 libaio**：改用 `--enghelp` 探测，按 `io_uring → libaio → psync` 回退；同步引擎下队列深度降级标注。
- **磁盘矩阵不全**：补上 YABS 兼容的 4k/64k/512k/1m 50/50 混合随机读写（QD64、numjobs=2）。
- **延迟混入 DNS**：改为先解析一次再固定对 IP 拨号，解析耗时单列；DNS 增加不计入统计的预热查询。
- **无三网回程**：新增 `backtrace` 模块，识别 CN2/163/CUII/169/CMI/CMNET。
- **流媒体误报**：重写为规则引擎（33 平台），Netflix 用双非自制剧区分"仅自制剧"，403/404 一律不判"不解锁"。
- **脱敏有漏洞**：`RedactedCopy` 此前只处理 `Fields`，现覆盖 `TextBlocks` 与 `Tables`（按段遮盖，保留 `59.43` 这类前缀供复核）。

---

## 二、P0：Linux-only 重构（下一步第一优先）

**决策**：本项目只面向 Linux VPS。删除全部 macOS / Windows / BSD 代码路径，不做兼容、不做降级。

理由：VPS 99% 是 Linux 发行版。为其他平台保留分支会让测试断言被迫放宽到"跨平台都成立"，
结果是**真实生产路径反而没被测到**——`4457bda` 就是这个错误的产物。

### 2.1 删除平台分支

| 文件 | 位置 | 处理 |
| --- | --- | --- |
| `internal/probe/system.go` | `:176` `switch runtime.GOOS` | 直接调用 `collectLinuxSystem` |
| | `:281` `collectDarwinSystem` | 删除整个函数 |
| | `:345` `collectBSDSystem` | 删除整个函数 |
| | `:353` `collectWindowsSystem` | 删除整个函数 |
| | `:364` `collectDisk` 的 windows 分支 | 删除 |
| `internal/probe/icmp.go` | `:46` `pingCommandName` | 固定为 `ping` |
| | `:56` `pingArguments` 的三分支 | 只保留 Linux（`-W` 是秒） |
| `internal/probe/disk_fio.go` | `:69` 非 Linux 直返 psync | 删除，始终走 `--enghelp` 探测 |
| `internal/probe/route.go` | `:120` windows 候选列表 | 只保留 `nexttrace`/`traceroute`/`tracepath` |
| `internal/probe/container.go` | `:69`/`:146`/`:227` 的 GOOS 检查 | 删除（恒为 Linux） |
| `cmd/ecs/signals_windows.go` | 整个文件 | 删除 |
| `cmd/ecs/signals_unix.go` | `//go:build !windows` | 删除约束，重命名为 `signals.go` |

保留 `runtime.GOARCH`：Linux VPS 有 ARM/RISC-V，架构维度不能砍。

### 2.2 收紧发布目标

- `scripts/package.sh`：13 个目标砍到 Linux 的 `amd64`/`arm64`/`armv7`/`386`/`s390x`/`riscv64`/`ppc64le`。
- `Makefile` 的 `cross`：同上。
- `install.sh`：`os_name` 只接受 `linux`，其余直接报错退出。
- `.github/workflows/ci.yml`：`cross` job 相应调整。

### 2.3 重写被弱化的测试（撤销 `4457bda` 的妥协）

这两处当前是"放宽到跨平台成立"，必须改成断言 Linux 真实行为：

1. **`probe_test.go` 的 fio 适配器**
   - 现状：假脚本 `--enghelp` 只输出 `psync`，于是测到的是同步引擎 QD1 路径。
   - 应改为：输出 `libaio`，断言 `--iodepth=64`、method 名含 `qd64`，即真实 VPS 路径。
   - 另建一个用例专门覆盖 psync 降级标注。

2. **`probe_test.go` 的 sysbench CPU**
   - 现状：按 key 断言，允许 steal 指标缺失。
   - 应改为：**断言 `cpu_steal_percent_during_test` 必须存在**——Linux 上读不到 `/proc/stat` 就是 bug。

3. 删除所有 `if runtime.GOOS == "windows" { t.Skip(...) }`（`probe_test.go:136/196/276`）。
4. `icmp_test.go:16` 的三平台 `switch` 改为只断言 Linux 参数形态。

### 2.4 文档

`README.md`、`docs/research.md`、`THIRD_PARTY.md`、`install.sh` 的用法说明都要写明**仅支持 Linux**，并删掉 macOS/Windows/FreeBSD 的表述。

---

## 三、P1：必须在真实 Linux VPS 上验证

**此前的实测是在 macOS + 中国大陆家宽完成的，对本项目毫无代表性，结论不可信。**
需要在真实 VPS 上重跑并校准：

1. **三网回程**：本地实测 6 个目标只识别出 1 个，其余因 ICMP 限速判为"可能被限速"。
   必须在海外 VPS（美西、日本、香港各一台为佳）上验证特征表是否命中，并校准：
   - `backtraceMaxHops = 20` 是否足够；
   - `backtraceConcurrency = 2` 是否仍被限速；
   - CN2 GIA/GT 的启发式（59.43 与 202.97 先后）是否可靠——这条目前只是"推测"标注，
     需要真实 GIA 与 GT 机器各一台来确认，否则考虑降级为只报 "CN2"。
2. **cgroup 感知**：在 LXC、Docker、限核 KVM 上验证配额读取与线程数计算。
   OpenVZ 的 `/proc/meminfo` 行为需单独确认。
3. **steal**：找一台已知超售的机器验证累计值与告警阈值（当前 5%）是否合理。
4. **fio 引擎探测**：在精简发行版（Alpine、无 libaio 的 Debian slim）上验证回退链。
5. **流媒体规则**：33 条规则在解锁/不解锁地区各跑一次，核对强规则判定；
   弱规则的"公开页 2xx"目前只能说明可达性，需逐步升级为强规则。
6. **iperf3 节点池**：8 个公共节点的可用性与端口范围需实测核对（清单来自记忆，未经抓取验证）。

---

## 四、P2：未完成的功能

- **CN2 GIA/GT 精确区分**：需要维护入口段表，当前只做位置启发式。
- **流媒体规则外置**：拆成带版本号和过期时间的独立文件，支持不重新编译更新。
- **弱规则升级**：Disney+、HBO Max 等 20+ 平台目前只测可达性。
- **离线 GeoIP**：MaxMind/DB-IP/IP2Location 本地库，降低对在线 API 的依赖。
- **UnixBench/Phoronix**：可选长测套件。
- **回归样本库**：KVM、LXC、OpenVZ、低内存 NAT VPS 的基线数据。
- **发布**：仓库已建但**尚未发 Release**。`install.sh:5` 的 `ECS_REPOSITORY` 仍需手动传，
  发版后应把默认值填成 `CST-Cat/ecs`，并把 README 的"快速开始"从 `go build` 改成 `install.sh` 优先。

---

## 五、新机器环境准备

```bash
git clone https://github.com/CST-Cat/ecs.git && cd ecs

# Go 1.22+（CI 同时验 1.22 与 stable）
go build ./cmd/ecs

# 本地检查（提交前必跑，与 CI 一致）
gofmt -l cmd internal     # 必须无输出
go test ./...
go vet ./...
go test -race ./...

# 标准基准工具（缺失时对应模块只告警，不会生成替代分数）
apt-get install -y sysbench fio iperf3 traceroute
# 或 ./install.sh --with-benchmarks
```

`.gitignore` 已排除 `bin/`、`dist/`、`reports/`，构建产物不会入库。

## 六、开发约束（来自 CONTRIBUTING.md，务必遵守）

1. 性能成绩只能来自 sysbench / fio / iperf3，**不得引入自研工作负载或替代分数**；
2. 不做跨供应商平均、跨节点均值、综合跑分；
3. 反爬、超时、限流一律返回"未知"，不得判成"不解锁"；
4. 外部程序只作可关闭的适配器，记录版本与 SHA-256，参数以数组传入不经过 shell；
5. 解析、遮盖、报告、协议逻辑都要有**不依赖公网**的测试；
6. 工作负载语义变化时升级 `measurement.method` 的版本号；
7. 不加入广告、推广、遥测或默认上传。

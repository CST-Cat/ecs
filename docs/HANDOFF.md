# 工作交接与后续计划

> 更新于 2026-07-31。仓库 <https://github.com/CST-Cat/ecs>，默认分支 `main`。
> 后续一律直接在 `main` 上开发，不再新建分支。

## 一、当前状态

`main` 已包含 7 个提交，CI（`test` × Go 1.22/stable、`race`、`cross`）在提交前于本地全绿。

| 提交 | 内容 |
| --- | --- |
| `11259b1` | 初始提交 |
| `73a2445` | 标准化：采样窗口、cgroup、steal、fio 引擎探测与混合矩阵、延迟与 DNS 采样 |
| `fd003b0` | 功能补齐：三网回程、流媒体规则引擎、ICMP、UDP 丢包、脱敏边界、节点池 |
| `4457bda` | CI 修复（临时方案，已被 `a829a54` 撤销） |
| `668eb7d` | 合并 `standardize-benchmarks`（分支保留未删） |
| `bb4cf2e` | 补充工作交接文档 |
| `a829a54` | **P0：Linux-only 重构（见第二节）** |

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

## 二、P0：Linux-only 重构 —— 已完成

**决策（长期有效）**：本项目只面向 Linux VPS，不保留 macOS / Windows / BSD 的代码路径、
测试分支与发布目标，不做兼容、不做降级。

理由：VPS 99% 是 Linux 发行版。为其他平台保留分支会让测试断言被迫放宽到"跨平台都成立"，
结果是**真实生产路径反而没被测到**——`4457bda` 就是这个错误的产物。

这条约束已写入 `CONTRIBUTING.md` 第 11 条与 `docs/research.md` 设计约束第 13 条，
避免后续贡献重新引入 `runtime.GOOS` 分支。

### 2.1 已删除的平台分支

| 文件 | 处理结果 |
| --- | --- |
| `internal/probe/system.go` | `switch runtime.GOOS` 改为直接调用 `collectLinuxSystem`；`collectDarwinSystem`/`collectBSDSystem`/`collectWindowsSystem` 三个函数已删除；`collectDisk` 的 windows 提前返回已删除；随之成为孤儿的 `parseHumanBytes`（macOS `vm.swapusage` 专用）与 `parseIntDefault`（sysctl 专用）一并清理 |
| `internal/probe/icmp.go` | `pingCommandName()` 换成常量 `pingCommand = "ping"`；`pingArguments` 只保留 Linux 形态（`-W` 以秒计、不足一秒进位） |
| `internal/probe/disk_fio.go` | 删除非 Linux 直返 psync 的提前返回，始终走 `--enghelp` 探测 |
| `internal/probe/route.go` | 候选固定为 `nexttrace`/`traceroute`/`tracepath`；删除 `.exe` 后缀处理与 `tracert` 参数分支 |
| `internal/probe/container.go` | 删除 `detectCPUAllowance`、`cgroupMemoryLimit`、`readCPUTimes` 里的三处 GOOS 检查 |
| `cmd/ecs/signals_windows.go` | 已删除 |
| `cmd/ecs/signals_unix.go` | 已重命名为 `signals.go`，去掉 `//go:build !windows` |

`runtime` 包在 `internal/probe` 中现在只剩 `GOARCH` 与 `NumCPU()`：Linux VPS 有 ARM/RISC-V，
架构维度不能砍。

### 2.2 已收紧的发布目标

- `scripts/package.sh`：13 个目标砍到 Linux 的 `amd64`/`arm64`/`armv7`/`386`/`s390x`/`riscv64`/`ppc64le`；
  windows 的 zip 分支与 checksums 里的 `ecs_*.zip` 一并删除（否则 glob 展开失败会中断打包）。
- `Makefile` 的 `cross`：同上七个目标。
- `install.sh`：`os_name` 不是 `linux` 直接报错退出；删除已无对应资产的 darwin/freebsd 组合校验，
  以及 `pkg`（FreeBSD）与 `brew`（macOS）包管理器分支。
- `.github/workflows/ci.yml`：`cross` job 调用 `make cross`，随 Makefile 自动收敛，未改动。

已实跑验证：七个架构全部构建成功；`scripts/package.sh` 产出 7 个 tar.gz 与格式正确的
`checksums.txt`，资产名与 `install.sh` 的期望一致。

### 2.3 已重写的测试（`4457bda` 的妥协已撤销）

1. **fio 适配器**拆成两个用例，假脚本的 `--enghelp` 输出改为可配置：
   - `TestRunFIODiskWithAsyncEngine`：报告 `libaio`，断言参数含 `--iodepth=32`/`--iodepth=64`、
     method 名含 `qd32`/`qd64`、ioengine 字段标"异步"，且不得出现同步降级说明——即真实 VPS 路径。
   - `TestRunFIODiskDowngradesSynchronousEngine`：报告 `psync`，断言参数**不含**高队列深度、
     method 名为 `qd1`、并且必须披露降级说明。
   两个用例对同一份代码作相反断言，引擎探测一旦失效必有一个失败。
2. **sysbench CPU** 断言 `cpu_steal_percent_during_test` 必须存在。假脚本的 cpu 分支加了
   `sleep 0.2`：`/proc/stat` 是 jiffies 计数，瞬时返回的假脚本会让前后两次采样完全相同，
   steal 增量无从计算。状态断言放宽为"不是 error"——宿主机 steal 高时降级 warning 是正确行为。
   另加 `TestStealSamplingReadsProcStat` 覆盖累计口径、增量口径与计数器倒退。
3. 三处 `if runtime.GOOS == "windows" { t.Skip(...) }` 已删除。
4. `icmp_test.go` 的三平台 `switch` 改为只断言 Linux 参数形态，并新增不足一秒进位的断言。

### 2.4 文档

`README.md`（快速开始已写明仅支持 Linux 与七个发布架构）、`docs/research.md`（设计约束第 13 条
与实测教训第 4 条）、`THIRD_PARTY.md`、`SECURITY.md`、`CONTRIBUTING.md`、`install.sh` 用法说明
均已更新，macOS/Windows/FreeBSD 与 `tracert` 的表述已删除。

### 2.5 顺带修复：busybox ping

Linux-only 的目的是覆盖真实生产路径，因此补上了一个此前被跨平台正则掩盖的差异：
Alpine 等精简镜像默认是 busybox ping，统计行只有 `min/avg/max` 三段，没有 iputils 的 `mdev`，
原四段正则在这些镜像上解析不出 RTT（丢包率仍可解析，于是 RTT 静默为 0）。现增加三段回退正则，
并给 `icmpStats` 加 `StdDevKnown` 字段，区分"标准差为 0"与"该 ping 实现不报告标准差"。

> 这一条超出了原 P0 清单。如认为不该在本次改动中携带，回退它不影响 P0 其余部分。

---

## 三、P1：必须在真实 Linux VPS 上验证（现为第一优先）

**此前的实测是在 macOS + 中国大陆家宽完成的，对本项目毫无代表性，结论不可信。**
P0 已完成，此项现在是最高优先级。需要在真实 VPS 上重跑并校准：

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
   注意本地开发机没有 fio/sysbench/iperf3，P0 只用假适配器覆盖了两条引擎路径，
   **真实 `fio --enghelp` 的输出格式尚未在任何机器上验证过**。
5. **busybox ping**：2.5 的三段回退只有单元测试覆盖，需要在 Alpine 上跑一次 `latency` 模块确认。
6. **流媒体规则**：33 条规则在解锁/不解锁地区各跑一次，核对强规则判定；
   弱规则的"公开页 2xx"目前只能说明可达性，需逐步升级为强规则。
7. **iperf3 节点池**：8 个公共节点的可用性与端口范围需实测核对（清单来自记忆，未经抓取验证）。

---

## 四、P2：未完成的功能

- **CN2 GIA/GT 精确区分**：需要维护入口段表，当前只做位置启发式。
- **流媒体规则外置**：拆成带版本号和过期时间的独立文件，支持不重新编译更新。
- **弱规则升级**：Disney+、HBO Max 等 20+ 平台目前只测可达性。
- **离线 GeoIP**：MaxMind/DB-IP/IP2Location 本地库，降低对在线 API 的依赖。
- **UnixBench/Phoronix**：可选长测套件。
- **回归样本库**：KVM、LXC、OpenVZ、低内存 NAT VPS 的基线数据。
- **发布**：仓库已建但**尚未发 Release**。`install.sh` 的 `ECS_REPOSITORY` 仍需手动传。
  发版后应把默认值填成 `CST-Cat/ecs`，并把 README 的"快速开始"从 `go build` 改成 `install.sh` 优先。
  **在 Release 存在之前不要填默认值**，否则 `install.sh` 会指向一个 404 的下载地址；
  `SECURITY.md` 当前的"必须显式设置 `ECS_REPOSITORY`"说明也要同步改。

---

## 五、新机器环境准备

```bash
git clone https://github.com/CST-Cat/ecs.git && cd ecs

# 仅支持 Linux。Go 1.22+（CI 同时验 1.22 与 stable）
go build ./cmd/ecs

# 本地检查（提交前必跑，与 CI 一致）
gofmt -l cmd internal     # 必须无输出
go test ./...
go vet ./...
go test -race ./...
sh -n install.sh
bash -n scripts/package.sh

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
7. 不加入广告、推广、遥测或默认上传；
8. **只面向 Linux**：不引入 `runtime.GOOS` 分支或其他系统的发布目标，测试断言真实 Linux 行为。

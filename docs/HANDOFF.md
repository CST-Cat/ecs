# 工作交接与后续计划

> 更新于 2026-08-10。仓库 <https://github.com/CST-Cat/ecs>，默认分支 `main`，当前工作位于 `v0.6.5` 之后的 Unreleased。
> 后续一律直接在 `main` 上开发，不再新建分支。
>
> 当前路由实现为 NextTrace Tiny；本地性能链路为 sysbench、zstd、NPB EP+FT、STREAM、OpenSSL speed、fio，网络吞吐为 iperf3。报告默认四格式 JSON/txt/md/html。

## 一、当前状态

`main` 当前维护 Linux-only 实现：标准性能链路固定为 sysbench CPU、zstd 1.5.7 + Silesia corpus、NASA NPB-OMP 3.4.4 EP/FT Class A、官方 STREAM、OpenSSL 3.5.7 speed、fio 和 iperf3；路由与回程固定使用官方 NextTrace Tiny；CI 负责七架构的 10 个工具、manifest、摘要、独立 corpus 资产与真实 smoke test。

### 已解决的问题

- **采样窗口过短**：sysbench 从 0.75/2/4s 提到 5/10/15s。低于 10 秒的窗口在突发性能机型上测的是 burst credit。
- **不感知 cgroup**：新增 `internal/probe/container.go`，读 cgroup v1/v2 的 CPU 配额与内存上限，线程数取 `min(NumCPU, ceil(quota))`。
- **无超售指标**：CPU 探针记录压测窗口内的 steal 增量，`system` 记录自开机累计值，累计超 5% 告警。
- **fio 硬选 libaio**：改用 `--enghelp` 探测，按 `io_uring → libaio → psync` 回退；同步引擎下队列深度降级标注。
- **磁盘矩阵不全**：补上 YABS 口径的 4k/64k/512k/1m 50/50 混合随机读写（QD64、numjobs=2）。
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
| `internal/probe/route.go` | 路由引擎固定为官方 `nexttrace-tiny`；删除其他实现、`.exe` 后缀处理与 `tracert` 参数分支 |
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

当前 CI 通过七架构 matrix 逐架构真实构建和 smoke；新增上游先按实际用途裁剪，四个交叉
目标由宿主交叉编译器完成全部构建后才交给 QEMU 做短功能验证，绝不在 QEMU 中编译或跑
性能测试。NPB 发行包仍只含 EP/FT Class A；交叉 job 用同源码/参数的临时 Class S 完成
Verification，并对发行 Class A ELF 做静态链接、架构、摘要与 manifest 校验。任一能力不满足
都会诚实失败，不用 fixture 或假 binary 继续打包。release 只消费 tools job 的真实 stage，
并由 `scripts/package.sh` 产出 7 个工具 tar.gz 与独立的 Silesia corpus 归档，最终由 release
workflow 统一生成 `checksums.txt`。工具包使用 gzip，避免最小系统因尚无 zstd 而无法解出工具包；
固定 corpus 只在 zstd 选中时从独立 Release 资产下载并校验。

### 2.3 已重写的测试（`4457bda` 的妥协已撤销）

1. **fio 适配器**：撤销"只报告 psync"的妥协，改为断言真实生效的队列深度与 `qd32`/`qd64`
   方法名。这一步初版仍用假脚本，随后被 2.6 全部替换为真实工具。
2. **sysbench CPU** 断言 `cpu_steal_percent_during_test` 必须存在——Linux 上读不到
   `/proc/stat` 就是 bug。状态断言放宽为"不是 error"：宿主机 steal 高时降级 warning 是
   正确行为。另加 `TestStealSamplingReadsProcStat` 覆盖累计口径、增量口径与计数器倒退。
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

### 2.6 后续：删除全部脚本替身，测试改用真实工具

P0 初版仍用 `#!/bin/sh` 假脚本冒充 fio / sysbench / iperf3，随后已全部删除。
替身只能证明"解析器认得它自己造出来的输出"，证明不了它认得工具的真实输出。
现在的做法：

| 工具 | 测试方式 |
| --- | --- |
| fio | 真实 `fio` 在临时目录跑完整 53 作业矩阵和固定 QD1 latency（配置档只决定模块预设）；断言探测到的引擎与队列深度标注自洽 |
| STREAM/sysbench | 真实官方 STREAM 跑 1T/NT 的 Copy/Scale/Add/Triad；STREAM 缺失时明确报告内存基准未运行；CPU 仍跑 sysbench |
| zstd | 固定 v1.5.7/level 3/5s/1T+NT；只保留 benchmark/压缩/解压/多线程能力，运行前校验 Silesia ZIP 与拼接 corpus SHA-256，smoke 真实压缩/解压 |
| NPB | 发布包只编译 gfortran/OpenMP EP/FT Class A；原生 smoke 跑 A，交叉 QEMU smoke 跑同工具链临时 Class S，均检查官方 header、线程、参数与 Verification |
| OpenSSL | 裁掉 TLS/网络/动态组件和无关算法族，只生成上游 mandatory files 并构建官方 `apps/openssl` 依赖；真实运行 `speed -mr -multi 1` 的三种固定算法 |
| iperf3 | 在回环起真实 `iperf3 -s`，跑 TCP 双向与 UDP |
| ping | 真实 `ping 127.0.0.1` |
| NextTrace | 真实官方 Tiny 的 JSON 路径探测（缺失时由 `run.sh` 临时准备） |

全部不依赖公网。`requireTool` 在 CI（`CI` 环境变量）下缺工具直接 `Fatal`，
本地缺工具才 `Skip`，避免测试静默跳过后仍然显示为绿；`ci.yml` 的 `test` 与 `race`
tools job 在架构容器中安装并真实验证 sysbench、zstd、NPB EP/FT、OpenSSL、STREAM、fio、iperf3、ping 和 NextTrace Tiny；
宿主机只运行 parser/renderer/config 等确定性 Go tests，真实外部工具 live tests 在容器 smoke 中完成。

第三方 HTTP 数据源（IP 质量、流媒体）的解析器仍用固定样本，但**不再以此为终点**：
真实调用的验证已补成 `//go:build live` 测试，见 3.3。原先援引"测试不依赖公网"来
不做真实调用是对该约束的误读——它要求的是"这些逻辑要有确定性测试"，从没说禁止
联网测试。`CONTRIBUTING.md` 第 7、13 条已改写澄清。
`ipquality_test.go` 里的 `httptest` 服务器测的是跨主机重定向拒绝这一安全边界，
真实数据源不会配合构造重定向，这个必须保留。

### 2.7 顺带修复：TCP 被透明代理代答时无提示

见第三节 3.2。

---

## 三、P1：真实环境验证（部分已完成）

### 3.1 已在真实 Linux + 真实工具上验证

开发机装上官方 STREAM、fio 3.39 / sysbench 1.0.20 / iperf3 3.18，并准备 NextTrace 后实跑，结论：

- **fio 引擎探测成立**。真实 `--enghelp` 每行带 TAB 缩进、首行是 `Available IO engines:`，
  `TrimSpace` 能正确处理；该机命中 `io_uring`，报告按 QD32/QD64 标注，10 项指标齐全，
  混合矩阵与临时文件清理均正常。
- **STREAM 解析成立**。官方 STREAM 1T/NT 四 kernel 由真实运行产出；sysbench CPU
  单/多线程事件率与 `cpu_steal_percent_during_test` 也由真实运行产出。
- **iperf3 解析成立**。回环起真实服务端跑通 TCP 双向与 UDP 丢包/抖动。
- **ping 与 NextTrace 解析成立**。iputils 四段统计行与 NextTrace JSON 跳点均正确解析。
- **测试已全部改用真实工具**，三个脚本替身删除，CI 会安装基准工具；路由测试缺失 NextTrace 时按设计跳过。

### 3.2 本次验证中发现并修复的缺陷

- **TCP 被透明代理代答时无任何提示**：`latency` 报告到 Cloudflare 的 TCP 建连 0.11 ms、
  状态 `ok`，而同表 ICMP 是 221 ms。已增加交叉校验，TCP 中位数不到同目标 ICMP 平均的
  1/5 时降级为 `warning` 并说明 TCP 列不能当作链路延迟。已在真实环境确认会触发。
- **iperf3 节点池有两个域名根本不存在**：`ams.speedtest.eranium.net` 与
  `speedtest.tyo1.jp.leaseweb.net` 在三个独立 DNS 上都无 A 记录，是凭印象编的；
  Eranium 的端口范围也错了。已逐条抄自 YABS `v2026-07-24` 的 `IPERF_LOCS` 并实测 7/7 可达。
  节点数因此从 8 变为 7；所有配置档选中 `speed` 时均使用这 7 个节点，说明已同步。
- **ULA 被当成可用 IPv6**：`hostHasUsableIPv6()` 把 Tailscale 的 `fd7a::/48` 判为公网 IPv6，
  于是每个节点白跑一轮 IPv6 并全部失败。现改为"全球可路由单播地址 + UDP dial 确认路由"。

### 3.3 新增的实网测试（`//go:build live`）

上面三个缺陷有两个是实网测试发现的。默认 `go test` 只跑确定性测试，
真实调用第三方与公共节点的验证放在 `internal/probe/live_test.go`：

```bash
go test -tags=live ./internal/probe/ -run TestLive -v
```

| 测试 | 覆盖 |
| --- | --- |
| `TestLiveIPLookup` | ipapi.is 出口发现 |
| `TestLiveIPQualityProviders` | 11 个 IP 质量数据源逐个真实调用，拿到响应却解析不出字段即失败 |
| `TestLiveCommunityGateway` | `check.place` 四条路的可用性，只记录不判失败 |
| `TestLiveMediaStrongRules` | 4 条强规则在真实页面上的判定 |
| `TestLiveIPerfNodeReachability` | 节点池逐个 TCP 可达性 |

策略是"个别源失败只记录、全部失败才判失败"——第三方限流、改版、地区封锁都会发生，
让它们阻塞每次主分支检查只会训练所有人忽略红灯。CI 用 `schedule`（每日）与
`workflow_dispatch` 运行 `live` job，不挂在普通 push 检查上。

### 3.4 仍然需要真实海外 VPS（网络类结论一律不可信）

**开发机虽然出口 IP 是 DigitalOcean，但它在家用 NAT 后面，经网关透明代理落地。**
因此本机的 `latency`、`speed`、`route`、`backtrace`、`media` 与 `network` 地理判断
全部不具代表性——实测中 6 个回程目标 20/20 跳无响应，正是代理吃掉了 NextTrace 探测包。
需要真正的海外 VPS（美西、日本、香港各一台为佳）重跑并校准：

1. **三网回程**：特征表是否命中，并校准 `backtraceMaxHops = 20` 是否足够、
   `backtraceConcurrency = 2` 是否仍被限速；CN2 GIA/GT 的启发式（59.43 与 202.97 先后）
   目前只是"推测"标注，需要真实 GIA 与 GT 机器各一台确认，否则考虑降级为只报 "CN2"。
2. **cgroup 感知**：在 LXC、Docker、限核 KVM 上验证配额读取与线程数计算。
   OpenVZ 的 `/proc/meminfo` 行为需单独确认。
3. **steal**：找一台已知超售的机器验证累计值与告警阈值（当前 5%）是否合理。
   开发机是裸机，steal 恒为 0，覆盖不到告警路径。
4. **fio 引擎回退链**：开发机命中 io_uring，`libaio` 与 `psync` 两条分支尚未在真实机器上
   走到。需要在精简发行版（Alpine、无 libaio 的 Debian slim）上确认。
5. **busybox ping**：三段回退目前只有单元测试覆盖，需要在 Alpine 上跑一次 `latency`。
6. **iperf3 公网吞吐**：节点清单已修正并实测 7/7 可达，公网 TCP 吞吐也在本机跑通
   （IPv4 三节点有真实数据）。但本机经代理出网，吞吐数字不代表任何真实 VPS 的带宽，
   仍需在海外 VPS 上复跑。
7. **流媒体规则**：33 条规则在解锁/不解锁地区各跑一次，核对强规则判定；
   弱规则的"公开页 2xx"目前只能说明可达性，需逐步升级为强规则。
8. **`check.place` 被拦范围**：已确认不是全局下线（Azure 出口可用、DigitalOcean 出口 403），
   还需要更多 VPS 厂商的出口样本来判断被拦范围，见 P2 第一条。

---

## 三·五、新增：NAT 类型检测（对比同类项目后补齐的功能缺口）

2026-08-01 逐个抓取 bench.sh / YABS / superbench / nench / spiritLHLS / oneclickvirt
的源码做功能对照（表见 `docs/research.md` 末尾），唯一的实质缺口是 **NAT 类型检测**——
只有 oneclickvirt 与 spiritLHLS 具备，两者都调用 `oneclickvirt/gostun`。

已按 RFC 5389/5780 自行实现，零第三方依赖：

- `internal/probe/stun.go`：STUN Binding 协议编解码，只实现行为发现所需部分，
  不做 TURN/ICE/认证；事务 ID 与 magic cookie 都校验，UDP 上任何人都能发包。
- `internal/probe/nat.go`：映射行为（RFC 5780 §4.3）与过滤行为（§4.4）检测，
  折算成社区习惯的 NAT1–NAT4，证据不足时报"未判定"而不是凑一个等级。

**关键实现差异**：多数公共 STUN 服务器禁用或**忽略** CHANGE-REQUEST。实测
`stun.l.google.com` 与 `stun.cloudflare.com` 收到 CHANGE-REQUEST 后照常从原地址回包，
只看"有没有回包"会把对称型 NAT 后的机器误报成全锥型 NAT1。因此判定过滤行为时
必须核对响应的**源地址**，核不上就记为"服务器忽略了该属性"并报未知。
这个缺陷是实网测试 `TestLiveSTUNServers` 抓到的，已加确定性测试
`TestProbeNATRejectsIgnoredChangeRequest` 锁住。

STUN 清单同样照抄实测结果（各探测三次确认稳定）：`stun.miwifi.com`、`stun.1und1.de`、
`stun.hoiio.com` 提供稳定的双 IP 备用地址；`stun.schlund.de` 与 `stun.gmx.net` 因
DNS 轮询会落到备用地址等于自身的那台，已剔除。

同时给 `system` 补上几乎所有竞品都采集、ecs 却缺的两项：**CPU 缓存**与
**VT-x/AMD-V**（决定能否跑嵌套虚拟化）。

仍需真实 VPS 验证：本机在家用 NAT 后，测得对称型 NAT4 属预期结果，
但**没有覆盖到公网直连（无 NAT）与各类锥形 NAT 的判定路径**——
独立 IP 的 VPS 应当报"公网直连（无 NAT）"，NAT 小鸡应当报锥形或对称型，
这两条路径目前只有单元测试覆盖。

---

## 四、P2：未完成的功能

- **社区中转按出口被拦**：`ipinfo.check.place` 从一个 DigitalOcean 出口访问全部 403，
  但同期从 GitHub Actions（Azure AS8075）出口访问四条路全可用、11 个数据源 0 失败。
  它没有下线，是部分数据中心 IP 段被 Cloudflare 拦。代码行为在两种情况下都正确。
  待办：收集更多出口样本（尤其常见 VPS 厂商），判断被拦范围有多广；
  如果主流 VPS 出口普遍被拦，就要把这四家标为"实际需要密钥"，
  并优先推进下面的离线 GeoIP 以替代 MaxMind 那一路。
- **CN2 GIA/GT 精确区分**：需要维护入口段表，当前只做位置启发式。
- **流媒体规则外置**：拆成带版本号和过期时间的独立文件，支持不重新编译更新。
- **弱规则升级**：Disney+、HBO Max 等 20+ 平台目前只测可达性。
- **离线 GeoIP**：MaxMind/DB-IP/IP2Location 本地库，降低对在线 API 的依赖。
- **UnixBench/Phoronix**：可选长测套件。
- **回归样本库**：KVM、LXC、OpenVZ、低内存 NAT VPS 的基线数据。
- **发布**：v0.6.0 只从 `main` 的已验证提交打包；发布顺序是提交、推送 `main`、确认远端提交、创建并推送 `v0.6.0` tag。工具包由 CI 的真实七架构 stage 生成，Ookla 保持独立，不进入 `ecs-tools`。

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
apt-get install -y sysbench fio iperf3
# zstd、NPB、OpenSSL、STREAM、NextTrace Tiny 和 iputils ping 由 ecs-tools 按架构临时提供；
# zstd 选中时，固定 corpus 由 run.sh 从独立 Release 资产临时提供
# ./install.sh --with-benchmarks 只持久安装上面的三个系统包
```

`.gitignore` 已排除 `bin/`、`dist/`、`reports/`，构建产物不会入库。

## 六、开发约束（摘自 CONTRIBUTING.md，以该文件为准）

1. 性能成绩只能来自 sysbench / zstd / NASA NPB EP+FT / 官方 STREAM / OpenSSL speed / fio / iperf3，**不得引入自研工作负载或替代分数**；
2. 不做跨供应商平均、跨节点均值、综合跑分；
3. 反爬、超时、限流一律返回"未知"，不得判成"不解锁"；
4. 外部程序只作可关闭的适配器，记录版本与 SHA-256，参数以数组传入不经过 shell；
5. 解析、遮盖、报告、协议逻辑要有**不依赖公网的确定性测试**——目的是让默认
   `go test` 快、稳、可重复，**不是**禁止联网测试。别再拿这条当不做真实调用的理由，
   本项目自己犯过这个错；
6. 外部工具的适配器测试必须调用**真实工具**，不得用脚本替身冒充 fio、sysbench、
   iperf3、ping 或 NextTrace，需要隔离时用回环；
7. 依赖第三方服务或公共节点的能力，除确定性测试外**还必须有 `//go:build live`
   的实网测试**。固定样本只能证明解析器认得样本格式，证明不了上游没变；
   实网测试不挂在普通主分支检查上，由定时任务与手动触发运行；
8. 工作负载语义变化时升级 `measurement.method` 的版本号；
9. 数据源清单与节点地址一律照抄上游并注明版本，不得凭记忆填写；
10. 不加入广告、推广、遥测或默认上传；
11. **只面向 Linux**：不引入 `runtime.GOOS` 分支或其他系统的发布目标，测试断言真实 Linux 行为。

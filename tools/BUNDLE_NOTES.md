# Third-party Tool Package 发布说明 / Bundle release notes

本文件是 Bundle 工具包的发布说明来源（bundle 自己的位置）：`tools/BUNDLE`
每递增一个版本，在此追加对应的 `## bundle-vN` 章节并同步维护双语小节。
`scripts/release/publish.sh --kind bundle` 从最新章节读取发布说明。

This file is the source of bundle release notes (bundle's own place): bump
`tools/BUNDLE`, append a matching `## bundle-vN` section with matching
bilingual subsections, and `scripts/release/publish.sh --kind bundle` reads
the newest section.

## bundle-v1.7

### 中文

- Windows Server 2022+ x64 的 `windows_amd64` Bundle 从六工具扩展为七工具：`zstd`、NPB EP、NPB FT、OpenSSL、STREAM、`fio` 和官方 NextTrace Tiny v1.7.1 预编译资产 `nexttrace-tiny_windows_amd64.exe`。
- NextTrace 资产固定 SHA-256 为 `16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b`；构建阶段验证来源和摘要，打包资产与已验证输入逐字节一致，manifest、许可证文件与现有六项 benchmark contract 一并保留。
- Windows Server 2022 与 2025 的最终 gate 通过生产 `ecs.exe` 路径运行 route/backtrace：IPv4 两个 runner 都必须真实执行，存在 global IPv6/default route 时执行 IPv6，否则明确报告 capability missing；结果必须证明 canonical args、`nexttrace-json-v1` adapter、target/family/hops 和 NextTrace provenance。该网络 gate 不在 package-only gate 中联网，也不改变 FreeBSD 的 base-system traceroute 合同。

### English

- The Windows Server 2022+ x64 `windows_amd64` Bundle grows from six to seven tools: `zstd`, NPB EP, NPB FT, OpenSSL, STREAM, `fio`, and the official NextTrace Tiny v1.7.1 prebuilt asset `nexttrace-tiny_windows_amd64.exe`.
- The NextTrace asset is pinned to SHA-256 `16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b`; the build verifies its source and digest, the packaged asset is byte-identical to the verified input, and the manifest, license files, and existing six benchmark contracts remain intact.
- The final Windows Server 2022 and 2025 gates run route/backtrace through the production `ecs.exe` path: both runners must execute a genuine IPv4 gate, and IPv6 runs when a global IPv6 address/default route exists; otherwise the missing capability is reported explicitly. Results must prove canonical args, the `nexttrace-json-v1` adapter, target/family/hops, and NextTrace provenance. The network gate is not run by the package-only gate and does not change the FreeBSD base-system traceroute contract.

## bundle-v1.6

### 中文

- 新增 Windows Server 2022+ x64 的 `windows_amd64` 工具 Bundle contract，资产格式为 `ecs-tools_windows_amd64.zip`；主程序对应的 Windows ZIP asset 为 `ecs_windows_amd64.zip`。
- Windows frozen benchmark set 只包含 `zstd`、NPB EP、NPB FT、OpenSSL、STREAM 和 `fio`；Windows gate 要求 fio 的 `windowsaio` engine，并保留每个工具的 manifest、PE/DLL allowlist 和上游许可证文件。
- Windows 的系统与 ICMP 事实由主程序的 native Win32 probes 提供，不在工具 Bundle 中增加 `ping.exe`；NextTrace 不进入 Bundle，Windows route/backtrace 仍 unsupported。Windows Server 2022 x64 与 Windows Server 2025 x64 的 GitHub Actions 真实 gates（包括 native runtime、工具验证、打包 workloads 与当前 ZIP/bootstrap E2E）已通过彩排。

### English

- Added the Windows Server 2022+ x64 `windows_amd64` tool Bundle contract, with `ecs-tools_windows_amd64.zip` as its asset; the corresponding Windows main-program ZIP asset is `ecs_windows_amd64.zip`.
- The Windows frozen benchmark set contains only `zstd`, NPB EP, NPB FT, OpenSSL, STREAM and `fio`; the Windows gate requires fio's `windowsaio` engine and retains each tool's manifest, PE/DLL allowlist and upstream license files.
- Windows system and ICMP facts come from the main program's native Win32 probes, so the tool Bundle adds no `ping.exe`; NextTrace is not in the Bundle, and Windows route/backtrace remain unsupported. The Windows Server 2022 x64 and Windows Server 2025 x64 GitHub Actions genuine gates—including native runtime, tool verification, packaged workloads and current ZIP/bootstrap E2E—have passed rehearsal.

## bundle-v1.5

### 中文

- 修复 FreeBSD C/GNU 构建器在复用相对 sysroot、目标依赖和 SDK 路径时，进入上游源码子目录后路径失效的问题；两架构构建现在统一解析为绝对路径。
- 修复 FreeBSD 15.1 VM prepare 对 `pkg add` 错误传入 `-y` 的问题，继续保留精确 package lock、SHA256 校验、4 vCPU guest、真实 REAL GATE 和 ARTIFACT E2E。
- Bundle 继续消费 immutable `ci-freebsd-gnu-sdk-v1.3`，保持双架构、8 个工具、manifest、运行参数和发布产物语义不变；本版本完整彩排已通过。

### English

- Fixed FreeBSD C/GNU builders losing reused relative sysroot, target-dependency, and SDK paths after entering upstream source directories; both architectures now resolve these paths to absolute paths consistently.
- Fixed FreeBSD 15.1 VM preparation passing the unsupported `-y` flag to `pkg add`, while retaining the exact package lock, SHA256 checks, 4-vCPU guests, genuine REAL GATE, and ARTIFACT E2E.
- Bundle continues to consume the immutable `ci-freebsd-gnu-sdk-v1.3` snapshot with the same two architectures, eight tools, manifest, runtime arguments, and release-artifact semantics; the complete rehearsal passed.

## bundle-v1.4

### 中文

- FreeBSD 工具包继续使用双架构、8 个工具和完整真实客户机门禁；本版本将 REAL GATE 与 PACKAGE/E2E 解耦并行，ASSEMBLE 仍等待两条路径全部通过。
- C/GNU 构建在同一 job/work-dir 内复用已下载并校验的 sysroot、目标依赖和 GNU SDK；Bundle lock 更新为 immutable `ci-freebsd-gnu-sdk-v1.3`，并固定其双架构 SHA256。
- FreeBSD 15.1 consumer VM 固定 4 vCPU，启用可验证的 prepare cache 和精确 package lock；预压缩中转使用零压缩，不改变工具、manifest、运行参数或发布字节语义。

### English

- The FreeBSD tool package keeps its two architectures, eight tools, and complete genuine-client gates; this release separates REAL GATE from PACKAGE/E2E so they run in parallel, while ASSEMBLE still waits for both paths.
- The C/GNU builds reuse downloaded and verified sysroot, target dependencies, and GNU SDK inputs within one job/work directory; the Bundle lock now consumes the immutable `ci-freebsd-gnu-sdk-v1.3` snapshot pinned by both architecture SHA256 values.
- FreeBSD 15.1 consumer VMs are fixed at 4 vCPUs with a verifiable prepare cache and exact package lock; zero compression is used for pre-compressed transit without changing the tools, manifest, runtime arguments, or release-byte semantics.

## bundle-v1.3

### 中文

- FreeBSD 工具包继续使用原有双架构、8 个工具和真实 FreeBSD 门禁；本版本收口私有构建 helper 的参数与调用链，不改变工具集合、运行参数或 manifest 语义。
- GNU SDK 发布器拆分为编排层与私有 publisher/verifier，保留 `share/man`/`share/info` 精确裁剪、host ELF debug strip、非 host 字节不变、硬链接组不变、driver-chain 证明，以及 NPB EP/FT + STREAM consumer gate；上述门禁已在 `ci-freebsd-gnu-sdk-v1.2` 双架构快照上通过。
- Bundle 发布链改为消费 lock 钉定的 `ci-freebsd-gnu-sdk-v1.2` immutable SDK 快照；工具依赖、双架构并行拓扑和真实客户机验收合同不变。

### English

- The FreeBSD tool package keeps the existing two architectures, eight tools and genuine FreeBSD gates; this release closes the private build-helper argument and call-chain cleanup without changing the tool set, runtime arguments or manifest semantics.
- The GNU SDK publisher is split into orchestration and private publisher/verifier layers while retaining exact `share/man`/`share/info` slimming, host-ELF debug stripping, non-host byte identity, hardlink-group identity, driver-chain proof, and the NPB EP/FT plus STREAM consumer gate; all of these gates passed on the dual-architecture `ci-freebsd-gnu-sdk-v1.2` snapshot.
- The Bundle release path now consumes the lock-pinned `ci-freebsd-gnu-sdk-v1.2` immutable SDK snapshot; tool dependencies, dual-architecture parallel topology and genuine-client acceptance contract are unchanged.

## bundle-v1.2

### 中文

- FreeBSD 工具发布物全量 strip：8 个工具 × 双架构在 provenance 计算前统一 `--strip-unneeded`，strip 前后全部 SHF_ALLOC runtime section（name/flags/size/address/内容 SHA256）逐字节等价，`.debug_*` 字节清零，manifest `stripped: true` 与实际字节一致。
- 工具包体 22 MiB → 6.1 MiB（`freebsd_amd64`）/ 5.6 MiB（`freebsd_arm64`）；工具集合仍为 sysbench、zstd、npb-ep、npb-ft、openssl、stream、fio、iperf3 共 8 个静态 FreeBSD ELF，编译器、构建参数、manifest 与真实 FreeBSD 15.1 门禁合同不变。

### English

- The FreeBSD release tools are fully stripped: all eight tools on both architectures are uniformly passed through `--strip-unneeded` before provenance is computed, every SHF_ALLOC runtime section (name/flags/size/address/content SHA256) is proven byte-for-byte identical across the strip, `.debug_*` bytes drop to zero, and the manifest `stripped: true` matches the actual bytes.
- The tool package shrinks from 22 MiB to 6.1 MiB (`freebsd_amd64`) and 5.6 MiB (`freebsd_arm64`); the tool set remains the same eight static FreeBSD ELFs — sysbench, zstd, npb-ep, npb-ft, openssl, stream, fio and iperf3 — with the compiler, build flags, manifest and real FreeBSD 15.1 gate contracts unchanged.

## bundle-v1.1

### 中文

- 工具包新增 `freebsd_amd64` 与 `freebsd_arm64` 双架构，各含 sysbench、zstd、npb-ep、npb-ft、openssl、stream、fio、iperf3 共 8 个静态 FreeBSD ELF 工具，不含 `ping` 与 `nexttrace-tiny`。
- FreeBSD 工具全部在 ubuntu-24.04 交叉构建：Clang/LLD 构建 C 工具，源码构建 GNU 14.2.0 C/Fortran/OpenMP SDK（Binutils 2.43.1，无 g++/libstdc++）构建 NPB 3.4.4 EP/FT 与 STREAM；FreeBSD 15.1 sysroot 与 GNU SDK 均以 immutable 快照（lock 钉 URL+SHA256）直接消费。
- 打包前每架构必须通过真实 FreeBSD 15.1 客户机内的 8/8 运行门禁（含 NPB `Verification = SUCCESSFUL`、STREAM `Solution Validates`、fio posixaio QD32/QD64 有效队列深度），`bundle-release` 的 assemble 依赖该门禁。
- 工具包带每工具 manifest（compiler_family/compiler_version/target_triple/build_host/openmp_runtime），发布物经真实 FreeBSD 客户机内的发布物级 E2E 验收后才会发布；校验收敛为按包 SHA256。
- CI 重构：双架构独立并行链抽取为 reusable workflow，全仓零 `actions/cache`。

### English

- The tool package gains `freebsd_amd64` and `freebsd_arm64` targets, each shipping eight static FreeBSD ELF tools — sysbench, zstd, npb-ep, npb-ft, openssl, stream, fio and iperf3 — with no `ping` or `nexttrace-tiny`.
- All FreeBSD tools are cross-built on ubuntu-24.04: Clang/LLD builds the C tools, while a from-source GNU 14.2.0 C/Fortran/OpenMP SDK (Binutils 2.43.1, no g++/libstdc++) builds NPB 3.4.4 EP/FT and STREAM. The FreeBSD 15.1 sysroot and the GNU SDK are consumed directly as immutable snapshots pinned by URL+SHA256 in their locks.
- Before packaging, each architecture must pass the 8/8 runtime gate inside a genuine FreeBSD 15.1 guest (including NPB `Verification = SUCCESSFUL`, STREAM `Solution Validates`, and fio posixaio QD32/QD64 effective queue depth); `bundle-release` assemble depends on that gate.
- The package carries a per-tool manifest (compiler_family/compiler_version/target_triple/build_host/openmp_runtime); releases run an artifact-level E2E inside a genuine FreeBSD guest, and verification is consolidated to package-level SHA256.
- CI refactor: the two per-architecture parallel chains were extracted into a reusable workflow, and the repository now has zero `actions/cache`.

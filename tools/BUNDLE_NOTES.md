# Third-party Tool Package 发布说明 / Bundle release notes

本文件是 Bundle 工具包的发布说明来源（bundle 自己的位置）：`tools/BUNDLE`
每递增一个版本，在此追加对应的 `## bundle-vN` 章节并同步维护双语小节。
`scripts/release/publish.sh --kind bundle` 从最新章节读取发布说明。

This file is the source of bundle release notes (bundle's own place): bump
`tools/BUNDLE`, append a matching `## bundle-vN` section with matching
bilingual subsections, and `scripts/release/publish.sh --kind bundle` reads
the newest section.

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

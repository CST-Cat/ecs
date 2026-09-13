# Third-party Tool Package 发布说明 / Bundle release notes

本文件是 Bundle 工具包的发布说明来源（bundle 自己的位置）：`tools/BUNDLE`
每递增一个版本，在此追加对应的 `## bundle-vN` 章节并同步维护双语小节。
`scripts/release/publish.sh --kind bundle` 从最新章节读取发布说明。

This file is the source of bundle release notes (bundle's own place): bump
`tools/BUNDLE`, append a matching `## bundle-vN` section with matching
bilingual subsections, and `scripts/release/publish.sh --kind bundle` reads
the newest section.

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

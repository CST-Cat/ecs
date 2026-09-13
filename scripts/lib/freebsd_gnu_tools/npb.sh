#!/usr/bin/env bash
# FreeBSD GNU/OpenMP build of NPB 3.4.4 EP + FT Class A (Stage 5).
#
# 来源是 NASA 官方分发渠道，URL 与 SHA256 已通过代理下载两次交叉验证。
# 禁止修改 NPB source：config/make.def 按 NPB 自带 config/make.def.template
# 的官方结构生成（README.install 认可的配置机制），npbparams.h 由包内
# sys/setparams 在宿主机上生成（sys/Makefile 用 UCC=宿主 gcc 编译并运行
# setparams），两者都只是生成配置文件，不动任何源文件。
# 2026-09-13 起 NASA 渠道持续 HTTP 500，下载允许临时回退到逐文件校验过的
# GitHub 镜像（见 ecs_freebsd_gnu_npb_fetch_mirror 的 TEMPORARY 注释）；
# 官方 tarball 钉值 ECS_NPB_SHA256 不变，镜像消费集以它为对应基准。

ECS_NPB_VERSION=3.4.4
ECS_NPB_URL="https://www.nas.nasa.gov/assets/npb/NPB3.4.4.tar.gz"
ECS_NPB_SHA256="1ae219398e02a0a79ad51b7460fcffbf7b5df83a69d5d3d3a9dc2d8acf523549"
ECS_NPB_CLASS=A
ECS_NPB_RAND=randi8
ECS_NPB_FFLAGS="-O3 -fopenmp"
ECS_NPB_FLINKFLAGS="-O3 -fopenmp -static"

# ecs_freebsd_gnu_npb_download OUTPUT 下载并校验官方 NPB 3.4.4 tarball。
#
# 官方渠道只试 1 次且限时 60s：curl --retry 对 5xx 做指数退避，NASA 持续
# 500 时曾把 CI 烧掉 35 分钟（run 34757858241），不再允许。下载失败返回 1，
# 由调用方决定是否走 TEMPORARY 镜像回退；SHA-256 与钉值不匹配是官方渠道
# 异常（不是网络失败），直接终止构建，不得静默换源。
ecs_freebsd_gnu_npb_download() {
  local output=$1 actual
  mkdir -p "$(dirname "$output")"

  echo "npb: official NASA download (single attempt, max-time 60s)" >&2
  if ! curl -fsSL --connect-timeout 30 --max-time 60 \
    "$ECS_NPB_URL" -o "$output"; then
    rm -f -- "$output"
    return 1
  fi
  actual=$(sha256sum "$output" | awk '{print $1}')
  if [[ "$actual" != "$ECS_NPB_SHA256" ]]; then
    rm -f -- "$output"
    ecs_freebsd_gnu_die \
      "npb: official NPB tarball SHA-256 mismatch: expected $ECS_NPB_SHA256, got $actual"
  fi
}

# TEMPORARY（NASA 500 期间回退，NASA 恢复后整段移除）
#
# NASA 官方下载渠道自 2026-09-13 起持续 HTTP 500（CI run 34757858241 两个
# 架构 job 各 3 次尝试全部 curl 22/500 或 52/empty reply），经用户批准临时
# 改用 GitHub 镜像：
#   https://github.com/ndkimdavy/architecture-opt-hpc-NPBMG
#   子树 NPBx/OFFICIAL-NASA/NPB3.4.4 == 官方 tarball 的顶层 NPB3.4.4/ 目录。
# 官方 tarball（Wayback 存档取得，sha256 == 上方 ECS_NPB_SHA256 钉值
# 1ae219398e02a0a79ad51b7460fcffbf7b5df83a69d5d3d3a9dc2d8acf523549）与该
# 子树的本消费集（下方 40 文件 = NPB3.4-OMP/{EP,FT,common,config}，已排除
# 生成物 config/make.def）逐文件逐字节一致；消费集 manifest 摘要
# 4e9da92676707f80435d39bfec4394560f3e1fdbfecaf7603252bf01fae661a1，对应
# 关系记录在 VALIDATION.md「阶段 2–5 合并批次」节。
# 不在 manifest 内因此不校验不使用的文件：
#   - NPB3.4-OMP/MG/mg.f90：镜像里只有缩进重排差异，本构建只编译 OMP
#     EP/FT，MG 不参与；
#   - 4 个 setparams 生成残留（NPB3.4-MPI/common/mpinpb.h、
#     NPB3.4-MPI/config/make.def、NPB3.4-MPI/MG/mpinpb.f90、
#     NPB3.4-OMP/config/make.def）：npb.sh 本就按 make.def.template 重写
#     config/make.def，MPI 树与生成物不进入 OMP EP/FT 构建。
# 使用前仍逐文件 sha256 校验消费集；任何缺失或不匹配都终止构建。
ecs_freebsd_gnu_npb_mirror_manifest() {
  cat <<'ECS_NPB_MIRROR_MANIFEST'
4f2000e6463615b1ede2d2fefff64ec6d237980d6d7a4a36ff09beb0bfa4ab19  ./NPB3.4-OMP/common/c_print_results.c
7a90448ec48db5bb43f5fb588b68f31cabbcc1a9c073de9da13aebdeea5124ad  ./NPB3.4-OMP/common/c_timers.c
f66736ec04b6c1866ffff6a861c6564914996595b53958095f7d56c311aba334  ./NPB3.4-OMP/common/c_timers.h
6dec3960c094c6272d16e41548020e40b0fe02248ec4764543e31ff83911625c  ./NPB3.4-OMP/common/print_results.f90
363d488bbcbdc33c1e20df698caea21807fb957eb5550d4ad3ff5f3fc632e9fd  ./NPB3.4-OMP/common/randdp.f90
9b9a860eef8e3da905a6221c024b58fed0808c0648c337bad40c5e5f018ac683  ./NPB3.4-OMP/common/randdpvec.f90
3760ce3974cce4f7d781e7385b838d79255869077f3f03a3feced42498d05d2c  ./NPB3.4-OMP/common/randi8.f90
31c8b2cd9f3b36cd53d809e26b394ed1e0bbd82a09f84189e49e8a41f29314dc  ./NPB3.4-OMP/common/randi8_safe.f90
8a30f46181595096b8dcb491e9d6e458d87d4eedf4425bcaaf8f6a134f0c2ad3  ./NPB3.4-OMP/common/timers.f90
18de9969b33629f4f912b48a27de4a05c0f5a72daa7df7849f9b07b19762f057  ./NPB3.4-OMP/common/wtime.c
e2ac895b6d24d98f50b84311d4e491b85adb85fcb9e181ab30e1798852c9e865  ./NPB3.4-OMP/common/wtime.h
a304a2664f7fe1b49a360d783c2cecd8f2fc3ead974d3cfd0c6f19c879345a11  ./NPB3.4-OMP/common/wtime_sgi64.c
7fe63d84c1fb6c472bd952c2845ef8f54fa06e95f72dfd38939f7dc677933dd1  ./NPB3.4-OMP/config/make.def.template
7fe63d84c1fb6c472bd952c2845ef8f54fa06e95f72dfd38939f7dc677933dd1  ./NPB3.4-OMP/config/NAS.samples/make.def_gcc
e1930daf77daf4d04ed3cbb016881c5c3253cbed4dba5f8037afe77e8b4afa6d  ./NPB3.4-OMP/config/NAS.samples/make.def_gcc_m
3419bbb3069783f4400b22913804f477f0117e23f8edac53bfce8b246107d832  ./NPB3.4-OMP/config/NAS.samples/make.def_itc
afb08dcb5b8cc9e823400002299825d65604afa33db06c92bb23ae6222a48158  ./NPB3.4-OMP/config/NAS.samples/make.def_itc_p
9d3fe6c9fac3cd3ae1a4fc653df37c3638160ba455680c09929b1abb7f9e276e  ./NPB3.4-OMP/config/NAS.samples/make.def_pgi
4267b5bc9250a9a12cea12a154c61b26f1a91167a7f3f9cef6a39827e95153b4  ./NPB3.4-OMP/config/NAS.samples/make.def_sun
00b081e3852d1fee4df4fce1816f6e6bb2b5158a97902aee151800faa0466a8b  ./NPB3.4-OMP/config/NAS.samples/README
626bce4ebf7d5dcd9dccede1da953f41fbdec6db9b802427d0929564f7a05e93  ./NPB3.4-OMP/config/NAS.samples/suite.def.bt
3fdec9db30cba0ebe2135d588107c955659e90fca733132f49e0f2a54bfe6261  ./NPB3.4-OMP/config/NAS.samples/suite.def.cg
1e484ab03a2efea87d9daac717ce388b31f2db5e80a227e720e954cbd98b286b  ./NPB3.4-OMP/config/NAS.samples/suite.def.ep
734209802cf5d48767e1913bbe7fcb74c07d9cd8b0501e538e72168c45c16972  ./NPB3.4-OMP/config/NAS.samples/suite.def.ft
7886abc78f916adfcbb820a1176ab26433aac2fdebfb4d3539da0a2298ae1372  ./NPB3.4-OMP/config/NAS.samples/suite.def.is
d4cdb5c7423ae0c7c47d7d388c1934d0be1cf621522560af6862acf4c60d7fa5  ./NPB3.4-OMP/config/NAS.samples/suite.def.lu
39bf0f39835baff99355025c1debc951fbff969b6cd3581f7d44dc62a6cc70b0  ./NPB3.4-OMP/config/NAS.samples/suite.def.mg
1161ff28c2f8c47a46a2b17c080a2c59e33db9130fef28af48bbfa958b624d77  ./NPB3.4-OMP/config/NAS.samples/suite.def.sp
9eae75a20c40dbe14a9e7f1251e672b51aa16f7baf3f49ccf5820d84aa6570e4  ./NPB3.4-OMP/config/suite.def.template
115c2336cec2b994780c2310df7768e96af4cd49297ec106f5e2ad0947fce34f  ./NPB3.4-OMP/EP/ep_data.f90
6f9b8aefcf9e878563e4844e787f9d3e192a06b525e892d27c9e6a7b61887794  ./NPB3.4-OMP/EP/ep.f90
6fc5b5530ef4726b4f0f5cce34a0f6bdb10b1149f02e0cb4c0504fc7bbd92206  ./NPB3.4-OMP/EP/Makefile
feb85392bed79779153735c0c085055549dbfcc51e30d9f762e42361bb55ee69  ./NPB3.4-OMP/EP/README
4957ccf6bb49e6f8106dddf3c4a01e1416d1f0eae4cd4dbfdd237d159ae7c915  ./NPB3.4-OMP/EP/verify.f90
a26820c048cc9ca9227f93b35d46270b2351a9e83b717ac0cee615a52ad23173  ./NPB3.4-OMP/FT/blk_par0.h
2c7f5a6e55d332caa15abc7789182c72db3b13ba688e4b3229781a3f011a4587  ./NPB3.4-OMP/FT/ft_data.f90
5c348d02d190398055daf558dedd46b3fbdd2c6a955699b5b8438774b3fe8d9d  ./NPB3.4-OMP/FT/ft.f90
3c0c9efbc917820bf19d9178abf8ba61b2cacfaa0ca20ec813fdb4b8d1240c76  ./NPB3.4-OMP/FT/inputft.data.sample
6b4b87809eba71980d8526fab2780541f144360b51d3aec39b7d84607b5eb66c  ./NPB3.4-OMP/FT/Makefile
8e1af2a74f57618dbdcefdd9f8da3df927f2c7503a14c66ffbf7b39c55c509a4  ./NPB3.4-OMP/FT/README
ECS_NPB_MIRROR_MANIFEST
}

# ecs_freebsd_gnu_npb_fetch_mirror SRCROOT
#
# sparse clone 镜像仓库（CI 直连 GitHub，无需代理），校验上方消费集
# manifest（40/40）后，把子树内容按官方 tarball 布局放到
# SRCROOT/NPB3.4.4（npb.sh 现有消费逻辑只认这个布局）。
ecs_freebsd_gnu_npb_fetch_mirror() {
  local srcroot=$1 tmp mirror_root target
  command -v git >/dev/null 2>&1 ||
    ecs_freebsd_gnu_die "npb: TEMPORARY mirror fallback requires git"
  target="$srcroot/NPB${ECS_NPB_VERSION}"
  [[ ! -e "$target" ]] ||
    ecs_freebsd_gnu_die "npb: mirror target already exists: $target"
  tmp=$(mktemp -d "$(dirname "$srcroot")/npb-mirror.XXXXXX")
  git clone --depth 1 --filter=blob:none --sparse \
    "https://github.com/ndkimdavy/architecture-opt-hpc-NPBMG.git" \
    "$tmp/repo" 1>&2 ||
    { rm -rf "$tmp"; ecs_freebsd_gnu_die "npb: mirror clone failed"; }
  git -C "$tmp/repo" sparse-checkout set \
    "NPBx/OFFICIAL-NASA/NPB3.4.4" 1>&2 ||
    { rm -rf "$tmp"; ecs_freebsd_gnu_die "npb: mirror sparse-checkout failed"; }
  mirror_root="$tmp/repo/NPBx/OFFICIAL-NASA/NPB3.4.4"
  [[ -d "$mirror_root/NPB3.4-OMP" ]] ||
    { rm -rf "$tmp"; ecs_freebsd_gnu_die "npb: unexpected mirror layout: $mirror_root"; }
  ecs_freebsd_gnu_npb_mirror_manifest >"$tmp/consumed.sha256"
  echo "npb: verifying 40-file mirror consumed set" >&2
  (cd "$mirror_root" && sha256sum -c "$tmp/consumed.sha256") 1>&2 ||
    { rm -rf "$tmp"; ecs_freebsd_gnu_die "npb: mirror consumed-set verification failed"; }
  mv "$mirror_root" "$target"
  rm -rf "$tmp"
  echo "npb: mirror fallback staged verified NPB${ECS_NPB_VERSION} into $srcroot" >&2
}

# ecs_freebsd_gnu_npb_write_make_def MAKEDEF WRAPBIN
#
# 与 config/make.def.template 同构：Fortran 基准（EP/FT）与 common/wtime.c
# 用 wrapper（Stage 4 SDK target 编译器 + FreeBSD sysroot）；setparams 用
# UCC=宿主 gcc。-static 只落在链接旗标上。
ecs_freebsd_gnu_npb_write_make_def() {
  local make_def=$1 wrap_bin=$2
  cat >"$make_def" <<EOF
#---------------------------------------------------------------------------
# SITE- AND/OR PLATFORM-SPECIFIC DEFINITIONS.
#
# Generated by scripts/build_tools_freebsd_gnu.sh following the structure of
# config/make.def.template (the official configuration mechanism documented
# in README.install). npbparams.h is generated by the bundled sys/setparams
# run on the build host. NPB sources themselves are never modified.
#---------------------------------------------------------------------------

#---------------------------------------------------------------------------
# Parallel Fortran (EP, FT): Stage 4 GNU SDK target compiler + FreeBSD sysroot.
#---------------------------------------------------------------------------
FC = ${wrap_bin}/gfortran
FLINK = \$(FC)
F_LIB  =
F_INC =
FFLAGS = ${ECS_NPB_FFLAGS}
FLINKFLAGS = ${ECS_NPB_FLINKFLAGS}

#---------------------------------------------------------------------------
# Parallel C: only common/wtime.c is compiled here (linked into the Fortran
# binaries), so CC must be the target compiler as well.
#---------------------------------------------------------------------------
CC = ${wrap_bin}/gcc
CLINK = \$(CC)
C_LIB  =
C_INC =
CFLAGS = ${ECS_NPB_FFLAGS}
CLINKFLAGS = ${ECS_NPB_FLINKFLAGS}

#---------------------------------------------------------------------------
# Utilities C: setparams runs on the Linux build host and only generates
# config files; it must be compiled with the host compiler, never the target.
#---------------------------------------------------------------------------
UCC	= gcc

BINDIR	= ../bin

RAND   = ${ECS_NPB_RAND}
WTIME  = wtime.c
EOF
}

# ecs_freebsd_gnu_build_npb WORK STAGE WRAPBIN FILE_MACHINE
#
# 只构建 EP 与 FT Class A（Fortran 版）；产物为 static FreeBSD ELF。
ecs_freebsd_gnu_build_npb() {
  local work=$1 stage=$2 wrap_bin=$3 file_machine=$4
  local tarball="$work/src-npb.tar.gz" srcroot="$work/src-npb"
  local src="$srcroot/NPB${ECS_NPB_VERSION}/NPB${ECS_NPB_VERSION%.*}-OMP"

  echo "freebsd-gnu-tools: downloading NPB $ECS_NPB_VERSION" >&2
  mkdir -p "$srcroot"
  if ecs_freebsd_gnu_npb_download "$tarball"; then
    tar -xzf "$tarball" -C "$srcroot"
  else
    # TEMPORARY（NASA 500 期间回退，恢复后移除）：官方渠道不可用时改用
    # GitHub 镜像，消费集 40 文件逐字节 sha256 校验通过才进入构建。
    echo "npb: official download unavailable, falling back to TEMPORARY mirror" >&2
    ecs_freebsd_gnu_npb_fetch_mirror "$srcroot"
  fi
  [[ -d "$src" ]] || ecs_freebsd_gnu_die "unexpected NPB archive layout: $src missing"

  ecs_freebsd_gnu_npb_write_make_def "$src/config/make.def" "$wrap_bin"

  # NPB 的 make 体系不能并行：config target 与 npbparams.h 规则都会调用
  # sys/setparams，而 setparams 用 fopen("w") 原地写 npbparams.h。基准编译
  # 只需数秒，串行 make 是官方用法，也消除并发写同一文件的风险。
  (
    cd "$src/EP" && make CLASS="$ECS_NPB_CLASS"
  )
  cp "$src/bin/ep.$ECS_NPB_CLASS.x" "$stage/bin/npb-ep"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$stage/bin/npb-ep" "$file_machine"
  ecs_freebsd_gnu_assert_libgomp "$stage/bin/npb-ep"

  (
    cd "$src/FT" && make CLASS="$ECS_NPB_CLASS"
  )
  cp "$src/bin/ft.$ECS_NPB_CLASS.x" "$stage/bin/npb-ft"
  ecs_freebsd_gnu_assert_static_freebsd_elf "$stage/bin/npb-ft" "$file_machine"
  ecs_freebsd_gnu_assert_libgomp "$stage/bin/npb-ft"

  echo "NPB $ECS_NPB_VERSION (NAS Parallel Benchmarks), NASA Ames Research Center." \
    >"$stage/LICENSES/npb-ep.LICENSE"
  echo "License terms: see the official distribution at $ECS_NPB_URL." \
    >>"$stage/LICENSES/npb-ep.LICENSE"
  cp "$stage/LICENSES/npb-ep.LICENSE" "$stage/LICENSES/npb-ft.LICENSE"
}

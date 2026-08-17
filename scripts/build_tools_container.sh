#!/usr/bin/env bash
set -euo pipefail

# 在目标架构容器里构建并验证 ecs 的十个基准工具。
#
# 本脚本是"怎么构建"的唯一定义。GitHub Actions 只负责 when / where /
# permission / dependency：它挑选 runner、传一个架构名，其余全部在这里。
# 因此只要 clone 仓库到一台带 Docker 的 Linux 主机，就能按与 CI 完全相同的
# 定义构建工具，不必去读 workflow YAML 反推 apt 包名和交叉三元组。
#
# 三种构建模式：
#
#   native        目标架构有原生 runner（amd64 / arm64），容器直接跑目标架构。
#   native-compat 用 x86-64 的原生 32 位兼容执行（386），仍是原生速度。
#   cross         宿主原生跑交叉编译器，只让短 smoke 显式走 QEMU。整包在
#                 模拟器里编译要几十分钟且不稳定，而 smoke 只需证明产物能在
#                 目标架构上启动并给出可解析输出。
#
# 用法：
#   scripts/build_tools_container.sh --arch ARCH --stage-root DIR
#   scripts/build_tools_container.sh --arch ARCH --print-params
#
# --print-params 打印某架构解析后的全部构建参数而不实际构建，用于秒级比对
# 构建定义是否等价——不需要为了确认一个参数而启动 40-90 分钟的真实构建。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build_tools_container.sh --arch ARCH --stage-root DIR
       scripts/build_tools_container.sh --arch ARCH --print-params

  --arch ARCH        amd64 | arm64 | armv7 | 386 | s390x | riscv64 | ppc64le
  --stage-root DIR   构建产物的落地目录（宿主路径）
  --print-params     只打印解析后的构建参数，不构建
USAGE
}

die() {
  echo "build-tools-container: $*" >&2
  exit 1
}

arch=""
stage_root=""
print_params=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --arch)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--arch requires a value"
      arch=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --print-params)
      print_params=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$arch" ]] || {
  usage
  die "--arch is required"
}

# 架构表。每行：镜像、容器 platform、构建模式、Debian 架构、交叉三元组、smoke 运行器。
#
# ppc64le 用 forky 而不是 trixie：trixie 的 ppc64el 交叉工具链缺少构建 NPB 需要的
# gfortran 组合。其余架构留在 trixie，减少镜像差异带来的不确定性。
case "$arch" in
  amd64)
    image=debian:trixie
    platform=linux/amd64
    build_mode=native
    debian_arch=""
    cross_triplet=""
    target_runner=direct
    ;;
  arm64)
    image=debian:trixie
    platform=linux/arm64
    build_mode=native
    debian_arch=""
    cross_triplet=""
    target_runner=direct
    ;;
  armv7)
    image=debian:trixie
    platform=linux/arm64
    build_mode=cross
    debian_arch=armhf
    cross_triplet=arm-linux-gnueabihf
    target_runner=qemu-arm-static
    ;;
  386)
    image=debian:trixie
    platform=linux/386
    build_mode=native-compat
    debian_arch=""
    cross_triplet=""
    target_runner=direct
    ;;
  s390x)
    image=debian:trixie
    platform=linux/amd64
    build_mode=cross
    debian_arch=s390x
    cross_triplet=s390x-linux-gnu
    target_runner=qemu-s390x-static
    ;;
  riscv64)
    image=debian:trixie
    platform=linux/amd64
    build_mode=cross
    debian_arch=riscv64
    cross_triplet=riscv64-linux-gnu
    target_runner=qemu-riscv64-static
    ;;
  ppc64le)
    image=debian:forky
    platform=linux/amd64
    build_mode=cross
    debian_arch=ppc64el
    cross_triplet=powerpc64le-linux-gnu
    target_runner=qemu-ppc64le
    ;;
  *)
    die "unsupported architecture: $arch (supported: ${ECS_ARCHES[*]})"
    ;;
esac

# 原生构建的 apt 依赖。libck / libluajit 是 sysbench 的依赖，gfortran 是 NPB 的。
native_packages=(
  autoconf automake binutils build-essential ca-certificates curl
  gfortran git jq libaio-dev libck-dev libluajit-5.1-dev libtool libtool-bin
  meson ninja-build perl pkg-config unzip zlib1g-dev
)

# 交叉构建的 apt 依赖：宿主架构的构建工具 + 目标架构的开发库 + QEMU smoke 运行器。
cross_packages=(
  autoconf automake binutils build-essential ca-certificates curl git jq
  libtool libtool-bin meson ninja-build perl pkg-config qemu-user-static unzip
)
cross_target_packages=(
  "binutils-CROSS_TRIPLET" "gcc-CROSS_TRIPLET" "g++-CROSS_TRIPLET" "gfortran-CROSS_TRIPLET"
  "libaio-dev:DEBIAN_ARCH" "libck-dev:DEBIAN_ARCH"
  "libluajit-5.1-dev:DEBIAN_ARCH" "zlib1g-dev:DEBIAN_ARCH"
)

expand_cross_target_packages() {
  local package
  for package in "${cross_target_packages[@]}"; do
    package=${package//CROSS_TRIPLET/$cross_triplet}
    package=${package//DEBIAN_ARCH/$debian_arch}
    printf '%s\n' "$package"
  done
}

if [[ "$print_params" -eq 1 ]]; then
  printf 'arch=%s\n' "$arch"
  printf 'image=%s\n' "$image"
  printf 'platform=%s\n' "$platform"
  printf 'build_mode=%s\n' "$build_mode"
  printf 'debian_arch=%s\n' "$debian_arch"
  printf 'cross_triplet=%s\n' "$cross_triplet"
  printf 'target_runner=%s\n' "$target_runner"
  if [[ "$build_mode" == cross ]]; then
    printf 'toolchain_mode=cross\n'
    printf 'npb_ci_smoke_class=S\n'
    printf 'packages=%s\n' "${cross_packages[*]} $(expand_cross_target_packages | tr '\n' ' ')"
  else
    printf 'toolchain_mode=native\n'
    printf 'npb_ci_smoke_class=A\n'
    printf 'packages=%s\n' "${native_packages[*]}"
  fi
  exit 0
fi

[[ -n "$stage_root" ]] || {
  usage
  die "--stage-root is required unless --print-params is given"
}
command -v docker >/dev/null 2>&1 || die "docker is required"
mkdir -p "$stage_root"
stage_root=$(cd "$stage_root" && pwd)

# GITHUB_TOKEN 只用于抬高 GitHub 上游下载的匿名限流额度，缺失时构建照常进行。
docker_env=(--env ARCH --env DEBIAN_FRONTEND=noninteractive)
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  docker_env+=(--env GITHUB_TOKEN)
fi

# seccomp=unconfined：交叉工具链与 QEMU user-mode 会用到默认 profile 拦截的
# 系统调用（personality、部分 mmap 组合）。容器里只跑本仓库定义的构建。
docker_common=(
  docker run --rm
  --platform "$platform"
  --security-opt seccomp=unconfined
  --mount "type=bind,source=${ECS_REPO_ROOT},target=/src"
  --mount "type=bind,source=${stage_root},target=/stage"
  --workdir /src
)

echo "build-tools-container: arch=$arch mode=$build_mode image=$image platform=$platform" >&2

if [[ "$build_mode" == cross ]]; then
  mapfile -t expanded_target_packages < <(expand_cross_target_packages)
  ARCH="$arch" \
    CROSS_TRIPLET="$cross_triplet" \
    DEBIAN_ARCH="$debian_arch" \
    TARGET_RUNNER="$target_runner" \
    CROSS_PACKAGES="${cross_packages[*]} ${expanded_target_packages[*]}" \
    "${docker_common[@]}" \
    "${docker_env[@]}" \
    --env CROSS_TRIPLET --env DEBIAN_ARCH --env TARGET_RUNNER --env CROSS_PACKAGES \
    "$image" \
    bash -euo pipefail -c '
      dpkg --add-architecture "$DEBIAN_ARCH"
      apt-get update
      # shellcheck disable=SC2086
      apt-get install -y --no-install-recommends $CROSS_PACKAGES
      # 交叉编译必须只看目标架构的 pkg-config 数据，否则会链上宿主架构的库。
      export PKG_CONFIG_PATH=
      export PKG_CONFIG_LIBDIR="/usr/lib/$CROSS_TRIPLET/pkgconfig:/usr/share/pkgconfig"
      # STREAM 的多线程 smoke 在 QEMU 下按核数起线程会拖到分钟级，固定两条足够验证。
      export STREAM_NT_THREADS=2
      scripts/build_tools.sh \
        --arch "$ARCH" \
        --stage-root /stage \
        --cross-prefix "${CROSS_TRIPLET}-" \
        --target-runner "$TARGET_RUNNER"
    '
else
  ARCH="$arch" \
    NATIVE_PACKAGES="${native_packages[*]}" \
    "${docker_common[@]}" \
    "${docker_env[@]}" \
    --env NATIVE_PACKAGES \
    "$image" \
    bash -euo pipefail -c '
      apt-get update
      # shellcheck disable=SC2086
      apt-get install -y --no-install-recommends $NATIVE_PACKAGES
      scripts/build_tools.sh --arch "$ARCH" --stage-root /stage
    '
fi

echo "build-tools-container: $arch staged at $stage_root/linux_$arch" >&2

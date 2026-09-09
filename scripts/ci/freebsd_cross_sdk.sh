#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_cross_sdk.sh [all|sysroot|toolchain|probe]

Build and validate the pinned amd64-FreeBSD -> arm64-FreeBSD application SDK.
The compiler itself always runs natively on the amd64 FreeBSD host. QEMU user
mode is used only by the probe to execute finished arm64 FreeBSD binaries.
USAGE
}

die() {
  echo "freebsd-cross-sdk: $*" >&2
  exit 1
}

phase=${1:-all}
case "$phase" in
  all | sysroot | toolchain | probe) ;;
  *) usage; die "unsupported phase: $phase" ;;
esac

[[ "$(uname -s)" == FreeBSD ]] || die 'host must be FreeBSD'
case "$(uname -m)" in
  amd64 | x86_64) ;;
  *) die "host must be FreeBSD/amd64, got $(uname -m)" ;;
esac

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
lock_file="$repo_root/tools/freebsd-cross-sdk.lock.json"
[[ -s "$lock_file" ]] || die "missing SDK lock: $lock_file"

for command_name in cc c++ curl gmake jq sha256 sha512 tar xz; do
  command -v "$command_name" >/dev/null 2>&1 || die "missing host command: $command_name"
done

lock() {
  jq -er "$1" "$lock_file"
}

freebsd_release=$(lock '.freebsd.release')
release_url=$(lock '.freebsd.release_url')
target_triple=$(lock '.freebsd.target_triple')
gcc_version=$(lock '.gcc.version')
gcc_url=$(lock '.gcc.source_url')
gcc_sha512=$(lock '.gcc.source_sha512')
binutils_prefix=$(lock '.binutils.target_prefix')

sdk_root=${ECS_FREEBSD_CROSS_SDK_ROOT:-/tmp/ecs-freebsd-arm64-sdk}
[[ "$sdk_root" = /* && "$sdk_root" != / ]] || die 'SDK root must be an absolute non-root path'
sysroot="$sdk_root/sysroot"
sources="$sdk_root/sources"
build="$sdk_root/build-gcc"
prefix="$sdk_root/toolchain"
manifest="$sources/MANIFEST"
base_archive="$sources/base.txz"
gcc_archive="$sources/gcc-$gcc_version.tar.xz"
gcc_source="$sources/gcc-$gcc_version"

jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || jobs=2

fetch_file() {
  local url=$1 output=$2
  mkdir -p "$(dirname "$output")"
  if [[ ! -s "$output" ]]; then
    curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 "$url" -o "$output"
  fi
}

phase_sysroot() {
  mkdir -p "$sources"
  fetch_file "$release_url/MANIFEST" "$manifest"
  local expected actual
  expected=$(awk -F '\t' '$1 == "base.txz" { print $2; exit }' "$manifest")
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die 'official MANIFEST has no valid base.txz SHA-256'
  fetch_file "$release_url/base.txz" "$base_archive"
  actual=$(sha256 -q "$base_archive")
  [[ "$actual" == "$expected" ]] ||
    die "FreeBSD base.txz SHA-256 mismatch: expected $expected, got $actual"

  rm -rf -- "$sysroot"
  mkdir -p "$sysroot"
  tar -xJf "$base_archive" -C "$sysroot" \
    ./lib ./libexec ./usr/include ./usr/lib ./usr/libdata
  [[ -s "$sysroot/usr/lib/crt1.o" ]] || die 'sysroot omitted crt1.o'
  [[ -s "$sysroot/usr/lib/libc.a" ]] || die 'sysroot omitted libc.a'
  [[ -s "$sysroot/usr/include/sys/param.h" ]] || die 'sysroot omitted system headers'

  cat >"$sdk_root/SYSROOT" <<EOF
release=$freebsd_release
source=$release_url/base.txz
sha256=$expected
target=$target_triple
EOF
  echo "freebsd-cross-sdk: verified $freebsd_release arm64 sysroot ($expected)"
}

phase_toolchain() {
  [[ -s "$sysroot/usr/lib/libc.a" ]] || die 'run sysroot phase first'
  mkdir -p "$sources"
  fetch_file "$gcc_url" "$gcc_archive"
  local actual
  actual=$(sha512 -q "$gcc_archive")
  [[ "$actual" == "$gcc_sha512" ]] ||
    die "GCC source SHA-512 mismatch: expected $gcc_sha512, got $actual"

  local target_as target_ld
  target_as=$(command -v "${binutils_prefix}-as" || true)
  target_ld=$(command -v "${binutils_prefix}-ld" || true)
  [[ -n "$target_as" ]] || die "missing ${binutils_prefix}-as; install aarch64-binutils"
  [[ -n "$target_ld" ]] || die "missing ${binutils_prefix}-ld; install aarch64-binutils"

  if [[ ! -d "$gcc_source" ]]; then
    tar -xJf "$gcc_archive" -C "$sources"
  fi
  [[ -x "$gcc_source/configure" ]] || die 'GCC source extraction is incomplete'

  rm -rf -- "$build" "$prefix"
  mkdir -p "$build" "$prefix"

  local host_triplet
  host_triplet=$(cc -dumpmachine)
  (
    cd "$build"
    CC=cc CXX=c++ "$gcc_source/configure" \
      --build="$host_triplet" \
      --host="$host_triplet" \
      --target="$target_triple" \
      --prefix="$prefix" \
      --with-sysroot="$sysroot" \
      --with-build-sysroot="$sysroot" \
      --with-native-system-header-dir=/usr/include \
      --with-as="$target_as" \
      --with-ld="$target_ld" \
      --with-gmp=/usr/local \
      --with-mpfr=/usr/local \
      --with-mpc=/usr/local \
      --with-system-zlib \
      --without-zstd \
      --without-isl \
      --enable-languages=c,c++,fortran \
      --enable-threads=posix \
      --enable-initfini-array \
      --enable-gnu-indirect-function \
      --disable-bootstrap \
      --disable-multilib \
      --disable-nls \
      --disable-libssp \
      --disable-libsanitizer \
      --disable-libvtv \
      --disable-shared \
      --enable-static

    gmake -j"$jobs" all-gcc
    gmake -j"$jobs" all-target-libgcc
    gmake -j"$jobs" all-target-libstdc++-v3
    gmake -j"$jobs" all-target-libquadmath
    gmake -j"$jobs" all-target-libgfortran
    gmake -j"$jobs" all-target-libgomp

    gmake install-gcc
    gmake install-target-libgcc
    gmake install-target-libstdc++-v3
    gmake install-target-libquadmath
    gmake install-target-libgfortran
    gmake install-target-libgomp
  )

  [[ -x "$prefix/bin/${target_triple}-gcc" ]] || die 'cross gcc was not installed'
  [[ -x "$prefix/bin/${target_triple}-gfortran" ]] || die 'cross gfortran was not installed'
  "$prefix/bin/${target_triple}-gcc" -dumpmachine | grep -Fx "$target_triple" >/dev/null ||
    die 'cross gcc reports the wrong target'
  "$prefix/bin/${target_triple}-gfortran" -dumpmachine | grep -Fx "$target_triple" >/dev/null ||
    die 'cross gfortran reports the wrong target'

  echo "freebsd-cross-sdk: host-native GCC $gcc_version SDK installed at $prefix"
}

run_target() {
  local runner=${ECS_TARGET_RUNNER:-qemu-aarch64-static}
  command -v "$runner" >/dev/null 2>&1 || die "missing target runner: $runner"
  "$runner" "$@"
}

phase_probe() {
  local gcc="$prefix/bin/${target_triple}-gcc"
  local gfortran="$prefix/bin/${target_triple}-gfortran"
  [[ -x "$gcc" && -x "$gfortran" ]] || die 'run toolchain phase first'

  local probe_dir="$sdk_root/probe"
  rm -rf -- "$probe_dir"
  mkdir -p "$probe_dir"

  cat >"$probe_dir/c.c" <<'EOF'
#include <stdio.h>
int main(void) {
  puts("ecs-freebsd-arm64-c-ok");
  return 0;
}
EOF
  "$gcc" -O2 -static "$probe_dir/c.c" -o "$probe_dir/c"
  file "$probe_dir/c"
  run_target "$probe_dir/c" | grep -Fx 'ecs-freebsd-arm64-c-ok' >/dev/null ||
    die 'arm64 FreeBSD C target probe failed'

  cat >"$probe_dir/fortran.f90" <<'EOF'
program ecs_freebsd_arm64_fortran_probe
  use, intrinsic :: ieee_arithmetic, only : ieee_is_nan
  use omp_lib
  implicit none
  integer :: count
  real(kind=kind(0.0d0)) :: value

  count = 0
  value = 0.0d0
!$omp parallel reduction(+:count)
  count = count + 1
!$omp end parallel

  if (count < 1) error stop 1
  if (ieee_is_nan(value)) error stop 2
  print '(A)', 'ecs-freebsd-arm64-fortran-openmp-ieee-ok'
end program ecs_freebsd_arm64_fortran_probe
EOF
  "$gfortran" -O2 -fopenmp -static "$probe_dir/fortran.f90" -o "$probe_dir/fortran"
  file "$probe_dir/fortran"
  OMP_NUM_THREADS=2 run_target "$probe_dir/fortran" |
    grep -Fx 'ecs-freebsd-arm64-fortran-openmp-ieee-ok' >/dev/null ||
    die 'arm64 FreeBSD Fortran/OpenMP/IEEE target probe failed'

  echo 'freebsd-cross-sdk: C + Fortran + OpenMP + ieee_arithmetic probes passed'
}

case "$phase" in
  all)
    rm -rf -- "$sdk_root"
    phase_sysroot
    phase_toolchain
    phase_probe
    ;;
  sysroot) phase_sysroot ;;
  toolchain) phase_toolchain ;;
  probe) phase_probe ;;
esac

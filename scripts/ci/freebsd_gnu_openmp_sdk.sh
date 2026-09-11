#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD GNU C/Fortran/OpenMP SDK (Stage 4).
#
# Builds Binutils 2.43.1 and GCC 14.2.0 (c,fortran only) targeting the
# FreeBSD 15.1 sysroot. Does not build NPB or STREAM.
#
# Probes: C/Fortran static hello, ieee_arithmetic, C OpenMP, Fortran OpenMP.
# Asserts required runtime libs exist and g++/libstdc++ do not.

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

LOCK_FILE="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"
SYSROOT_LOCK="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_gnu_openmp_sdk.sh --target freebsd_amd64|freebsd_arm64
                                            --prefix DIR [--work-dir DIR] [--jobs N]
       scripts/ci/freebsd_gnu_openmp_sdk.sh --print-lock --target TARGET
USAGE
}

die() {
  echo "freebsd-gnu-openmp-sdk: $*" >&2
  exit 1
}

target=""
prefix=""
work_dir=""
print_lock=0
jobs="${JOBS:-$(nproc 2>/dev/null || echo 2)}"
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --prefix)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--prefix requires a value"
      prefix=$2
      shift 2
      ;;
    --work-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--work-dir requires a value"
      work_dir=$2
      shift 2
      ;;
    --jobs)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--jobs requires a value"
      jobs=$2
      shift 2
      ;;
    --print-lock)
      print_lock=1
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

[[ -s "$LOCK_FILE" ]] || die "missing gnu lock: $LOCK_FILE"
[[ "$(jq -er '.schema_version' "$LOCK_FILE")" == "ecs.freebsd-gnu-openmp.lock/v1" ]] ||
  die "unsupported gnu lock schema"
case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac

gnu_triple=$(jq -er --arg t "$target" '.targets[$t].gnu_target_triple' "$LOCK_FILE")
clang_triple=$(jq -er --arg t "$target" '.targets[$t].clang_target_triple' "$SYSROOT_LOCK")
gcc_version=$(jq -er '.gcc_version' "$LOCK_FILE")
binutils_version=$(jq -er '.binutils_version' "$LOCK_FILE")
gcc_url=$(jq -er '.sources.gcc.url' "$LOCK_FILE")
gcc_sha=$(jq -er '.sources.gcc.sha256' "$LOCK_FILE")
binutils_url=$(jq -er '.sources.binutils.url' "$LOCK_FILE")
binutils_sha=$(jq -er '.sources.binutils.sha256' "$LOCK_FILE")

if [[ "$print_lock" -eq 1 ]]; then
  cat <<EOF
target=$target
gnu_target_triple=$gnu_triple
clang_target_triple=$clang_triple
gcc_version=$gcc_version
binutils_version=$binutils_version
enable_languages=c,fortran
gcc_sha256=$gcc_sha
binutils_sha256=$binutils_sha
EOF
  exit 0
fi

[[ -n "$prefix" ]] || {
  usage
  die "--prefix is required"
}
[[ -z "$work_dir" ]] && work_dir="$ECS_REPO_ROOT/.ci/gnu-sdk-work"

for cmd in curl sha256sum tar make gcc g++ flex bison; do
  command -v "$cmd" >/dev/null 2>&1 || die "required command is missing: $cmd"
done
if ! command -v makeinfo >/dev/null 2>&1; then
  export MAKEINFO=true
fi

export LC_ALL=C
export PATH="$prefix/bin:$PATH"

mkdir -p "$work_dir" "$prefix"
src_root="$work_dir/src"
build_root="$work_dir/build"
sysroot="$work_dir/sysroot"
mkdir -p "$src_root" "$build_root"

fetch_verify() {
  local url=$1 sha=$2 dest=$3
  if [[ -s "$dest" && "$(sha256sum "$dest" | awk '{print $1}')" == "$sha" ]]; then
    return 0
  fi
  rm -f "$dest"
  echo "freebsd-gnu-openmp-sdk: downloading $(basename "$dest")" >&2
  curl -fL --retry 4 --retry-delay 2 --connect-timeout 30 -o "$dest" "$url"
  local actual
  actual=$(sha256sum "$dest" | awk '{print $1}')
  [[ "$actual" == "$sha" ]] || die "SHA256 mismatch for $url expected=$sha actual=$actual"
}

echo "freebsd-gnu-openmp-sdk: target=$target triple=$gnu_triple gcc=$gcc_version binutils=$binutils_version" >&2

echo "freebsd-gnu-openmp-sdk: installing FreeBSD sysroot" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" \
  --target "$target" \
  --sysroot-dir "$sysroot" \
  --work-dir "$work_dir/sysroot-work"

fetch_verify "$binutils_url" "$binutils_sha" "$src_root/binutils-$binutils_version.tar.xz"
fetch_verify "$gcc_url" "$gcc_sha" "$src_root/gcc-$gcc_version.tar.xz"

if [[ ! -d "$src_root/binutils-$binutils_version" ]]; then
  tar -xJf "$src_root/binutils-$binutils_version.tar.xz" -C "$src_root"
fi
if [[ ! -d "$src_root/gcc-$gcc_version" ]]; then
  tar -xJf "$src_root/gcc-$gcc_version.tar.xz" -C "$src_root"
fi

# Use host libgmp/libmpfr/libmpc. Do not download GCC prerequisites (slow
# and unnecessary when Ubuntu packages are present).

echo "freebsd-gnu-openmp-sdk: building binutils" >&2
mkdir -p "$build_root/binutils"
(
  cd "$build_root/binutils"
  MAKEINFO=true "$src_root/binutils-$binutils_version/configure" \
    --target="$gnu_triple" \
    --prefix="$prefix" \
    --with-sysroot="$sysroot" \
    --disable-nls \
    --disable-werror \
    --disable-multilib \
    --with-native-system-header-dir=/include
  MAKEINFO=true make -j"$jobs"
  MAKEINFO=true make install
)

echo "freebsd-gnu-openmp-sdk: building gcc (c,fortran only)" >&2
mkdir -p "$build_root/gcc"
(
  cd "$build_root/gcc"
  "$src_root/gcc-$gcc_version/configure" \
    --target="$gnu_triple" \
    --prefix="$prefix" \
    --with-sysroot="$sysroot" \
    --with-native-system-header-dir=/usr/include \
    --enable-languages=c,fortran \
    --disable-bootstrap \
    --disable-multilib \
    --disable-nls \
    --disable-shared \
    --enable-static \
    --disable-libstdcxx \
    --disable-libatomic \
    --disable-libitm \
    --disable-libsanitizer \
    --disable-libvtv \
    --disable-libssp \
    --without-isl \
    --with-gmp \
    --with-mpfr \
    --with-mpc
  MAKEINFO=true make -j"$jobs" all-gcc all-target-libgcc all-target-libgfortran all-target-libgomp all-target-libquadmath
  MAKEINFO=true make install-gcc install-target-libgcc install-target-libgfortran install-target-libgomp install-target-libquadmath
)

gcc_bin="$prefix/bin/${gnu_triple}-gcc"
gfortran_bin="$prefix/bin/${gnu_triple}-gfortran"
[[ -x "$gcc_bin" ]] || die "missing $gcc_bin"
[[ -x "$gfortran_bin" ]] || die "missing $gfortran_bin"

# Forbidden: g++ and libstdc++
if [[ -x "$prefix/bin/${gnu_triple}-g++" ]]; then
  die "g++ must not be installed"
fi
if find "$prefix" -name 'libstdc++.a' | grep -q .; then
  die "libstdc++.a must not be installed"
fi
if find "$prefix" -name 'libstdc++.so*' | grep -q .; then
  die "libstdc++.so must not be installed"
fi

# Required runtime static libs
libdir="$prefix/lib/gcc/$gnu_triple/$gcc_version"
# Also search the broader prefix because libgcc/libgfortran may install elsewhere.
for lib in libgcc.a libgfortran.a libgomp.a libquadmath.a; do
  if ! find "$prefix" -name "$lib" | grep -q .; then
    die "required runtime library missing: $lib"
  fi
done

probe_dir="$work_dir/probe"
rm -rf "$probe_dir"
mkdir -p "$probe_dir"
cd "$probe_dir"

cat >hello.c <<'EOF'
#include <stdio.h>
int main(void) {
    puts("gnu-c-ok");
    return 0;
}
EOF
"$gcc_bin" -static -o hello-c hello.c
file_out=$(file hello-c)
echo "C hello: $file_out" >&2
case "$file_out" in
  *FreeBSD*) ;;
  *) die "C hello is not a FreeBSD ELF: $file_out" ;;
esac
case "$file_out" in
  *static* | *statically*) ;;
  *) die "C hello is not static: $file_out" ;;
esac

cat >hello.f90 <<'EOF'
program hello
  print *, 'gnu-fortran-ok'
end program hello
EOF
"$gfortran_bin" -static -o hello-f hello.f90
file_out=$(file hello-f)
echo "Fortran hello: $file_out" >&2
case "$file_out" in
  *FreeBSD*) ;;
  *) die "Fortran hello is not a FreeBSD ELF: $file_out" ;;
esac
case "$file_out" in
  *static* | *statically*) ;;
  *) die "Fortran hello is not static: $file_out" ;;
esac

cat >ieee.f90 <<'EOF'
program ieee_check
  use, intrinsic :: ieee_arithmetic
  print *, ieee_support_nan(1.0)
end program ieee_check
EOF
"$gfortran_bin" -static -o ieee ieee.f90
find "$prefix" -name 'ieee_arithmetic.mod' | grep -q . ||
  die "ieee_arithmetic.mod was not installed"

cat >omp.c <<'EOF'
#include <stdio.h>
#ifdef _OPENMP
#include <omp.h>
#endif
int main(void) {
#ifdef _OPENMP
    int n = 0;
#pragma omp parallel
    {
#pragma omp atomic
        n += 1;
    }
    printf("c-openmp-threads=%d\n", n);
    return n > 0 ? 0 : 1;
#else
    puts("openmp-not-enabled");
    return 1;
#endif
}
EOF
"$gcc_bin" -static -fopenmp -o omp-c omp.c
file_out=$(file omp-c)
case "$file_out" in
  *FreeBSD*static* | *static*FreeBSD*) ;;
  *FreeBSD*)
    case "$file_out" in
      *static* | *statically*) ;;
      *) die "C OpenMP hello is not static: $file_out" ;;
    esac
    ;;
  *) die "C OpenMP hello is not a FreeBSD ELF: $file_out" ;;
esac

cat >omp.f90 <<'EOF'
program ompf
  use omp_lib
  integer :: n
  n = 0
!$omp parallel
!$omp atomic
  n = n + 1
!$omp end parallel
  print *, 'fortran-openmp-threads=', n
end program ompf
EOF
"$gfortran_bin" -static -fopenmp -o omp-f omp.f90
file_out=$(file omp-f)
case "$file_out" in
  *FreeBSD*)
    case "$file_out" in
      *static* | *statically*) ;;
      *) die "Fortran OpenMP hello is not static: $file_out" ;;
    esac
    ;;
  *) die "Fortran OpenMP hello is not a FreeBSD ELF: $file_out" ;;
esac

cat >"$prefix/sdk-provenance.json" <<EOF
{
  "target": "$target",
  "gnu_target_triple": "$gnu_triple",
  "gcc_version": "$gcc_version",
  "binutils_version": "$binutils_version",
  "enable_languages": ["c", "fortran"],
  "build_host": "ubuntu-24.04-amd64",
  "toolchain_mode": "cross",
  "openmp_runtime": "libgomp",
  "probes": ["c-static-hello", "fortran-static-hello", "ieee_arithmetic", "c-openmp", "fortran-openmp"]
}
EOF

echo "freebsd-gnu-openmp-sdk: $target SDK ready at $prefix" >&2
"$gcc_bin" --version | head -n1 >&2
"$gfortran_bin" --version | head -n1 >&2

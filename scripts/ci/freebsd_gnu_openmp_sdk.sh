#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD GNU C/Fortran/OpenMP SDK (Stage 4).
#
# Default mode consumes the immutable prebuilt snapshot pinned in the lock
# (Release tag ci-freebsd-gnu-sdk-v1.2; SDK publishing uses version-incrementing
# tags since v1.1, and the maintainer re-points the lock after each publish):
# download, verify SHA256, unpack, then run the full assertion and probe
# suite. --acquire-only stops after the same download/verify/unpack (no
# probes, no build, no sysroot install): it re-acquires the exact bytes the
# gate job already probed, pinned by the same SHA256. --from-source instead
# builds Binutils 2.43.1 and GCC 14.2.0 (c,fortran only) targeting the
# FreeBSD 15.1 sysroot. GCC is configured with --disable-lto and
# --disable-gcov, the host-side ELF binaries are slimmed with an exact
# `strip --strip-debug` pass, and the two documentation directories
# (share/man, share/info) are dropped from the CI snapshot.
#
# Probes: C/Fortran static hello, ieee_arithmetic, C OpenMP, Fortran OpenMP.
# Asserts required runtime libs exist and g++/libstdc++ do not.
#
# Slimming pipeline (REQUIREMENTS.md stages 2-5, --from-source publisher
# only), in order: install (GCC configured --disable-lto/--disable-gcov)
# → drop share/man + share/info (stage 5: exactly these two directories)
# → host ELF --strip-debug (stage 2: host ELFs identified by byte identity,
# e_machine == x86-64 and OS/ABI != FreeBSD, inode-deduplicated so hardlink
# aliases stay shared; no component removed, target ELFs/.a/.o/.mod/headers
# stay byte-identical) → manifest before/after + verify-tree (non-host files
# byte-identical, hardlink groups intact, .debug_* gone) → LTO/gcov
# inventory (stages 3/4: existence recorded, never hand-deleted) → the five
# probes plus the driver -### call-chain check on the final bytes → a single
# consumer build gate (NPB EP/FT Class A + STREAM via
# scripts/build_tools_freebsd_gnu.sh). The gate probes always run on the
# bytes that get published (post-strip in from-source mode), so the release
# proves its own final bytes.

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

LOCK_FILE="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"
SYSROOT_LOCK="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_gnu_openmp_sdk.sh --target freebsd_amd64|freebsd_arm64
                                            --prefix DIR [--work-dir DIR] [--jobs N]
                                            [--acquire-only] [--from-source]
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
from_source=0
acquire_only=0
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
    --from-source)
      from_source=1
      shift
      ;;
    --acquire-only)
      acquire_only=1
      shift
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

# Acquisition mode: the default consumes the immutable prebuilt snapshot
# pinned at targets[$t].prebuilt; --acquire-only stops right after unpacking
# that same snapshot; --from-source forces the full fetch/build path used by
# the freebsd-sdk-release.yml publisher.
if [[ "$acquire_only" -eq 1 && "$from_source" -eq 1 ]]; then
  die "--acquire-only consumes the locked prebuilt snapshot and cannot be combined with --from-source"
fi
if [[ "$from_source" -eq 1 ]]; then
  sdk_mode="from-source"
else
  prebuilt_url=$(jq -er --arg t "$target" '.targets[$t].prebuilt.url' "$LOCK_FILE") ||
    die "lock has no prebuilt snapshot for $target; run the freebsd-sdk-release workflow first or pass --from-source"
  prebuilt_sha256=$(jq -er --arg t "$target" '.targets[$t].prebuilt.sha256' "$LOCK_FILE") ||
    die "lock prebuilt snapshot for $target is missing sha256"
  sdk_mode="prebuilt"
fi

[[ -n "$prefix" ]] || {
  usage
  die "--prefix is required"
}
[[ -z "$work_dir" ]] && work_dir="$ECS_REPO_ROOT/.ci/gnu-sdk-work"

# acquire-only never compiles: it only fetches, verifies and unpacks, so the
# build toolchain commands are not required in that mode. file/readelf/python3
# serve the probe and slimming verification machinery; strip is the host debug
# stripper of the stage-2 pass.
if [[ "$acquire_only" -eq 1 ]]; then
  required_commands=(curl sha256sum tar)
else
  required_commands=(curl sha256sum tar make gcc g++ flex bison file strip readelf python3)
fi
for cmd in "${required_commands[@]}"; do
  command -v "$cmd" >/dev/null 2>&1 || die "required command is missing: $cmd"
done
if ! command -v makeinfo >/dev/null 2>&1; then
  export MAKEINFO=true
fi

export LC_ALL=C
export TZ=UTC
# Pin the consumer-build environment so the consumer build gate (and every
# build through build_tools_freebsd_gnu.sh, which shares this default) is
# deterministic regardless of locale/timezone or a different default
# SOURCE_DATE_EPOCH.
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-946684800}"
export PATH="$prefix/bin:$PATH"

mkdir -p "$work_dir"
sysroot="$work_dir/sysroot"

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
echo "freebsd-gnu-openmp-sdk: mode=$sdk_mode acquire_only=$acquire_only" >&2

# ---------------------------------------------------------------------------
# Low-frequency publisher machinery lives outside this orchestration script.
# It owns the from-source tree evidence/slimming pass and the shared driver-chain
# proof; the runtime probes remain in this script because both acquisition modes
# execute them on the bytes being consumed or published.
sdk_tree_verifier="$ECS_REPO_ROOT/scripts/lib/freebsd_gnu_sdk/tree_verify.py"
sdk_publisher_lib="$ECS_REPO_ROOT/scripts/lib/freebsd_gnu_sdk/publisher.sh"
[[ -r "$sdk_tree_verifier" ]] || die "missing SDK tree verifier: $sdk_tree_verifier"
[[ -r "$sdk_publisher_lib" ]] || die "missing SDK publisher library: $sdk_publisher_lib"
# shellcheck source=scripts/lib/freebsd_gnu_sdk/publisher.sh
source "$sdk_publisher_lib"

# The five release probes, run against the given output directory on the
# final published bytes with the full assertion set.
run_probes() {
  local out_dir=$1
  rm -rf "$out_dir"
  mkdir -p "$out_dir"
  cd "$out_dir"

  cat >hello.c <<'EOF'
#include <stdio.h>
int main(void) {
    puts("gnu-c-ok");
    return 0;
}
EOF
  # The snapshot bakes the publisher's sysroot path (configure-time
  # --with-sysroot); pin the freshly installed sysroot explicitly — the same
  # contract the Stage 5 builder wrappers apply — so consumption never depends
  # on where the snapshot was produced.
  "$gcc_bin" --sysroot="$sysroot" -static -o hello-c hello.c
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
  "$gfortran_bin" --sysroot="$sysroot" -static -o hello-f hello.f90
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
  "$gfortran_bin" --sysroot="$sysroot" -static -o ieee ieee.f90
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
  "$gcc_bin" --sysroot="$sysroot" -static -fopenmp -o omp-c omp.c
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
  "$gfortran_bin" --sysroot="$sysroot" -static -fopenmp -o omp-f omp.f90
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
}

# Acquire the prebuilt snapshot: download from the lock-pinned URL, verify the
# pinned SHA256, unpack into the prefix and drop any leftover cache marker.
# Shared by the default prebuilt mode (which continues with the assertion and
# probe suite) and by --acquire-only (which exits right after unpacking: the
# gate job has already probed these exact bytes, pinned by the same SHA256).
acquire_prebuilt() {
  local sdk_tarball="$work_dir/$(basename "$prebuilt_url")"
  fetch_verify "$prebuilt_url" "$prebuilt_sha256" "$sdk_tarball"
  echo "freebsd-gnu-openmp-sdk: unpacking prebuilt SDK snapshot into $prefix" >&2
  tar -xaf "$sdk_tarball" -C "$prefix" --strip-components=1
  # Older snapshots still carry the marker of the retired cache mechanism;
  # it is not part of the SDK contract.
  rm -f "$prefix/.ecs-gnu-sdk-cache.id"
}

rm -rf "$prefix"
mkdir -p "$prefix"
if [[ "$sdk_mode" == "from-source" ]]; then
  src_root="$work_dir/src"
  build_root="$work_dir/build"
  mkdir -p "$src_root" "$build_root"
  # Batch evidence root (stages 2-5). What the release proves about the
  # slimming (manifest pair, host-ELF inode list, evidence summary, LTO/gcov
  # inventory, share/ listings) is collected here in the job's temp dir.
  evidence_dir="$work_dir/slim-evidence"
  rm -rf "$evidence_dir"
  mkdir -p "$evidence_dir"
  # The immutable repository verifier is invoked directly for publisher evidence.
fi

if [[ "$acquire_only" -eq 1 ]]; then
  acquire_prebuilt
  echo "freebsd-gnu-openmp-sdk: $target prebuilt SDK acquired at $prefix (acquire-only: no probes, no build)" >&2
  exit 0
fi

echo "freebsd-gnu-openmp-sdk: installing FreeBSD sysroot" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" \
  --target "$target" \
  --sysroot-dir "$sysroot" \
  --work-dir "$work_dir/sysroot-work"

if [[ "$sdk_mode" == "prebuilt" ]]; then
  acquire_prebuilt
else
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
      --disable-lto \
      --disable-gcov \
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
    # Full all/install builds every configured target lib. GCC gates
    # libquadmath on a per-target __float128 probe (BUILD_LIBQUADMATH): the
    # probe fails on aarch64, so upstream does not build libquadmath for
    # arm64 and its all/install are no-ops there. languages=c,fortran keeps
    # libstdc++ out of the graph.
    MAKEINFO=true make -j"$jobs" all
    MAKEINFO=true make install
  )

  # Stage 5 (contract 5.2): drop exactly the two documentation directories
  # from the CI snapshot. Nothing else under share/ is touched; the
  # before/after listings are part of the release evidence.
  ecs_freebsd_gnu_sdk_slim_remove_docs
fi

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

# Required runtime static libs, per target: GCC's per-target BUILD_LIBQUADMATH
# probe fails on aarch64, so upstream never builds libquadmath for arm64.
# Also search the broader prefix because libgcc/libgfortran may install elsewhere.
required_libs=$(jq -er --arg t "$target" '.targets[$t].required_libraries[]' "$LOCK_FILE") ||
  die "lock has no required_libraries for target: $target"
while IFS= read -r lib; do
  if ! find "$prefix" -name "$lib" | grep -q .; then
    die "required runtime library missing: $lib"
  fi
done <<<"$required_libs"

if [[ "$sdk_mode" == "from-source" ]]; then
  echo "freebsd-gnu-openmp-sdk: [stage2] manifest before strip" >&2
  python3 "$sdk_tree_verifier" manifest "$prefix" "$evidence_dir/manifest.before.tsv"
  python3 "$sdk_tree_verifier" host-elfs "$evidence_dir/manifest.before.tsv" \
    "$evidence_dir/host-elf-inodes.tsv"

  echo "freebsd-gnu-openmp-sdk: [stage2] host ELF debug strip + invariants" >&2
  ecs_freebsd_gnu_sdk_phase2_strip_and_verify
  python3 "$sdk_tree_verifier" evidence "$target" "$evidence_dir" "$evidence_dir/evidence.json"

  echo "freebsd-gnu-openmp-sdk: [stage3/4] LTO/gcov feature inventory" >&2
  ecs_freebsd_gnu_sdk_feature_inventory "$evidence_dir/feature-inventory.tsv"
fi

# Gate probes run on exactly the bytes that get published: post-strip in
# from-source mode (contract 2.8: slimming completes before the probes),
# consumed-snapshot bytes in prebuilt mode.
run_probes "$work_dir/probe"

# Stage 3 gate on the bytes about to be published (and, in prebuilt mode, on
# the consumed snapshot): every -###-referenced tool must exist.
ecs_freebsd_gnu_sdk_driver_chain_check "$work_dir/probe"

if [[ "$sdk_mode" == "from-source" ]]; then
  # Consumer build gate (contract 2.9): the release must build NPB EP/FT
  # Class A + STREAM through the unmodified Stage 5 builder on the final
  # bytes; set -e turns any build failure into a release failure. Nothing
  # is kept afterwards. Prebuilt mode skips this: the gnu-bench chain
  # already builds the same workloads through this builder on every run.
  ecs_freebsd_gnu_sdk_consumer_build_gate
fi

if [[ "$sdk_mode" == "prebuilt" ]]; then
  acquisition_fields="\"acquisition\": \"prebuilt\",
  \"url\": \"$prebuilt_url\",
  \"sha256\": \"$prebuilt_sha256\""
  # Prebuilt consumption probes the released snapshot; the snapshot's own
  # provenance already carries its slimming record, nothing new is computed.
  slim_provenance_fields=""
else
  acquisition_fields="\"acquisition\": \"from-source\""
  # Provenance must describe the real final artifact (contract 0.3):
  # from-source snapshots carry the strip record, the disabled configure
  # features and the removed documentation directories.
  slim_provenance_fields="\"host_debug_strip\": $(jq -c '.strip' "$evidence_dir/evidence.json"),
  \"configure_features\": {\"lto\": \"disabled\", \"gcov\": \"disabled\"},
  \"removed_directories\": [\"share/man\", \"share/info\"],"
fi

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
  "probes": ["c-static-hello", "fortran-static-hello", "ieee_arithmetic", "c-openmp", "fortran-openmp"],
  $slim_provenance_fields
  $acquisition_fields
}
EOF

echo "freebsd-gnu-openmp-sdk: $target SDK ready at $prefix" >&2
"$gcc_bin" --version | head -n1 >&2
"$gfortran_bin" --version | head -n1 >&2

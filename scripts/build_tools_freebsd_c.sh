#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD C-tools builder (Stage 3).
#
# Builds sysbench, zstd, openssl, fio, iperf3 for freebsd_amd64 and
# freebsd_arm64 using Ubuntu Clang + LLD and a FreeBSD 15.1 sysroot.
# LuaJIT and Concurrency Kit come only from the immutable project snapshot.
#
# Output layout (fragment only — no final manifest.json):
#   <stage-root>/<target>/bin/{sysbench,zstd,openssl,fio,iperf3}
#   <stage-root>/<target>/LICENSES/
#   <stage-root>/<target>/provenance.json

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/common.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/sysbench.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/zstd.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/openssl.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/fio.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_c_tools/iperf3.sh"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build_tools_freebsd_c.sh --target freebsd_amd64|freebsd_arm64
                                        --stage-root DIR [--jobs N]
       scripts/build_tools_freebsd_c.sh --target TARGET --print-params
USAGE
}

target=""
stage_root=""
print_params=0
jobs="${JOBS:-$(nproc 2>/dev/null || echo 2)}"
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      target=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      stage_root=$2
      shift 2
      ;;
    --jobs)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      jobs=$2
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
      exit 2
      ;;
  esac
done

die() {
  echo "build-tools-freebsd-c: $*" >&2
  exit 1
}

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac

ecs_freebsd_target=$target
triple=$(jq -er --arg t "$target" '.targets[$t].clang_target_triple' \
  "$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json")
elf_machine=$(jq -er --arg t "$target" '.targets[$t].elf_machine' \
  "$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json")
# Tool scripts assert against this shared global.
ecs_freebsd_elf_machine=$elf_machine
release=$(jq -er '.freebsd_release' "$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json")

if [[ "$print_params" -eq 1 ]]; then
  cat <<EOF
target=$target
toolchain_mode=cross
compiler_family=clang
linker=lld
target_runner=none
freebsd_release=$release
clang_target_triple=$triple
elf_machine=$elf_machine
openmp_runtime=none
npb_ci_smoke_class=none
EOF
  exit 0
fi

[[ -n "$stage_root" ]] || {
  usage
  die "--stage-root is required"
}

for command_name in curl git jq sha256sum clang lld file make tar gcc perl pkg-config; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

export LC_ALL=C
export TZ=UTC
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-946684800}

stage="$stage_root/$target"
work=/tmp/ecs-freebsd-c-tools-build
[[ ! -e "$work" ]] || die "deterministic build directory already exists: $work"
mkdir -m 0700 -- "$work"
cleanup() {
  local status=$?
  trap - EXIT
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT

mkdir -p "$stage/bin" "$stage/LICENSES"

sysroot="$work/sysroot"
deps_prefix="$work/deps"

echo "build-tools-freebsd-c: installing FreeBSD $release sysroot for $target" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" \
  --target "$target" \
  --sysroot-dir "$sysroot" \
  --work-dir "$work/sysroot-work"

echo "build-tools-freebsd-c: installing immutable target deps for $target" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_target_deps.sh" \
  --target "$target" \
  --prefix "$deps_prefix" \
  --work-dir "$work/deps-work"

wrap_bin=$(ecs_freebsd_c_write_wrappers "$work" "$triple" "$sysroot")

export CC="$wrap_bin/clang"
export CXX="$wrap_bin/clang++"
export CPP="$wrap_bin/cpp"
export AR=llvm-ar
export RANLIB=llvm-ranlib
export STRIP=llvm-strip
export PATH="$wrap_bin:$PATH"

# Shared configure/link flags. LDFLAGS stay compiler/linker-only — never
# Libtool-only switches such as -all-static.
export CPPFLAGS="-I$deps_prefix/usr/local/include"
export CFLAGS="-O2 -fPIC"
export CXXFLAGS="$CFLAGS"
export LDFLAGS="-static -L$sysroot/usr/lib -L$deps_prefix/usr/local/lib"
# FreeBSD .pc files use prefix=/usr/local; rewrite -I/-L into the extract root.
export PKG_CONFIG_SYSROOT_DIR="$deps_prefix"
export PKG_CONFIG_PATH="$deps_prefix/usr/local/libdata/pkgconfig:$deps_prefix/usr/local/lib/pkgconfig"
export PKG_CONFIG_LIBDIR="$deps_prefix/usr/local/libdata/pkgconfig:$deps_prefix/usr/local/lib/pkgconfig"

echo "build-tools-freebsd-c: probing clang wrappers with the configure environment" >&2
ecs_freebsd_c_probe_wrappers "$work"

ecs_freebsd_c_build_sysbench "$work" "$stage" "$deps_prefix" "$jobs"
ecs_freebsd_c_build_zstd "$work" "$stage" "$jobs"
ecs_freebsd_c_build_openssl "$work" "$stage" "$jobs"
ecs_freebsd_c_build_fio "$work" "$stage" "$jobs"
ecs_freebsd_c_build_iperf3 "$work" "$stage" "$jobs"

# Licenses: copy upstream LICENSE files when present.
for tool in sysbench zstd openssl fio iperf3; do
  src="$work/src-$tool"
  mkdir -p "$stage/LICENSES"
  if [[ -f "$src/LICENSE" ]]; then
    cp "$src/LICENSE" "$stage/LICENSES/$tool.LICENSE"
  elif [[ -f "$src/COPYING" ]]; then
    cp "$src/COPYING" "$stage/LICENSES/$tool.LICENSE"
  elif [[ -f "$src/LICENSE.txt" ]]; then
    cp "$src/LICENSE.txt" "$stage/LICENSES/$tool.LICENSE"
  else
    # OpenSSL and others may use different names; leave a pointer file.
    echo "See upstream repository for $tool license terms." >"$stage/LICENSES/$tool.LICENSE"
  fi
done

ecs_freebsd_c_write_provenance "$stage" "$target" "$triple"

# Hard contract checks: exactly the five C tools, no extras, no manifest yet.
expected=(sysbench zstd openssl fio iperf3)
actual=$(find "$stage/bin" -maxdepth 1 -type f -printf '%f\n' | sort)
expected_sorted=$(printf '%s\n' "${expected[@]}" | sort)
[[ "$actual" == "$expected_sorted" ]] ||
  die "unexpected stage bin contents:
expected:
$expected_sorted
actual:
$actual"
[[ ! -e "$stage/manifest.json" ]] || die "C-tools fragment must not emit final manifest.json"

echo "build-tools-freebsd-c: $target C-tools stage complete" >&2
find "$stage/bin" -type f | sort >&2

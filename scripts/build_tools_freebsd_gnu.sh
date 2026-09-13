#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD GNU/OpenMP benchmark builder (Stage 5).
#
# Builds npb-ep, npb-ft (NPB 3.4.4 EP/FT Class A) and STREAM for
# freebsd_amd64 and freebsd_arm64 using the Stage 4 GNU SDK (GCC 14.2.0
# cross to the FreeBSD 15.1 sysroot) on an Ubuntu amd64 host.
#
# Inputs:
#   --sdk-prefix  installed Stage 4 SDK prefix (scripts/ci/freebsd_gnu_openmp_sdk.sh
#                 output, acquired straight from the immutable Release snapshot;
#                 the GNU SDK gate job has probed the same SHA256-pinned bytes);
#                 the FreeBSD sysroot is re-installed from the pinned Stage 2
#                 lock via scripts/ci/freebsd_sysroot.sh, exactly like the
#                 Stage 3 C-tools builder does.
#
# Output layout (fragment only — no final manifest.json):
#   <stage-root>/<target>/bin/{npb-ep,npb-ft,stream}
#   <stage-root>/<target>/LICENSES/
#   <stage-root>/<target>/provenance.json
#   <stage-root>/<target>/SHA256SUMS

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

# STREAM 的 URL/SHA256/数组大小/迭代次数统一合同（与 Linux 发布构建共享）。
source "$ECS_REPO_ROOT/scripts/lib/stream.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_gnu_tools/common.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_gnu_tools/npb.sh"
source "$ECS_REPO_ROOT/scripts/lib/freebsd_gnu_tools/stream.sh"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build_tools_freebsd_gnu.sh --target freebsd_amd64|freebsd_arm64
                                          --stage-root DIR --sdk-prefix DIR
       scripts/build_tools_freebsd_gnu.sh --target TARGET --print-params
USAGE
}

target=""
stage_root=""
sdk_prefix=""
print_params=0
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
    --sdk-prefix)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      sdk_prefix=$2
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
  echo "build-tools-freebsd-gnu: $*" >&2
  exit 1
}

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac

gnu_lock="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"
sysroot_lock="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"
triple=$(jq -er --arg t "$target" '.targets[$t].gnu_target_triple' "$gnu_lock")
gcc_version=$(jq -er '.gcc_version' "$gnu_lock")
release=$(jq -er '.freebsd_release' "$sysroot_lock")
# `file` prints x86-64 for freebsd_amd64 and aarch64 for freebsd_arm64; the
# sysroot lock's elf_machine labels (amd64/arm64) do not appear in `file`
# output, so the structural assertion uses the real `file` token.
case "$target" in
  freebsd_amd64) file_machine="x86-64" ;;
  freebsd_arm64) file_machine="aarch64" ;;
esac

if [[ "$print_params" -eq 1 ]]; then
  cat <<EOF
target=$target
toolchain_mode=cross
compiler_family=gcc
compiler_version=$gcc_version
linker=target-gnu-ld
target_runner=none
freebsd_release=$release
gnu_target_triple=$triple
file_machine=$file_machine
openmp_runtime=libgomp
npb_version=$ECS_NPB_VERSION
npb_suite=ep,ft
npb_class=$ECS_NPB_CLASS
npb_rand=$ECS_NPB_RAND
npb_fflags=$ECS_NPB_FFLAGS
npb_flinkflags=$ECS_NPB_FLINKFLAGS
stream_array_size=$ECS_STREAM_ARRAY_SIZE
stream_ntimes=$ECS_STREAM_NTIMES
EOF
  exit 0
fi

[[ -n "$stage_root" ]] || {
  usage
  die "--stage-root is required"
}
[[ -n "$sdk_prefix" ]] || {
  usage
  die "--sdk-prefix is required (installed Stage 4 GNU SDK prefix)"
}

for command_name in curl jq sha256sum make gcc file tar nm readelf strings; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

export LC_ALL=C
export TZ=UTC
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-946684800}

stage="$stage_root/$target"
work=/tmp/ecs-freebsd-gnu-build
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

echo "build-tools-freebsd-gnu: installing FreeBSD $release sysroot for $target" >&2
bash "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" \
  --target "$target" \
  --sysroot-dir "$sysroot" \
  --work-dir "$work/sysroot-work"

echo "build-tools-freebsd-gnu: validating Stage 4 GNU SDK at $sdk_prefix" >&2
ecs_freebsd_gnu_restore_sdk_exec_bits "$sdk_prefix" "$triple"
ecs_freebsd_gnu_validate_sdk "$sdk_prefix" "$triple"

# The SDK driver looks up the target assembler/linker (${triple}-as/-ld) on
# PATH; the SDK installs them in its own bin directory.
export PATH="$sdk_prefix/bin:$PATH"

wrap_bin=$(ecs_freebsd_gnu_write_wrappers "$work" "$sdk_prefix" "$triple" "$sysroot")

echo "build-tools-freebsd-gnu: probing GNU wrappers with the benchmark environment" >&2
ecs_freebsd_gnu_probe_wrappers "$work" "$file_machine"

ecs_freebsd_gnu_build_npb "$work" "$stage" "$wrap_bin" "$file_machine"
ecs_freebsd_gnu_build_stream "$work" "$stage" "$wrap_bin" "$file_machine"

ecs_freebsd_gnu_write_provenance "$stage" "$target" "$triple" "$gcc_version" \
  "$ECS_NPB_URL" "$ECS_NPB_SHA256" "$ECS_STREAM_URL" "$ECS_STREAM_SOURCE_SHA256"

# Package-level checksum manifest over the whole fragment (bin, licenses and
# provenance; SHA256SUMS itself is excluded by construction). The per-tool
# sha256 values stay in provenance.json as record fields; the fragment's
# integrity is asserted once with `sha256sum -c` at merge time instead of
# being re-asserted tool by tool.
(
  cd "$stage"
  find bin LICENSES provenance.json -type f -print0 | LC_ALL=C sort -z |
    xargs -0 sha256sum >SHA256SUMS
)

# Hard contract checks: exactly the three GNU/OpenMP tools, no extras, no
# manifest yet (Stage 6 merge owns the final manifest).
expected=(npb-ep npb-ft stream)
actual=$(find "$stage/bin" -maxdepth 1 -type f -printf '%f\n' | sort)
expected_sorted=$(printf '%s\n' "${expected[@]}" | sort)
[[ "$actual" == "$expected_sorted" ]] ||
  die "unexpected stage bin contents:
expected:
$expected_sorted
actual:
$actual"
[[ ! -e "$stage/manifest.json" ]] || die "GNU-tools fragment must not emit final manifest.json"

echo "build-tools-freebsd-gnu: $target GNU/OpenMP stage complete" >&2
find "$stage/bin" -type f | sort >&2

#!/usr/bin/env bash
set -Eeuo pipefail

# Native FreeBSD builder for the benchmark tools ECS ships on FreeBSD.
# The default "all" mode preserves the one-shot builder interface. CI can use
# --phase to keep one VM/workspace alive while exposing each logical tool stage
# as a separate GitHub Actions step.

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build_tools_freebsd.sh --target freebsd_amd64|freebsd_arm64 \
       --stage-root STAGE_ROOT [--phase PHASE]
       scripts/build_tools_freebsd.sh --target TARGET --print-params

PHASE is one of:
  all sources sysbench zstd npb openssl stream fio iperf3 manifest

freebsd_amd64 builds natively on a FreeBSD/amd64 host. freebsd_arm64 is a
host-native cross build against the pinned FreeBSD/amd64 -> arm64 SDK
(ECS_FREEBSD_CROSS_SDK); qemu-user only executes finished target smoke binaries.
USAGE
}

die() {
  echo "build-tools-freebsd: $*" >&2
  exit 1
}

target=""
stage_root=""
phase=all
print_params=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --phase)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--phase requires a value"
      phase=$2
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

case "$phase" in
  all | sources | sysbench | zstd | npb | openssl | stream | fio | iperf3 | manifest) ;;
  *) die "unsupported phase: $phase" ;;
esac

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
lock_file="$repo_root/tools/lock.json"
[[ -s "$lock_file" ]] || die "missing tools lock: $lock_file"
command -v jq >/dev/null 2>&1 || die 'jq is required to read tools/lock.json'

goos=$(jq -er --arg target "$target" '.architectures[] | select(.target == $target) | .goos' "$lock_file") ||
  die "unsupported target: ${target:-<empty>}"
[[ "$goos" == freebsd ]] || die "unsupported target: $target (this builder only supports FreeBSD)"
goarch=$(jq -er --arg target "$target" '.architectures[] | select(.target == $target) | .goarch' "$lock_file") ||
  die "target $target has no GOARCH"
package_arch=$(jq -er --arg target "$target" '.architectures[] | select(.target == $target) | .package' "$lock_file") ||
  die "target $target has no package architecture"
openssl_target=$(jq -er --arg target "$target" '.architectures[] | select(.target == $target) | .openssl_target' "$lock_file") ||
  die "target $target has no OpenSSL target"
supported_architectures_json=$(jq -c '[.architectures[] | select(.goos == "freebsd") | .package]' "$lock_file")
supported_targets_json=$(jq -c '[.architectures[] | select(.goos == "freebsd") | .target]' "$lock_file")

if [[ "$print_params" -eq 1 ]]; then
  printf 'target=%s\n' "$target"
  printf 'goos=freebsd\n'
  printf 'goarch=%s\n' "$goarch"
  printf 'package_arch=%s\n' "$package_arch"
  case "$target" in
    freebsd_amd64)
      printf 'toolchain_mode=native\n'
      printf 'target_runner=direct\n'
      ;;
    freebsd_arm64)
      printf 'toolchain_mode=cross\n'
      printf 'target_runner=qemu-aarch64-static\n'
      ;;
  esac
  printf 'npb_ci_smoke_class=A\n'
  printf 'tools=sysbench zstd npb-ep npb-ft openssl stream fio iperf3\n'
  exit 0
fi

[[ -n "$stage_root" ]] || { usage; exit 2; }
[[ "$stage_root" = /* ]] || die "stage root must be an absolute path"
stage="$stage_root/$target"
work=${ECS_TOOLS_WORK:-/tmp/ecs-tools-freebsd-build}
[[ "$work" = /* && "$work" != / ]] || die "build work directory must be an absolute non-root path"

export LC_ALL=C
export LANG=C
export TZ=UTC
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-946684800}
[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || die 'SOURCE_DATE_EPOCH must be an integer'
jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || jobs=2

toolchain_mode=native
configure_cross_args=()
sdk_deps_localbase=''
cross_target_triplet=''

case "$target" in
  freebsd_amd64)
    [[ -z "${ECS_FREEBSD_CROSS_SDK:-}" ]] ||
      die 'freebsd_amd64 is a native target; unset ECS_FREEBSD_CROSS_SDK'
    cc_command=${CC:-gcc14}
    cxx_command=${CXX:-g++14}
    fc_command=${FC:-gfortran14}
    for command_name in bash "$cc_command" "$cxx_command" "$fc_command" git gmake jq perl pkg pkg-config sha256 tar sed awk grep file readelf nm; do
      command -v "$command_name" >/dev/null 2>&1 || die "required FreeBSD build command is missing: $command_name"
    done
    sysbench_luajit_version=$(pkg-config --modversion luajit) ||
      die 'FreeBSD builder is missing the LuaJIT pkg-config metadata'
    sysbench_ck_version=$(pkg-config --modversion ck) ||
      die 'FreeBSD builder is missing the Concurrency Kit pkg-config metadata'
    sysbench_luajit_package=$(pkg query '%n-%v' luajit) ||
      die 'FreeBSD builder cannot resolve the LuaJIT package identity'
    sysbench_ck_package=$(pkg query '%n-%v' concurrencykit) ||
      die 'FreeBSD builder cannot resolve the Concurrency Kit package identity'
    freebsd_localbase=$(pkg-config --variable=prefix luajit) ||
      die 'FreeBSD builder cannot resolve the dependency installation prefix'
    [[ -n "$freebsd_localbase" && "$freebsd_localbase" = /* ]] ||
      die "invalid dependency installation prefix: ${freebsd_localbase:-<empty>}"
    ;;
  freebsd_arm64)
    [[ -n "${ECS_FREEBSD_CROSS_SDK:-}" ]] ||
      die 'freebsd_arm64 requires ECS_FREEBSD_CROSS_SDK (host-native FreeBSD/amd64 -> arm64 SDK)'
    sdk_root=$ECS_FREEBSD_CROSS_SDK
    [[ -d "$sdk_root" ]] || die "SDK root does not exist: $sdk_root"
    sdk_triple=$(jq -er '.freebsd.target_triple' "$repo_root/tools/freebsd-cross-sdk.lock.json") ||
      die 'cannot read target triple from tools/freebsd-cross-sdk.lock.json'
    cc_command="$sdk_root/toolchain/bin/${sdk_triple}-gcc"
    cxx_command="$sdk_root/toolchain/bin/${sdk_triple}-g++"
    fc_command="$sdk_root/toolchain/bin/${sdk_triple}-gfortran"
    readelf_command="$sdk_root/target-aliases/${sdk_triple}-readelf"
    nm_command="$sdk_root/target-aliases/${sdk_triple}-nm"
    [[ -x "$cc_command" ]] || die "SDK cross gcc is missing: $cc_command"
    [[ -x "$cxx_command" ]] || die "SDK cross g++ is missing: $cxx_command"
    [[ -x "$fc_command" ]] || die "SDK cross gfortran is missing: $fc_command"
    [[ -x "$readelf_command" ]] || readelf_command=$(command -v readelf || true)
    [[ -n "$readelf_command" && -x "$readelf_command" ]] || die 'readelf is required for ELF validation'
    [[ -n "${ECS_TARGET_RUNNER:-}" ]] ||
      die 'freebsd_arm64 requires ECS_TARGET_RUNNER (e.g. qemu-aarch64-static)'
    toolchain_mode=cross
    cross_target_triplet=$sdk_triple
    sdk_deps_localbase="$sdk_root/deps/usr/local"
    export ECS_FREEBSD_CROSS_SYSROOT="$sdk_root/sysroot"
    [[ -d "$ECS_FREEBSD_CROSS_SYSROOT" ]] ||
      die "SDK sysroot is missing; run freebsd_cross_sdk.sh sysroot ($ECS_FREEBSD_CROSS_SYSROOT)"
    [[ -s "$sdk_deps_localbase/lib/libluajit-5.1.a" ]] ||
      die "SDK deps are missing; run freebsd_cross_sdk.sh deps ($sdk_deps_localbase)"
    export PKG_CONFIG_PATH="$sdk_deps_localbase/libdata/pkgconfig"
    export PKG_CONFIG_LIBDIR="$sdk_deps_localbase/libdata/pkgconfig"
    sysbench_luajit_version=$(pkg-config --modversion luajit) ||
      die 'SDK deps are missing the LuaJIT pkg-config metadata'
    sysbench_ck_version=$(pkg-config --modversion ck) ||
      die 'SDK deps are missing the Concurrency Kit pkg-config metadata'
    # Ports license directories use the package version, not pkg-config's
    # LuaJIT "githash" version. Discover the extracted license dirs.
    freebsd_localbase=$sdk_deps_localbase
    freebsd_license_root="$freebsd_localbase/share/licenses"
    luajit_license_candidates=()
    ck_license_candidates=()
    mapfile -t luajit_license_candidates < <(printf '%s\n' "$freebsd_license_root"/luajit-*)
    mapfile -t ck_license_candidates < <(printf '%s\n' "$freebsd_license_root"/concurrencykit-*)
    [[ "${#luajit_license_candidates[@]}" -eq 1 && -d "${luajit_license_candidates[0]}" ]] ||
      die "SDK deps must contain exactly one LuaJIT license directory under $freebsd_license_root"
    [[ "${#ck_license_candidates[@]}" -eq 1 && -d "${ck_license_candidates[0]}" ]] ||
      die "SDK deps must contain exactly one Concurrency Kit license directory under $freebsd_license_root"
    sysbench_luajit_package=$(basename "${luajit_license_candidates[0]}")
    sysbench_ck_package=$(basename "${ck_license_candidates[0]}")
    configure_cross_args=("--build=$(cc -dumpmachine)" "--host=$sdk_triple")
    export CC="$cc_command"
    export CXX="$cxx_command"
    export AR="$sdk_root/target-aliases/${sdk_triple}-ar"
    export RANLIB="$sdk_root/target-aliases/${sdk_triple}-ranlib"
    [[ -x "$AR" ]] || die "SDK cross ar alias is missing: $AR"
    [[ -x "$RANLIB" ]] || die "SDK cross ranlib alias is missing: $RANLIB"
    # Host file(1)/elftoolchain readelf already understand AArch64 ELF, so
    # validate_binary keeps using the unprefixed host tools. PATH only needs
    # the SDK compilers for anything that invokes them by name.
    export PATH="$sdk_root/toolchain/bin:$sdk_root/target-aliases:$PATH"
    ;;
  *)
    die "unsupported target: $target"
    ;;
esac

for command_name in curl git gmake jq perl sha256 tar sed awk grep file; do
  command -v "$command_name" >/dev/null 2>&1 || die "required host command is missing: $command_name"
done
command -v curl >/dev/null 2>&1 || command -v fetch >/dev/null 2>&1 ||
  die 'curl or fetch is required to download pinned sources'

freebsd_license_root="$freebsd_localbase/share/licenses"
luajit_license_dir="$freebsd_license_root/$sysbench_luajit_package"
ck_license_dir="$freebsd_license_root/$sysbench_ck_package"
for license_file in "$luajit_license_dir/MIT" "$luajit_license_dir/PD" "$ck_license_dir/BSD2CLAUSE"; do
  [[ -s "$license_file" ]] || die "required dependency license is missing: $license_file"
done

if [[ "$toolchain_mode" == cross ]]; then
  build_triplet=$(cc -dumpmachine)
  target_triplet=$cross_target_triplet
else
  build_triplet=$("$cc_command" -dumpmachine)
  target_triplet=$build_triplet
fi

source "$repo_root/scripts/lib/freebsd_tools/common.sh"

sysbench_repository=$(lock_tool_field sysbench repository)
sysbench_tag=$(lock_tool_field sysbench tag)
sysbench_commit=$(lock_tool_field sysbench commit)
zstd_repository=$(lock_tool_field zstd repository)
zstd_tag=$(lock_tool_field zstd tag)
zstd_commit=$(lock_tool_field zstd commit)
openssl_repository=$(lock_tool_field openssl repository)
openssl_tag=$(lock_tool_field openssl tag)
openssl_commit=$(lock_tool_field openssl commit)
fio_repository=$(lock_tool_field fio repository)
fio_tag=$(lock_tool_field fio tag)
fio_commit=$(lock_tool_field fio commit)
iperf3_repository=$(lock_tool_field iperf3 repository)
iperf3_tag=$(lock_tool_field iperf3 tag)
iperf3_commit=$(lock_tool_field iperf3 commit)
npb_version=$(lock_tool_field npb-ep version)
npb_tag=$(lock_tool_field npb-ep tag)
npb_url=$(lock_tool_field npb-ep source_url)
npb_sha=$(lock_tool_field npb-ep source_sha256)
stream_url=$(lock_tool_field stream source_url)
stream_sha=$(lock_tool_field stream source_sha256)
stream_array_size=$(lock_tool_field stream array_size)
stream_ntimes=$(lock_tool_field stream ntimes)
zstd_corpus_name=$(jq -er '.corpus.name' "$lock_file")
zstd_corpus_url=$(jq -er '.corpus.source_url' "$lock_file")
zstd_corpus_source_sha=$(jq -er '.corpus.source_sha256' "$lock_file")
zstd_corpus_sha=$(jq -er '.corpus.sha256' "$lock_file")
zstd_corpus_bytes=$(jq -er '.corpus.bytes' "$lock_file")

sysbench_src="$work/sysbench"
zstd_src="$work/zstd"
openssl_src="$work/openssl"
fio_src="$work/fio"
iperf3_src="$work/iperf3"
npb_archive="$work/$npb_tag.tar.gz"
npb_src="$work/$npb_tag/NPB3.4-OMP"
stream_src="$work/stream.c"

source "$repo_root/scripts/lib/freebsd_tools/core.sh"
source "$repo_root/scripts/lib/freebsd_tools/extra.sh"
source "$repo_root/scripts/lib/freebsd_tools/manifest.sh"

run_phase() {
  local requested=$1
  echo "== FreeBSD tools phase: $requested ($target) =="
  "phase_$requested"
}

if [[ "$phase" == all ]]; then
  stage_created=0
  cleanup() {
    local status=$?
    trap - EXIT
    rm -rf -- "$work"
    if [[ "$status" -ne 0 && "$stage_created" -eq 1 ]]; then
      rm -rf -- "$stage"
    fi
    exit "$status"
  }
  trap cleanup EXIT
  run_phase sources
  stage_created=1
  run_phase sysbench
  run_phase zstd
  run_phase npb
  run_phase openssl
  run_phase stream
  run_phase fio
  run_phase iperf3
  run_phase manifest
else
  run_phase "$phase"
fi

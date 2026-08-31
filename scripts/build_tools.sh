#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'EOF'
usage: scripts/build_tools.sh --arch ARCH --stage-root STAGE_ROOT
                              [--cross-prefix PREFIX --target-runner COMMAND]

Build the ECS benchmark tools for one Linux architecture. This script is
intended to run inside the native or cross-build container used by CI; it never
uses a fixture or a distribution-provided benchmark binary. Cross mode keeps
the compiler native and uses TARGET_RUNNER only for final binary smoke tests.
EOF
}

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
source "$(dirname "${BASH_SOURCE[0]}")/lib/stream.sh"
source "$(dirname "${BASH_SOURCE[0]}")/lib/corpus.sh"

die() {
  echo "build-tools: $*" >&2
  exit 1
}

arch=""
stage_root=""
cross_prefix=""
target_runner=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --arch)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      arch=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      stage_root=$2
      shift 2
      ;;
    --cross-prefix)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      cross_prefix=$2
      shift 2
      ;;
    --target-runner)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      target_runner=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

case " ${ECS_ARCHES[*]} " in
  *" $arch "*) ;;
  *) die "unsupported architecture: ${arch:-<empty>}" ;;
esac
[[ -n "$stage_root" ]] || { usage; exit 2; }
[[ "$stage_root" = /* ]] || die "stage root must be an absolute path"
openssl_target=$(ecs_lock_architecture_field "$arch" openssl_target) ||
  die "tools lock has no OpenSSL target for $arch"
if [[ -n "$cross_prefix" || -n "$target_runner" ]]; then
  case "$arch" in
    armv7|s390x|riscv64|ppc64le) ;;
    *) die "cross mode is not supported for $arch" ;;
  esac
  [[ -n "$cross_prefix" && -n "$target_runner" ]] ||
    die "--cross-prefix and --target-runner must be provided together"
fi

for command_name in \
  curl git jq sha256sum gcc make readelf strip tar meson ninja autoconf automake \
  libtoolize perl pkg-config unzip; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

build_triplet=$(gcc -dumpmachine)
target_triplet=$build_triplet
build_mode=native
smoke_runner=direct
cc_command=${CC:-gcc}
cxx_command=${CXX:-g++}
fc_command=${FC:-gfortran}
ar_command=${AR:-ar}
ranlib_command=${RANLIB:-ranlib}
readelf_command=${READELF:-readelf}
strip_command=${STRIP:-strip}
configure_cross_args=()
fio_cross_args=()
meson_cross_args=()
target_runner_command=()

if [[ -n "$cross_prefix" ]]; then
  build_mode=cross
  case "$arch" in
    armv7)
      expected_target_triplet=arm-linux-gnueabihf
      fio_target_cpu=arm
      meson_cpu_family=arm
      meson_cpu=armv7
      meson_endian=little
      ;;
    s390x)
      expected_target_triplet=s390x-linux-gnu
      fio_target_cpu=s390x
      meson_cpu_family=s390x
      meson_cpu=s390x
      meson_endian=big
      ;;
    riscv64)
      expected_target_triplet=riscv64-linux-gnu
      fio_target_cpu=riscv64
      meson_cpu_family=riscv64
      meson_cpu=riscv64
      meson_endian=little
      ;;
    ppc64le)
      expected_target_triplet=powerpc64le-linux-gnu
      fio_target_cpu=ppc64
      meson_cpu_family=ppc64
      meson_cpu=ppc64le
      meson_endian=little
      ;;
  esac
  cc_command="${cross_prefix}gcc"
  cxx_command="${cross_prefix}g++"
  fc_command="${cross_prefix}gfortran"
  ar_command="${cross_prefix}ar"
  ranlib_command="${cross_prefix}ranlib"
  readelf_command="${cross_prefix}readelf"
  strip_command="${cross_prefix}strip"
  for command_name in "$cc_command" "$cxx_command" "$fc_command" "$ar_command" "$ranlib_command" \
    "$readelf_command" "$strip_command" "$target_runner"; do
    command -v "$command_name" >/dev/null 2>&1 || die "required cross command is missing: $command_name"
  done
  target_triplet=$("$cc_command" -dumpmachine)
  [[ "$target_triplet" == "$expected_target_triplet"* ]] ||
    die "$arch cross compiler reported unexpected target: $target_triplet"
  configure_cross_args=("--build=$build_triplet" "--host=$target_triplet")
  fio_cross_args=("--cpu=$fio_target_cpu" "--cc=$cc_command")
  target_runner_command=("$target_runner")
  smoke_runner=$target_runner
fi

command -v "$fc_command" >/dev/null 2>&1 || die "required Fortran compiler is missing: $fc_command"

echo "toolchain mode: $build_mode ($build_triplet -> $target_triplet); smoke runner: $smoke_runner"

export CC="$cc_command"
export CXX="$cxx_command"
export AR="$ar_command"
export RANLIB="$ranlib_command"
export STRIP="$strip_command"
# These variables already contain fully qualified target tool names in cross
# mode. Do not also export CROSS_COMPILE: OpenSSL would prepend it a second
# time and look for e.g. arm-linux-gnueabihf-arm-linux-gnueabihf-gcc.

run_target() {
  if [[ "${#target_runner_command[@]}" -gt 0 ]]; then
    "${target_runner_command[@]}" "$@"
  else
    "$@"
  fi
}

# Each tool owns its build flags and functional smoke contract. This file keeps
# only shared source staging, static ELF checks, licensing and manifest output.
source "$(dirname "${BASH_SOURCE[0]}")/tools/sysbench.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/zstd.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/npb.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/fio.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/iperf3.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/stream.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/ping.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/openssl.sh"
source "$(dirname "${BASH_SOURCE[0]}")/tools/nexttrace.sh"

stage="$stage_root/linux_${arch}"
work=/tmp/ecs-tools-build
[[ ! -e "$work" ]] || die "deterministic build directory already exists: $work"
mkdir -m 0700 -- "$work"
cleanup() {
  local status=$?
  trap - EXIT
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT

# Stable locale, timestamps, and source paths keep consecutive builds of the
# same inputs byte-identical. Each architecture runs in its own disposable
# container, so the fixed path cannot collide with another matrix job.
export LC_ALL=C
export TZ=UTC
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-946684800}
[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || die "SOURCE_DATE_EPOCH must be an integer"

if [[ -n "$cross_prefix" ]]; then
  [[ -n "${PKG_CONFIG_LIBDIR:-}" ]] ||
    die "cross mode requires PKG_CONFIG_LIBDIR to point at target metadata"
  meson_cross_file="$work/meson-cross.ini"
  {
    printf '%s\n' '[binaries]'
    printf "c = '%s'\n" "$cc_command"
    printf "ar = '%s'\n" "$ar_command"
    printf "strip = '%s'\n" "$strip_command"
    printf "pkg-config = 'pkg-config'\n"
    printf '%s\n' '[properties]'
    printf '%s\n' 'needs_exe_wrapper = true'
    printf '%s\n' '[host_machine]'
    printf "%s\n" "system = 'linux'"
    printf "%s\n" "cpu_family = '$meson_cpu_family'"
    printf "%s\n" "cpu = '$meson_cpu'"
    printf "%s\n" "endian = '$meson_endian'"
  } >"$meson_cross_file"
  meson_cross_args=(--cross-file "$meson_cross_file")
fi

mkdir -p "$stage/bin" "$stage/LICENSES" "$stage/share/ecs/corpus"

curl_options=(-fsSL --retry 4 --retry-delay 2 --connect-timeout 30)
github_api_curl_options=("${curl_options[@]}")
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  github_api_curl_options+=( -H "Authorization: Bearer ${GITHUB_TOKEN}" )
fi


github_release_by_tag() {
  local repository=$1
  local tag=$2
  local output=$3
  curl "${github_api_curl_options[@]}" \
    -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${repository}/releases/tags/${tag}" >"$output"
  jq -e --arg tag "$tag" '
    (.draft == false) and (.prerelease == false) and (.tag_name == $tag)
  ' "$output" >/dev/null || die "${repository} ${tag} is not a stable tagged release"
}

clone_release() {
  local repository=$1
  local tag=$2
  local destination=$3
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/${repository}.git" "$destination" >/dev/null
  git -C "$destination" rev-parse HEAD
}

git_source() {
  local repository=$1
  local commit=$2
  printf 'git+https://github.com/%s.git@%s\n' "$repository" "$commit"
}

sysbench_repository=$(ecs_lock_tool_field sysbench repository)
sysbench_tag=$(ecs_lock_tool_field sysbench tag)
sysbench_expected_commit=$(ecs_lock_tool_field sysbench commit)
zstd_repository=$(ecs_lock_tool_field zstd repository)
zstd_tag=$(ecs_lock_tool_field zstd tag)
zstd_expected_commit=$(ecs_lock_tool_field zstd commit)
openssl_repository=$(ecs_lock_tool_field openssl repository)
openssl_tag=$(ecs_lock_tool_field openssl tag)
openssl_expected_commit=$(ecs_lock_tool_field openssl commit)
fio_repository=$(ecs_lock_tool_field fio repository)
fio_tag=$(ecs_lock_tool_field fio tag)
fio_expected_commit=$(ecs_lock_tool_field fio commit)
iperf3_repository=$(ecs_lock_tool_field iperf3 repository)
iperf3_tag=$(ecs_lock_tool_field iperf3 tag)
iperf3_expected_commit=$(ecs_lock_tool_field iperf3 commit)
iputils_repository=$(ecs_lock_tool_field ping repository)
iputils_tag=$(ecs_lock_tool_field ping tag)
iputils_expected_commit=$(ecs_lock_tool_field ping commit)
nexttrace_repository=$(ecs_lock_tool_field nexttrace-tiny repository)
nexttrace_tag=$(ecs_lock_tool_field nexttrace-tiny tag)
nexttrace_expected_commit=$(ecs_lock_tool_field nexttrace-tiny commit)

nexttrace_release="$work/nexttrace-release.json"
github_release_by_tag "$nexttrace_repository" "$nexttrace_tag" "$nexttrace_release"

sysbench_src="$work/sysbench"
zstd_src="$work/zstd"
openssl_src="$work/openssl"
fio_src="$work/fio"
iperf3_src="$work/iperf3"
iputils_src="$work/iputils"
nexttrace_src="$work/nexttrace"
sysbench_commit=$(clone_release "$sysbench_repository" "$sysbench_tag" "$sysbench_src")
[[ "$sysbench_commit" == "$sysbench_expected_commit" ]] ||
  die "sysbench $sysbench_tag resolved to $sysbench_commit, expected $sysbench_expected_commit"
zstd_commit=$(clone_release "$zstd_repository" "$zstd_tag" "$zstd_src")
[[ "$zstd_commit" == "$zstd_expected_commit" ]] ||
  die "zstd $zstd_tag resolved to $zstd_commit, expected $zstd_expected_commit"
openssl_commit=$(clone_release "$openssl_repository" "$openssl_tag" "$openssl_src")
[[ "$openssl_commit" == "$openssl_expected_commit" ]] ||
  die "OpenSSL $openssl_tag resolved to $openssl_commit, expected $openssl_expected_commit"
fio_commit=$(clone_release "$fio_repository" "$fio_tag" "$fio_src")
[[ "$fio_commit" == "$fio_expected_commit" ]] ||
  die "fio $fio_tag resolved to $fio_commit, expected $fio_expected_commit"
iperf3_commit=$(clone_release "$iperf3_repository" "$iperf3_tag" "$iperf3_src")
[[ "$iperf3_commit" == "$iperf3_expected_commit" ]] ||
  die "iperf3 $iperf3_tag resolved to $iperf3_commit, expected $iperf3_expected_commit"

# iputils is deliberately built from its official release source rather than
# copied from a distribution package: ECS needs the upstream ping output.
iputils_commit=$(clone_release "$iputils_repository" "$iputils_tag" "$iputils_src")
[[ "$iputils_commit" == "$iputils_expected_commit" ]] ||
  die "iputils $iputils_tag resolved to $iputils_commit, expected $iputils_expected_commit"
nexttrace_commit=$(clone_release "$nexttrace_repository" "$nexttrace_tag" "$nexttrace_src")
[[ "$nexttrace_commit" == "$nexttrace_expected_commit" ]] ||
  die "NextTrace $nexttrace_tag resolved to $nexttrace_commit, expected $nexttrace_expected_commit"

# The official STREAM source is a single file served over HTTPS with no release
# feed and no upstream checksum.  Pin its SHA-256 here: the RCS revision comment
# alone is not evidence, since a tampered file can keep that line unchanged.
stream_url=$ECS_STREAM_URL
stream_expected_sha=$ECS_STREAM_SOURCE_SHA256
stream_src="$work/stream.c"
ecs_stream_download "$stream_src" || die 'official STREAM source did not match its pinned SHA-256'
stream_sha=$stream_expected_sha
stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
[[ -n "$stream_revision" ]] || die "could not read the official STREAM revision from $stream_url"
stream_version=${stream_revision%%-*}

npb_version=$(ecs_lock_tool_field npb-ep version)
npb_tag=$(ecs_lock_tool_field npb-ep tag)
npb_archive_url=$(ecs_lock_tool_field npb-ep source_url)
npb_archive_sha=$(ecs_lock_tool_field npb-ep source_sha256)
npb_archive="$work/${npb_tag}.tar.gz"
ecs_download_sha256 "$npb_archive_url" "$npb_archive_sha" "$npb_archive" \
  "NPB ${npb_tag} source archive"
tar -xzf "$npb_archive" -C "$work"
npb_release_root="$work/$npb_tag"
npb_src="$npb_release_root/NPB3.4-OMP"
[[ -d "$npb_src/EP" && -d "$npb_src/FT" ]] || die 'NPB archive omitted NPB3.4-OMP EP or FT'

# The corpus is part of the benchmark contract, not an incidental input. The
# 语料构建（下载、校验、按 lock.json 顺序拼接）由 lib/corpus.sh 唯一实现，
# 独立语料发布物走同一个函数，两条路径不再各写一遍。
zstd_corpus_url=$(ecs_lock_corpus_field source_url)
zstd_corpus_source_sha=$(ecs_lock_corpus_field source_sha256)
zstd_corpus_sha=$(ecs_lock_corpus_field sha256)
zstd_corpus_bytes=$(ecs_lock_corpus_field bytes)
zstd_corpus_name=$(ecs_lock_corpus_field name)
zstd_corpus_path="$stage/share/ecs/corpus/$zstd_corpus_name"
ecs_build_silesia_corpus "$work" "$zstd_corpus_path" ||
  die 'could not build the fixed Silesia corpus'

nexttrace_asset_name="nexttrace-tiny_linux_${arch}"
nexttrace_asset_url=$(jq -er --arg name "$nexttrace_asset_name" \
  '.assets[] | select(.name == $name) | .browser_download_url' "$nexttrace_release")
nexttrace_asset_digest=$(jq -r --arg name "$nexttrace_asset_name" \
  '.assets[] | select(.name == $name) | .digest // empty' "$nexttrace_release")
[[ -n "$nexttrace_asset_url" ]] || die "official NextTrace release has no $nexttrace_asset_name asset"
nexttrace_download="$work/nexttrace-tiny"
curl "${curl_options[@]}" "$nexttrace_asset_url" -o "$nexttrace_download"
chmod 0755 "$nexttrace_download"
nexttrace_sha=$(sha256sum "$nexttrace_download" | awk '{print $1}')
# A missing digest is a verification failure, not a reason to skip verification.
# NextTrace Tiny is the one prebuilt binary in the package; shipping it without
# a checksum would leave the whole route/backtrace path unverified.
[[ -n "$nexttrace_asset_digest" ]] ||
  die "NextTrace release does not publish a digest for $nexttrace_asset_name"
[[ "$nexttrace_asset_digest" == "sha256:${nexttrace_sha}" ]] ||
  die "NextTrace asset digest disagrees with GitHub: expected $nexttrace_asset_digest, got sha256:$nexttrace_sha"

jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || jobs=2

ecs_tool_build_sysbench
ecs_tool_build_zstd
ecs_tool_build_npb

ecs_tool_build_fio
ecs_tool_build_iperf3
ecs_tool_build_stream
ecs_tool_build_ping

ecs_tool_build_openssl
ecs_tool_build_nexttrace

# Remove compiler/debug sections from binaries built in this job. The official
# NextTrace asset is kept byte-for-byte so its GitHub release digest remains
# independently verifiable.
for tool in "${ECS_TOOL_NAMES[@]}"; do
  [[ "$tool" == nexttrace-tiny ]] && continue
  "$strip_command" --strip-unneeded "$stage/bin/$tool"
done

for tool in "${ECS_TOOL_NAMES[@]}"; do
  chmod 0755 "$stage/bin/$tool"
  [[ -s "$stage/bin/$tool" ]] || die "built $tool is empty"
done

# Preserve upstream license texts in every architecture package. STREAM embeds
# its license in the official source file, so retain the complete header.
cp "$sysbench_src/COPYING" "$stage/LICENSES/SYSBENCH-COPYING"
cp "$zstd_src/LICENSE" "$stage/LICENSES/ZSTD-LICENSE"
cp "$zstd_src/COPYING" "$stage/LICENSES/ZSTD-COPYING"
sed -n '1,31p' "$npb_src/EP/ep.f90" >"$stage/LICENSES/NPB-LICENSE.txt"
cp "$npb_release_root/README" "$stage/LICENSES/NPB-README.txt"
cp "$openssl_src/LICENSE.txt" "$stage/LICENSES/OPENSSL-LICENSE.txt"
cp "$fio_src/COPYING" "$stage/LICENSES/FIO-COPYING"
cp "$iperf3_src/LICENSE" "$stage/LICENSES/IPERF3-LICENSE"
cp "$iputils_src/LICENSE" "$stage/LICENSES/IPUTILS-LICENSE"
cp "$iputils_src/Documentation/LICENSE.GPL2" "$stage/LICENSES/IPUTILS-GPL2"
cp "$nexttrace_src/LICENSE" "$stage/LICENSES/NEXTTRACE-LICENSE"
sed -n '1,/^ \*\/$/p' "$stream_src" >"$stage/LICENSES/STREAM-LICENSE.txt"
chmod 0644 "$stage/LICENSES/"*

sysbench_bin="$stage/bin/sysbench"
zstd_bin="$stage/bin/zstd"
npb_ep_bin="$stage/bin/npb-ep"
npb_ft_bin="$stage/bin/npb-ft"
openssl_bin="$stage/bin/openssl"
stream_bin="$stage/bin/stream"
fio_bin="$stage/bin/fio"
iperf3_bin="$stage/bin/iperf3"
nexttrace_bin="$stage/bin/nexttrace-tiny"
ping_bin="$stage/bin/ping"

# Check all resulting binaries before executing any target program. This
# catches accidental linkage even if a future base image contains excluded
# libraries. The package carries no runtime library directory, so a missing
# dependency or a benchmark-specific runtime library is a hard failure. Do not
# use ldd here: under binfmt/QEMU it executes the target loader and can crash on
# a valid static binary. ELF program and dynamic headers are architecture-safe.
for tool in "${ECS_TOOL_NAMES[@]}"; do
  binary="$stage/bin/$tool"
  "$readelf_command" -dW "$binary" >"$work/${tool}.dynamic" 2>&1 || die "dynamic-header readelf failed for $tool"
  "$readelf_command" -lW "$binary" >"$work/${tool}.program" 2>&1 || die "program-header readelf failed for $tool"
  cat "$work/${tool}.dynamic" "$work/${tool}.program"
  if grep -Eq '\(NEEDED\)' "$work/${tool}.dynamic" ||
    grep -Eq '(^|[[:space:]])INTERP([[:space:]]|$)' "$work/${tool}.program"; then
    cat "$work/${tool}.dynamic" "$work/${tool}.program" >&2
    die "$tool is not fully static"
  fi
  if grep -Eiq 'not found|ceph|rbd|rados|gluster|gfapi|rdma|ibverbs|rdmacm|libaio|liburing|libgomp|libgcc_s' \
      "$work/${tool}.dynamic" "$work/${tool}.program"; then
    cat "$work/${tool}.dynamic" "$work/${tool}.program" >&2
    die "$tool has an unresolved or forbidden runtime dependency"
  fi
done

# Transient NPB Class S smoke binaries are never packaged, but cross jobs still
# require the same static-target guarantee before QEMU executes them.
if [[ "$build_mode" == cross ]]; then
  for npb_smoke_binary in "$npb_ep_smoke_bin" "$npb_ft_smoke_bin"; do
    "$readelf_command" -dW "$npb_smoke_binary" >"$work/npb-smoke.dynamic" 2>&1 ||
      die "dynamic-header readelf failed for $npb_smoke_binary"
    "$readelf_command" -lW "$npb_smoke_binary" >"$work/npb-smoke.program" 2>&1 ||
      die "program-header readelf failed for $npb_smoke_binary"
    if grep -Eq '\(NEEDED\)' "$work/npb-smoke.dynamic" ||
      grep -Eq '(^|[[:space:]])INTERP([[:space:]]|$)' "$work/npb-smoke.program"; then
      cat "$work/npb-smoke.dynamic" "$work/npb-smoke.program" >&2
      die "$npb_smoke_binary is not fully static"
    fi
  done
fi

echo 'running functional binary smoke tests only; benchmark values are not valid performance measurements'
ecs_tool_smoke_sysbench
ecs_tool_smoke_zstd

ecs_tool_smoke_npb

ecs_tool_smoke_openssl

ecs_tool_smoke_stream

ecs_tool_smoke_fio
ecs_tool_smoke_iperf3

ecs_tool_smoke_nexttrace
ecs_tool_smoke_ping

sysbench_source=$(git_source "$sysbench_repository" "$sysbench_commit")
zstd_source=$(git_source "$zstd_repository" "$zstd_commit")
openssl_source=$(git_source "$openssl_repository" "$openssl_commit")
fio_source=$(git_source "$fio_repository" "$fio_commit")
iperf3_source=$(git_source "$iperf3_repository" "$iperf3_commit")
iputils_source=$(git_source "$iputils_repository" "$iputils_commit")
nexttrace_version=$(ecs_lock_tool_field nexttrace-tiny version)
sysbench_version=$(ecs_lock_tool_field sysbench version)
zstd_version=$(ecs_lock_tool_field zstd version)
fio_version=$(ecs_lock_tool_field fio version)
iperf3_version=$(ecs_lock_tool_field iperf3 version)
iputils_version=$(ecs_lock_tool_field ping version)
sysbench_upstream=$(ecs_lock_tool_field sysbench upstream)
zstd_upstream=$(ecs_lock_tool_field zstd upstream)
npb_upstream=$(ecs_lock_tool_field npb-ep upstream)
openssl_upstream=$(ecs_lock_tool_field openssl upstream)
fio_upstream=$(ecs_lock_tool_field fio upstream)
iperf3_upstream=$(ecs_lock_tool_field iperf3 upstream)
stream_upstream=$(ecs_lock_tool_field stream upstream)
nexttrace_upstream=$(ecs_lock_tool_field nexttrace-tiny upstream)
iputils_upstream=$(ecs_lock_tool_field ping upstream)
supported_architectures_json=$(printf '%s\n' "${ECS_ARCHES[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')

jq -n \
  --arg architecture "$arch" \
  --argjson supported_architectures "$supported_architectures_json" \
  --arg toolchain_mode "$build_mode" \
  --arg build_triplet "$build_triplet" \
  --arg target_triplet "$target_triplet" \
  --arg smoke_runner "$smoke_runner" \
  --arg sysbench_version "$sysbench_version" \
  --arg sysbench_tag "$sysbench_tag" \
  --arg sysbench_source "$sysbench_source" \
  --arg sysbench_commit "$sysbench_commit" \
  --arg sysbench_luajit_version "$sysbench_luajit_version" \
  --arg sysbench_ck_version "$sysbench_ck_version" \
  --argjson sysbench_build_flags "$sysbench_build_flags_json" \
  --argjson sysbench_disabled_features "$sysbench_disabled_features_json" \
  --arg zstd_version "$zstd_version" \
  --arg zstd_tag "$zstd_tag" \
  --arg zstd_source "$zstd_source" \
  --arg zstd_commit "$zstd_commit" \
  --argjson zstd_build_flags "$zstd_build_flags_json" \
  --arg zstd_corpus_url "$zstd_corpus_url" \
  --arg zstd_corpus_source_sha "$zstd_corpus_source_sha" \
  --arg zstd_corpus_sha "$zstd_corpus_sha" \
  --argjson zstd_corpus_bytes "$zstd_corpus_bytes" \
  --arg zstd_corpus_name "$zstd_corpus_name" \
  --arg npb_version "$npb_version" \
  --arg npb_tag "$npb_tag" \
  --arg npb_archive_url "$npb_archive_url" \
  --arg npb_archive_sha "$npb_archive_sha" \
  --arg npb_gfortran_version "$npb_gfortran_version" \
  --arg npb_compile_date "$npb_compile_date" \
  --arg npb_flags "$npb_flags" \
  --arg npb_smoke_class "$npb_smoke_class" \
  --argjson npb_build_flags "$npb_build_flags_json" \
  --arg openssl_version "$openssl_version" \
  --arg openssl_tag "$openssl_tag" \
  --arg openssl_source "$openssl_source" \
  --arg openssl_commit "$openssl_commit" \
  --arg openssl_target "$openssl_target" \
  --argjson openssl_build_flags "$openssl_build_flags_json" \
  --arg fio_version "$fio_version" \
  --arg fio_tag "$fio_tag" \
  --arg fio_source "$fio_source" \
  --arg fio_commit "$fio_commit" \
  --arg iperf3_version "$iperf3_version" \
  --arg iperf3_tag "$iperf3_tag" \
  --arg iperf3_source "$iperf3_source" \
  --arg iperf3_commit "$iperf3_commit" \
  --arg stream_version "$stream_version" \
  --arg stream_revision "$stream_revision" \
  --arg stream_url "$stream_url" \
  --arg stream_sha "$stream_sha" \
  --argjson stream_build_flags "$stream_build_flags_json" \
  --argjson stream_array_size "$stream_array_size" \
  --argjson stream_ntimes "$stream_ntimes" \
  --arg iperf3_upstream "$iperf3_upstream" \
  --arg sysbench_upstream "$sysbench_upstream" \
  --arg zstd_upstream "$zstd_upstream" \
  --arg npb_upstream "$npb_upstream" \
  --arg openssl_upstream "$openssl_upstream" \
  --arg fio_upstream "$fio_upstream" \
  --arg stream_upstream "$stream_upstream" \
  --arg nexttrace_asset_source "$nexttrace_asset_url" \
  --arg nexttrace_version "$nexttrace_version" \
  --arg nexttrace_tag "$nexttrace_tag" \
  --arg nexttrace_release_digest "$nexttrace_asset_digest" \
  --arg nexttrace_commit "$nexttrace_commit" \
  --arg iputils_version "$iputils_version" \
  --arg iputils_tag "$iputils_tag" \
  --arg iputils_source "$iputils_source" \
  --arg iputils_commit "$iputils_commit" \
  --arg iputils_upstream "$iputils_upstream" \
  --arg nexttrace_upstream "$nexttrace_upstream" \
  ' {
      schema_version: "ecs-tools.manifest/v1",
      architecture: $architecture,
      supported_architectures: $supported_architectures,
      build: {
        toolchain_mode: $toolchain_mode,
        build_triplet: $build_triplet,
        target_triplet: $target_triplet,
        smoke_runner: $smoke_runner,
        validation: {
          scope: "functional",
          performance_valid: false
        }
      },
      tools: [
        {
          name: "sysbench",
          upstream: $sysbench_upstream,
          version: $sysbench_version,
          tag_or_commit: $sysbench_tag,
          source: $sysbench_source,
          build_flags: $sysbench_build_flags,
          enabled_features: ["cpu", "LuaJIT", "Concurrency Kit"],
          disabled_features: $sysbench_disabled_features,
          architecture: $architecture,
          license: "GPL-2.0-only",
          parameters: {source_commit: $sysbench_commit, configure_supported_flags: $sysbench_build_flags, system_luajit_version: $sysbench_luajit_version, system_ck_version: $sysbench_ck_version, fully_static: true, stripped: true}
        },
        {
          name: "zstd",
          upstream: $zstd_upstream,
          version: $zstd_version,
          tag_or_commit: $zstd_tag,
          source: $zstd_source,
          build_flags: $zstd_build_flags,
          enabled_features: ["benchmark", "multithread", "compression", "decompression"],
          disabled_features: ["zlib", "lzma", "lz4", "legacy-formats", "dictionary-builder", "trace"],
          architecture: $architecture,
          license: "BSD-3-Clause OR GPL-2.0-only",
          parameters: {source_commit: $zstd_commit, level: 3, evaluation_seconds: 5, thread_modes: ["1T", "NT"], corpus_name: $zstd_corpus_name, corpus_path: ("runtime/" + $zstd_corpus_name), corpus_bytes: $zstd_corpus_bytes, corpus_sha256: $zstd_corpus_sha, corpus_source: $zstd_corpus_url, corpus_source_url: $zstd_corpus_url, corpus_source_sha256: $zstd_corpus_source_sha, corpus_construction: "raw concatenation: dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml", fully_static: true, stripped: true}
        },
        {
          name: "npb-ep",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: $npb_tag,
          source: $npb_archive_url,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "EP", "Class A", "OpenMP"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          parameters: {source_sha256: $npb_archive_sha, implementation: "NPB3.4-OMP", benchmark: "EP", problem_class: "A", problem_size: "2^29 random numbers reported", compiler: $npb_gfortran_version, compiler_flags: $npb_flags, linker_flags: $npb_flags, random_generator: "randi8", compile_date: $npb_compile_date, thread_modes: ["1T", "NT"], ci_smoke_class: $npb_smoke_class, ci_smoke_scope: (if $npb_smoke_class == "A" then "release Class A binary" else "transient Class S binary from identical source and target toolchain; release Class A ELF statically validated" end), fully_static: true, stripped: true}
        },
        {
          name: "npb-ft",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: $npb_tag,
          source: $npb_archive_url,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "FT", "Class A", "OpenMP", "3D FFT"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          parameters: {source_sha256: $npb_archive_sha, implementation: "NPB3.4-OMP", benchmark: "FT", problem_class: "A", dimensions: "256x256x128", iterations: 6, compiler: $npb_gfortran_version, compiler_flags: $npb_flags, linker_flags: $npb_flags, random_generator: "randi8", compile_date: $npb_compile_date, thread_modes: ["1T", "NT"], ci_smoke_class: $npb_smoke_class, ci_smoke_scope: (if $npb_smoke_class == "A" then "release Class A binary" else "transient Class S binary from identical source and target toolchain; release Class A ELF statically validated" end), fully_static: true, stripped: true}
        },
        {
          name: "openssl",
          upstream: $openssl_upstream,
          version: $openssl_version,
          tag_or_commit: $openssl_tag,
          source: $openssl_source,
          build_flags: $openssl_build_flags,
          enabled_features: ["speed", "EVP", "AES-256-GCM", "ChaCha20-Poly1305", "SHA-256", "multi-process", "architecture assembly"],
          disabled_features: ["TLS/DTLS/QUIC", "network/HTTP", "shared libraries/modules/engines", "EC/DH/DSA/PQ families", "unrequested cipher/digest families", "tests/documentation"],
          architecture: $architecture,
          license: "Apache-2.0",
          parameters: {source_commit: $openssl_commit, configure_target: $openssl_target, generated_target: "build_generated", build_target: "apps/openssl", algorithms: ["aes-256-gcm", "chacha20-poly1305", "sha256"], block_bytes: 16384, duration_seconds: 5, worker_modes: [1, "detected_cpu_allowance"], elapsed_wall_clock: true, machine_readable: true, capability_detection: "automatic with override environment removed", fully_static: true, stripped: true}
        },
        {
          name: "stream",
          upstream: $stream_upstream,
          version: $stream_version,
          tag_or_commit: $stream_revision,
          source: $stream_url,
          build_flags: $stream_build_flags,
          enabled_features: ["Copy", "Scale", "Add", "Triad", "OpenMP"],
          disabled_features: [],
          architecture: $architecture,
          license: "STREAM-custom",
          parameters: {source_sha256: $stream_sha, array_size: $stream_array_size, ntimes: $stream_ntimes, thread_modes: ["1T", "NT"], run_rules: "official STREAM 5.10 defaults; each array must exceed cache as required; NT uses runtime CPU allowance", fully_static: true, stripped: true}
        },
        {
          name: "fio",
          upstream: $fio_upstream,
          version: $fio_version,
          tag_or_commit: $fio_tag,
          source: $fio_source,
          build_flags: ["--build-static", "--disable-numa", "--disable-rdma", "--disable-rados", "--disable-rbd", "--disable-gfapi", "--disable-http", "--disable-pmem", "--disable-libzbc", "--disable-xnvme", "--disable-libblkio", "--disable-libnfs", "--disable-dfs", "--disable-tcmalloc", "--disable-native", "generated-config: require CONFIG_LIBAIO=y", "generated-config: omit CONFIG_POSIXAIO"],
          enabled_features: ["io_uring", "libaio", "psync"],
          disabled_features: ["ceph", "rbd", "rados", "gluster", "gfapi", "rdma", "posix-aio"],
          architecture: $architecture,
          license: "GPL-2.0-only",
          parameters: {source_commit: $fio_commit, qd: 1, fully_static: true, stripped: true}
        },
        {
          name: "iperf3",
          upstream: $iperf3_upstream,
          version: $iperf3_version,
          tag_or_commit: $iperf3_tag,
          source: $iperf3_source,
          build_flags: ["--enable-static-bin", "--without-sctp", "--without-openssl", "--without-ldconfig", "strip --strip-unneeded"],
          enabled_features: ["tcp", "udp", "ipv4", "ipv6", "parallel", "reverse", "json"],
          disabled_features: ["sctp", "openssl/auth"],
          architecture: $architecture,
          license: "BSD-3-Clause",
          parameters: {source_commit: $iperf3_commit, fully_static: true, stripped: true}
        },
        {
          name: "nexttrace-tiny",
          upstream: $nexttrace_upstream,
          version: $nexttrace_version,
          tag_or_commit: $nexttrace_tag,
          source: $nexttrace_asset_source,
          build_flags: ["official-release-asset", "tiny"],
          enabled_features: ["tiny", "json"],
          disabled_features: ["full", "mtr", "globalping", "webui"],
          architecture: $architecture,
          license: "GPL-3.0-only",
          parameters: {release_commit: $nexttrace_commit, github_asset_digest: $nexttrace_release_digest}
        },
        {
          name: "ping",
          upstream: $iputils_upstream,
          version: $iputils_version,
          tag_or_commit: $iputils_tag,
          source: $iputils_source,
          build_flags: ["LDFLAGS=-static", "meson", "-DBUILD_PING=true", "-DBUILD_ARPING=false", "-DBUILD_CLOCKDIFF=false", "-DBUILD_TRACEPATH=false", "-DUSE_CAP=false", "-DUSE_IDN=false", "-DUSE_GETTEXT=false", "-DNO_SETCAP_OR_SUID=true"],
          enabled_features: ["icmp", "ipv4", "ipv6", "iputils-parser"],
          disabled_features: ["arping", "clockdiff", "tracepath", "capabilities", "idn", "gettext", "setcap"],
          architecture: $architecture,
          license: "GPL-2.0-or-later",
          parameters: {source_commit: $iputils_commit, fully_static: true, stripped: true}
        }
      ]
    }' | jq . >"$stage/manifest.json"

echo "completed real tools stage: $stage"

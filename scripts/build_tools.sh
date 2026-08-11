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

architectures=(amd64 arm64 armv7 386 s390x riscv64 ppc64le)
case " ${architectures[*]} " in
  *" $arch "*) ;;
  *) die "unsupported architecture: ${arch:-<empty>}" ;;
esac
[[ -n "$stage_root" ]] || { usage; exit 2; }
[[ "$stage_root" = /* ]] || die "stage root must be an absolute path"
case "$arch" in
  amd64) openssl_target=linux-x86_64 ;;
  arm64) openssl_target=linux-aarch64 ;;
  armv7) openssl_target=linux-armv4 ;;
  386) openssl_target=linux-x86 ;;
  s390x) openssl_target=linux64-s390x ;;
  riscv64) openssl_target=linux64-riscv64 ;;
  ppc64le) openssl_target=linux-ppc64le ;;
esac
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

download_sha256() {
  local url=$1
  local expected_sha=$2
  local output=$3
  local label=$4
  local attempt actual_sha actual_bytes

  for attempt in 1 2 3; do
    if curl "${curl_options[@]}" "$url" -o "$output"; then
      actual_sha=$(sha256sum "$output" | awk '{print $1}')
      if [[ "$actual_sha" == "$expected_sha" ]]; then
        return 0
      fi
      actual_bytes=$(stat -c %s "$output")
      echo "build-tools: $label SHA-256 mismatch on attempt $attempt/3: expected $expected_sha, got $actual_sha ($actual_bytes bytes)" >&2
    else
      echo "build-tools: $label download failed on attempt $attempt/3" >&2
    fi
    rm -f -- "$output"
  done

  die "$label did not match its pinned SHA-256 after 3 attempts"
}

github_latest_release() {
  local repository=$1
  local output=$2
  curl "${github_api_curl_options[@]}" \
    -H 'Accept: application/vnd.github+json' \
    "https://api.github.com/repos/${repository}/releases/latest" >"$output"
  jq -e '
    (.draft == false) and (.prerelease == false) and
    (.tag_name | type == "string" and length > 0)
  ' "$output" >/dev/null || die "${repository} latest release is not a stable tagged release"
}

clone_release() {
  local repository=$1
  local tag=$2
  local destination=$3
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/${repository}.git" "$destination" >/dev/null
  git -C "$destination" rev-parse HEAD
}

release_version() {
  local tag=$1
  case "$tag" in
    fio-*) printf '%s\n' "${tag#fio-}" ;;
    v*) printf '%s\n' "${tag#v}" ;;
    *) printf '%s\n' "$tag" ;;
  esac
}

git_source() {
  local repository=$1
  local commit=$2
  printf 'git+https://github.com/%s.git@%s\n' "$repository" "$commit"
}

sysbench_release="$work/sysbench-release.json"
fio_release="$work/fio-release.json"
iperf3_release="$work/iperf3-release.json"
nexttrace_release="$work/nexttrace-release.json"
github_latest_release akopytov/sysbench "$sysbench_release"
github_latest_release axboe/fio "$fio_release"
github_latest_release esnet/iperf "$iperf3_release"
github_latest_release nxtrace/NTrace-core "$nexttrace_release"

sysbench_tag=$(jq -er '.tag_name' "$sysbench_release")
fio_tag=$(jq -er '.tag_name' "$fio_release")
iperf3_tag=$(jq -er '.tag_name' "$iperf3_release")
nexttrace_tag=$(jq -er '.tag_name' "$nexttrace_release")

sysbench_src="$work/sysbench"
zstd_src="$work/zstd"
openssl_src="$work/openssl"
fio_src="$work/fio"
iperf3_src="$work/iperf3"
iputils_src="$work/iputils"
nexttrace_src="$work/nexttrace"
sysbench_commit=$(clone_release akopytov/sysbench "$sysbench_tag" "$sysbench_src")
zstd_tag='v1.5.7'
zstd_expected_commit='f8745da6ff1ad1e7bab384bd1f9d742439278e99'
zstd_commit=$(clone_release facebook/zstd "$zstd_tag" "$zstd_src")
[[ "$zstd_commit" == "$zstd_expected_commit" ]] ||
  die "zstd $zstd_tag resolved to $zstd_commit, expected $zstd_expected_commit"
openssl_tag='openssl-3.5.7'
openssl_expected_commit='8cf17aaeb4599f8af87fefd810b5b5fee90fe69e'
openssl_commit=$(clone_release openssl/openssl "$openssl_tag" "$openssl_src")
[[ "$openssl_commit" == "$openssl_expected_commit" ]] ||
  die "OpenSSL $openssl_tag resolved to $openssl_commit, expected $openssl_expected_commit"
fio_commit=$(clone_release axboe/fio "$fio_tag" "$fio_src")
iperf3_commit=$(clone_release esnet/iperf "$iperf3_tag" "$iperf3_src")

# iputils is deliberately built from its official release source rather than
# copied from a distribution package: ECS needs the upstream ping output.
iputils_release="$work/iputils-release.json"
github_latest_release iputils/iputils "$iputils_release"
iputils_tag=$(jq -er '.tag_name' "$iputils_release")
iputils_commit=$(clone_release iputils/iputils "$iputils_tag" "$iputils_src")
nexttrace_commit=$(clone_release nxtrace/NTrace-core "$nexttrace_tag" "$nexttrace_src")

stream_url='https://www.cs.virginia.edu/stream/FTP/Code/stream.c'
stream_src="$work/stream.c"
curl "${curl_options[@]}" "$stream_url" -o "$stream_src"
stream_sha=$(sha256sum "$stream_src" | awk '{print $1}')
stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
[[ -n "$stream_revision" ]] || die "could not read the official STREAM revision from $stream_url"
stream_version=${stream_revision%%-*}

npb_version='3.4.4'
npb_archive_url='https://www.nas.nasa.gov/assets/npb/NPB3.4.4.tar.gz'
npb_archive_sha='1ae219398e02a0a79ad51b7460fcffbf7b5df83a69d5d3d3a9dc2d8acf523549'
npb_archive="$work/NPB3.4.4.tar.gz"
download_sha256 "$npb_archive_url" "$npb_archive_sha" "$npb_archive" \
  'NPB 3.4.4 source archive'
tar -xzf "$npb_archive" -C "$work"
npb_release_root="$work/NPB3.4.4"
npb_src="$npb_release_root/NPB3.4-OMP"
[[ -d "$npb_src/EP" && -d "$npb_src/FT" ]] || die 'NPB archive omitted NPB3.4-OMP EP or FT'

# The corpus is part of the benchmark contract, not an incidental input. The
# ZIP mirror and the concatenated byte stream are both pinned. Rebuilding the
# package therefore fails if the mirror changes any byte or file ordering.
zstd_corpus_url='https://sun.aei.polsl.pl/~sdeor/corpus/silesia.zip'
zstd_corpus_source_sha='0626e25f45c0ffb5dc801f13b7c82a3b75743ba07e3a71835a41e3d9f63c77af'
zstd_corpus_sha='8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0'
zstd_corpus_bytes=211938580
zstd_corpus_name='ecs-silesia-v1.corpus'
zstd_corpus_order=(dickens mozilla mr nci ooffice osdb reymont samba sao webster x-ray xml)
zstd_corpus_zip="$work/silesia.zip"
zstd_corpus_dir="$work/silesia"
zstd_corpus_path="$stage/share/ecs/corpus/$zstd_corpus_name"
download_sha256 "$zstd_corpus_url" "$zstd_corpus_source_sha" "$zstd_corpus_zip" \
  'Silesia source ZIP'
unzip -tq "$zstd_corpus_zip" >/dev/null ||
  die 'Silesia download is not a valid ZIP archive'
mkdir -p "$zstd_corpus_dir"
unzip -q "$zstd_corpus_zip" -d "$zstd_corpus_dir"
: >"$zstd_corpus_path"
for corpus_member in "${zstd_corpus_order[@]}"; do
  [[ -f "$zstd_corpus_dir/$corpus_member" ]] || die "Silesia ZIP omitted $corpus_member"
  cat "$zstd_corpus_dir/$corpus_member" >>"$zstd_corpus_path"
done
[[ "$(stat -c %s "$zstd_corpus_path")" -eq "$zstd_corpus_bytes" ]] ||
  die 'fixed Silesia corpus byte length mismatch'
[[ "$(sha256sum "$zstd_corpus_path" | awk '{print $1}')" == "$zstd_corpus_sha" ]] ||
  die 'fixed Silesia corpus SHA-256 mismatch'
chmod 0644 "$zstd_corpus_path"

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
if [[ -n "$nexttrace_asset_digest" ]]; then
  [[ "$nexttrace_asset_digest" == "sha256:${nexttrace_sha}" ]] ||
    die "NextTrace asset digest disagrees with GitHub: expected $nexttrace_asset_digest, got sha256:$nexttrace_sha"
fi

jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || jobs=2

echo "building sysbench ${sysbench_tag} (${sysbench_commit})"
# Build the upstream CPU benchmark with the target container's static LuaJIT
# and Concurrency Kit development libraries. The bundled copies do not cover
# all seven release architectures; using the system libraries keeps sysbench's
# benchmark implementation intact while allowing the real target toolchain to
# build every package. The runtime package still contains only this final
# sysbench binary, never a distribution-provided benchmark executable.
(
  cd "$sysbench_src"
  ./autogen.sh
  ./configure --help >"$work/sysbench-configure-help.txt"
)

# Sysbench has carried different database-driver switches across stable
# releases. Inspect the exact checkout fetched above and pass only switches
# which its generated configure help or configure.ac declares. The same
# discovered list is written to the manifest, so metadata cannot drift from
# the command that configured this binary.
sysbench_configure_args=(
  "${configure_cross_args[@]}"
  "--prefix=$work/sysbench-prefix"
  '--with-system-luajit'
  '--with-system-ck'
  '--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed'
)
sysbench_luajit_version=$(pkg-config --modversion luajit) ||
  die 'target container is missing the LuaJIT pkg-config metadata'
sysbench_ck_version=$(pkg-config --modversion ck) ||
  die 'target container is missing the Concurrency Kit pkg-config metadata'
sysbench_manifest_flags=(
  "${configure_cross_args[@]}"
  '--with-system-luajit'
  '--with-system-ck'
  '--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed'
)
sysbench_disabled_features=('database-drivers')
for driver in mysql pgsql drizzle attachsql oracle; do
  option="--without-${driver}"
  if grep -Fq -- "$option" "$work/sysbench-configure-help.txt" ||
    grep -Eq "AC_ARG_WITH[[:space:]]*\\([[:space:]]*\\[?${driver}(\\]|,|[[:space:]])" \
      "$sysbench_src/configure.ac"; then
    sysbench_configure_args+=("$option")
    sysbench_manifest_flags+=("$option")
    sysbench_disabled_features+=("$driver")
  else
    sysbench_disabled_features+=("${driver}-driver-not-supported-by-upstream")
  fi
done
sysbench_build_flags_json=$(printf '%s\n' "${sysbench_manifest_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
sysbench_disabled_features_json=$(printf '%s\n' "${sysbench_disabled_features[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')

(
  cd "$sysbench_src"
  ./configure "${sysbench_configure_args[@]}"
  make -j"$jobs"
)
cp "$sysbench_src/src/sysbench" "$stage/bin/sysbench"

echo "building zstd ${zstd_tag} (${zstd_commit})"
zstd_build_flags=(
  "$cc_command"
  '-O3'
  '-static'
  '-static-libgcc'
  '-DZSTD_NODICT'
  '-DZSTD_NOTRACE'
  'HAVE_ZLIB=0'
  'HAVE_LZMA=0'
  'HAVE_LZ4=0'
  'ZSTD_LEGACY_SUPPORT=0'
)
zstd_build_flags_json=$(printf '%s\n' "${zstd_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
make -C "$zstd_src/programs" -j"$jobs" zstd-release \
  CC="$cc_command" \
  MOREFLAGS='-O3 -static -static-libgcc -DZSTD_NODICT -DZSTD_NOTRACE' \
  HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
cp "$zstd_src/programs/zstd" "$stage/bin/zstd"

echo "building NPB ${npb_version} OpenMP EP + FT Class A"
npb_flags='-O3 -fopenmp -static'
npb_compile_date=$(date -u -d "@$SOURCE_DATE_EPOCH" '+%d %b %Y')
npb_build_flags=(
  "$fc_command"
  '-O3'
  '-fopenmp'
  '-static'
  'CLASS=A'
  'RAND=randi8'
  'OMP'
)
npb_build_flags_json=$(printf '%s\n' "${npb_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
npb_gfortran_version=$("$fc_command" --version | sed -n '1p')
cat >"$npb_src/config/make.def" <<EOF
FC = $fc_command
FLINK = $fc_command
F_LIB =
F_INC =
FFLAGS = $npb_flags
FLINKFLAGS = $npb_flags
CC = $cc_command
CLINK = $cc_command
C_LIB = -lm
C_INC =
CFLAGS = $npb_flags
CLINKFLAGS = $npb_flags
UCC = gcc
BINDIR = ../bin
RAND = randi8
WTIME = wtime.c
EOF
mkdir -p "$npb_src/bin"
make -C "$npb_src/sys" all

configure_npb_class() {
  local benchmark=$1
  local class=$2
  local benchmark_lower=${benchmark,,}
  local params="$npb_src/$benchmark/npbparams.h"
  (
    cd "$npb_src/$benchmark"
    ../sys/setparams "$benchmark_lower" "$class"
  )
  sed -i "s@parameter (compiletime='[^']*')@parameter (compiletime='$npb_compile_date')@" "$params"
  grep -F "parameter (compiletime='$npb_compile_date')" "$params" >/dev/null ||
    die "could not pin NPB compile date in $params"
}

# The release contains only the requested EP/FT Class A binaries.  Build them
# completely with the target compiler before any target executable is handed
# to QEMU.
configure_npb_class EP A
configure_npb_class FT A
make -C "$npb_src" -j"$jobs" ep CLASS=A
make -C "$npb_src" -j"$jobs" ft CLASS=A
cp "$npb_src/bin/ep.A.x" "$stage/bin/npb-ep"
cp "$npb_src/bin/ft.A.x" "$stage/bin/npb-ft"

npb_smoke_class=A
npb_ep_smoke_bin="$stage/bin/npb-ep"
npb_ft_smoke_bin="$stage/bin/npb-ft"
if [[ "$build_mode" == cross ]]; then
  # Class A is the released benchmark contract, but running that workload
  # under instruction emulation would turn a functional CI check into a slow,
  # meaningless performance run.  Build transient Class S binaries from the
  # same source, flags and target toolchain; QEMU runs those to completion.
  # The packaged Class A ELFs are still checked separately below.
  npb_smoke_class=S
  configure_npb_class EP S
  make -C "$npb_src" -j"$jobs" ep CLASS=S
  configure_npb_class FT S
  make -C "$npb_src" -j"$jobs" ft CLASS=S
  npb_ep_smoke_bin="$work/npb-ep-class-s-smoke"
  npb_ft_smoke_bin="$work/npb-ft-class-s-smoke"
  cp "$npb_src/bin/ep.S.x" "$npb_ep_smoke_bin"
  cp "$npb_src/bin/ft.S.x" "$npb_ft_smoke_bin"
  "$strip_command" --strip-unneeded "$npb_ep_smoke_bin" "$npb_ft_smoke_bin"
fi

echo "building fio ${fio_tag} (${fio_commit})"
(
  cd "$fio_src"
  ./configure \
    "${fio_cross_args[@]}" \
    --prefix="$work/fio-prefix" \
    --build-static \
    --disable-numa \
    --disable-rdma \
    --disable-rados \
    --disable-rbd \
    --disable-gfapi \
    --disable-http \
    --disable-pmem \
    --disable-libzbc \
    --disable-xnvme \
    --disable-libblkio \
    --disable-libnfs \
    --disable-dfs \
    --disable-tcmalloc \
    --disable-native
  # Do not assume a particular fio version exposes a POSIX-AIO configure
  # switch. Remove only generated POSIX-AIO symbols, leaving upstream source
  # and Makefile intact; ECS deliberately ships io_uring, libaio, and psync.
  sed -i '/^CONFIG_POSIXAIO=y$/d; /^CONFIG_POSIXAIO_FSYNC=y$/d' config-host.mak
  if grep -Eq '^CONFIG_(RDMA|RADOS|RBD|GFAPI|POSIXAIO)=y$' config-host.mak; then
    echo 'fio generated configuration enabled an excluded engine' >&2
    grep -E '^CONFIG_(RDMA|RADOS|RBD|GFAPI|POSIXAIO)=y$' config-host.mak >&2
    exit 1
  fi
  grep -Eq '^CONFIG_LIBAIO=y$' config-host.mak || {
    echo 'fio configure did not enable libaio' >&2
    exit 1
  }
  make -j"$jobs"
)
cp "$fio_src/fio" "$stage/bin/fio"

echo "building iperf3 ${iperf3_tag} (${iperf3_commit})"
(
  cd "$iperf3_src"
  ./configure \
    "${configure_cross_args[@]}" \
    --prefix="$work/iperf3-prefix" \
    --enable-static-bin \
    --without-sctp \
    --without-openssl \
    --without-ldconfig
  make -j"$jobs"
)
cp "$iperf3_src/src/iperf3" "$stage/bin/iperf3"

echo "building official STREAM ${stream_revision}"
# STREAM 5.10's official source defaults are 10,000,000 elements and 10
# iterations.  Keep those defaults explicit in the command and manifest so a
# cache-sized/short-run build cannot silently change the benchmark semantics.
stream_array_size=10000000
stream_ntimes=10
stream_build_flags=(
  "$cc_command" -O3 -fopenmp -static -static-libgcc
  "-DSTREAM_ARRAY_SIZE=$stream_array_size" "-DNTIMES=$stream_ntimes"
)
stream_build_flags_json=$(printf '%s\n' "${stream_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
"${stream_build_flags[@]}" "$stream_src" -o "$stage/bin/stream"

echo "building iputils ping ${iputils_tag} (${iputils_commit})"
ping_build="$work/iputils-build"
ping_install="$work/iputils-install"
LDFLAGS='-static' meson setup "$ping_build" "$iputils_src" \
  "${meson_cross_args[@]}" \
  --prefix=/usr/local \
  --buildtype=release \
  -DUSE_CAP=false \
  -DUSE_IDN=false \
  -DUSE_GETTEXT=false \
  -DBUILD_ARPING=false \
  -DBUILD_CLOCKDIFF=false \
  -DBUILD_PING=true \
  -DBUILD_TRACEPATH=false \
  -DBUILD_MANS=false \
  -DBUILD_HTML_MANS=false \
  -DSKIP_TESTS=true \
  -DNO_SETCAP_OR_SUID=true
meson compile -C "$ping_build" -j "$jobs"
meson install -C "$ping_build" --destdir "$ping_install"
cp "$ping_install/usr/local/bin/ping" "$stage/bin/ping"

echo "building OpenSSL ${openssl_tag} (${openssl_commit})"
openssl_version='3.5.7'
openssl_prefix='/opt/ecs-openssl'
openssl_build_flags=(
  "$openssl_target"
  '-O3'
  'no-shared'
  'no-module'
  'no-pinshared'
  'no-tests'
  'no-docs'
  'no-ssl'
  'no-sock'
  'no-dgram'
  'no-http'
  'no-cmp'
  'no-cms'
  'no-ct'
  'no-ocsp'
  'no-dso'
  'no-engine'
  'no-static-engine'
  'no-legacy'
  'no-async'
  'no-atexit'
  'no-autoload-config'
  'no-cached-fetch'
  'no-comp'
  'no-dh'
  'no-dsa'
  'no-ec'
  'no-aria'
  'no-bf'
  'no-blake2'
  'no-camellia'
  'no-cast'
  'no-cmac'
  'no-des'
  'no-idea'
  'no-md4'
  'no-mdc2'
  'no-ocb'
  'no-rc2'
  'no-rc4'
  'no-rmd160'
  'no-scrypt'
  'no-seed'
  'no-siphash'
  'no-siv'
  'no-sm2'
  'no-sm3'
  'no-sm4'
  'no-whirlpool'
  'no-ml-dsa'
  'no-ml-kem'
  'no-slh-dsa'
  'no-rfc3779'
  'no-srp'
  'no-srtp'
  'no-ts'
  '-static'
  "--prefix=$openssl_prefix"
  "--openssldir=$openssl_prefix/ssl"
)
openssl_build_flags_json=$(printf '%s\n' "${openssl_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
(
  cd "$openssl_src"
  perl ./Configure "${openssl_build_flags[@]}"
  # OpenSSL's individual executable target does not depend on its mandatory
  # generated headers. Generate only those first, then build the official CLI
  # target and its exact link dependencies. The Configure exclusions above
  # leave speed/version plus the EVP families required by ECS.
  make -j"$jobs" build_generated
  make -j"$jobs" apps/openssl
)
cp "$openssl_src/apps/openssl" "$stage/bin/openssl"

cp "$nexttrace_download" "$stage/bin/nexttrace-tiny"

# Remove compiler/debug sections from binaries built in this job. The official
# NextTrace asset is kept byte-for-byte so its GitHub release digest remains
# independently verifiable.
for tool in sysbench zstd npb-ep npb-ft stream fio iperf3 openssl ping; do
  "$strip_command" --strip-unneeded "$stage/bin/$tool"
done

for tool in sysbench zstd npb-ep npb-ft stream fio iperf3 openssl nexttrace-tiny ping; do
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
for tool in sysbench zstd npb-ep npb-ft stream fio iperf3 openssl nexttrace-tiny ping; do
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
run_target "$sysbench_bin" --version
run_target "$sysbench_bin" cpu --cpu-max-prime=1000 --threads=1 run >"$work/sysbench-smoke.txt"
grep -Eq 'events per second|total time' "$work/sysbench-smoke.txt" || {
  cat "$work/sysbench-smoke.txt" >&2
  die 'sysbench CPU smoke output was not recognized'
}

run_target "$zstd_bin" --version >"$work/zstd-version.txt" 2>&1
grep -Eq 'v1\.5\.7([^0-9]|$)' "$work/zstd-version.txt" || {
  cat "$work/zstd-version.txt" >&2
  die 'zstd version smoke did not report v1.5.7'
}
head -c 1048576 "$zstd_corpus_path" >"$work/zstd-smoke.corpus"
run_target "$zstd_bin" -q -b3 -i1 -T1 "$work/zstd-smoke.corpus" >"$work/zstd-smoke.txt" 2>&1
grep -Eq 'bench 1\.5\.7.*input 1048576 bytes, 1 seconds' "$work/zstd-smoke.txt" || {
  cat "$work/zstd-smoke.txt" >&2
  die 'zstd benchmark smoke output was not recognized'
}
grep -Eq '^-3[[:space:]].*MB/s[[:space:]].*MB/s' "$work/zstd-smoke.txt" || {
  cat "$work/zstd-smoke.txt" >&2
  die 'zstd benchmark smoke omitted compression/decompression throughput'
}

mkdir -p "$work/npb-smoke-run"
for npb_benchmark in EP FT; do
  case "$npb_benchmark" in
    EP) npb_smoke_bin=$npb_ep_smoke_bin ;;
    FT) npb_smoke_bin=$npb_ft_smoke_bin ;;
  esac
  (
    cd "$work/npb-smoke-run"
    OMP_NUM_THREADS=1 \
      OMP_DYNAMIC=FALSE \
      OMP_PROC_BIND=close \
      OMP_PLACES=cores \
      OMP_SCHEDULE=static \
      OMP_DISPLAY_ENV=FALSE \
      NPB_TIMER_FLAG=0 \
      run_target "$npb_smoke_bin"
  ) >"$work/npb-${npb_benchmark,,}-smoke.txt" 2>&1
  npb_smoke_output="$work/npb-${npb_benchmark,,}-smoke.txt"
  grep -Eq "NAS Parallel Benchmarks \\(NPB3\\.4-OMP\\) - ${npb_benchmark} Benchmark" "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke omitted the official header"
  grep -Eq "^[[:space:]]*Class[[:space:]]*=[[:space:]]*${npb_smoke_class}[[:space:]]*$" "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke did not run Class ${npb_smoke_class}"
  grep -Eq '^[[:space:]]*Total threads[[:space:]]*=[[:space:]]*1[[:space:]]*$' "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke did not use one OpenMP thread"
  grep -Eq '^[[:space:]]*Verification[[:space:]]*=[[:space:]]*SUCCESSFUL[[:space:]]*$' "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} Class ${npb_smoke_class} verification failed"
  grep -Eq '^[[:space:]]*Version[[:space:]]*=[[:space:]]*3\.4\.4[[:space:]]*$' "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke reported the wrong version"
  grep -Eq '^[[:space:]]*FC[[:space:]]*=[[:space:]]*.*gfortran[[:space:]]*$' "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke reported the wrong target compiler"
  grep -F 'FFLAGS       = -O3 -fopenmp -static' "$npb_smoke_output" >/dev/null ||
    die "NPB ${npb_benchmark} smoke reported unexpected compiler flags"
  grep -Eq '^[[:space:]]*RAND[[:space:]]*=[[:space:]]*randi8[[:space:]]*$' "$npb_smoke_output" ||
    die "NPB ${npb_benchmark} smoke reported the wrong random generator"
done

run_target "$openssl_bin" version >"$work/openssl-version.txt" 2>&1
grep -Eq '^OpenSSL 3\.5\.7([[:space:]]|$)' "$work/openssl-version.txt" || {
  cat "$work/openssl-version.txt" >&2
  die 'OpenSSL version smoke did not report 3.5.7'
}
mkdir -p "$work/openssl-smoke/modules" "$work/openssl-smoke/engines"
for openssl_algorithm in aes-256-gcm chacha20-poly1305 sha256; do
  case "$openssl_algorithm" in
    aes-256-gcm) openssl_output_name='AES-256-GCM'; openssl_aead=(-aead) ;;
    chacha20-poly1305) openssl_output_name='ChaCha20-Poly1305'; openssl_aead=(-aead) ;;
    sha256) openssl_output_name='sha256'; openssl_aead=() ;;
  esac
  OPENSSL_CONF=/dev/null \
    OPENSSL_MODULES="$work/openssl-smoke/modules" \
    OPENSSL_ENGINES="$work/openssl-smoke/engines" \
    run_target "$openssl_bin" speed \
      -elapsed -seconds 1 -bytes 16384 -mr -multi 1 \
      -evp "$openssl_algorithm" "${openssl_aead[@]}" \
      >"$work/openssl-${openssl_algorithm}-smoke.txt" 2>&1
  openssl_smoke_output="$work/openssl-${openssl_algorithm}-smoke.txt"
  grep -F "+DT:${openssl_output_name}:1:16384" "$openssl_smoke_output" >/dev/null ||
    die "OpenSSL speed ${openssl_algorithm} smoke omitted fixed parameters"
  grep -Eq "^\\+F:[0-9]+:${openssl_output_name}:[0-9]+(\\.[0-9]+)?[[:space:]]*$" "$openssl_smoke_output" ||
    die "OpenSSL speed ${openssl_algorithm} smoke omitted aggregate machine-readable throughput"
done

stream_nt_threads=${STREAM_NT_THREADS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$stream_nt_threads" =~ ^[1-9][0-9]*$ ]] || die "invalid STREAM_NT_THREADS=$stream_nt_threads"
for stream_threads in 1 "$stream_nt_threads"; do
  stream_context=nt
  [[ "$stream_threads" -eq 1 ]] && stream_context=1t
  OMP_NUM_THREADS="$stream_threads" run_target "$stream_bin" >"$work/stream-${stream_context}.txt"
done
cat "$work/stream-1t.txt" "$work/stream-nt.txt"
for stream_context in 1t nt; do
  for kernel in Copy Scale Add Triad; do
    grep -q "${kernel}:" "$work/stream-${stream_context}.txt" ||
      die "STREAM ${stream_context} output omitted ${kernel}"
  done
  grep -q 'Solution Validates' "$work/stream-${stream_context}.txt" ||
    die "STREAM ${stream_context} validation failed"
done

dd if=/dev/zero of="$work/fio-smoke.data" bs=4096 count=1 status=none
fio_json="$work/fio-smoke.json"
run_target "$fio_bin" \
  --name=ecs-smoke \
  --filename="$work/fio-smoke.data" \
  --rw=read \
  --bs=4k \
  --size=4k \
  --ioengine=psync \
  --iodepth=1 \
  --numjobs=1 \
  --direct=1 \
  --output-format=json \
  --output="$fio_json"
jq -e '(.jobs | length == 1) and .jobs[0].jobname == "ecs-smoke"' "$fio_json" >/dev/null || {
  cat "$fio_json" >&2
  die 'fio JSON/QD1 smoke failed'
}
fio_io_uring_json="$work/fio-io-uring-smoke.json"
fio_io_uring_error="$work/fio-io-uring-smoke.err"
fio_io_uring_output="$work/fio-io-uring-smoke.out"
if run_target "$fio_bin" \
  --name=ecs-io-uring-smoke \
  --filename="$work/fio-smoke.data" \
  --rw=read \
  --bs=4k \
  --size=4k \
  --ioengine=io_uring \
  --iodepth=1 \
  --numjobs=1 \
  --direct=1 \
  --output-format=json \
  --output="$fio_io_uring_json" \
  >"$fio_io_uring_output" 2>"$fio_io_uring_error"; then
  jq -e '(.jobs | length == 1) and .jobs[0].jobname == "ecs-io-uring-smoke"' \
    "$fio_io_uring_json" >/dev/null || {
    cat "$fio_io_uring_json" "$fio_io_uring_error" >&2
    die 'fio io_uring JSON/QD1 smoke failed'
  }
else
  if grep -Eiq "kernel.*support.*io_uring|io_uring[^[:alpha:]]+(is )?not supported" \
    "$fio_io_uring_error" "$fio_io_uring_output"; then
    echo 'fio io_uring runtime smoke skipped: the architecture test kernel does not support io_uring'
  else
    cat "$fio_io_uring_error" "$fio_io_uring_output" "$fio_io_uring_json" 2>/dev/null >&2 || true
    die 'fio io_uring runtime smoke failed for a reason other than kernel support'
  fi
fi
run_target "$fio_bin" --version
run_target "$fio_bin" --enghelp >"$work/fio-engines.txt"
for required_engine in io_uring libaio psync; do
  grep -Eiq "(^|[^[:alnum:]_])${required_engine}([^[:alnum:]_]|$)" "$work/fio-engines.txt" || {
    cat "$work/fio-engines.txt" >&2
    die "fio omitted required engine: $required_engine"
  }
done
if grep -Eiq '(^|[^[:alnum:]])(rados|rbd|gfapi|gluster|rdma)([^[:alnum:]]|$)' "$work/fio-engines.txt"; then
  cat "$work/fio-engines.txt" >&2
  die 'fio exposed a disabled external engine'
fi

run_target "$iperf3_bin" --version
iperf_port=$((42000 + (${RANDOM:-1} % 1000)))
run_target "$iperf3_bin" -s -1 -p "$iperf_port" >"$work/iperf3-server.txt" 2>&1 &
iperf_server=$!
stop_iperf_server() {
  if [[ -n "${iperf_server:-}" ]]; then
    kill "$iperf_server" 2>/dev/null || true
    wait "$iperf_server" 2>/dev/null || true
    iperf_server=""
  fi
}
trap 'stop_iperf_server; cleanup' EXIT
iperf_json="$work/iperf3-smoke.json"
iperf_client_error="$work/iperf3-client.txt"
iperf_client_status=1
for _ in {1..50}; do
  if run_target "$iperf3_bin" -J -c 127.0.0.1 -p "$iperf_port" -t 1 -P 1 \
    >"$iperf_json" 2>"$iperf_client_error"; then
    iperf_client_status=0
    break
  fi
  if ! kill -0 "$iperf_server" 2>/dev/null; then
    break
  fi
  sleep 0.1
done
if ((iperf_client_status != 0)); then
  cat "$work/iperf3-server.txt" "$iperf_client_error" >&2
  stop_iperf_server
  die 'iperf3 loopback smoke failed'
fi
wait "$iperf_server" || true
iperf_server=""
trap cleanup EXIT
jq -e 'type == "object" and (.start | type == "object") and (.end | type == "object")' \
  "$iperf_json" >/dev/null || {
  cat "$work/iperf3-server.txt" "$iperf_client_error" >&2
  cat "$iperf_json" >&2
  die 'iperf3 JSON parser smoke failed'
}

run_target "$nexttrace_bin" --help >"$work/nexttrace-help.txt" 2>&1 || {
  cat "$work/nexttrace-help.txt" >&2
  die 'NextTrace help failed'
}
grep -Eq -- '--(json|raw)' "$work/nexttrace-help.txt" || {
  cat "$work/nexttrace-help.txt" >&2
  die 'NextTrace Tiny help omitted the JSON/raw flag'
}

run_target "$ping_bin" -V
run_target "$ping_bin" -c 1 -W 1 127.0.0.1 >"$work/ping-smoke.txt"
grep -Eq '1 packets transmitted|1 packets received|1 received' "$work/ping-smoke.txt" || {
  cat "$work/ping-smoke.txt" >&2
  die 'iputils ping loopback smoke failed'
}

sysbench_sha=$(sha256sum "$sysbench_bin" | awk '{print $1}')
zstd_sha=$(sha256sum "$zstd_bin" | awk '{print $1}')
npb_ep_sha=$(sha256sum "$npb_ep_bin" | awk '{print $1}')
npb_ft_sha=$(sha256sum "$npb_ft_bin" | awk '{print $1}')
openssl_sha=$(sha256sum "$openssl_bin" | awk '{print $1}')
stream_binary_sha=$(sha256sum "$stream_bin" | awk '{print $1}')
fio_sha=$(sha256sum "$fio_bin" | awk '{print $1}')
iperf3_sha=$(sha256sum "$iperf3_bin" | awk '{print $1}')
nexttrace_binary_sha=$(sha256sum "$nexttrace_bin" | awk '{print $1}')
ping_sha=$(sha256sum "$ping_bin" | awk '{print $1}')

for digest in "$sysbench_sha" "$zstd_sha" "$npb_ep_sha" "$npb_ft_sha" "$openssl_sha" "$stream_binary_sha" "$fio_sha" "$iperf3_sha" \
  "$nexttrace_binary_sha" "$ping_sha"; do
  [[ "$digest" =~ ^[[:xdigit:]]{64}$ ]] || die "invalid binary SHA-256: $digest"
done

sysbench_source=$(git_source akopytov/sysbench "$sysbench_commit")
zstd_source=$(git_source facebook/zstd "$zstd_commit")
openssl_source=$(git_source openssl/openssl "$openssl_commit")
fio_source=$(git_source axboe/fio "$fio_commit")
iperf3_source=$(git_source esnet/iperf "$iperf3_commit")
iputils_source=$(git_source iputils/iputils "$iputils_commit")
nexttrace_version=$(release_version "$nexttrace_tag")
sysbench_version=$(release_version "$sysbench_tag")
zstd_version=$(release_version "$zstd_tag")
fio_version=$(release_version "$fio_tag")
iperf3_version=$(release_version "$iperf3_tag")
iputils_version=$(release_version "$iputils_tag")

jq -n \
  --arg architecture "$arch" \
  --arg toolchain_mode "$build_mode" \
  --arg build_triplet "$build_triplet" \
  --arg target_triplet "$target_triplet" \
  --arg smoke_runner "$smoke_runner" \
  --arg sysbench_version "$sysbench_version" \
  --arg sysbench_tag "$sysbench_tag" \
  --arg sysbench_source "$sysbench_source" \
  --arg sysbench_sha "$sysbench_sha" \
  --arg sysbench_commit "$sysbench_commit" \
  --arg sysbench_luajit_version "$sysbench_luajit_version" \
  --arg sysbench_ck_version "$sysbench_ck_version" \
  --argjson sysbench_build_flags "$sysbench_build_flags_json" \
  --argjson sysbench_disabled_features "$sysbench_disabled_features_json" \
  --arg zstd_version "$zstd_version" \
  --arg zstd_tag "$zstd_tag" \
  --arg zstd_source "$zstd_source" \
  --arg zstd_sha "$zstd_sha" \
  --arg zstd_commit "$zstd_commit" \
  --argjson zstd_build_flags "$zstd_build_flags_json" \
  --arg zstd_corpus_url "$zstd_corpus_url" \
  --arg zstd_corpus_source_sha "$zstd_corpus_source_sha" \
  --arg zstd_corpus_sha "$zstd_corpus_sha" \
  --argjson zstd_corpus_bytes "$zstd_corpus_bytes" \
  --arg zstd_corpus_name "$zstd_corpus_name" \
  --arg npb_version "$npb_version" \
  --arg npb_archive_url "$npb_archive_url" \
  --arg npb_archive_sha "$npb_archive_sha" \
  --arg npb_ep_sha "$npb_ep_sha" \
  --arg npb_ft_sha "$npb_ft_sha" \
  --arg npb_gfortran_version "$npb_gfortran_version" \
  --arg npb_compile_date "$npb_compile_date" \
  --arg npb_flags "$npb_flags" \
  --arg npb_smoke_class "$npb_smoke_class" \
  --argjson npb_build_flags "$npb_build_flags_json" \
  --arg openssl_version "$openssl_version" \
  --arg openssl_tag "$openssl_tag" \
  --arg openssl_source "$openssl_source" \
  --arg openssl_sha "$openssl_sha" \
  --arg openssl_commit "$openssl_commit" \
  --arg openssl_target "$openssl_target" \
  --argjson openssl_build_flags "$openssl_build_flags_json" \
  --arg fio_version "$fio_version" \
  --arg fio_tag "$fio_tag" \
  --arg fio_source "$fio_source" \
  --arg fio_sha "$fio_sha" \
  --arg fio_commit "$fio_commit" \
  --arg iperf3_version "$iperf3_version" \
  --arg iperf3_tag "$iperf3_tag" \
  --arg iperf3_source "$iperf3_source" \
  --arg iperf3_sha "$iperf3_sha" \
  --arg iperf3_commit "$iperf3_commit" \
  --arg stream_version "$stream_version" \
  --arg stream_revision "$stream_revision" \
  --arg stream_url "$stream_url" \
  --arg stream_sha "$stream_sha" \
  --arg stream_binary_sha "$stream_binary_sha" \
  --argjson stream_build_flags "$stream_build_flags_json" \
  --argjson stream_array_size "$stream_array_size" \
  --argjson stream_ntimes "$stream_ntimes" \
  --arg iperf3_upstream 'https://github.com/esnet/iperf' \
  --arg sysbench_upstream 'https://github.com/akopytov/sysbench' \
  --arg zstd_upstream 'https://github.com/facebook/zstd' \
  --arg npb_upstream 'https://www.nas.nasa.gov/software/npb.html' \
  --arg openssl_upstream 'https://github.com/openssl/openssl' \
  --arg fio_upstream 'https://github.com/axboe/fio' \
  --arg stream_upstream 'https://www.cs.virginia.edu/stream/' \
  --arg nexttrace_asset_source "$nexttrace_asset_url" \
  --arg nexttrace_version "$nexttrace_version" \
  --arg nexttrace_tag "$nexttrace_tag" \
  --arg nexttrace_sha "$nexttrace_binary_sha" \
  --arg nexttrace_release_digest "$nexttrace_asset_digest" \
  --arg nexttrace_commit "$nexttrace_commit" \
  --arg iputils_version "$iputils_version" \
  --arg iputils_tag "$iputils_tag" \
  --arg iputils_source "$iputils_source" \
  --arg iputils_sha "$ping_sha" \
  --arg iputils_commit "$iputils_commit" \
  --arg iputils_upstream 'https://github.com/iputils/iputils' \
  --arg nexttrace_upstream 'https://github.com/nxtrace/NTrace-core' \
  ' {
      schema_version: "ecs-tools.manifest/v1",
      architecture: $architecture,
      supported_architectures: ["amd64", "arm64", "armv7", "386", "s390x", "riscv64", "ppc64le"],
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
          sha256: $sysbench_sha,
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
          sha256: $zstd_sha,
          parameters: {source_commit: $zstd_commit, level: 3, evaluation_seconds: 5, thread_modes: ["1T", "NT"], corpus_name: $zstd_corpus_name, corpus_path: ("runtime/" + $zstd_corpus_name), corpus_bytes: $zstd_corpus_bytes, corpus_sha256: $zstd_corpus_sha, corpus_source: $zstd_corpus_url, corpus_source_url: $zstd_corpus_url, corpus_source_sha256: $zstd_corpus_source_sha, corpus_construction: "raw concatenation: dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml", fully_static: true, stripped: true}
        },
        {
          name: "npb-ep",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: "NPB3.4.4",
          source: $npb_archive_url,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "EP", "Class A", "OpenMP"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          sha256: $npb_ep_sha,
          parameters: {source_sha256: $npb_archive_sha, implementation: "NPB3.4-OMP", benchmark: "EP", problem_class: "A", problem_size: "2^29 random numbers reported", compiler: $npb_gfortran_version, compiler_flags: $npb_flags, linker_flags: $npb_flags, random_generator: "randi8", compile_date: $npb_compile_date, thread_modes: ["1T", "NT"], ci_smoke_class: $npb_smoke_class, ci_smoke_scope: (if $npb_smoke_class == "A" then "release Class A binary" else "transient Class S binary from identical source and target toolchain; release Class A ELF statically validated" end), fully_static: true, stripped: true}
        },
        {
          name: "npb-ft",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: "NPB3.4.4",
          source: $npb_archive_url,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "FT", "Class A", "OpenMP", "3D FFT"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          sha256: $npb_ft_sha,
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
          sha256: $openssl_sha,
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
          sha256: $stream_binary_sha,
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
          sha256: $fio_sha,
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
          sha256: $iperf3_sha,
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
          sha256: $nexttrace_sha,
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
          sha256: $iputils_sha,
          parameters: {source_commit: $iputils_commit, fully_static: true, stripped: true}
        }
      ]
    }' | jq . >"$stage/manifest.json"

for tool in sysbench zstd npb-ep npb-ft openssl stream fio iperf3 nexttrace-tiny ping; do
  actual=$(sha256sum "$stage/bin/$tool" | awk '{print $1}')
  recorded=$(jq -er --arg name "$tool" '.tools[] | select(.name == $name) | .sha256' "$stage/manifest.json")
  [[ "$actual" == "$recorded" ]] || die "manifest hash mismatch for $tool"
  [[ "$recorded" =~ ^[[:xdigit:]]{64}$ ]] || die "manifest hash is not concrete for $tool"
done
jq -e --arg architecture "$arch" '
  .schema_version == "ecs-tools.manifest/v1" and
  .architecture == $architecture and
  (.build.toolchain_mode == "native" or .build.toolchain_mode == "cross") and
  (.build.build_triplet | type == "string" and length > 0) and
  (.build.target_triplet | type == "string" and length > 0) and
  (.build.smoke_runner | type == "string" and length > 0) and
  .build.validation.scope == "functional" and
  .build.validation.performance_valid == false and
  (.tools | length == 10) and
  (all(.tools[]; .architecture == $architecture and .version != "unknown" and .tag_or_commit != "unknown" and .source != "unknown" and (.sha256 | test("^[0-9A-Fa-f]{64}$"))))
' "$stage/manifest.json" >/dev/null || die "generated manifest failed concrete metadata checks"

echo "completed real tools stage: $stage"

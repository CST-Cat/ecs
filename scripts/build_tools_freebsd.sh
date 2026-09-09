#!/usr/bin/env bash
set -Eeuo pipefail

# Native FreeBSD builder for the eight benchmark tools that ECS can ship on
# FreeBSD. This file is run inside a real FreeBSD build VM; it does not use
# distribution benchmark binaries and never downloads ping or NextTrace.

usage() {
  cat >&2 <<'USAGE'
usage: scripts/build_tools_freebsd.sh --target freebsd_amd64|freebsd_arm64 \
       --stage-root STAGE_ROOT
       scripts/build_tools_freebsd.sh --target TARGET --print-params

The target stage contains native sysbench, zstd, NPB EP/FT, OpenSSL, STREAM,
fio and iperf3 binaries. ping and NextTrace are FreeBSD base-system tools and
are deliberately never packaged here.
USAGE
}

die() {
  echo "build-tools-freebsd: $*" >&2
  exit 1
}

target=""
stage_root=""
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
  printf 'toolchain_mode=native\n'
  printf 'target_runner=direct\n'
  printf 'npb_ci_smoke_class=A\n'
  printf 'tools=sysbench zstd npb-ep npb-ft openssl stream fio iperf3\n'
  exit 0
fi

[[ -n "$stage_root" ]] || { usage; exit 2; }
[[ "$stage_root" = /* ]] || die "stage root must be an absolute path"

stage="$stage_root/$target"
work=${ECS_TOOLS_WORK:-/tmp/ecs-tools-freebsd-build}
[[ "$work" = /* && "$work" != / ]] || die "build work directory must be an absolute non-root path"
mkdir -p -- "$stage_root"
[[ ! -e "$stage" ]] || die "target stage already exists: $stage"
[[ ! -e "$work" ]] || die "deterministic build directory already exists: $work"
mkdir -m 0700 -- "$work"
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

freebsd_write_manifest() {
  cp "$sysbench_src/COPYING" "$stage/LICENSES/SYSBENCH-COPYING"
  cp "$zstd_src/LICENSE" "$stage/LICENSES/ZSTD-LICENSE"
  cp "$zstd_src/COPYING" "$stage/LICENSES/ZSTD-COPYING"
  sed -n '1,31p' "$npb_src/EP/ep.f90" >"$stage/LICENSES/NPB-LICENSE.txt"
  cp "$work/$npb_tag/README" "$stage/LICENSES/NPB-README.txt"
  cp "$openssl_src/LICENSE.txt" "$stage/LICENSES/OPENSSL-LICENSE.txt"
  cp "$fio_src/COPYING" "$stage/LICENSES/FIO-COPYING"
  cp "$iperf3_src/LICENSE" "$stage/LICENSES/IPERF3-LICENSE"
  sed -n '1,/^ \*\/$/p' "$stream_src" >"$stage/LICENSES/STREAM-LICENSE.txt"
  cp "$luajit_license_dir/MIT" "$stage/LICENSES/LUAJIT-MIT"
  cp "$luajit_license_dir/PD" "$stage/LICENSES/LUAJIT-PUBLIC-DOMAIN"
  cp "$ck_license_dir/BSD2CLAUSE" "$stage/LICENSES/CONCURRENCY-KIT-BSD2CLAUSE"
  chmod 0644 "$stage/LICENSES/"*

  sysbench_source=$(git_source "$sysbench_repository" "$sysbench_commit")
  zstd_source=$(git_source "$zstd_repository" "$zstd_commit")
  openssl_source=$(git_source "$openssl_repository" "$openssl_commit")
  fio_source=$(git_source "$fio_repository" "$fio_commit")
  iperf3_source=$(git_source "$iperf3_repository" "$iperf3_commit")
  sysbench_version=$(lock_tool_field sysbench version)
  zstd_version=$(lock_tool_field zstd version)
  openssl_version=$(lock_tool_field openssl version)
  fio_version=$(lock_tool_field fio version)
  iperf3_version=$(lock_tool_field iperf3 version)
  sysbench_upstream=$(lock_tool_field sysbench upstream)
  zstd_upstream=$(lock_tool_field zstd upstream)
  npb_upstream=$(lock_tool_field npb-ep upstream)
  openssl_upstream=$(lock_tool_field openssl upstream)
  fio_upstream=$(lock_tool_field fio upstream)
  iperf3_upstream=$(lock_tool_field iperf3 upstream)
  jq -n \
  --arg target "$target" \
  --arg goarch "$goarch" \
  --arg architecture "$package_arch" \
  --arg cc "$cc_command" \
  --arg cxx "$cxx_command" \
  --arg fc "$fc_command" \
  --argjson supported_architectures "$supported_architectures_json" \
  --argjson supported_targets "$supported_targets_json" \
  --arg build_triplet "$build_triplet" \
  --arg target_triplet "$target_triplet" \
  --arg sysbench_version "$sysbench_version" --arg sysbench_tag "$sysbench_tag" \
  --arg sysbench_source "$sysbench_source" --arg sysbench_commit "$sysbench_commit" \
  --arg sysbench_luajit_version "$sysbench_luajit_version" --arg sysbench_ck_version "$sysbench_ck_version" \
  --arg sysbench_luajit_package "$sysbench_luajit_package" --arg sysbench_ck_package "$sysbench_ck_package" \
  --arg zstd_version "$zstd_version" --arg zstd_tag "$zstd_tag" \
  --arg zstd_source "$zstd_source" --arg zstd_commit "$zstd_commit" \
  --arg npb_version "$npb_version" --arg npb_tag "$npb_tag" --arg npb_url "$npb_url" --arg npb_sha "$npb_sha" \
  --arg npb_gfortran_version "$npb_gfortran_version" \
  --arg openssl_version "$openssl_version" --arg openssl_tag "$openssl_tag" \
  --arg openssl_source "$openssl_source" --arg openssl_commit "$openssl_commit" --arg openssl_target "$openssl_target" \
  --argjson openssl_build_flags "$openssl_build_flags_json" \
  --arg fio_version "$fio_version" --arg fio_tag "$fio_tag" --arg fio_source "$fio_source" --arg fio_commit "$fio_commit" \
  --arg iperf3_version "$iperf3_version" --arg iperf3_tag "$iperf3_tag" --arg iperf3_source "$iperf3_source" --arg iperf3_commit "$iperf3_commit" \
  --arg stream_version "${stream_revision%%-*}" --arg stream_revision "$stream_revision" --arg stream_url "$stream_url" --arg stream_sha "$stream_sha" \
  --arg zstd_corpus_name "$zstd_corpus_name" --arg zstd_corpus_url "$zstd_corpus_url" \
  --arg zstd_corpus_sha "$zstd_corpus_sha" --arg zstd_corpus_source_sha "$zstd_corpus_source_sha" \
  --argjson zstd_corpus_bytes "$zstd_corpus_bytes" \
  --arg sysbench_upstream "$sysbench_upstream" --arg zstd_upstream "$zstd_upstream" --arg npb_upstream "$npb_upstream" \
  --arg openssl_upstream "$openssl_upstream" --arg fio_upstream "$fio_upstream" --arg iperf3_upstream "$iperf3_upstream" \
  --argjson stream_array_size "$stream_array_size" --argjson stream_ntimes "$stream_ntimes" \
  ' {
      schema_version: "ecs-tools.manifest/v1",
      target: $target,
      goos: "freebsd",
      goarch: $goarch,
      architecture: $architecture,
      supported_architectures: $supported_architectures,
      supported_targets: $supported_targets,
      build: {toolchain_mode: "native", build_triplet: $build_triplet, target_triplet: $target_triplet, smoke_runner: "direct", validation: {scope: "functional", performance_valid: false}},
      tools: [
        {name: "sysbench", upstream: $sysbench_upstream, version: $sysbench_version, tag_or_commit: $sysbench_tag, source: $sysbench_source, build_flags: [("CC=" + $cc), ("CXX=" + $cxx), "LDFLAGS=-static", "--without-gcc-arch", "--with-system-luajit", "--with-system-ck", "--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed", "--without-mysql", "--without-pgsql", "--without-drizzle", "--without-attachsql", "--without-oracle"], enabled_features: ["cpu", "LuaJIT", "Concurrency Kit"], disabled_features: ["database-drivers", "host-CPU-specific architecture flags", "mysql", "pgsql", "drizzle", "attachsql", "oracle"], architecture: $architecture, license: "GPL-2.0-only", parameters: {source_commit: $sysbench_commit, system_luajit_version: $sysbench_luajit_version, system_luajit_package: $sysbench_luajit_package, system_ck_version: $sysbench_ck_version, system_ck_package: $sysbench_ck_package, fully_static: true, stripped: false}},
        {name: "zstd", upstream: $zstd_upstream, version: $zstd_version, tag_or_commit: $zstd_tag, source: $zstd_source, build_flags: [$cc, "-O3", "-static", "-static-libgcc", "-DZSTD_NODICT", "-DZSTD_NOTRACE", "HAVE_ZLIB=0", "HAVE_LZMA=0", "HAVE_LZ4=0", "ZSTD_LEGACY_SUPPORT=0"], enabled_features: ["benchmark", "multithread", "compression", "decompression"], disabled_features: ["zlib", "lzma", "lz4", "legacy-formats", "dictionary-builder", "trace"], architecture: $architecture, license: "BSD-3-Clause OR GPL-2.0-only", parameters: {source_commit: $zstd_commit, level: 3, evaluation_seconds: 5, thread_modes: ["1T", "NT"], corpus_name: $zstd_corpus_name, corpus_path: ("runtime/" + $zstd_corpus_name), corpus_bytes: $zstd_corpus_bytes, corpus_sha256: $zstd_corpus_sha, corpus_source_url: $zstd_corpus_url, corpus_source_sha256: $zstd_corpus_source_sha, corpus_construction: "raw concatenation: dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml", fully_static: true, stripped: false}},
        {name: "npb-ep", upstream: $npb_upstream, version: $npb_version, tag_or_commit: $npb_tag, source: $npb_url, build_flags: [$fc, "-O3", "-fopenmp", "-static", "CLASS=A", "RAND=randi8", "OMP"], enabled_features: ["NPB3.4-OMP", "EP", "Class A", "OpenMP"], disabled_features: ["MPI", "other NPB kernels", "other problem classes"], architecture: $architecture, license: "NASA-NPB-permissive", parameters: {source_sha256: $npb_sha, compiler: $npb_gfortran_version, compiler_flags: "-O3 -fopenmp -static", random_generator: "randi8", ci_smoke_class: "A", ci_smoke_scope: "release Class A binary", fully_static: true, stripped: false}},
        {name: "npb-ft", upstream: $npb_upstream, version: $npb_version, tag_or_commit: $npb_tag, source: $npb_url, build_flags: [$fc, "-O3", "-fopenmp", "-static", "CLASS=A", "RAND=randi8", "OMP"], enabled_features: ["NPB3.4-OMP", "FT", "Class A", "OpenMP", "3D FFT"], disabled_features: ["MPI", "other NPB kernels", "other problem classes"], architecture: $architecture, license: "NASA-NPB-permissive", parameters: {source_sha256: $npb_sha, compiler: $npb_gfortran_version, compiler_flags: "-O3 -fopenmp -static", random_generator: "randi8", ci_smoke_class: "A", ci_smoke_scope: "release Class A binary", fully_static: true, stripped: false}},
        {name: "openssl", upstream: $openssl_upstream, version: $openssl_version, tag_or_commit: $openssl_tag, source: $openssl_source, build_flags: $openssl_build_flags, enabled_features: ["speed", "EVP", "AES-256-GCM", "ChaCha20-Poly1305", "SHA-256", "multi-process", "architecture assembly"], disabled_features: ["TLS/DTLS/QUIC", "network/HTTP", "shared libraries/modules/engines", "EC/DH/DSA/PQ families", "unrequested cipher/digest families", "tests/documentation"], architecture: $architecture, license: "Apache-2.0", parameters: {source_commit: $openssl_commit, configure_target: $openssl_target, generated_target: "build_generated", build_target: "apps/openssl", algorithms: ["aes-256-gcm", "chacha20-poly1305", "sha256"], block_bytes: 16384, duration_seconds: 5, worker_modes: [1, "detected_cpu_allowance"], elapsed_wall_clock: true, machine_readable: true, fully_static: true, stripped: false}},
        {name: "stream", upstream: "https://www.cs.virginia.edu/stream/", version: $stream_version, tag_or_commit: $stream_revision, source: $stream_url, build_flags: [$cc, "-O3", "-fopenmp", "-static", "-static-libgcc", ("-DSTREAM_ARRAY_SIZE=" + ($stream_array_size|tostring)), ("-DNTIMES=" + ($stream_ntimes|tostring))], enabled_features: ["Copy", "Scale", "Add", "Triad", "OpenMP"], disabled_features: [], architecture: $architecture, license: "STREAM-custom", parameters: {source_sha256: $stream_sha, array_size: $stream_array_size, ntimes: $stream_ntimes, fully_static: true, stripped: false}},
        {name: "fio", upstream: $fio_upstream, version: $fio_version, tag_or_commit: $fio_tag, source: $fio_source, build_flags: ["--build-static", "--disable-numa", "--disable-rdma", "--disable-rados", "--disable-rbd", "--disable-gfapi", "--disable-http", "--disable-pmem", "--disable-libzbc", "--disable-xnvme", "--disable-libblkio", "--disable-libnfs", "--disable-dfs", "--disable-tcmalloc", "--disable-native", "generated-config: require CONFIG_POSIXAIO=y"], enabled_features: ["posixaio", "psync"], disabled_features: ["io_uring", "libaio", "ceph", "rbd", "rados", "gluster", "gfapi", "rdma"], architecture: $architecture, license: "GPL-2.0-only", parameters: {source_commit: $fio_commit, engine_order: ["posixaio", "psync"], qd_validation: [32, 64], fully_static: true, stripped: false}},
        {name: "iperf3", upstream: $iperf3_upstream, version: $iperf3_version, tag_or_commit: $iperf3_tag, source: $iperf3_source, build_flags: ["--enable-static-bin", "--without-sctp", "--without-openssl", "--without-ldconfig"], enabled_features: ["tcp", "udp", "ipv4", "ipv6", "parallel", "reverse", "json"], disabled_features: ["sctp", "openssl/auth"], architecture: $architecture, license: "BSD-3-Clause", parameters: {source_commit: $iperf3_commit, fully_static: true, stripped: false}}
      ]
    }' | jq . >"$stage/manifest.json"

  echo "completed native FreeBSD tools stage: $stage"
}

cc_command=${CC:-gcc14}
cxx_command=${CXX:-g++14}
fc_command=${FC:-gfortran14}
for command_name in bash "$cc_command" "$cxx_command" "$fc_command" git gmake jq perl pkg pkg-config sha256 tar sed awk grep file strip readelf; do
  command -v "$command_name" >/dev/null 2>&1 || die "required FreeBSD build command is missing: $command_name"
done
command -v curl >/dev/null 2>&1 || command -v fetch >/dev/null 2>&1 ||
  die 'curl or fetch is required to download pinned sources'
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
freebsd_license_root="$freebsd_localbase/share/licenses"
luajit_license_dir="$freebsd_license_root/$sysbench_luajit_package"
ck_license_dir="$freebsd_license_root/$sysbench_ck_package"
for license_file in "$luajit_license_dir/MIT" "$luajit_license_dir/PD" "$ck_license_dir/BSD2CLAUSE"; do
  [[ -s "$license_file" ]] || die "required dependency license is missing: $license_file"
done
build_triplet=$("$cc_command" -dumpmachine)
target_triplet=$build_triplet

export LC_ALL=C
export LANG=C
export TZ=UTC
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-946684800}
[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || die 'SOURCE_DATE_EPOCH must be an integer'
jobs=${JOBS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || jobs=2

lock_tool_field() {
  local tool=$1 field=$2
  jq -er --arg tool "$tool" --arg field "$field" \
    '.tools[] | select(.name == $tool) | .[$field] // empty' "$lock_file"
}

sha256_file() {
  sha256 -q "$1"
}

download_sha256() {
  local url=$1 expected=$2 output=$3 label=$4 actual
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 "$url" -o "$output"
  else
    fetch -o "$output" "$url"
  fi
  actual=$(sha256_file "$output")
  [[ "$actual" == "$expected" ]] || die "$label SHA-256 mismatch: expected $expected, got $actual"
}

clone_release() {
  local repository=$1 tag=$2 expected=$3 destination=$4 actual
  git -c advice.detachedHead=false clone --depth 1 --branch "$tag" \
    "https://github.com/$repository.git" "$destination" >/dev/null
  actual=$(git -C "$destination" rev-parse HEAD)
  [[ "$actual" == "$expected" ]] || die "$repository $tag resolved to $actual, expected $expected"
}

git_source() {
  printf 'git+https://github.com/%s.git@%s\n' "$1" "$2"
}

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
clone_release "$sysbench_repository" "$sysbench_tag" "$sysbench_commit" "$sysbench_src"
clone_release "$zstd_repository" "$zstd_tag" "$zstd_commit" "$zstd_src"
clone_release "$openssl_repository" "$openssl_tag" "$openssl_commit" "$openssl_src"
clone_release "$fio_repository" "$fio_tag" "$fio_commit" "$fio_src"
clone_release "$iperf3_repository" "$iperf3_tag" "$iperf3_commit" "$iperf3_src"
sysbench_short_commit=$(git -C "$sysbench_src" rev-parse --short HEAD)

npb_archive="$work/$npb_tag.tar.gz"
download_sha256 "$npb_url" "$npb_sha" "$npb_archive" "NPB $npb_tag source archive"
tar -xzf "$npb_archive" -C "$work"
npb_src="$work/$npb_tag/NPB3.4-OMP"
[[ -d "$npb_src/EP" && -d "$npb_src/FT" ]] || die 'NPB archive omitted NPB3.4-OMP EP or FT'

stream_src="$work/stream.c"
download_sha256 "$stream_url" "$stream_sha" "$stream_src" 'official STREAM source'
stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
[[ -n "$stream_revision" ]] || die 'could not read official STREAM revision'

mkdir -- "$stage"
stage_created=1
mkdir -p "$stage/bin" "$stage/LICENSES"

echo "building sysbench $sysbench_tag ($sysbench_commit)"
(
  cd "$sysbench_src"
  ./autogen.sh
  CC="$cc_command" CXX="$cxx_command" LDFLAGS=-static ./configure \
    --prefix="$work/sysbench-prefix" \
    --without-gcc-arch \
    --with-system-luajit \
    --with-system-ck \
    '--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed' \
    --without-mysql \
    --without-pgsql \
    --without-drizzle \
    --without-attachsql \
    --without-oracle
  gmake -j"$jobs"
)
cp "$sysbench_src/src/sysbench" "$stage/bin/sysbench"

echo "building zstd $zstd_tag ($zstd_commit)"
gmake -C "$zstd_src/programs" -j"$jobs" zstd-release \
  CC="$cc_command" MOREFLAGS='-O3 -static -static-libgcc -DZSTD_NODICT -DZSTD_NOTRACE' \
  HAVE_ZLIB=0 HAVE_LZMA=0 HAVE_LZ4=0 ZSTD_LEGACY_SUPPORT=0
cp "$zstd_src/programs/zstd" "$stage/bin/zstd"

echo "building NPB $npb_version OpenMP EP + FT Class A"
npb_flags='-O3 -fopenmp -static'
npb_gfortran_version=$("$fc_command" --version | sed -n '1p')
npb_compile_date=$(date -u -r "$SOURCE_DATE_EPOCH" '+%d %b %Y')
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
UCC = $cc_command
BINDIR = ../bin
RAND = randi8
WTIME = wtime.c
EOF
mkdir -p "$npb_src/bin"
gmake -C "$npb_src/sys" all
for benchmark in EP FT; do
  benchmark_lower=${benchmark,,}
  (
    cd "$npb_src/$benchmark"
    ../sys/setparams "$benchmark_lower" A
    params=../"$benchmark"/npbparams.h
    sed -i '' "s@parameter (compiletime='[^']*')@parameter (compiletime='$npb_compile_date')@" "$params"
  )
done
gmake -C "$npb_src" -j"$jobs" ep CLASS=A
gmake -C "$npb_src" -j"$jobs" ft CLASS=A
cp "$npb_src/bin/ep.A.x" "$stage/bin/npb-ep"
cp "$npb_src/bin/ft.A.x" "$stage/bin/npb-ft"

echo "building OpenSSL $openssl_tag ($openssl_commit)"
openssl_prefix="$work/openssl-prefix"
openssl_build_flags=(
  "$openssl_target" -O3 no-shared no-module no-pinshared no-tests no-docs
  no-ssl no-sock no-dgram no-http no-cmp no-cms no-ct no-ocsp no-dso
  no-engine no-static-engine no-legacy no-async no-atexit no-autoload-config
  no-cached-fetch no-comp no-dh no-dsa no-ec no-aria no-bf no-blake2
  no-camellia no-cast no-cmac no-des no-idea no-md4 no-mdc2 no-ocb
  no-rc2 no-rc4 no-rmd160 no-scrypt no-seed no-siphash no-siv no-sm2
  no-sm3 no-sm4 no-whirlpool no-ml-dsa no-ml-kem no-slh-dsa no-rfc3779
  no-srp no-srtp no-ts -static "--prefix=$openssl_prefix"
  "--openssldir=$openssl_prefix/ssl"
)
openssl_build_flags_json=$(printf '%s\n' "${openssl_build_flags[@]}" |
  jq -Rsc 'split("\n") | map(select(length > 0))')
(
  cd "$openssl_src"
  CC="$cc_command" perl ./Configure "${openssl_build_flags[@]}"
  gmake -j"$jobs" build_generated
  gmake -j"$jobs" apps/openssl
)
cp "$openssl_src/apps/openssl" "$stage/bin/openssl"

echo "building STREAM $stream_revision"
"$cc_command" -O3 -fopenmp -static -static-libgcc \
  -DSTREAM_ARRAY_SIZE="$stream_array_size" -DNTIMES="$stream_ntimes" \
  "$stream_src" -o "$stage/bin/stream"

echo "building fio $fio_tag ($fio_commit) with posixaio + psync"
(
  cd "$fio_src"
  CC="$cc_command" ./configure \
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
  grep -Eq '^CONFIG_POSIXAIO=y$' config-host.mak || die 'FreeBSD fio did not enable CONFIG_POSIXAIO'
  grep -Eq '^CONFIG_LIBAIO=y$' config-host.mak && die 'FreeBSD fio unexpectedly enabled Linux libaio'
  gmake -j"$jobs"
)
cp "$fio_src/fio" "$stage/bin/fio"

echo "building iperf3 $iperf3_tag ($iperf3_commit)"
(
  cd "$iperf3_src"
  CC="$cc_command" ./configure --prefix="$work/iperf3-prefix" --enable-static-bin \
    --without-sctp --without-openssl --without-ldconfig
  gmake -j"$jobs"
)
cp "$iperf3_src/src/iperf3" "$stage/bin/iperf3"

for tool in sysbench zstd npb-ep npb-ft openssl stream fio iperf3; do
  chmod 0755 "$stage/bin/$tool"
  [[ -s "$stage/bin/$tool" ]] || die "built $tool is empty"
  file "$stage/bin/$tool"
  readelf -h "$stage/bin/$tool" | grep -Eq 'FreeBSD|UNIX - FreeBSD' ||
    die "$tool is not a FreeBSD ELF binary"
done

# A user run must not need any package-manager or base-system shared library.
# Inspect ELF headers directly and make the manifest's fully_static claim a
# verified fact rather than accepting a dynamically linked FreeBSD binary.
for tool in sysbench zstd npb-ep npb-ft openssl stream fio iperf3; do
  readelf -dW "$stage/bin/$tool" >"$work/${tool}.dynamic" 2>&1 ||
    die "dynamic-header readelf failed for $tool"
  readelf -lW "$stage/bin/$tool" >"$work/${tool}.program" 2>&1 ||
    die "program-header readelf failed for $tool"
  if grep -Eq '\(NEEDED\)' "$work/${tool}.dynamic" ||
    grep -Eq '(^|[[:space:]])INTERP([[:space:]]|$)' "$work/${tool}.program"; then
    cat "$work/${tool}.dynamic" "$work/${tool}.program" >&2
    die "$tool is not fully static"
  fi
done

echo 'running functional FreeBSD tool smoke tests'
run="$stage/bin"
sysbench_version=$(lock_tool_field sysbench version)
"$run/sysbench" --version >"$work/sysbench-version.txt" 2>&1
expected_sysbench_version="sysbench $sysbench_version-$sysbench_short_commit"
grep -Fx "$expected_sysbench_version" "$work/sysbench-version.txt" >/dev/null || {
  cat "$work/sysbench-version.txt" >&2
  die "sysbench version smoke did not report $expected_sysbench_version"
}
"$run/sysbench" cpu --cpu-max-prime=1000 --threads=1 run >"$work/sysbench-smoke.txt"
grep -Eq 'events per second|total time' "$work/sysbench-smoke.txt" || die 'sysbench CPU smoke output was not recognized'

printf '%s\n' 'ECS FreeBSD zstd smoke input' >"$work/zstd-input"
zstd_version=$(lock_tool_field zstd version)
"$run/zstd" --version >"$work/zstd-version.txt" 2>&1
grep -Eq "v${zstd_version//./\\.}([^0-9]|$)" "$work/zstd-version.txt" ||
  die "zstd version smoke did not report $zstd_version"
"$run/zstd" -q -f "$work/zstd-input" -o "$work/zstd-output.zst"
"$run/zstd" -q -d -f "$work/zstd-output.zst" -o "$work/zstd-roundtrip"
cmp "$work/zstd-input" "$work/zstd-roundtrip" || die 'zstd round trip failed'

for benchmark in ep ft; do
  (
    cd "$work"
    OMP_NUM_THREADS=1 OMP_DYNAMIC=FALSE OMP_PROC_BIND=close OMP_PLACES=cores \
      OMP_SCHEDULE=static OMP_DISPLAY_ENV=FALSE NPB_TIMER_FLAG=0 \
      "$run/npb-$benchmark"
  ) >"$work/npb-$benchmark-smoke.txt" 2>&1
  grep -F 'Verification = SUCCESSFUL' "$work/npb-$benchmark-smoke.txt" >/dev/null || {
    cat "$work/npb-$benchmark-smoke.txt" >&2
    die "NPB $benchmark smoke verification failed"
  }
  grep -Eq "^[[:space:]]*Version[[:space:]]*=[[:space:]]*${npb_version//./\\.}[[:space:]]*$" \
    "$work/npb-$benchmark-smoke.txt" || die "NPB $benchmark reported the wrong version"
done

openssl_version=$(lock_tool_field openssl version)
OPENSSL_CONF=/dev/null "$run/openssl" version >"$work/openssl-version.txt" 2>&1
grep -Eq "^OpenSSL ${openssl_version//./\\.}([[:space:]]|$)" "$work/openssl-version.txt" ||
  die "OpenSSL version smoke did not report $openssl_version"
mkdir -p "$work/openssl-smoke/modules" "$work/openssl-smoke/engines"
for openssl_algorithm in aes-256-gcm chacha20-poly1305 sha256; do
  case "$openssl_algorithm" in
    aes-256-gcm) openssl_output_name=AES-256-GCM; openssl_aead=(-aead) ;;
    chacha20-poly1305) openssl_output_name=ChaCha20-Poly1305; openssl_aead=(-aead) ;;
    sha256) openssl_output_name=sha256; openssl_aead=() ;;
  esac
  OPENSSL_CONF=/dev/null \
    OPENSSL_MODULES="$work/openssl-smoke/modules" \
    OPENSSL_ENGINES="$work/openssl-smoke/engines" \
    "$run/openssl" speed -elapsed -seconds 1 -bytes 16384 -mr -multi 1 \
    -evp "$openssl_algorithm" "${openssl_aead[@]}" \
    >"$work/openssl-${openssl_algorithm}-smoke.txt" 2>&1
  grep -F "+DT:${openssl_output_name}:1:16384" \
    "$work/openssl-${openssl_algorithm}-smoke.txt" >/dev/null ||
    die "OpenSSL speed $openssl_algorithm smoke omitted fixed parameters"
  grep -Eq "^\\+F:[0-9]+:${openssl_output_name}:[0-9]+(\\.[0-9]+)?[[:space:]]*$" \
    "$work/openssl-${openssl_algorithm}-smoke.txt" ||
    die "OpenSSL speed $openssl_algorithm smoke omitted machine-readable throughput"
done

OMP_NUM_THREADS=1 "$run/stream" >"$work/stream-smoke.txt"
for kernel in Copy Scale Add Triad; do
  grep -q "$kernel:" "$work/stream-smoke.txt" || die "STREAM smoke omitted $kernel"
done
grep -q 'Solution Validates' "$work/stream-smoke.txt" || die 'STREAM smoke did not validate'

dd if=/dev/zero of="$work/fio-smoke.data" bs=4096 count=2048 >/dev/null 2>&1
fio_version=$(lock_tool_field fio version)
"$run/fio" --version >"$work/fio-version.txt" 2>&1
grep -Eq "^fio-${fio_version//./\\.}([[:space:]]|$)" "$work/fio-version.txt" ||
  die "fio version smoke did not report $fio_version"
"$run/fio" --enghelp >"$work/fio-engines.txt"
for required_engine in posixaio psync; do
  grep -Eiq "(^|[^[:alnum:]_])${required_engine}([^[:alnum:]_]|$)" "$work/fio-engines.txt" ||
    die "FreeBSD fio omitted required engine: $required_engine"
done
for requested_depth in 32 64; do
  fio_json="$work/fio-qd${requested_depth}.json"
  "$run/fio" --name="ecs-qd${requested_depth}" --filename="$work/fio-smoke.data" \
    --rw=read --bs=4k --size=4m --runtime=1 --time_based=1 \
    --ioengine=posixaio --iodepth="$requested_depth" --numjobs=1 --direct=1 \
    --output-format=json --output="$fio_json"
  jq -e --argjson depth "$requested_depth" \
    '(.jobs | length == 1) and
     (.jobs[0].error == 0) and
     (.jobs[0]["job options"].ioengine == "posixaio") and
     ((.jobs[0]["job options"].iodepth | tonumber) == $depth) and
     ([.jobs[0].iodepth_level | to_entries[] | select(.key != "1") | .value] | any(. > 0))' \
    "$fio_json" >/dev/null || {
    cat "$fio_json" >&2
    die "FreeBSD fio posixaio QD${requested_depth} did not show effective depth"
  }
done

iperf3_version=$(lock_tool_field iperf3 version)
"$run/iperf3" --version >"$work/iperf3-version.txt" 2>&1
grep -Eq "iperf ${iperf3_version//./\\.}([^0-9]|$)" "$work/iperf3-version.txt" ||
  die "iperf3 version smoke did not report $iperf3_version"
iperf_port=$((42000 + (${RANDOM:-1} % 1000)))
"$run/iperf3" -s -1 -p "$iperf_port" >"$work/iperf3-server.txt" 2>&1 &
iperf_server=$!
trap 'kill "$iperf_server" 2>/dev/null || true; wait "$iperf_server" 2>/dev/null || true; cleanup' EXIT
iperf_json="$work/iperf3-smoke.json"
iperf_ok=0
for _ in {1..50}; do
  if "$run/iperf3" -J -c 127.0.0.1 -p "$iperf_port" -t 1 -P 1 >"$iperf_json" 2>"$work/iperf3-client.txt"; then
    iperf_ok=1
    break
  fi
  kill -0 "$iperf_server" 2>/dev/null || die 'iperf3 server exited before loopback client connected'
  sleep 0.1
done
[[ "$iperf_ok" -eq 1 ]] || die 'iperf3 loopback smoke failed'
kill -0 "$iperf_server" 2>/dev/null && kill "$iperf_server" 2>/dev/null || true
wait "$iperf_server" 2>/dev/null || true
iperf_server=""
jq -e 'type == "object" and (.start | type == "object") and (.end | type == "object")' \
  "$iperf_json" >/dev/null || die 'iperf3 loopback JSON smoke failed'
trap cleanup EXIT
freebsd_write_manifest

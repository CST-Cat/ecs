#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'EOF'
usage: scripts/build_tools.sh --arch ARCH --stage-root STAGE_ROOT

Build the six ECS benchmark tools for one Linux architecture. This script is
intended to run inside the architecture container used by CI; it never uses a
fixture or a distribution-provided benchmark binary.
EOF
}

die() {
  echo "build-tools: $*" >&2
  exit 1
}

arch=""
stage_root=""
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

for command_name in \
  curl git jq sha256sum gcc make readelf ldd meson ninja autoconf automake \
  libtoolize pkg-config; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

stage="$stage_root/linux_${arch}"
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-build.XXXXXX")
cleanup() {
  local status=$?
  trap - EXIT
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT

mkdir -p "$stage/bin" "$stage/LICENSES"

curl_options=(-fsSL --retry 4 --retry-delay 2 --connect-timeout 30)
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  curl_options+=( -H "Authorization: Bearer ${GITHUB_TOKEN}" )
fi

github_latest_release() {
  local repository=$1
  local output=$2
  curl "${curl_options[@]}" \
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
fio_src="$work/fio"
iperf3_src="$work/iperf3"
iputils_src="$work/iputils"
nexttrace_src="$work/nexttrace"
sysbench_commit=$(clone_release akopytov/sysbench "$sysbench_tag" "$sysbench_src")
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
# The upstream sysbench release owns its bundled LuaJIT build. If that
# upstream dependency or the target distro toolchain lacks the requested
# architecture (for example a future LuaJIT port gap), configure must fail;
# there is intentionally no substitute benchmark or shell placeholder.
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
  "--prefix=$work/sysbench-prefix"
  '--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed'
)
sysbench_manifest_flags=(
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

echo "building fio ${fio_tag} (${fio_commit})"
(
  cd "$fio_src"
  ./configure \
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
  LDFLAGS='-static' ./configure \
    --prefix="$work/iperf3-prefix" \
    --disable-shared \
    --enable-static \
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
  gcc -O3 -fopenmp -static -static-libgcc
  "-DSTREAM_ARRAY_SIZE=$stream_array_size" "-DNTIMES=$stream_ntimes"
)
stream_build_flags_json=$(printf '%s\n' "${stream_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
"${stream_build_flags[@]}" "$stream_src" -o "$stage/bin/stream"

echo "building iputils ping ${iputils_tag} (${iputils_commit})"
ping_build="$work/iputils-build"
ping_install="$work/iputils-install"
LDFLAGS='-static' meson setup "$ping_build" "$iputils_src" \
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

cp "$nexttrace_download" "$stage/bin/nexttrace-tiny"

for tool in sysbench stream fio iperf3 nexttrace-tiny ping; do
  chmod 0755 "$stage/bin/$tool"
  [[ -s "$stage/bin/$tool" ]] || die "built $tool is empty"
done

# Preserve upstream license texts in every architecture package. STREAM embeds
# its license in the official source file, so retain the complete header.
cp "$sysbench_src/COPYING" "$stage/LICENSES/SYSBENCH-COPYING"
cp "$fio_src/COPYING" "$stage/LICENSES/FIO-COPYING"
cp "$iperf3_src/LICENSE" "$stage/LICENSES/IPERF3-LICENSE"
cp "$iputils_src/LICENSE" "$stage/LICENSES/IPUTILS-LICENSE"
cp "$iputils_src/Documentation/LICENSE.GPL2" "$stage/LICENSES/IPUTILS-GPL2"
cp "$nexttrace_src/LICENSE" "$stage/LICENSES/NEXTTRACE-LICENSE"
sed -n '1,/^ \*\/$/p' "$stream_src" >"$stage/LICENSES/STREAM-LICENSE.txt"

sysbench_bin="$stage/bin/sysbench"
stream_bin="$stage/bin/stream"
fio_bin="$stage/bin/fio"
iperf3_bin="$stage/bin/iperf3"
nexttrace_bin="$stage/bin/nexttrace-tiny"
ping_bin="$stage/bin/ping"

echo 'running real binary smoke tests'
"$sysbench_bin" --version
"$sysbench_bin" cpu --cpu-max-prime=1000 --threads=1 run >"$work/sysbench-smoke.txt"
grep -Eq 'events per second|total time' "$work/sysbench-smoke.txt" || {
  cat "$work/sysbench-smoke.txt" >&2
  die 'sysbench CPU smoke output was not recognized'
}

stream_nt_threads=${STREAM_NT_THREADS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}
[[ "$stream_nt_threads" =~ ^[1-9][0-9]*$ ]] || die "invalid STREAM_NT_THREADS=$stream_nt_threads"
for stream_threads in 1 "$stream_nt_threads"; do
  stream_context=nt
  [[ "$stream_threads" -eq 1 ]] && stream_context=1t
  OMP_NUM_THREADS="$stream_threads" "$stream_bin" >"$work/stream-${stream_context}.txt"
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
"$fio_bin" \
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
"$fio_bin" \
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
  --output="$fio_io_uring_json"
jq -e '(.jobs | length == 1) and .jobs[0].jobname == "ecs-io-uring-smoke"' \
  "$fio_io_uring_json" >/dev/null || {
  cat "$fio_io_uring_json" >&2
  die 'fio io_uring JSON/QD1 smoke failed'
}
"$fio_bin" --version
"$fio_bin" --enghelp >"$work/fio-engines.txt"
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

"$iperf3_bin" --version
iperf_port=$((42000 + (${RANDOM:-1} % 1000)))
"$iperf3_bin" -s -1 -p "$iperf_port" >"$work/iperf3-server.txt" 2>&1 &
iperf_server=$!
iperf_json="$work/iperf3-smoke.json"
"$iperf3_bin" -J -c 127.0.0.1 -p "$iperf_port" -t 1 -P 1 >"$iperf_json"
wait "$iperf_server" || true
jq -e 'type == "object" and (.start | type == "object") and (.end | type == "object")' \
  "$iperf_json" >/dev/null || {
  cat "$iperf_json" >&2
  die 'iperf3 JSON parser smoke failed'
}

"$nexttrace_bin" --help >"$work/nexttrace-help.txt" 2>&1 || {
  cat "$work/nexttrace-help.txt" >&2
  die 'NextTrace help failed'
}
grep -Eq -- '--(json|raw)' "$work/nexttrace-help.txt" || {
  cat "$work/nexttrace-help.txt" >&2
  die 'NextTrace Tiny help omitted the JSON/raw flag'
}

"$ping_bin" -V
"$ping_bin" -c 1 -W 1 127.0.0.1 >"$work/ping-smoke.txt"
grep -Eq '1 packets transmitted|1 packets received|1 received' "$work/ping-smoke.txt" || {
  cat "$work/ping-smoke.txt" >&2
  die 'iputils ping loopback smoke failed'
}

# Check all six resulting binaries rather than package-manager state. This
# catches accidental linkage even if a future base image contains excluded
# libraries. The package carries no runtime library directory, so a missing
# dependency or a benchmark-specific runtime library is a hard failure.
for tool in sysbench stream fio iperf3 nexttrace-tiny ping; do
  binary="$stage/bin/$tool"
  readelf -d "$binary" >"$work/${tool}.readelf" 2>&1 || die "readelf failed for $tool"
  ldd "$binary" >"$work/${tool}.ldd" 2>&1 || true
  cat "$work/${tool}.readelf" "$work/${tool}.ldd"
  if grep -Eiq 'not found|ceph|rbd|rados|gluster|gfapi|rdma|ibverbs|rdmacm|libaio|liburing|libgomp|libgcc_s' \
      "$work/${tool}.readelf" "$work/${tool}.ldd"; then
    cat "$work/${tool}.readelf" "$work/${tool}.ldd" >&2
    die "$tool has an unresolved or forbidden runtime dependency"
  fi
done

sysbench_sha=$(sha256sum "$sysbench_bin" | awk '{print $1}')
stream_binary_sha=$(sha256sum "$stream_bin" | awk '{print $1}')
fio_sha=$(sha256sum "$fio_bin" | awk '{print $1}')
iperf3_sha=$(sha256sum "$iperf3_bin" | awk '{print $1}')
nexttrace_binary_sha=$(sha256sum "$nexttrace_bin" | awk '{print $1}')
ping_sha=$(sha256sum "$ping_bin" | awk '{print $1}')

for digest in "$sysbench_sha" "$stream_binary_sha" "$fio_sha" "$iperf3_sha" \
  "$nexttrace_binary_sha" "$ping_sha"; do
  [[ "$digest" =~ ^[[:xdigit:]]{64}$ ]] || die "invalid binary SHA-256: $digest"
done

sysbench_source=$(git_source akopytov/sysbench "$sysbench_commit")
fio_source=$(git_source axboe/fio "$fio_commit")
iperf3_source=$(git_source esnet/iperf "$iperf3_commit")
iputils_source=$(git_source iputils/iputils "$iputils_commit")
nexttrace_version=$(release_version "$nexttrace_tag")
sysbench_version=$(release_version "$sysbench_tag")
fio_version=$(release_version "$fio_tag")
iperf3_version=$(release_version "$iperf3_tag")
iputils_version=$(release_version "$iputils_tag")

jq -n \
  --arg architecture "$arch" \
  --arg sysbench_version "$sysbench_version" \
  --arg sysbench_tag "$sysbench_tag" \
  --arg sysbench_source "$sysbench_source" \
  --arg sysbench_sha "$sysbench_sha" \
  --arg sysbench_commit "$sysbench_commit" \
  --argjson sysbench_build_flags "$sysbench_build_flags_json" \
  --argjson sysbench_disabled_features "$sysbench_disabled_features_json" \
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
      tools: [
        {
          name: "sysbench",
          upstream: $sysbench_upstream,
          version: $sysbench_version,
          tag_or_commit: $sysbench_tag,
          source: $sysbench_source,
          build_flags: $sysbench_build_flags,
          enabled_features: ["cpu", "LuaJIT"],
          disabled_features: $sysbench_disabled_features,
          architecture: $architecture,
          license: "GPL-2.0-only",
          sha256: $sysbench_sha,
          parameters: {source_commit: $sysbench_commit, configure_supported_flags: $sysbench_build_flags}
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
          parameters: {source_sha256: $stream_sha, array_size: $stream_array_size, ntimes: $stream_ntimes, thread_modes: ["1T", "NT"], run_rules: "official STREAM 5.10 defaults; each array must exceed cache as required; NT uses runtime CPU allowance"}
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
          parameters: {source_commit: $fio_commit, qd: 1}
        },
        {
          name: "iperf3",
          upstream: $iperf3_upstream,
          version: $iperf3_version,
          tag_or_commit: $iperf3_tag,
          source: $iperf3_source,
          build_flags: ["LDFLAGS=-static", "--disable-shared", "--enable-static", "--without-sctp", "--without-openssl", "--without-ldconfig"],
          enabled_features: ["tcp", "udp", "ipv4", "ipv6", "parallel", "reverse", "json"],
          disabled_features: ["sctp", "openssl/auth"],
          architecture: $architecture,
          license: "BSD-3-Clause",
          sha256: $iperf3_sha,
          parameters: {source_commit: $iperf3_commit}
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
          parameters: {source_commit: $iputils_commit}
        }
      ]
    }' | jq . >"$stage/manifest.json"

for tool in sysbench stream fio iperf3 nexttrace-tiny ping; do
  actual=$(sha256sum "$stage/bin/$tool" | awk '{print $1}')
  recorded=$(jq -er --arg name "$tool" '.tools[] | select(.name == $name) | .sha256' "$stage/manifest.json")
  [[ "$actual" == "$recorded" ]] || die "manifest hash mismatch for $tool"
  [[ "$recorded" =~ ^[[:xdigit:]]{64}$ ]] || die "manifest hash is not concrete for $tool"
done
jq -e --arg architecture "$arch" '
  .schema_version == "ecs-tools.manifest/v1" and
  .architecture == $architecture and
  (.tools | length == 6) and
  (all(.tools[]; .architecture == $architecture and .version != "unknown" and .tag_or_commit != "unknown" and .source != "unknown" and (.sha256 | test("^[0-9A-Fa-f]{64}$"))))
' "$stage/manifest.json" >/dev/null || die "generated manifest failed concrete metadata checks"

echo "completed real tools stage: $stage"

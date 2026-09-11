#!/usr/bin/env bash
set -euo pipefail

# FreeBSD tools stage merge (Stage 6).
#
# Merges the Stage 3 C-tools fragment (sysbench, zstd, openssl, fio, iperf3)
# and the Stage 5 GNU/OpenMP fragment (npb-ep, npb-ft, stream) into the first
# complete FreeBSD tools stage and emits the final manifest.json.
#
# The merge is purely structural:
#   - no binary is rebuilt, modified or re-linked (sha256 values recorded in
#     the fragments' provenance.json must match the binaries byte for byte);
#   - both fragments are strictly validated first: missing files, extra files
#     and overlapping tool names are hard errors;
#   - LICENSES/ is merged from both fragments; a filename collision is an error;
#   - the final manifest is assembled from the per-tool provenance records and
#     the repository locks — the fragment provenance.json files are never
#     copied through as the manifest.
#
# Output layout:
#   <stage-root>/<target>/bin/{sysbench,zstd,npb-ep,npb-ft,openssl,stream,fio,iperf3}
#   <stage-root>/<target>/LICENSES/
#   <stage-root>/<target>/manifest.json

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/merge_freebsd_tools_stage.sh --target freebsd_amd64|freebsd_arm64
                                               --c-fragment DIR --gnu-fragment DIR
                                               --stage-root DIR
USAGE
}

die() {
  echo "merge-freebsd-tools-stage: $*" >&2
  exit 1
}

target=""
c_fragment=""
gnu_fragment=""
stage_root=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      target=$2
      shift 2
      ;;
    --c-fragment)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      c_fragment=$2
      shift 2
      ;;
    --gnu-fragment)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      gnu_fragment=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      stage_root=$2
      shift 2
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

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac
[[ -n "$c_fragment" ]] || { usage; die "--c-fragment is required"; }
[[ -n "$gnu_fragment" ]] || { usage; die "--gnu-fragment is required"; }
[[ -n "$stage_root" ]] || { usage; die "--stage-root is required"; }

for command_name in jq sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

sysroot_lock="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"
gnu_lock="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"

c_tools=(sysbench zstd openssl fio iperf3)
gnu_tools=(npb-ep npb-ft stream)
# 与 internal/toolsmanifest 的 freeBSDToolNames 相同的固定顺序。
all_tools=(sysbench zstd npb-ep npb-ft openssl stream fio iperf3)

# ---- fragment 布局校验：缺文件、多余文件都拒绝 ----------------------------

check_fragment_layout() {
  local fragment=$1 label=$2
  shift 2
  local expected=("$@")
  [[ -d "$fragment" ]] || die "$label fragment does not exist: $fragment"

  local top
  top=$(find "$fragment" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
  local expected_top
  expected_top=$(printf 'LICENSES\nbin\nprovenance.json\n')
  [[ "$top" == "$expected_top" ]] ||
    die "$label fragment must contain exactly bin/, LICENSES/ and provenance.json; found:
$top"

  local actual_bin
  actual_bin=$(find "$fragment/bin" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)
  local expected_bin
  expected_bin=$(printf '%s\n' "${expected[@]}" | LC_ALL=C sort)
  [[ "$actual_bin" == "$expected_bin" ]] ||
    die "$label fragment bin must contain exactly: ${expected[*]}
found:
$actual_bin"
  local extra
  extra=$(find "$fragment/bin" -mindepth 1 -maxdepth 1 ! -type f -printf '%f\n')
  [[ -z "$extra" ]] || die "$label fragment bin contains non-file entries: $extra"

  [[ -d "$fragment/LICENSES" ]] || die "$label fragment is missing LICENSES"
  [[ -n "$(find "$fragment/LICENSES" -mindepth 1 -maxdepth 1 -type f -printf '%f\n')" ]] ||
    die "$label fragment LICENSES is empty"
  local license_extra
  license_extra=$(find "$fragment/LICENSES" -mindepth 1 -maxdepth 1 ! -type f -printf '%f\n')
  [[ -z "$license_extra" ]] || die "$label fragment LICENSES contains non-file entries: $license_extra"
}

check_fragment_layout "$c_fragment" C "${c_tools[@]}"
check_fragment_layout "$gnu_fragment" GNU "${gnu_tools[@]}"

# ---- provenance 校验：合并前先钉死输入事实 --------------------------------

c_prov="$c_fragment/provenance.json"
gnu_prov="$gnu_fragment/provenance.json"
[[ -s "$c_prov" ]] || die "missing C fragment provenance: $c_prov"
[[ -s "$gnu_prov" ]] || die "missing GNU fragment provenance: $gnu_prov"
jq -e . "$c_prov" >/dev/null 2>&1 || die "C fragment provenance is not valid JSON: $c_prov"
jq -e . "$gnu_prov" >/dev/null 2>&1 || die "GNU fragment provenance is not valid JSON: $gnu_prov"

triple=$(jq -er --arg t "$target" '.targets[$t].clang_target_triple' "$sysroot_lock") ||
  die "sysroot lock has no clang_target_triple for $target"
gnu_triple=$(jq -er --arg t "$target" '.targets[$t].gnu_target_triple' "$gnu_lock") ||
  die "GNU lock has no gnu_target_triple for $target"
[[ "$triple" == "$gnu_triple" ]] ||
  die "clang and GNU target triples disagree: $triple != $gnu_triple"
gcc_version=$(jq -er '.gcc_version' "$gnu_lock") ||
  die "GNU lock has no gcc_version"

assert_prov_facts() {
  local prov=$1 label=$2 triple_expected=$3
  [[ $(jq -er '.target' "$prov") == "$target" ]] ||
    die "$label provenance target does not match $target"
  [[ $(jq -er '.toolchain_mode' "$prov") == "cross" ]] ||
    die "$label provenance toolchain_mode is not cross"
  [[ $(jq -er '.target_triple' "$prov") == "$triple_expected" ]] ||
    die "$label provenance target_triple does not match the pinned triple $triple_expected"
}

assert_prov_facts "$c_prov" C "$triple"
assert_prov_facts "$gnu_prov" GNU "$gnu_triple"

# C fragment 全局事实（Stage 3 合同）：clang + lld、无 OpenMP、ubuntu 宿主。
[[ $(jq -er '.compiler_family' "$c_prov") == "clang" ]] ||
  die "C provenance compiler_family is not clang"
c_linker=$(jq -er '.linker' "$c_prov") || die "C provenance has no linker"
[[ "$c_linker" == "lld" ]] || die "C provenance linker is not lld"
jq -e '.openmp_runtime == null' "$c_prov" >/dev/null ||
  die "C provenance openmp_runtime must be null (no OpenMP)"
c_compiler_version=$(jq -er '.compiler_version' "$c_prov") ||
  die "C provenance has no compiler_version"
[[ -n "$c_compiler_version" ]] || die "C provenance compiler_version is empty"
c_build_host=$(jq -er '.build_host' "$c_prov") ||
  die "C provenance has no build_host"

# GNU fragment 全局事实（Stage 5 合同）：gcc + libgomp、与 lock 相同的版本。
[[ $(jq -er '.compiler_family' "$gnu_prov") == "gcc" ]] ||
  die "GNU provenance compiler_family is not gcc"
[[ $(jq -er '.openmp_runtime' "$gnu_prov") == "libgomp" ]] ||
  die "GNU provenance openmp_runtime is not libgomp"
[[ $(jq -er '.compiler_version' "$gnu_prov") == "$gcc_version" ]] ||
  die "GNU provenance compiler_version does not match the locked $gcc_version"

# 宿主事实只用于全局 build 元数据：两条链都在 ubuntu-24.04 amd64 宿主上交叉
# 构建，per-tool 记录仍逐字保留各 fragment 自己登记的 build_host。
case "$c_build_host" in
  ubuntu-24.04-amd64) ;;
  *) die "unexpected C provenance build_host: $c_build_host" ;;
esac
gnu_build_host=$(jq -er '.build_host' "$gnu_prov") || die "GNU provenance has no build_host"
case "$gnu_build_host" in
  ubuntu-24.04 | ubuntu-24.04-amd64) ;;
  *) die "unexpected GNU provenance build_host: $gnu_build_host" ;;
esac
build_triplet=x86_64-linux-gnu

check_prov_tools() {
  local prov=$1 label=$2
  shift 2
  local expected=("$@")
  local names count
  names=$(jq -r '.tools[].name' "$prov" | LC_ALL=C sort)
  count=$(jq -er '.tools | length' "$prov")
  [[ "$count" -eq "${#expected[@]}" ]] ||
    die "$label provenance must record exactly ${#expected[@]} tools, found $count"
  local expected_names
  expected_names=$(printf '%s\n' "${expected[@]}" | LC_ALL=C sort)
  [[ "$names" == "$expected_names" ]] ||
    die "$label provenance tool names do not match ${expected[*]}"
}

check_prov_tools "$c_prov" C "${c_tools[@]}"
check_prov_tools "$gnu_prov" GNU "${gnu_tools[@]}"

# 同名 binary 视为错误：两个 fragment 的工具名本应不相交。
overlap=$(comm -12 \
  <(printf '%s\n' "${c_tools[@]}" | LC_ALL=C sort) \
  <(printf '%s\n' "${gnu_tools[@]}" | LC_ALL=C sort))
[[ -z "$overlap" ]] || die "C and GNU fragments overlap on tools: $overlap"

prov_sha256() {
  local prov=$1 tool=$2
  jq -er --arg tool "$tool" \
    '.tools[] | select(.name == $tool) | .sha256' "$prov" ||
    die "provenance has no sha256 for $tool"
}

assert_sha_matches_binary() {
  local path=$1 expected=$2 label=$3
  local actual
  actual=$(sha256sum "$path" | awk '{print $1}')
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] ||
    die "$label provenance sha256 is not a sha-256 digest: $expected"
  [[ "$actual" == "$expected" ]] ||
    die "$label sha256 mismatch: provenance $expected != binary $actual"
}

# 工具名里的 "-" 不能出现在 bash 变量名里，动态变量统一用 "_" 代替。
for tool in "${c_tools[@]}"; do
  [[ -f "$c_fragment/bin/$tool" && -s "$c_fragment/bin/$tool" ]] ||
    die "C fragment binary is missing or empty: $tool"
  sha=$(prov_sha256 "$c_prov" "$tool")
  assert_sha_matches_binary "$c_fragment/bin/$tool" "$sha" "C $tool"
  [[ $(jq -er --arg tool "$tool" \
    '.tools[] | select(.name == $tool) | .compiler_family' "$c_prov") == "clang" ]] ||
    die "C provenance $tool compiler_family is not clang"
  printf -v "c_sha_${tool//-/_}" '%s' "$sha"
done

for tool in "${gnu_tools[@]}"; do
  key=${tool//-/_}
  [[ -f "$gnu_fragment/bin/$tool" && -s "$gnu_fragment/bin/$tool" ]] ||
    die "GNU fragment binary is missing or empty: $tool"
  sha=$(prov_sha256 "$gnu_prov" "$tool")
  assert_sha_matches_binary "$gnu_fragment/bin/$tool" "$sha" "GNU $tool"
  record=$(jq -cer --arg tool "$tool" \
    '.tools[] | select(.name == $tool) |
     {compiler_family, compiler_version, target_triple, build_host, openmp_runtime, source}' \
    "$gnu_prov") || die "GNU provenance record for $tool is incomplete"
  [[ $(jq -er '.compiler_family' <<<"$record") == "gcc" ]] ||
    die "GNU provenance $tool compiler_family is not gcc"
  [[ $(jq -er '.openmp_runtime' <<<"$record") == "libgomp" ]] ||
    die "GNU provenance $tool openmp_runtime is not libgomp"
  compiler_version=$(jq -er '.compiler_version' <<<"$record")
  [[ -n "$compiler_version" ]] || die "GNU provenance $tool compiler_version is empty"
  [[ $(jq -er '.target_triple' <<<"$record") == "$gnu_triple" ]] ||
    die "GNU provenance $tool target_triple does not match $gnu_triple"
  build_host=$(jq -er '.build_host' <<<"$record")
  [[ -n "$build_host" ]] || die "GNU provenance $tool build_host is empty"
  printf -v "gnu_sha_$key" '%s' "$sha"
  printf -v "gnu_compiler_version_$key" '%s' "$compiler_version"
  printf -v "gnu_build_host_$key" '%s' "$build_host"
done

# 源身份核对：GNU provenance 记录的 URL/SHA256 必须与 tools/lock.json 一致。
npb_source_url=$(ecs_lock_tool_field npb-ep source_url) ||
  die "tools lock has no npb source_url"
npb_source_sha=$(ecs_lock_tool_field npb-ep source_sha256) ||
  die "tools lock has no npb source_sha256"
stream_source_url=$(ecs_lock_tool_field stream source_url) ||
  die "tools lock has no stream source_url"
stream_source_sha=$(ecs_lock_tool_field stream source_sha256) ||
  die "tools lock has no stream source_sha256"

check_gnu_source() {
  local tool=$1 url_expected=$2 sha_expected=$3
  jq -e --arg tool "$tool" --arg url "$url_expected" --arg sha "$sha_expected" \
    '.tools[] | select(.name == $tool) | .source.url == $url and .source.sha256 == $sha' \
    "$gnu_prov" >/dev/null ||
    die "GNU provenance source for $tool does not match the pinned tools lock identity"
}
check_gnu_source npb-ep "$npb_source_url" "$npb_source_sha"
check_gnu_source npb-ft "$npb_source_url" "$npb_source_sha"
check_gnu_source stream "$stream_source_url" "$stream_source_sha"

# STREAM 的版本取自构建自身写出的许可文件首行（官方分发是单一 C 文件，
# 没有 tag/commit；其身份是版本号 + parameters 里钉死的 source sha256）。
stream_license="$gnu_fragment/LICENSES/stream.LICENSE"
[[ -f "$stream_license" ]] || die "GNU fragment is missing LICENSES/stream.LICENSE"
stream_line=$(head -n1 "$stream_license")
stream_pattern='^STREAM ([0-9][0-9.]*), John D\. McCalpin\.$'
if [[ "$stream_line" =~ $stream_pattern ]]; then
  stream_version=${BASH_REMATCH[1]}
else
  die "could not read the STREAM version from $stream_license: $stream_line"
fi

# ---- 合并 ----------------------------------------------------------------

out_stage="$stage_root/$target"
[[ ! -e "$out_stage" ]] || die "output stage already exists: $out_stage"
mkdir -p "$out_stage/bin" "$out_stage/LICENSES"

for tool in "${c_tools[@]}"; do
  cp -- "$c_fragment/bin/$tool" "$out_stage/bin/$tool"
  chmod 0755 "$out_stage/bin/$tool"
done
for tool in "${gnu_tools[@]}"; do
  cp -- "$gnu_fragment/bin/$tool" "$out_stage/bin/$tool"
  chmod 0755 "$out_stage/bin/$tool"
done

# LICENSES/ 合并自两个 fragment；文件名冲突视为错误。
for fragment in "$c_fragment" "$gnu_fragment"; do
  while IFS= read -r -d '' license; do
    name=$(basename "$license")
    [[ ! -e "$out_stage/LICENSES/$name" ]] ||
      die "license filename collision: $name exists in both fragments"
    cp -- "$license" "$out_stage/LICENSES/$name"
    chmod 0644 "$out_stage/LICENSES/$name"
  done < <(find "$fragment/LICENSES" -mindepth 1 -maxdepth 1 -type f -print0 | LC_ALL=C sort -z)
done

# ---- 最终 manifest（ecs-tools.manifest/v1，沿用既有 schema 约定）----------
#
# 全局只记录 build.toolchain_mode=cross；编译器事实逐工具记录在
# parameters.compiler_family / compiler_version / target_triple / build_host /
# openmp_runtime / sha256，与二进制逐一对应。工具元数据（upstream/version/
# tag/source）与 Linux 发布构建一样取自 tools/lock.json。

goos=$(ecs_lock_target_field "$target" goos) || die "tools lock has no goos for $target"
goarch=$(ecs_lock_target_field "$target" goarch) || die "tools lock has no goarch for $target"
architecture=$(ecs_lock_target_field "$target" package) ||
  die "tools lock has no package architecture for $target"
openssl_target=$(ecs_lock_target_field "$target" openssl_target) ||
  die "tools lock has no OpenSSL target for $target"

sysbench_upstream=$(ecs_lock_tool_field sysbench upstream)
sysbench_version=$(ecs_lock_tool_field sysbench version)
sysbench_tag=$(ecs_lock_tool_field sysbench tag)
sysbench_commit=$(ecs_lock_tool_field sysbench commit)
zstd_upstream=$(ecs_lock_tool_field zstd upstream)
zstd_version=$(ecs_lock_tool_field zstd version)
zstd_tag=$(ecs_lock_tool_field zstd tag)
zstd_commit=$(ecs_lock_tool_field zstd commit)
openssl_upstream=$(ecs_lock_tool_field openssl upstream)
openssl_version=$(ecs_lock_tool_field openssl version)
openssl_tag=$(ecs_lock_tool_field openssl tag)
openssl_commit=$(ecs_lock_tool_field openssl commit)
fio_upstream=$(ecs_lock_tool_field fio upstream)
fio_version=$(ecs_lock_tool_field fio version)
fio_tag=$(ecs_lock_tool_field fio tag)
fio_commit=$(ecs_lock_tool_field fio commit)
iperf3_upstream=$(ecs_lock_tool_field iperf3 upstream)
iperf3_version=$(ecs_lock_tool_field iperf3 version)
iperf3_tag=$(ecs_lock_tool_field iperf3 tag)
iperf3_commit=$(ecs_lock_tool_field iperf3 commit)
npb_upstream=$(ecs_lock_tool_field npb-ep upstream)
npb_version=$(ecs_lock_tool_field npb-ep version)
npb_tag=$(ecs_lock_tool_field npb-ep tag)
stream_upstream=$(ecs_lock_tool_field stream upstream)
stream_array_size=$(ecs_lock_stream_field array_size)
stream_ntimes=$(ecs_lock_stream_field ntimes)

git_source() {
  local repository=$1 commit=$2
  printf 'git+https://github.com/%s.git@%s\n' "$repository" "$commit"
}

supported_architectures_json=$(printf '%s\n' "${ECS_FREEBSD_ARCHES[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
supported_targets_json=$(printf '%s\n' "${ECS_FREEBSD_TARGET_IDS[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
npb_build_flags_json=$(jq -cn '["gfortran","-O3","-fopenmp","-static","CLASS=A","RAND=randi8","OMP"]')
stream_build_flags_json=$(jq -cn --argjson array_size "$stream_array_size" --argjson ntimes "$stream_ntimes" \
  '["gcc","-O3","-fopenmp","-static","-static-libgcc",("-DSTREAM_ARRAY_SIZE=" + ($array_size | tostring)),("-DNTIMES=" + ($ntimes | tostring))]')
openssl_build_flags_json=$(jq -cn --arg target "$openssl_target" \
  '[$target, "-O3", "-static", "no-shared", "no-module", "no-pinshared", "no-tests", "no-docs",
    "no-ssl", "no-sock", "no-dgram", "no-http", "no-cmp", "no-cms", "no-ct", "no-ocsp",
    "no-dso", "no-engine", "no-static-engine", "no-legacy", "no-async", "no-atexit",
    "no-autoload-config", "no-cached-fetch", "no-comp", "no-dh", "no-dsa", "no-ec",
    "no-aria", "no-bf", "no-blake2", "no-camellia", "no-cast", "no-cmac", "no-des",
    "no-idea", "no-md4", "no-mdc2", "no-ocb", "no-rc2", "no-rc4", "no-rmd160",
    "no-scrypt", "no-seed", "no-siphash", "no-siv", "no-sm2", "no-sm3", "no-sm4",
    "no-whirlpool", "no-ml-dsa", "no-ml-kem", "no-slh-dsa", "no-rfc3779", "no-srp",
    "no-srtp", "no-ts"]')

manifest="$out_stage/manifest.json"
jq -n \
  --arg target "$target" \
  --arg goos "$goos" \
  --arg goarch "$goarch" \
  --arg architecture "$architecture" \
  --argjson supported_architectures "$supported_architectures_json" \
  --argjson supported_targets "$supported_targets_json" \
  --arg build_triplet "$build_triplet" \
  --arg target_triplet "$triple" \
  --arg c_compiler_version "$c_compiler_version" \
  --arg c_build_host "$c_build_host" \
  --arg c_linker "$c_linker" \
  --arg sysbench_upstream "$sysbench_upstream" \
  --arg sysbench_version "$sysbench_version" \
  --arg sysbench_tag "$sysbench_tag" \
  --arg sysbench_source "$(git_source "$(ecs_lock_tool_field sysbench repository)" "$sysbench_commit")" \
  --arg sysbench_commit "$sysbench_commit" \
  --arg sysbench_sha256 "${c_sha_sysbench}" \
  --arg zstd_upstream "$zstd_upstream" \
  --arg zstd_version "$zstd_version" \
  --arg zstd_tag "$zstd_tag" \
  --arg zstd_source "$(git_source "$(ecs_lock_tool_field zstd repository)" "$zstd_commit")" \
  --arg zstd_commit "$zstd_commit" \
  --arg zstd_sha256 "${c_sha_zstd}" \
  --arg openssl_upstream "$openssl_upstream" \
  --arg openssl_version "$openssl_version" \
  --arg openssl_tag "$openssl_tag" \
  --arg openssl_source "$(git_source "$(ecs_lock_tool_field openssl repository)" "$openssl_commit")" \
  --arg openssl_commit "$openssl_commit" \
  --arg openssl_target "$openssl_target" \
  --arg openssl_sha256 "${c_sha_openssl}" \
  --arg fio_upstream "$fio_upstream" \
  --arg fio_version "$fio_version" \
  --arg fio_tag "$fio_tag" \
  --arg fio_source "$(git_source "$(ecs_lock_tool_field fio repository)" "$fio_commit")" \
  --arg fio_commit "$fio_commit" \
  --arg fio_sha256 "${c_sha_fio}" \
  --arg iperf3_upstream "$iperf3_upstream" \
  --arg iperf3_version "$iperf3_version" \
  --arg iperf3_tag "$iperf3_tag" \
  --arg iperf3_source "$(git_source "$(ecs_lock_tool_field iperf3 repository)" "$iperf3_commit")" \
  --arg iperf3_commit "$iperf3_commit" \
  --arg iperf3_sha256 "${c_sha_iperf3}" \
  --arg npb_upstream "$npb_upstream" \
  --arg npb_version "$npb_version" \
  --arg npb_tag "$npb_tag" \
  --arg npb_source "$npb_source_url" \
  --arg npb_source_sha256 "$npb_source_sha" \
  --arg npb_ep_sha256 "${gnu_sha_npb_ep}" \
  --arg npb_ep_compiler_version "${gnu_compiler_version_npb_ep}" \
  --arg npb_ep_build_host "${gnu_build_host_npb_ep}" \
  --arg npb_ft_sha256 "${gnu_sha_npb_ft}" \
  --arg npb_ft_compiler_version "${gnu_compiler_version_npb_ft}" \
  --arg npb_ft_build_host "${gnu_build_host_npb_ft}" \
  --arg stream_upstream "$stream_upstream" \
  --arg stream_version "$stream_version" \
  --arg stream_source "$stream_source_url" \
  --arg stream_source_sha256 "$stream_source_sha" \
  --arg stream_sha256 "${gnu_sha_stream}" \
  --arg stream_compiler_version "${gnu_compiler_version_stream}" \
  --arg stream_build_host "${gnu_build_host_stream}" \
  --argjson npb_build_flags "$npb_build_flags_json" \
  --argjson stream_build_flags "$stream_build_flags_json" \
  --argjson openssl_build_flags "$openssl_build_flags_json" \
  --argjson stream_array_size "$stream_array_size" \
  --argjson stream_ntimes "$stream_ntimes" \
  ' {
      schema_version: "ecs-tools.manifest/v1",
      target: $target,
      goos: $goos,
      goarch: $goarch,
      architecture: $architecture,
      supported_architectures: $supported_architectures,
      supported_targets: $supported_targets,
      build: {
        toolchain_mode: "cross",
        build_triplet: $build_triplet,
        target_triplet: $target_triplet,
        smoke_runner: "none",
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
          build_flags: ["--with-system-luajit", "--with-system-ck", "--with-extra-ldflags=-all-static", "--without-mysql", "--without-pgsql", "--without-drizzle", "--without-attachsql", "--without-oracle"],
          enabled_features: ["cpu", "LuaJIT", "Concurrency Kit"],
          disabled_features: ["mysql", "pgsql", "drizzle", "attachsql", "oracle"],
          architecture: $architecture,
          license: "GPL-2.0-only",
          parameters: {source_commit: $sysbench_commit, compiler_family: "clang", compiler_version: $c_compiler_version, target_triple: $target_triplet, build_host: $c_build_host, openmp_runtime: "none", linker: $c_linker, sha256: $sysbench_sha256, fully_static: true, stripped: false}
        },
        {
          name: "zstd",
          upstream: $zstd_upstream,
          version: $zstd_version,
          tag_or_commit: $zstd_tag,
          source: $zstd_source,
          build_flags: ["make -C programs zstd-release", "HAVE_ZLIB=0", "HAVE_LZMA=0", "HAVE_LZ4=0", "ZSTD_LEGACY_SUPPORT=0", "MOREFLAGS=-O2 -fPIC -static -DZSTD_NODICT -DZSTD_NOTRACE"],
          enabled_features: ["benchmark", "multithread", "compression", "decompression"],
          disabled_features: ["zlib", "lzma", "lz4", "legacy-formats", "dictionary-builder", "trace"],
          architecture: $architecture,
          license: "BSD-3-Clause OR GPL-2.0-only",
          parameters: {source_commit: $zstd_commit, compiler_family: "clang", compiler_version: $c_compiler_version, target_triple: $target_triplet, build_host: $c_build_host, openmp_runtime: "none", linker: $c_linker, sha256: $zstd_sha256, fully_static: true, stripped: false}
        },
        {
          name: "npb-ep",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: $npb_tag,
          source: $npb_source,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "EP", "Class A", "OpenMP"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          parameters: {source_sha256: $npb_source_sha256, implementation: "NPB3.4-OMP", benchmark: "EP", problem_class: "A", problem_size: "2^29 random numbers reported", compiler_flags: "-O3 -fopenmp", linker_flags: "-O3 -fopenmp -static", random_generator: "randi8", thread_modes: ["1T", "NT"], ci_smoke_class: "none", compiler_family: "gcc", compiler_version: $npb_ep_compiler_version, target_triple: $target_triplet, build_host: $npb_ep_build_host, openmp_runtime: "libgomp", sha256: $npb_ep_sha256, fully_static: true, stripped: false}
        },
        {
          name: "npb-ft",
          upstream: $npb_upstream,
          version: $npb_version,
          tag_or_commit: $npb_tag,
          source: $npb_source,
          build_flags: $npb_build_flags,
          enabled_features: ["NPB3.4-OMP", "FT", "Class A", "OpenMP", "3D FFT"],
          disabled_features: ["MPI", "other NPB kernels", "other problem classes"],
          architecture: $architecture,
          license: "NASA-NPB-permissive",
          parameters: {source_sha256: $npb_source_sha256, implementation: "NPB3.4-OMP", benchmark: "FT", problem_class: "A", dimensions: "256x256x128", iterations: 6, compiler_flags: "-O3 -fopenmp", linker_flags: "-O3 -fopenmp -static", random_generator: "randi8", thread_modes: ["1T", "NT"], ci_smoke_class: "none", compiler_family: "gcc", compiler_version: $npb_ft_compiler_version, target_triple: $target_triplet, build_host: $npb_ft_build_host, openmp_runtime: "libgomp", sha256: $npb_ft_sha256, fully_static: true, stripped: false}
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
          parameters: {source_commit: $openssl_commit, configure_target: $openssl_target, generated_target: "build_generated", build_target: "apps/openssl", algorithms: ["aes-256-gcm", "chacha20-poly1305", "sha256"], compiler_family: "clang", compiler_version: $c_compiler_version, target_triple: $target_triplet, build_host: $c_build_host, openmp_runtime: "none", linker: $c_linker, sha256: $openssl_sha256, fully_static: true, stripped: false}
        },
        {
          name: "stream",
          upstream: $stream_upstream,
          version: $stream_version,
          tag_or_commit: $stream_version,
          source: $stream_source,
          build_flags: $stream_build_flags,
          enabled_features: ["Copy", "Scale", "Add", "Triad", "OpenMP"],
          disabled_features: [],
          architecture: $architecture,
          license: "STREAM-custom",
          parameters: {source_sha256: $stream_source_sha256, array_size: $stream_array_size, ntimes: $stream_ntimes, thread_modes: ["1T", "NT"], compiler_family: "gcc", compiler_version: $stream_compiler_version, target_triple: $target_triplet, build_host: $stream_build_host, openmp_runtime: "libgomp", sha256: $stream_sha256, fully_static: true, stripped: false}
        },
        {
          name: "fio",
          upstream: $fio_upstream,
          version: $fio_version,
          tag_or_commit: $fio_tag,
          source: $fio_source,
          build_flags: ["--build-static", "--disable-numa", "--disable-rdma", "--disable-rados", "--disable-rbd", "--disable-gfapi", "--disable-http", "--disable-pmem", "--disable-libzbc", "--disable-xnvme", "--disable-libblkio", "--disable-libnfs", "--disable-dfs", "--disable-tcmalloc", "--disable-native", "generated-config: require CONFIG_POSIXAIO=y", "generated-config: omit CONFIG_LIBAIO"],
          enabled_features: ["posixaio", "psync"],
          disabled_features: ["io_uring", "libaio", "ceph", "rbd", "rados", "gluster", "gfapi", "rdma", "http", "pmem"],
          architecture: $architecture,
          license: "GPL-2.0-only",
          parameters: {source_commit: $fio_commit, compiler_family: "clang", compiler_version: $c_compiler_version, target_triple: $target_triplet, build_host: $c_build_host, openmp_runtime: "none", linker: $c_linker, sha256: $fio_sha256, fully_static: true, stripped: false}
        },
        {
          name: "iperf3",
          upstream: $iperf3_upstream,
          version: $iperf3_version,
          tag_or_commit: $iperf3_tag,
          source: $iperf3_source,
          build_flags: ["--enable-static-bin", "--without-sctp", "--without-openssl", "--without-ldconfig"],
          enabled_features: ["tcp", "udp", "ipv4", "ipv6", "parallel", "reverse", "json"],
          disabled_features: ["sctp", "openssl/auth"],
          architecture: $architecture,
          license: "BSD-3-Clause",
          parameters: {source_commit: $iperf3_commit, compiler_family: "clang", compiler_version: $c_compiler_version, target_triple: $target_triplet, build_host: $c_build_host, openmp_runtime: "none", linker: $c_linker, sha256: $iperf3_sha256, fully_static: true, stripped: false}
        }
      ]
    }' | jq . >"$manifest"

# 合并自检：最终 stage 必须恰好包含 8 个工具与生成的 manifest。
actual=$(find "$out_stage/bin" -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)
expected=$(printf '%s\n' "${all_tools[@]}" | LC_ALL=C sort)
[[ "$actual" == "$expected" ]] ||
  die "unexpected merged stage bin contents:
expected:
$expected
actual:
$actual"
[[ $(jq -er '.tools | length' "$manifest") -eq 8 ]] ||
  die "merged manifest does not record exactly 8 tools"

echo "merge-freebsd-tools-stage: $target merged stage at $out_stage" >&2
find "$out_stage" -maxdepth 2 -type f | LC_ALL=C sort >&2

#!/usr/bin/env bash
set -Eeuo pipefail

# FreeBSD 发布物级端到端验收（artifact-level E2E）。
#
# 这个脚本回答的问题是：真实发布布局被真实 run.sh 消费时，FreeBSD 这条路径
# 是否仍然成立。它不重新构建任何东西，也不联网：
#
#   发布布局（ecs_<target>.tar.gz + ecs-tools_<target>.tar.gz + checksums.txt）
#     -> run.sh（真实下载器边界被本地 shim 接管）
#     -> SHA-256 校验 -> 解包真实 ecs -> version --bundle 解析 Bundle 基址
#     -> 解包真实固定工具包 -> 只把本次所需 binary 放进临时 PATH
#     -> ecs plan（FreeBSD 平台解析）-> 真实基准运行 -> 真实报告
#
# Case E 验收 bundle 归档本身的完整性：SHA-256 对 checksums.txt、归档成员恰
# 好是 8 个工具 + manifest + 许可文件（无 ping/nexttrace-tiny/多余文件）、
# manifest（ecs-tools.manifest/v1，toolchain_mode=cross）逐工具 sha256 与解包
# 后的二进制一致、逐工具 FreeBSD 静态 ELF 身份。8 个工具的真实执行由
# freebsd-tools.yml 的 REAL GATE 在真实 FreeBSD 15.1 VM 内对合并 stage 承担，
# stage 与 bundle 的工具字节经 manifest 逐工具 sha256 一一对应；本脚本的
# Cases A/D 仍真实执行 stream（bundle 内始终有工具被真实消费）。两个 FreeBSD
# 目标跑同一套口径。
#
# 与 scripts/run_test.sh 的分工：run_test.sh 在 Linux 上用 fixture 二进制覆盖
# wrapper 的确定性边界；这里用真实 FreeBSD 二进制、真实归档和真实工具，只把
# 网络传输替换掉。两者都覆盖 wrapper，但只有这里能证明"发布的字节确实能被
# 真实 run.sh 消费并跑出真实报告"。
#
# 三条网络路径都显式覆盖，而不是依赖 CI 镜像恰好装了什么：
#   A. curl 分支：把 curl shim 放在 PATH 最前。
#   B. FreeBSD base 分支：PATH 里没有 curl/wget，只留 timeout shim 拦截
#      `/usr/bin/fetch -q -o DEST URL`，断言这条分支真的被走到。
#   C. Ookla 失败关闭：FreeBSD 没有 Ookla 客户端，选中必须直接终止。

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_artifact_e2e.sh --layout DIR [--target freebsd_amd64] [--module NAME]

  --layout DIR    发布布局根目录，必须包含：
                    DIR/ecs-release/{ecs_<target>.tar.gz,checksums.txt}
                    DIR/bundle-release/{ecs-tools_<target>.tar.gz,checksums.txt}
  --target TARGET freebsd_amd64（默认）或 freebsd_arm64
  --module NAME   真实基准模块，默认 memory（STREAM，最轻且不需要语料）

必须在真实 FreeBSD 客户机上以普通用户运行，需要 bash、jq、tar、sha256。
USAGE
}

die() {
  echo "freebsd-artifact-e2e: $*" >&2
  exit 1
}

layout_root=""
target=freebsd_amd64
module=memory
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --layout)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--layout requires a value"
      layout_root=$2
      shift 2
      ;;
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --module)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--module requires a value"
      module=$2
      shift 2
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

[[ -n "$layout_root" ]] || {
  usage
  exit 2
}
[[ "$layout_root" = /* ]] || die "layout root must be an absolute path: $layout_root"
[[ -d "$layout_root" ]] || die "layout root does not exist: $layout_root"

[[ "$(id -u)" -ne 0 ]] || die "this gate must run as an ordinary user"
[[ "$(uname -s)" == FreeBSD ]] || die "this gate must run on FreeBSD, got $(uname -s)"

case "$target" in
  freebsd_amd64) guest_arch=amd64 ;;
  freebsd_arm64) guest_arch=arm64 ;;
  *) die "unsupported target: $target" ;;
esac
host_arch=$(uname -m)
[[ "$host_arch" == "$guest_arch" ]] ||
  die "target $target does not match the guest architecture $host_arch"

for required_command in bash jq tar sha256 file; do
  command -v "$required_command" >/dev/null 2>&1 ||
    die "missing required command: $required_command"
done

ecs_asset="ecs_${target}.tar.gz"
tools_asset="ecs-tools_${target}.tar.gz"
ecs_release_dir="$layout_root/ecs-release"
bundle_release_dir="$layout_root/bundle-release"

for required_file in \
  "$ecs_release_dir/$ecs_asset" \
  "$ecs_release_dir/checksums.txt" \
  "$bundle_release_dir/$tools_asset" \
  "$bundle_release_dir/checksums.txt"; do
  [[ -s "$required_file" ]] || die "release layout is missing $required_file"
done

# Bundle 基址由下载后的 ecs 自己报告，所以测试必须从发布布局里读出同一个值，
# 而不是另写一份。tools/BUNDLE 是那条版本线的唯一来源。
bundle_version=$(<"$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/tools/BUNDLE")
[[ -n "$bundle_version" ]] || die "tools/BUNDLE is empty"

repo_slug=ecs-artifact-e2e/ecs
ecs_version=v-artifact-e2e
ecs_base="https://github.com/${repo_slug}/releases/download/${ecs_version}"
bundle_base="https://github.com/${repo_slug}/releases/download/${bundle_version}"

scratch=$(mktemp -d "${TMPDIR:-/tmp}/ecs-freebsd-artifact-e2e.XXXXXX")
echo "freebsd-artifact-e2e: guest=$(uname -s)/$host_arch target=$target bundle=$bundle_version"
echo "freebsd-artifact-e2e: scratch=$scratch"

# 成功即清理；失败保留现场，因为这里唯一有价值的证据就是那次失败留下的布局、
# 日志和临时 PATH。
cleanup_scratch() {
  local status=$?
  trap - EXIT
  if [[ "$status" -eq 0 ]]; then
    rm -rf -- "$scratch"
  else
    echo "freebsd-artifact-e2e: scratch kept at $scratch" >&2
  fi
  exit "$status"
}
trap cleanup_scratch EXIT

shim_dir="$scratch/shim"
log_dir="$scratch/log"
mkdir -p "$shim_dir" "$log_dir"

# 下载器 shim：只认识这个布局里的四个 URL，其他一律留下哨兵并失败，所以本
# 测试不可能悄悄访问网络。它同时把 argv 以 NUL 分隔记下来，供 argv 形状断言。
write_downloader_shim() {
  local destination=$1
  cat >"$destination" <<'SHIM'
#!/bin/sh
set -eu

printf '%s\0' "$@" >>"$ECS_ARTIFACT_E2E_LOG/fetch-args.log"

url=""
dest=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o | -O)
      shift
      [ "$#" -gt 0 ] || exit 64
      dest=$1
      ;;
    https://*) url=$1 ;;
  esac
  shift
done

case "$url" in
  "$ECS_ARTIFACT_E2E_ECS_BASE/$ECS_ARTIFACT_E2E_ECS_ASSET") source_path="$ECS_ARTIFACT_E2E_ECS_DIR/$ECS_ARTIFACT_E2E_ECS_ASSET" ;;
  "$ECS_ARTIFACT_E2E_ECS_BASE/checksums.txt") source_path="$ECS_ARTIFACT_E2E_ECS_DIR/checksums.txt" ;;
  "$ECS_ARTIFACT_E2E_BUNDLE_BASE/checksums.txt") source_path="$ECS_ARTIFACT_E2E_BUNDLE_DIR/checksums.txt" ;;
  "$ECS_ARTIFACT_E2E_BUNDLE_BASE/$ECS_ARTIFACT_E2E_TOOLS_ASSET") source_path="$ECS_ARTIFACT_E2E_BUNDLE_DIR/$ECS_ARTIFACT_E2E_TOOLS_ASSET" ;;
  *)
    : >"$ECS_ARTIFACT_E2E_LOG/unexpected-network"
    exit 90
    ;;
esac
[ -n "$dest" ] || exit 65
printf '%s\n' "$url" >>"$ECS_ARTIFACT_E2E_LOG/fetch.log"
cp "$source_path" "$dest"
SHIM
  chmod 0755 "$destination"
}

write_downloader_shim "$shim_dir/curl"
write_downloader_shim "$shim_dir/wget"

# 导出给 shim 的布局坐标。
export ECS_ARTIFACT_E2E_LOG="$log_dir"
export ECS_ARTIFACT_E2E_ECS_BASE="$ecs_base"
export ECS_ARTIFACT_E2E_BUNDLE_BASE="$bundle_base"
export ECS_ARTIFACT_E2E_ECS_ASSET="$ecs_asset"
export ECS_ARTIFACT_E2E_TOOLS_ASSET="$tools_asset"
export ECS_ARTIFACT_E2E_ECS_DIR="$ecs_release_dir"
export ECS_ARTIFACT_E2E_BUNDLE_DIR="$bundle_release_dir"

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

# 只暴露真实系统目录里被 wrapper 需要的命令，用来证明"没有 curl/wget 时走
# base fetch 分支"。刻意不软链 curl 和 wget，也不软链 timeout——timeout 由
# 本测试自己的 shim 提供。
make_fetch_only_path() {
  local path=$1 command_name source
  mkdir -p "$path"
  # `command -v` also reports shell builtins by bare name. Only absolute paths
  # are linked, otherwise the reduced PATH would contain dangling links.
  for command_name in sh uname tr mkdir mktemp cp chmod mv id awk sed sort \
    grep wc cat rm ln dirname basename env date \
    sha256 sha256sum shasum tar gzip; do
    source=$(command -v "$command_name" 2>/dev/null) || continue
    case "$source" in
      /*) ;;
      *) continue ;;
    esac
    ln -s "$source" "$path/$command_name"
  done
}

# timeout shim：拦截 `timeout <max> /usr/bin/fetch -q -o DEST URL`。它先断言
# 这条 argv 形状确实是 run.sh 的 FreeBSD base 分支，再把布局文件交给它。
fetch_only_path="$scratch/fetch-only-path"
make_fetch_only_path "$fetch_only_path"
cat >"$fetch_only_path/timeout" <<'SHIM'
#!/bin/sh
set -eu

printf '%s\0' "$@" >>"$ECS_ARTIFACT_E2E_LOG/fetch-args.log"
[ "$#" -eq 6 ] || {
  printf 'freebsd fetch branch reached timeout with %s arguments\n' "$#" >&2
  exit 91
}
[ "$2" = /usr/bin/fetch ] || {
  printf 'freebsd fetch branch did not name /usr/bin/fetch: %s\n' "$2" >&2
  exit 91
}
[ "$3" = -q ] && [ "$4" = -o ] || {
  printf 'freebsd fetch branch argv changed: %s %s\n' "$3" "$4" >&2
  exit 91
}
case "$1" in
  '' | *[!0-9]*) exit 91 ;;
esac

exec "$ECS_ARTIFACT_E2E_FETCH_SHIM" "$5" "$6"
SHIM
chmod 0755 "$fetch_only_path/timeout"

# base 分支的取文件逻辑单独放在一个 shim 里，供 timeout shim exec，避免把
# URL 映射表写两份。
cat >"$scratch/fetch-shim" <<'SHIM'
#!/bin/sh
set -eu
dest=$1
url=$2
case "$url" in
  "$ECS_ARTIFACT_E2E_ECS_BASE/$ECS_ARTIFACT_E2E_ECS_ASSET") source_path="$ECS_ARTIFACT_E2E_ECS_DIR/$ECS_ARTIFACT_E2E_ECS_ASSET" ;;
  "$ECS_ARTIFACT_E2E_ECS_BASE/checksums.txt") source_path="$ECS_ARTIFACT_E2E_ECS_DIR/checksums.txt" ;;
  "$ECS_ARTIFACT_E2E_BUNDLE_BASE/checksums.txt") source_path="$ECS_ARTIFACT_E2E_BUNDLE_DIR/checksums.txt" ;;
  "$ECS_ARTIFACT_E2E_BUNDLE_BASE/$ECS_ARTIFACT_E2E_TOOLS_ASSET") source_path="$ECS_ARTIFACT_E2E_BUNDLE_DIR/$ECS_ARTIFACT_E2E_TOOLS_ASSET" ;;
  *)
    : >"$ECS_ARTIFACT_E2E_LOG/unexpected-network"
    exit 90
    ;;
esac
printf '%s\n' "$url" >>"$ECS_ARTIFACT_E2E_LOG/fetch.log"
cp "$source_path" "$dest"
SHIM
chmod 0755 "$scratch/fetch-shim"
export ECS_ARTIFACT_E2E_FETCH_SHIM="$scratch/fetch-shim"

fail() {
  echo "freebsd-artifact-e2e: $*" >&2
  exit 1
}

# 每个 case 独立一份 log/TMPDIR，避免一次失败被另一次的成功掩盖。
reset_case_log() {
  local name=$1
  mkdir -p "$log_dir/$name" "$scratch/tmp-$name"
  echo "$log_dir/$name"
}

expect_fetch_log() {
  local name=$1
  shift
  local case_log="$log_dir/$name"
  [[ ! -e "$case_log/unexpected-network" ]] ||
    fail "$name reached unexpected network access"
  local -a expected=("$@")
  local actual_count
  actual_count=$(wc -l <"$case_log/fetch.log" | tr -d '[:space:]')
  [[ "$actual_count" -eq "${#expected[@]}" ]] ||
    fail "$name fetched $actual_count URLs instead of ${#expected[@]}"
  local index=0
  while IFS= read -r line; do
    [[ "$line" == "${expected[$index]}" ]] ||
      fail "$name fetch $index changed: $line"
    index=$((index + 1))
  done <"$case_log/fetch.log"
}

# 从 wrapper 的 stderr 里取出 ECS_KEEP=1 保留的临时工作目录。
kept_work_dir() {
  local file=$1
  sed -n 's/^ecs: temporary directory kept at \(.*\)$/\1/p' "$file" | sed -n '1p'
}

expected_fetches=(
  "$ecs_base/$ecs_asset"
  "$ecs_base/checksums.txt"
  "$bundle_base/checksums.txt"
  "$bundle_base/$tools_asset"
)

# ---- Case A：curl 分支，真实归档 + 真实固定工具 + 真实基准 ----
case_a_log=$(reset_case_log case-a)
case_a_tmp="$scratch/tmp-case-a"
case_a_stderr="$scratch/case-a.stderr"
if ! ECS_LANG=en ECS_REPOSITORY="$repo_slug" ECS_VERSION="$ecs_version" \
    ECS_AUTO_DEPS=1 ECS_KEEP=1 TMPDIR="$case_a_tmp" \
    PATH="$shim_dir:$PATH" ECS_ARTIFACT_E2E_LOG="$case_a_log" \
    sh "$repo_root/run.sh" --only "$module" --yes --format json \
    >"$scratch/case-a.stdout" 2>"$case_a_stderr"; then
  cat "$case_a_stderr" >&2
  fail "case A wrapper run failed"
fi
expect_fetch_log case-a "${expected_fetches[@]}"

case_a_work=$(kept_work_dir "$case_a_stderr")
[[ -n "$case_a_work" && -d "$case_a_work" ]] ||
  fail "case A did not report a kept work directory"

# 只把本次所需的 binary 放进临时 PATH：多一个或少一个都说明 plan 与暂存脱节。
staged_count=$(find "$case_a_work/bin" -mindepth 1 -maxdepth 1 | wc -l | tr -d '[:space:]')
[[ "$staged_count" -eq 1 ]] ||
  fail "case A staged $staged_count tools for --only $module instead of 1"
[[ -x "$case_a_work/bin/stream" ]] ||
  fail "case A did not stage the frozen stream binary for --only $module"

report_count=$(find "$case_a_tmp" -mindepth 1 -maxdepth 1 -name 'ecs-report-*.json' | wc -l | tr -d '[:space:]')
[[ "$report_count" -eq 1 ]] ||
  fail "case A produced $report_count JSON reports instead of 1"

# 真实基准必须真的跑出测量值，而不是留下一个 warning 结果。
released_ecs="$case_a_work/ecs"
[[ -x "$released_ecs" ]] || fail "case A did not keep the released ecs binary"
case_a_report=$(find "$case_a_tmp" -mindepth 1 -maxdepth 1 -name 'ecs-report-*.json' | sed -n '1p')
# `measurements` carries omitempty, so a module with no usable sample omits the
# key entirely instead of writing an empty array.
jq -e '.results[] | select(.id == "'"$module"'") | ((.measurements // []) | length > 0)' \
  "$case_a_report" >/dev/null ||
  fail "case A $module module produced no measurements"
echo "freebsd-artifact-e2e: case A passed (released ecs, frozen stream, real report)"

# ---- Case B：发布二进制自己的平台解析 ----
plan_full="$scratch/plan-full.json"
"$released_ecs" plan --profile full --exposure any >"$plan_full" ||
  fail "case B plan --profile full failed"

jq -e '.schema_version == "ecs.plan/v1"' "$plan_full" >/dev/null ||
  fail "case B plan reported an unexpected schema version"
jq -e '(.required_tools | index("ping")) == null' "$plan_full" >/dev/null ||
  fail "case B FreeBSD plan still requires the Linux ping tool"
jq -e '(.required_tools | index("nexttrace-tiny")) == null' "$plan_full" >/dev/null ||
  fail "case B FreeBSD plan still requires nexttrace-tiny"
# FreeBSD 固定工具包的完整成员：ping 与 nexttrace-tiny 已由平台解析移除，
# speedtest 仍然在 plan 里（它是独立的签名包源路径，由 wrapper 决定是否终止）。
jq -e '.required_tools | sort ==
  ["fio","iperf3","npb-ep","npb-ft","openssl","speedtest","stream","sysbench","zstd"]' \
  "$plan_full" >/dev/null ||
  fail "case B FreeBSD plan tool set changed: $(jq -c '.required_tools | sort' "$plan_full")"

# base 系统替代品让这三个模块在 FreeBSD 上不再需要任何下载工具。这条断言比
# "不包含 ping"更强：它证明替代是完整的，而不是漏掉一个就报错。
# required_tools 是 nil 切片时序列化成 null，所以这里先归一成空数组。
plan_base="$scratch/plan-base.json"
"$released_ecs" plan --only dns,latency,media,route,backtrace --exposure any >"$plan_base" ||
  fail "case B base-substitute plan failed"
jq -e '(.required_tools // []) == []' "$plan_base" >/dev/null ||
  fail "case B base-substitute modules still require downloads: $(jq -c '.required_tools' "$plan_base")"
echo "freebsd-artifact-e2e: case B passed (FreeBSD RequiredTools resolution)"

# ---- Case C：Ookla 在 FreeBSD 上失败关闭 ----
case_c_log=$(reset_case_log case-c)
case_c_tmp="$scratch/tmp-case-c"
case_c_stderr="$scratch/case-c.stderr"
set +e
ECS_LANG=en ECS_REPOSITORY="$repo_slug" ECS_VERSION="$ecs_version" \
  ECS_AUTO_DEPS=1 TMPDIR="$case_c_tmp" \
  PATH="$shim_dir:$PATH" ECS_ARTIFACT_E2E_LOG="$case_c_log" \
  sh "$repo_root/run.sh" --only ookla --exposure thirdparty --yes \
  >"$scratch/case-c.stdout" 2>"$case_c_stderr"
case_c_status=$?
set -e
[[ "$case_c_status" -ne 0 ]] || fail "case C accepted an Ookla run on FreeBSD"
grep -F 'Ookla speedtest is not available on FreeBSD' "$case_c_stderr" >/dev/null ||
  fail "case C did not fail closed with the FreeBSD Ookla message"
[[ ! -e "$case_c_log/unexpected-network" ]] || fail "case C reached unexpected network access"
if find "$case_c_tmp" -mindepth 1 -maxdepth 1 -name 'ecs-report-*' -print -quit | grep -q .; then
  fail "case C produced a report instead of stopping"
fi
echo "freebsd-artifact-e2e: case C passed (Ookla fails closed)"

# ---- Case D：没有 curl/wget 时走 FreeBSD base fetch ----
case_d_log=$(reset_case_log case-d)
case_d_tmp="$scratch/tmp-case-d"
case_d_stderr="$scratch/case-d.stderr"
if ! ECS_LANG=en ECS_REPOSITORY="$repo_slug" ECS_VERSION="$ecs_version" \
    ECS_AUTO_DEPS=1 ECS_KEEP=1 TMPDIR="$case_d_tmp" \
    PATH="$fetch_only_path" ECS_ARTIFACT_E2E_LOG="$case_d_log" \
    sh "$repo_root/run.sh" --only "$module" --yes --format json \
    >"$scratch/case-d.stdout" 2>"$case_d_stderr"; then
  cat "$case_d_stderr" >&2
  fail "case D FreeBSD base fetch run failed"
fi
expect_fetch_log case-d "${expected_fetches[@]}"
case_d_work=$(kept_work_dir "$case_d_stderr")
[[ -n "$case_d_work" && -d "$case_d_work" ]] ||
  fail "case D did not report a kept work directory"
[[ -x "$case_d_work/bin/stream" ]] ||
  fail "case D did not stage the frozen stream binary through the base fetch path"
echo "freebsd-artifact-e2e: case D passed (FreeBSD base /usr/bin/fetch branch)"

# ---- Case E：bundle 归档内容（发布包完整性）验收 ---------------------------
# Case A-D 证明真实 run.sh 能消费这份发布布局（且真实执行 stream）；case E
# 直接验收 bundle 归档本身的完整性：checksums、成员集合（恰好 8 工具，无
# ping/nexttrace-tiny/多余文件）、manifest 契约与逐工具 sha256、FreeBSD 静态
# ELF 身份。工具的真实执行由 freebsd-tools.yml 的 REAL GATE 对合并 stage 承
# 担：REAL GATE 真实执行的 stage 字节与本归档内的工具字节经 manifest 逐工具
# sha256 一一对应，因此本脚本只验发布包完整性，不重复真执行。
case_e_dir="$scratch/case-e"
mkdir -p "$case_e_dir"

expected_bundle_sha=$(awk -v f="$tools_asset" '$2 == f {print $1; exit}' \
  "$bundle_release_dir/checksums.txt" | tr '[:upper:]' '[:lower:]')
[[ -n "$expected_bundle_sha" ]] ||
  fail "case E bundle checksums have no entry for $tools_asset"
actual_bundle_sha=$(sha256 -q "$bundle_release_dir/$tools_asset" | tr '[:upper:]' '[:lower:]')
[[ "$actual_bundle_sha" == "$expected_bundle_sha" ]] ||
  fail "case E bundle SHA-256 mismatch: checksums $expected_bundle_sha != actual $actual_bundle_sha"

tar -xzf "$bundle_release_dir/$tools_asset" -C "$case_e_dir"

bundle_tools=(sysbench zstd npb-ep npb-ft openssl stream fio iperf3)
expected_bin=$(printf '%s\n' "${bundle_tools[@]}" | LC_ALL=C sort)

archive_bin=$(tar -tzf "$bundle_release_dir/$tools_asset" |
  sed -n 's#^bin/##p' | sed '/^$/d' | LC_ALL=C sort)
[[ "$archive_bin" == "$expected_bin" ]] ||
  fail "case E bundle bin members changed: $(printf '%s ' $archive_bin)"
for forbidden in ping nexttrace-tiny; do
  if grep -Fx "$forbidden" <<<"$archive_bin" >/dev/null; then
    fail "case E bundle contains $forbidden, which the FreeBSD base system provides"
  fi
done

archive_top=$(tar -tzf "$bundle_release_dir/$tools_asset" |
  awk -F/ '{print $1}' | LC_ALL=C sort -u)
expected_top=$(printf '%s\n' LICENSE LICENSES NOTICE bin manifest.json | LC_ALL=C sort -u)
[[ "$archive_top" == "$expected_top" ]] ||
  fail "case E bundle top-level members changed: $(printf '%s ' $archive_top)"

manifest="$case_e_dir/manifest.json"
[[ -s "$manifest" ]] || fail "case E bundle has no manifest.json"
jq -e '.schema_version == "ecs-tools.manifest/v1"' "$manifest" >/dev/null ||
  fail "case E manifest schema changed"
jq -e '.build.toolchain_mode == "cross"' "$manifest" >/dev/null ||
  fail "case E manifest toolchain_mode is not cross"
jq -e --arg target "$target" '.target == $target' "$manifest" >/dev/null ||
  fail "case E manifest target changed"
[[ "$(jq -er '.tools | length' "$manifest")" -eq 8 ]] ||
  fail "case E manifest does not record exactly 8 tools"
manifest_tools=$(jq -r '.tools[].name' "$manifest" | LC_ALL=C sort)
[[ "$manifest_tools" == "$expected_bin" ]] ||
  fail "case E manifest tool set changed: $manifest_tools"

case "$target" in
  freebsd_amd64) machine_token='x86-64' ;;
  freebsd_arm64) machine_token='aarch64' ;;
esac

for tool in "${bundle_tools[@]}"; do
  binary="$case_e_dir/bin/$tool"
  [[ -f "$binary" && -s "$binary" ]] || fail "case E bundle is missing $tool"
  # tar 归档里已是 0755；恢复只防御解包路径的权限丢失，字节不变。
  chmod 0755 "$binary"
  manifest_sha=$(jq -er --arg tool "$tool" \
    '.tools[] | select(.name == $tool) | .parameters.sha256' "$manifest") ||
    fail "case E manifest has no sha256 for $tool"
  actual_tool_sha=$(sha256 -q "$binary" | tr '[:upper:]' '[:lower:]')
  [[ "$actual_tool_sha" == "$manifest_sha" ]] ||
    fail "case E $tool sha256 mismatch: manifest $manifest_sha != bundle $actual_tool_sha"
  identity=$(file -b "$binary")
  echo "case E $tool: $identity"
  case "$identity" in
    *FreeBSD*) ;;
    *) fail "case E $tool is not a FreeBSD ELF: $identity" ;;
  esac
  case "$identity" in
    *static*) ;;
    *) fail "case E $tool is not statically linked: $identity" ;;
  esac
  case "$identity" in
    *"$machine_token"*) ;;
    *) fail "case E $tool is not a $machine_token ELF: $identity" ;;
  esac
done

echo "freebsd-artifact-e2e: case E passed (bundle checksums, members, manifest, 8 identities)"

echo "freebsd-artifact-e2e: artifact-level E2E passed on real FreeBSD/$host_arch"

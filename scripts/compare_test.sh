#!/usr/bin/env bash
set -euo pipefail

# compare.sh 的确定性边界回归：Release 下载由本地 fixture 接管，任何未知
# URL 都会触发哨兵并失败，因此测试不访问网络。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-compare-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "compare.sh tests: $*" >&2
  exit 1
}

assert_empty_dir() {
  local directory=$1 description=$2
  if find "$directory" -mindepth 1 -print -quit | grep -q .; then
    fail "$description left files under $directory"
  fi
}

assert_staged_argv() {
  local log_file=$1 work_root=$2 description=$3
  shift 3
  local -a actual expected=("$@")
  mapfile -d '' -t actual <"$log_file"
  [[ "${#actual[@]}" -eq "$(( ${#expected[@]} + 3 ))" ]] ||
    fail "$description changed the argument count"
  [[ "${actual[0]}" == compare ]] || fail "$description did not invoke compare"
  [[ "${actual[1]}" == --output ]] || fail "$description did not inject output"
  case "${actual[2]}" in
    "$work_root"/ecs-compare.*/output) ;;
    *) fail "$description did not use private staged output: ${actual[2]}" ;;
  esac
  for i in "${!expected[@]}"; do
    [[ "${actual[$((i + 3))]}" == "${expected[$i]}" ]] ||
      fail "$description argument $((i + 3)) changed: ${actual[$((i + 3))]}"
  done
}

case "$(uname -m)" in
  x86_64|amd64) fixture_arch=amd64 ;;
  aarch64|arm64) fixture_arch=arm64 ;;
  armv7l|armv7) fixture_arch=armv7 ;;
  i386|i686|x86) fixture_arch=386 ;;
  s390x) fixture_arch=s390x ;;
  riscv64) fixture_arch=riscv64 ;;
  ppc64le) fixture_arch=ppc64le ;;
  *) fail "unsupported test architecture: $(uname -m)" ;;
esac

fixture_root="$test_root/fixture"
fixture_bin="$fixture_root/bin"
fixture_release="$fixture_root/release"
fixture_logs="$fixture_root/logs"
fixture_asset="ecs_linux_${fixture_arch}.tar.gz"
mkdir -p "$fixture_bin" "$fixture_release/archive" "$fixture_logs"

cat >"$fixture_release/archive/ecs" <<'EOF'
#!/bin/sh
set -eu

: >"$ECS_TEST_LOG_ROOT/compare.argv"
for arg in "$@"; do
  printf '%s\0' "$arg" >>"$ECS_TEST_LOG_ROOT/compare.argv"
done

[ "${1:-}" = compare ] || exit 64
shift
output=""
positional_only=0
while [ "$#" -gt 0 ]; do
  if [ "$1" = -- ]; then
    positional_only=1
    shift
    continue
  fi
  if [ "$positional_only" -eq 0 ]; then
    case "$1" in
      --output)
        shift
        [ "$#" -gt 0 ] || exit 65
        # The fixture only models the output side effect. Known report paths
        # are not output values, so no compare flag grammar is recreated here.
        if [ "$1" != "${ECS_TEST_REPORT_ONE:-}" ] &&
            [ "$1" != "${ECS_TEST_REPORT_TWO:-}" ]; then
          output=$1
        fi
        ;;
      --output=*) output=${1#--output=} ;;
    esac
  fi
  shift
done
[ -n "$output" ] || exit 66
mkdir -p -- "$output"
printf '{}\n' >"$output/comparison.json"
EOF
chmod 0755 "$fixture_release/archive/ecs"
tar -czf "$fixture_release/$fixture_asset" -C "$fixture_release/archive" ecs
fixture_digest=$(sha256sum "$fixture_release/$fixture_asset" | awk '{print $1}')
printf '%s  %s\n' "$fixture_digest" "$fixture_asset" >"$fixture_release/checksums.txt"

cat >"$fixture_bin/curl" <<'EOF'
#!/bin/sh
set -eu

url=""
destination=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      shift
      [ "$#" -gt 0 ] || exit 64
      destination=$1
      ;;
    https://*) url=$1 ;;
  esac
  shift
done

case "$url" in
  "$ECS_TEST_RELEASE_URL/$ECS_TEST_ASSET") source_path="$ECS_TEST_RELEASE_ROOT/$ECS_TEST_ASSET" ;;
  "$ECS_TEST_RELEASE_URL/checksums.txt") source_path="$ECS_TEST_RELEASE_ROOT/checksums.txt" ;;
  *)
    : >"$ECS_TEST_LOG_ROOT/unexpected-network"
    exit 90
    ;;
esac
[ -n "$destination" ] || exit 65
printf '%s\n' "$url" >>"$ECS_TEST_LOG_ROOT/fetch.log"
cp "$source_path" "$destination"
EOF
chmod 0755 "$fixture_bin/curl"

cat >"$fixture_bin/wget" <<'EOF'
#!/bin/sh
: >"$ECS_TEST_LOG_ROOT/unexpected-network"
exit 90
EOF
chmod 0755 "$fixture_bin/wget"

release_url="https://github.com/example/ecs/releases/download/v-test"
test_path="$fixture_bin:$PATH"
export ECS_TEST_LOG_ROOT="$fixture_logs"
export ECS_TEST_RELEASE_URL="$release_url"
export ECS_TEST_RELEASE_ROOT="$fixture_release"
export ECS_TEST_ASSET="$fixture_asset"
report_one="$test_root/report one.json"
report_two="$test_root/report-two.json"
export ECS_TEST_REPORT_ONE="$report_one"
export ECS_TEST_REPORT_TWO="$report_two"

mismatch_release="$test_root/mismatch-release"
mkdir -p "$mismatch_release"
cp "$fixture_release/$fixture_asset" "$mismatch_release/$fixture_asset"
mismatch_digest=$(printf '%064d' 0)
printf '%s  %s\n' "$mismatch_digest" "$fixture_asset" >"$mismatch_release/checksums.txt"

# --help 必须在 WORK 创建和下载前成功。
help_tmp="$test_root/help-tmp"
help_logs="$fixture_logs/help"
mkdir -p "$help_tmp"
if ! ECS_LANG=en TMPDIR="$help_tmp" PATH="$test_path" \
    ECS_TEST_LOG_ROOT="$help_logs" \
    sh "$repo_root/compare.sh" --help >"$test_root/help.stdout" 2>"$test_root/help.stderr"; then
  fail "--help returned a failure"
fi
grep -F 'Usage:' "$test_root/help.stderr" >/dev/null || fail "--help output lost its usage header"
assert_empty_dir "$help_tmp" "--help"
[[ ! -e "$help_logs/fetch.log" ]] || fail "--help attempted a download"
[[ ! -e "$help_logs/unexpected-network" ]] || fail "--help attempted unexpected network access"

# Wrapper help is not an arbitrary token search: an unknown compare option and
# a following --help must reach the binary fixture rather than being stolen by
# the wrapper. This also protects the single-owner grammar boundary.
passthrough_help_tmp="$test_root/passthrough-help-tmp"
passthrough_help_logs="$fixture_logs/passthrough-help"
mkdir -p "$passthrough_help_tmp" "$passthrough_help_logs"
if ! ECS_LANG=en TMPDIR="$passthrough_help_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$passthrough_help_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --future-compare-option --help "$report_one" "$report_two" \
    >"$test_root/passthrough-help.stdout" 2>"$test_root/passthrough-help.stderr"; then
  fail "non-leading --help fixture returned a failure: $(<"$test_root/passthrough-help.stderr")"
fi
grep -F 'starting comparison' "$test_root/passthrough-help.stderr" >/dev/null ||
  fail "non-leading --help did not reach ecs comparison"
if grep -F 'Usage:' "$test_root/passthrough-help.stderr" >/dev/null; then
  fail "non-leading --help unexpectedly printed wrapper help"
fi
[[ -s "$test_root/passthrough-help.stdout" ]] || fail "non-leading --help did not print an automatic output path"
mapfile -t passthrough_help_lines <"$test_root/passthrough-help.stdout"
[[ "${#passthrough_help_lines[@]}" -eq 1 ]] ||
  fail "non-leading --help stdout contained ${#passthrough_help_lines[@]} lines instead of one path"
passthrough_help_output="${passthrough_help_lines[0]}"
case "$passthrough_help_output" in
  "$passthrough_help_tmp"/ecs-comparison.*) ;;
  *) fail "non-leading --help output was not under TMPDIR: $passthrough_help_output" ;;
esac
[[ -f "$passthrough_help_output/comparison.json" ]] ||
  fail "non-leading --help output did not contain comparison.json: $passthrough_help_output"
[[ "$(wc -l <"$passthrough_help_logs/fetch.log")" -eq 2 ]] ||
  fail "non-leading --help did not perform exactly two fixture fetches"
[[ -e "$passthrough_help_logs/compare.argv" ]] || fail "non-leading --help did not execute fake ecs"
[[ ! -e "$passthrough_help_logs/unexpected-network" ]] ||
  fail "non-leading --help attempted unexpected network access"
[[ "$(find "$passthrough_help_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "non-leading --help TMPDIR contained more than the retained output"
if find "$passthrough_help_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "non-leading --help left an ecs-compare WORK directory"
fi
assert_staged_argv "$passthrough_help_logs/compare.argv" "$passthrough_help_tmp" \
  "non-leading --help" --future-compare-option --help "$report_one" "$report_two"

# 单值 output 的值可以是 --help；它必须透传给 ecs，而不能被 wrapper 抢先
# 当作自己的帮助。显式 output 也不能创建默认输出目录。
value_help_cwd="$test_root/value-help-cwd"
value_help_tmp="$test_root/value-help-tmp"
value_help_logs="$fixture_logs/value-help"
mkdir -p "$value_help_cwd" "$value_help_tmp" "$value_help_logs"
if ! (
  cd "$value_help_cwd"
  ECS_LANG=en TMPDIR="$value_help_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$value_help_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --output --help "$report_one" "$report_two" \
    >"$test_root/value-help.stdout" 2>"$test_root/value-help.stderr"
); then
  fail "value-position --help fixture returned a failure: $(<"$test_root/value-help.stderr")"
fi
[[ ! -s "$test_root/value-help.stdout" ]] || fail "value-position --help unexpectedly printed a wrapper output path"
grep -F 'starting comparison' "$test_root/value-help.stderr" >/dev/null ||
  fail "value-position --help did not reach ecs comparison"
if grep -F 'Usage:' "$test_root/value-help.stderr" >/dev/null; then
  fail "value-position --help unexpectedly printed wrapper help"
fi
[[ -f "$value_help_cwd/--help/comparison.json" ]] ||
  fail "value-position --help was not preserved as the output value"
[[ "$(wc -l <"$value_help_logs/fetch.log")" -eq 2 ]] ||
  fail "value-position --help did not perform exactly two fixture fetches"
[[ -e "$value_help_logs/compare.argv" ]] || fail "value-position --help did not execute fake ecs"
[[ ! -e "$value_help_logs/unexpected-network" ]] ||
  fail "value-position --help attempted unexpected network access"
assert_empty_dir "$value_help_tmp" "value-position --help"
assert_staged_argv "$value_help_logs/compare.argv" "$value_help_tmp" \
  "value-position --help" --output --help "$report_one" "$report_two"

# --name consumes the following --output as its value. Seeing that token must
# not suppress the wrapper's temporary output selection.
name_value_tmp="$test_root/name-value-tmp"
name_value_logs="$fixture_logs/name-value"
mkdir -p "$name_value_tmp" "$name_value_logs"
if ! ECS_LANG=en TMPDIR="$name_value_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$name_value_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --name --output "$report_one" "$report_two" \
    >"$test_root/name-value.stdout" 2>"$test_root/name-value.stderr"; then
  fail "--name value-position output fixture returned a failure: $(<"$test_root/name-value.stderr")"
fi
[[ -s "$test_root/name-value.stdout" ]] || fail "--name value-position did not print an automatic output path"
mapfile -t name_value_stdout_lines <"$test_root/name-value.stdout"
[[ "${#name_value_stdout_lines[@]}" -eq 1 ]] ||
  fail "--name value-position stdout contained ${#name_value_stdout_lines[@]} lines instead of one path"
name_value_output="${name_value_stdout_lines[0]}"
case "$name_value_output" in
  "$name_value_tmp"/ecs-comparison.*) ;;
  *) fail "--name value-position output was not under TMPDIR: $name_value_output" ;;
esac
[[ -f "$name_value_output/comparison.json" ]] ||
  fail "--name value-position output did not contain comparison.json: $name_value_output"
[[ "$(wc -l <"$name_value_logs/fetch.log")" -eq 2 ]] ||
  fail "--name value-position did not perform exactly two fixture fetches"
[[ -e "$name_value_logs/compare.argv" ]] || fail "--name value-position did not execute fake ecs"
[[ ! -e "$name_value_logs/unexpected-network" ]] ||
  fail "--name value-position attempted unexpected network access"
[[ "$(find "$name_value_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "--name value-position TMPDIR contained more than the retained output"
if find "$name_value_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "--name value-position left an ecs-compare WORK directory"
fi
assert_staged_argv "$name_value_logs/compare.argv" "$name_value_tmp" \
  "--name value-position" --name --output "$report_one" "$report_two"

# --format 也会消费下一个 token；即使这个 value 恰好叫 --output，wrapper
# 仍必须交给真实 compare parser 判断，不能据此跳过默认 output。
format_value_tmp="$test_root/format-value-tmp"
format_value_logs="$fixture_logs/format-value"
mkdir -p "$format_value_tmp" "$format_value_logs"
if ! ECS_LANG=en TMPDIR="$format_value_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$format_value_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --format --output "$report_one" "$report_two" \
    >"$test_root/format-value.stdout" 2>"$test_root/format-value.stderr"; then
  fail "--format value-position output fixture returned a failure: $(<"$test_root/format-value.stderr")"
fi
[[ -s "$test_root/format-value.stdout" ]] ||
  fail "--format value-position did not print an automatic output path"
mapfile -t format_value_stdout_lines <"$test_root/format-value.stdout"
[[ "${#format_value_stdout_lines[@]}" -eq 1 ]] ||
  fail "--format value-position stdout contained ${#format_value_stdout_lines[@]} lines instead of one path"
format_value_output="${format_value_stdout_lines[0]}"
case "$format_value_output" in
  "$format_value_tmp"/ecs-comparison.*) ;;
  *) fail "--format value-position output was not under TMPDIR: $format_value_output" ;;
esac
[[ -f "$format_value_output/comparison.json" ]] ||
  fail "--format value-position output did not contain comparison.json: $format_value_output"
[[ "$(wc -l <"$format_value_logs/fetch.log")" -eq 2 ]] ||
  fail "--format value-position did not perform exactly two fixture fetches"
[[ -e "$format_value_logs/compare.argv" ]] ||
  fail "--format value-position did not execute fake ecs"
[[ ! -e "$format_value_logs/unexpected-network" ]] ||
  fail "--format value-position attempted unexpected network access"
[[ "$(find "$format_value_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "--format value-position TMPDIR contained more than the retained output"
if find "$format_value_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "--format value-position left an ecs-compare WORK directory"
fi
assert_staged_argv "$format_value_logs/compare.argv" "$format_value_tmp" \
  "--format value-position" --format --output "$report_one" "$report_two"

# 相对 TMPDIR 被明确拒绝，且不能创建相对目录或开始下载。
relative_cwd="$test_root/relative-cwd"
relative_logs="$fixture_logs/relative"
mkdir -p "$relative_cwd"
set +e
(
  cd "$relative_cwd"
  ECS_LANG=en TMPDIR=relative-tmp PATH="$test_path" \
    ECS_TEST_LOG_ROOT="$relative_logs" \
    sh "$repo_root/compare.sh" "$report_one" "$report_two"
) >"$test_root/relative.stdout" 2>"$test_root/relative.stderr"
relative_status=$?
set -e
[[ "$relative_status" -eq 1 ]] || fail "relative TMPDIR returned $relative_status instead of 1"
[[ "$(<"$test_root/relative.stderr")" == 'ecs: TMPDIR must be an absolute path' ]] ||
  fail "relative TMPDIR diagnostic changed: $(<"$test_root/relative.stderr")"
assert_empty_dir "$relative_cwd" "relative TMPDIR"
[[ ! -e "$relative_logs/fetch.log" ]] || fail "relative TMPDIR attempted a download"
[[ ! -e "$relative_logs/unexpected-network" ]] || fail "relative TMPDIR attempted unexpected network access"

# 有效 fixture 必须按顺序下载 asset 和 checksums，并在校验后运行 fixture ecs。
normal_tmp="$test_root/normal-tmp"
normal_logs="$fixture_logs/normal"
normal_output="$test_root/normal-output"
mkdir -p "$normal_tmp" "$normal_logs"
if ! ECS_LANG=en TMPDIR="$normal_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$normal_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --output "$normal_output" "$report_one" "$report_two" \
    >"$test_root/normal.stdout" 2>"$test_root/normal.stderr"; then
  fail "valid local compare fixture returned a failure: $(<"$test_root/normal.stderr")"
fi
[[ -f "$normal_output/comparison.json" ]] || fail "valid fixture did not execute fake ecs"
[[ "$(wc -l <"$normal_logs/fetch.log")" -eq 2 ]] ||
  fail "valid fixture did not perform exactly two local fixture fetches"
mapfile -t normal_fetches <"$normal_logs/fetch.log"
expected_fetches=("$release_url/$fixture_asset" "$release_url/checksums.txt")
[[ "${#normal_fetches[@]}" -eq "${#expected_fetches[@]}" ]] ||
  fail "valid fixture fetched an unexpected number of URLs"
for i in "${!expected_fetches[@]}"; do
  [[ "${normal_fetches[$i]}" == "${expected_fetches[$i]}" ]] ||
    fail "valid fixture fetch $i changed: ${normal_fetches[$i]}"
done
[[ ! -e "$normal_logs/unexpected-network" ]] || fail "valid fixture attempted unexpected network access"
assert_empty_dir "$normal_tmp" "valid comparison"

# checksum 不匹配时必须拒绝执行下载的 fake ecs，并清理 WORK。
mismatch_tmp="$test_root/mismatch-tmp"
mismatch_logs="$fixture_logs/mismatch"
mismatch_output="$test_root/mismatch-output"
mkdir -p "$mismatch_tmp" "$mismatch_logs"
set +e
ECS_LANG=en TMPDIR="$mismatch_tmp" PATH="$test_path" \
  ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
  ECS_TEST_LOG_ROOT="$mismatch_logs" ECS_TEST_RELEASE_ROOT="$mismatch_release" \
  sh "$repo_root/compare.sh" --output "$mismatch_output" "$report_one" "$report_two" \
  >"$test_root/mismatch.stdout" 2>"$test_root/mismatch.stderr"
mismatch_status=$?
set -e
[[ "$mismatch_status" -eq 1 ]] || fail "checksum mismatch returned $mismatch_status instead of 1"
grep -F 'checksum mismatch; refusing to run the download' "$test_root/mismatch.stderr" >/dev/null ||
  fail "checksum mismatch diagnostic changed: $(<"$test_root/mismatch.stderr")"
[[ "$(wc -l <"$mismatch_logs/fetch.log")" -eq 2 ]] ||
  fail "checksum mismatch did not download asset and checksums"
[[ ! -e "$mismatch_logs/compare.argv" ]] || fail "checksum mismatch executed fake ecs"
[[ ! -e "$mismatch_output" ]] || fail "checksum mismatch created comparison output"
[[ ! -e "$mismatch_logs/unexpected-network" ]] ||
  fail "checksum mismatch attempted unexpected network access"
assert_empty_dir "$mismatch_tmp" "checksum mismatch"

# 成功路径下载本地 fixture，保留带空格参数，且输出目录不随 WORK 清理。
success_tmp="$test_root/success-tmp"
success_logs="$fixture_logs/success"
comparison_output="$test_root/comparison output"
mkdir -p "$success_tmp" "$success_logs"
if ! ECS_LANG=en TMPDIR="$success_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$success_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" --output "$comparison_output" --format json,md \
      --name 'node alpha' "$report_one" "$report_two" \
      >"$test_root/success.stdout" 2>"$test_root/success.stderr"; then
  fail "local compare fixture returned a failure: $(<"$test_root/success.stderr")"
fi

[[ -f "$comparison_output/comparison.json" ]] || fail "fake ecs did not generate the requested output"
[[ "$(wc -l <"$success_logs/fetch.log")" -eq 2 ]] || fail "success path did not perform exactly two local fixture fetches"
[[ ! -e "$success_logs/unexpected-network" ]] || fail "success path attempted unexpected network access"
assert_empty_dir "$success_tmp" "successful comparison"

assert_staged_argv "$success_logs/compare.argv" "$success_tmp" "success path" \
  --output "$comparison_output" --format json,md --name 'node alpha' "$report_one" "$report_two"

# flags-after-positional 仍由 normalizeCompareArgs 负责；wrapper 不应因此
# 创建自己的默认 output，也不能把用户指定的目录改掉。
after_path_tmp="$test_root/after-path-tmp"
after_path_logs="$fixture_logs/after-path"
after_path_output="$test_root/after-path-output"
mkdir -p "$after_path_tmp" "$after_path_logs"
if ! ECS_LANG=en TMPDIR="$after_path_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$after_path_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" "$report_one" "$report_two" --output "$after_path_output" \
    >"$test_root/after-path.stdout" 2>"$test_root/after-path.stderr"; then
  fail "flags-after-positional fixture returned a failure: $(<"$test_root/after-path.stderr")"
fi
[[ -d "$after_path_output" ]] || fail "flags-after-positional did not preserve output directory"
[[ -f "$after_path_output/comparison.json" ]] ||
  fail "flags-after-positional did not generate comparison.json"
[[ "$(wc -l <"$after_path_logs/fetch.log")" -eq 2 ]] ||
  fail "flags-after-positional did not perform exactly two fixture fetches"
[[ -e "$after_path_logs/compare.argv" ]] || fail "flags-after-positional did not execute fake ecs"
[[ ! -e "$after_path_logs/unexpected-network" ]] ||
  fail "flags-after-positional attempted unexpected network access"
assert_empty_dir "$after_path_tmp" "flags-after-positional"
assert_staged_argv "$after_path_logs/compare.argv" "$after_path_tmp" \
  "flags-after-positional" "$report_one" "$report_two" --output "$after_path_output"

# 不传 --output 时，wrapper 创建的 OUT 必须通过 stdout 返回并在 WORK 清理后保留。
auto_tmp="$test_root/auto-output-tmp"
auto_logs="$fixture_logs/auto-output"
mkdir -p "$auto_tmp" "$auto_logs"
if ! ECS_LANG=en TMPDIR="$auto_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$auto_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" "$report_one" "$report_two" \
    >"$test_root/auto-output.stdout" 2>"$test_root/auto-output.stderr"; then
  fail "automatic output fixture returned a failure: $(<"$test_root/auto-output.stderr")"
fi

[[ -s "$test_root/auto-output.stdout" ]] || fail "automatic output path was empty"
mapfile -t auto_stdout_lines <"$test_root/auto-output.stdout"
[[ "${#auto_stdout_lines[@]}" -eq 1 ]] ||
  fail "automatic output stdout contained ${#auto_stdout_lines[@]} lines instead of one path"
auto_output="${auto_stdout_lines[0]}"
[[ -n "$auto_output" ]] || fail "automatic output path was empty"
case "$auto_output" in
  "$auto_tmp"/ecs-comparison.*) ;;
  *) fail "automatic output path was not under TMPDIR: $auto_output" ;;
esac
[[ -d "$auto_output" ]] || fail "automatic output directory was not retained: $auto_output"
[[ -f "$auto_output/comparison.json" ]] ||
  fail "automatic output path did not contain comparison.json: $auto_output"
[[ "$(find "$auto_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "automatic output TMPDIR contained entries other than the retained output"
if find "$auto_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "automatic output left an ecs-compare WORK directory"
fi

# -- 后面的 --output 必须保持 positional：wrapper 仍然注入自己的 OUT。
boundary_output_tmp="$test_root/boundary-output-tmp"
boundary_output_logs="$fixture_logs/boundary-output"
mkdir -p "$boundary_output_tmp" "$boundary_output_logs"
if ! ECS_LANG=en TMPDIR="$boundary_output_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$boundary_output_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" -- --output "$report_one" "$report_two" \
    >"$test_root/boundary-output.stdout" 2>"$test_root/boundary-output.stderr"; then
  fail "boundary --output fixture returned a failure: $(<"$test_root/boundary-output.stderr")"
fi

[[ -s "$test_root/boundary-output.stdout" ]] || fail "boundary --output path was empty"
mapfile -t boundary_output_lines <"$test_root/boundary-output.stdout"
[[ "${#boundary_output_lines[@]}" -eq 1 ]] ||
  fail "boundary --output stdout contained ${#boundary_output_lines[@]} lines instead of one path"
boundary_output="${boundary_output_lines[0]}"
case "$boundary_output" in
  "$boundary_output_tmp"/ecs-comparison.*) ;;
  *) fail "boundary --output path was not under TMPDIR: $boundary_output" ;;
esac
[[ -f "$boundary_output/comparison.json" ]] ||
  fail "boundary --output path did not contain comparison.json: $boundary_output"
[[ "$(find "$boundary_output_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "boundary --output TMPDIR contained entries other than the retained output"
if find "$boundary_output_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "boundary --output left an ecs-compare WORK directory"
fi
assert_staged_argv "$boundary_output_logs/compare.argv" "$boundary_output_tmp" \
  "boundary --output" -- --output "$report_one" "$report_two"

# -- 后面的 --help 必须透传给 ecs，不得触发 wrapper 帮助。
boundary_help_tmp="$test_root/boundary-help-tmp"
boundary_help_logs="$fixture_logs/boundary-help"
mkdir -p "$boundary_help_tmp" "$boundary_help_logs"
if ! ECS_LANG=en TMPDIR="$boundary_help_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$boundary_help_logs" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    sh "$repo_root/compare.sh" -- --help "$report_one" "$report_two" \
    >"$test_root/boundary-help.stdout" 2>"$test_root/boundary-help.stderr"; then
  fail "boundary --help fixture returned a failure: $(<"$test_root/boundary-help.stderr")"
fi

grep -F 'starting comparison' "$test_root/boundary-help.stderr" >/dev/null ||
  fail "boundary --help did not reach ecs comparison"
if grep -F 'Usage:' "$test_root/boundary-help.stderr" >/dev/null; then
  fail "boundary --help unexpectedly printed wrapper help"
fi
[[ -s "$test_root/boundary-help.stdout" ]] || fail "boundary --help path was empty"
mapfile -t boundary_help_lines <"$test_root/boundary-help.stdout"
[[ "${#boundary_help_lines[@]}" -eq 1 ]] ||
  fail "boundary --help stdout contained ${#boundary_help_lines[@]} lines instead of one path"
boundary_help_output="${boundary_help_lines[0]}"
case "$boundary_help_output" in
  "$boundary_help_tmp"/ecs-comparison.*) ;;
  *) fail "boundary --help path was not under TMPDIR: $boundary_help_output" ;;
esac
[[ -f "$boundary_help_output/comparison.json" ]] ||
  fail "boundary --help path did not contain comparison.json: $boundary_help_output"
[[ "$(find "$boundary_help_tmp" -mindepth 1 -maxdepth 1 -print | wc -l)" -eq 1 ]] ||
  fail "boundary --help TMPDIR contained entries other than the retained output"
if find "$boundary_help_tmp" -mindepth 1 -maxdepth 1 -type d -name 'ecs-compare.*' -print -quit | grep -q .; then
  fail "boundary --help left an ecs-compare WORK directory"
fi
assert_staged_argv "$boundary_help_logs/compare.argv" "$boundary_help_tmp" \
  "boundary --help" -- --help "$report_one" "$report_two"

echo "compare.sh behavior tests passed"

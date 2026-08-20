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
  if [ "$positional_only" -eq 0 ] && [ "$1" = --output ]; then
    shift
    [ "$#" -gt 0 ] || exit 65
    output=$1
  fi
  shift
done
[ -n "$output" ] || exit 66
mkdir -p "$output"
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

mapfile -d '' -t compare_argv <"$success_logs/compare.argv"
expected_argv=(compare --output "$comparison_output" --format json,md --name 'node alpha' "$report_one" "$report_two")
[[ "${#compare_argv[@]}" -eq "${#expected_argv[@]}" ]] ||
  fail "ecs compare received ${#compare_argv[@]} arguments instead of ${#expected_argv[@]}"
for i in "${!expected_argv[@]}"; do
  [[ "${compare_argv[$i]}" == "${expected_argv[$i]}" ]] ||
    fail "ecs compare argument $i changed: ${compare_argv[$i]}"
done

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
mapfile -d '' -t boundary_output_argv <"$boundary_output_logs/compare.argv"
expected_boundary_output_argv=(compare --output "$boundary_output" -- --output "$report_one" "$report_two")
[[ "${#boundary_output_argv[@]}" -eq "${#expected_boundary_output_argv[@]}" ]] ||
  fail "boundary --output changed the argument count"
for i in "${!expected_boundary_output_argv[@]}"; do
  [[ "${boundary_output_argv[$i]}" == "${expected_boundary_output_argv[$i]}" ]] ||
    fail "boundary --output argument $i changed: ${boundary_output_argv[$i]}"
done

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
mapfile -d '' -t boundary_help_argv <"$boundary_help_logs/compare.argv"
expected_boundary_help_argv=(compare --output "$boundary_help_output" -- --help "$report_one" "$report_two")
[[ "${#boundary_help_argv[@]}" -eq "${#expected_boundary_help_argv[@]}" ]] ||
  fail "boundary --help changed the argument count"
for i in "${!expected_boundary_help_argv[@]}"; do
  [[ "${boundary_help_argv[$i]}" == "${expected_boundary_help_argv[$i]}" ]] ||
    fail "boundary --help argument $i changed: ${boundary_help_argv[$i]}"
done

echo "compare.sh behavior tests passed"

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
while [ "$#" -gt 0 ]; do
  if [ "$1" = --output ]; then
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
report_three="$test_root/report-three.json"
printf '{}\n' >"$report_one"
printf '{}\n' >"$report_two"
printf '{}\n' >"$report_three"

# --help 必须在 WORK 创建和下载前成功。
help_tmp="$test_root/help-tmp"
mkdir -p "$help_tmp"
if ! ECS_LANG=en TMPDIR="$help_tmp" PATH="$test_path" \
    sh "$repo_root/compare.sh" --help >"$test_root/help.stdout" 2>"$test_root/help.stderr"; then
  fail "--help returned a failure"
fi
grep -F 'Usage:' "$test_root/help.stderr" >/dev/null || fail "--help output lost its usage header"
assert_empty_dir "$help_tmp" "--help"
[[ ! -e "$fixture_logs/fetch.log" ]] || fail "--help attempted a download"

# 需要取值的选项必须稳定失败，且不能进入下载路径。
missing_tmp="$test_root/missing-tmp"
mkdir -p "$missing_tmp"
set +e
ECS_LANG=en TMPDIR="$missing_tmp" PATH="$test_path" \
  sh "$repo_root/compare.sh" --output >"$test_root/missing.stdout" 2>"$test_root/missing.stderr"
missing_status=$?
set -e
[[ "$missing_status" -eq 1 ]] || fail "missing --output value returned $missing_status instead of 1"
[[ "$(<"$test_root/missing.stderr")" == 'ecs: --output requires a value' ]] ||
  fail "missing --output diagnostic changed: $(<"$test_root/missing.stderr")"
assert_empty_dir "$missing_tmp" "missing --output value"
[[ ! -e "$fixture_logs/fetch.log" ]] || fail "missing --output value attempted a download"

# 一份报告不足以对比，必须给出明确诊断并在下载前退出。
count_tmp="$test_root/count-tmp"
mkdir -p "$count_tmp"
set +e
ECS_LANG=en TMPDIR="$count_tmp" PATH="$test_path" \
  sh "$repo_root/compare.sh" "$report_two" >"$test_root/count.stdout" 2>"$test_root/count.stderr"
count_status=$?
set -e
[[ "$count_status" -eq 1 ]] || fail "one report returned $count_status instead of 1"
[[ "$(tail -n 1 "$test_root/count.stderr")" == 'ecs: at least two reports are required' ]] ||
  fail "one-report diagnostic changed: $(tail -n 1 "$test_root/count.stderr")"
assert_empty_dir "$count_tmp" "one report"
[[ ! -e "$fixture_logs/fetch.log" ]] || fail "one report attempted a download"

# 相对 TMPDIR 被明确拒绝，且不能创建相对目录或开始下载。
relative_cwd="$test_root/relative-cwd"
mkdir -p "$relative_cwd"
set +e
(
  cd "$relative_cwd"
  ECS_LANG=en TMPDIR=relative-tmp PATH="$test_path" \
    sh "$repo_root/compare.sh" "$report_two" "$report_three"
) >"$test_root/relative.stdout" 2>"$test_root/relative.stderr"
relative_status=$?
set -e
[[ "$relative_status" -eq 1 ]] || fail "relative TMPDIR returned $relative_status instead of 1"
[[ "$(<"$test_root/relative.stderr")" == 'ecs: TMPDIR must be an absolute path' ]] ||
  fail "relative TMPDIR diagnostic changed: $(<"$test_root/relative.stderr")"
assert_empty_dir "$relative_cwd" "relative TMPDIR"
[[ ! -e "$fixture_logs/fetch.log" ]] || fail "relative TMPDIR attempted a download"
[[ ! -e "$fixture_logs/unexpected-network" ]] || fail "an early-exit case attempted unexpected network access"

# 成功路径下载本地 fixture，并完整保留普通参数、含空格路径和三份报告的 argv 边界。
success_tmp="$test_root/success-tmp"
comparison_output="$test_root/comparison output"
mkdir -p "$success_tmp"
if ! ECS_LANG=en TMPDIR="$success_tmp" PATH="$test_path" ECS_CACHE=0 \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    sh "$repo_root/compare.sh" --output "$comparison_output" --format txt,json \
      --name 'node alpha' "$report_one" "$report_two" "$report_three" \
      >"$test_root/success.stdout" 2>"$test_root/success.stderr"; then
  fail "local compare fixture returned a failure: $(<"$test_root/success.stderr")"
fi

[[ -f "$comparison_output/comparison.json" ]] || fail "fake ecs did not generate the requested output"
[[ "$(wc -l <"$fixture_logs/fetch.log")" -eq 2 ]] || fail "success path did not perform exactly two local fixture fetches"
[[ ! -e "$fixture_logs/unexpected-network" ]] || fail "success path attempted unexpected network access"
assert_empty_dir "$success_tmp" "successful comparison"

mapfile -d '' -t compare_argv <"$fixture_logs/compare.argv"
expected_argv=(compare --output "$comparison_output" --format txt,json --name 'node alpha' "$report_one" "$report_two" "$report_three")
[[ "${#compare_argv[@]}" -eq "${#expected_argv[@]}" ]] ||
  fail "ecs compare received ${#compare_argv[@]} arguments instead of ${#expected_argv[@]}"
for i in "${!expected_argv[@]}"; do
  [[ "${compare_argv[$i]}" == "${expected_argv[$i]}" ]] ||
    fail "ecs compare argument $i changed: ${compare_argv[$i]}"
done

echo "compare.sh behavior tests passed"

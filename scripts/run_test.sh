#!/usr/bin/env bash
set -euo pipefail

# run.sh 的确定性边界回归：所有“下载”都由本地 fixture 接管，未知 URL
# 会触发哨兵并立即失败，因此本测试不会访问网络或运行任何基准工具。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-run-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "run.sh tests: $*" >&2
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

record_argv() {
  record_path=$1
  shift
  : >"$record_path"
  for record_arg in "$@"; do
    printf '%s\0' "$record_arg" >>"$record_path"
  done
}

if [ "${1:-}" = list ] && [ "${2:-}" = --machine ]; then
  printf 'ecs-module-manifest\t1\n'
  printf 'profile\tstandard\tnoop\n'
  printf 'profile\tfull\tnoop\n'
  printf 'module\tnoop\tlocal\t\n'
  exit 0
fi

if [ "${1:-}" = submit ] && [ "${2:-}" = --help ]; then
  exit 0
fi

if [ "${1:-}" = submit ]; then
  record_argv "$ECS_TEST_LOG_ROOT/submit.argv" "$@"
  exit 0
fi

record_argv "$ECS_TEST_LOG_ROOT/run.argv" "$@"
report_dir=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = --output ]; then
    shift
    [ "$#" -gt 0 ] || exit 64
    report_dir=$1
  fi
  shift
done
[ -n "$report_dir" ] || exit 65
mkdir -p "$report_dir"
printf '{}\n' >"$report_dir/fixture.json"
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

# --help 必须在 mktemp、Release 下载和工具 staging 之前成功返回。
help_tmp="$test_root/help-tmp"
mkdir -p "$help_tmp"
if ! ECS_LANG=en TMPDIR="$help_tmp" PATH="$test_path" \
    ECS_TEST_LOG_ROOT="$fixture_logs" \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --help >"$test_root/help.stdout" 2>"$test_root/help.stderr"; then
  fail "--help returned a failure"
fi
grep -F 'Usage: run.sh' "$test_root/help.stdout" >/dev/null ||
  fail "--help output lost its usage header"
[[ ! -s "$fixture_logs/fetch.log" ]] || fail "--help attempted a download"
[[ ! -e "$fixture_logs/unexpected-network" ]] || fail "--help attempted unexpected network access"
assert_empty_dir "$help_tmp" "--help"

# 两个互斥 wrapper mode 要给出固定诊断，并同样不能创建 WORK 或下载。
conflict_tmp="$test_root/conflict-tmp"
mkdir -p "$conflict_tmp"
set +e
ECS_LANG=en TMPDIR="$conflict_tmp" PATH="$test_path" \
  ECS_TEST_LOG_ROOT="$fixture_logs" \
  ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
  ECS_TEST_ASSET="$fixture_asset" \
  sh "$repo_root/run.sh" --submit --compare >"$test_root/conflict.stdout" 2>"$test_root/conflict.stderr"
conflict_status=$?
set -e
[[ "$conflict_status" -eq 1 ]] || fail "--submit/--compare returned $conflict_status instead of 1"
[[ ! -s "$test_root/conflict.stdout" ]] || fail "--submit/--compare unexpectedly wrote stdout"
[[ "$(<"$test_root/conflict.stderr")" == 'ecs: --compare cannot be combined with --submit' ]] ||
  fail "--submit/--compare diagnostic changed: $(<"$test_root/conflict.stderr")"
[[ ! -s "$fixture_logs/fetch.log" ]] || fail "--submit/--compare attempted a download"
[[ ! -e "$fixture_logs/unexpected-network" ]] || fail "--submit/--compare attempted unexpected network access"
assert_empty_dir "$conflict_tmp" "--submit/--compare"

# submit/provider/region/用户 output 由 wrapper 消费；普通参数和含空格值必须仍是独立 argv。
run_tmp="$test_root/run-tmp"
submission_output="$test_root/submission file.json"
mkdir -p "$run_tmp"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$fixture_logs" \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --submit --name 'node alpha' \
      --provider 'Cloud Alpha' --yes --region='East Zone' --output "$submission_output" \
      >"$test_root/run.stdout" 2>"$test_root/run.stderr"; then
  fail "local submit fixture returned a failure: $(<"$test_root/run.stderr")"
fi

[[ ! -e "$fixture_logs/unexpected-network" ]] || fail "submit path attempted unexpected network access"
[[ "$(wc -l <"$fixture_logs/fetch.log")" -eq 2 ]] || fail "submit path did not perform exactly two local fixture fetches"
mapfile -d '' -t run_argv <"$fixture_logs/run.argv"
[[ "${#run_argv[@]}" -eq 9 ]] || fail "ecs run received ${#run_argv[@]} arguments instead of 9"
[[ "${run_argv[0]}" == --profile && "${run_argv[1]}" == standard ]] || fail "ordinary profile arguments changed"
[[ "${run_argv[2]}" == --name && "${run_argv[3]}" == 'node alpha' ]] || fail "space-containing --name value lost its argv boundary"
[[ "${run_argv[4]}" == --yes && "${run_argv[5]}" == --format && "${run_argv[6]}" == json ]] || fail "ordinary/forced run arguments changed"
[[ "${run_argv[7]}" == --output ]] || fail "temporary run output flag is missing"
[[ "${run_argv[8]}" == "$run_tmp"/ecs-run.*/report ]] || fail "temporary run output escaped WORK: ${run_argv[8]}"
[[ "${run_argv[8]}" != "$submission_output" ]] || fail "user submission output leaked into ecs run"

mapfile -d '' -t submit_argv <"$fixture_logs/submit.argv"
[[ "${#submit_argv[@]}" -eq 9 ]] || fail "ecs submit received ${#submit_argv[@]} arguments instead of 9"
[[ "${submit_argv[0]}" == submit && "${submit_argv[1]}" == --input ]] || fail "submit prefix changed"
[[ "${submit_argv[2]}" == "${run_argv[8]}/fixture.json" ]] || fail "submit input does not match the temporary report"
[[ "${submit_argv[3]}" == --output && "${submit_argv[4]}" == "$submission_output" ]] || fail "space-containing submit output lost its argv boundary"
[[ "${submit_argv[5]}" == --provider && "${submit_argv[6]}" == 'Cloud Alpha' ]] || fail "wrapper provider was not consumed and reconstructed correctly"
[[ "${submit_argv[7]}" == --region && "${submit_argv[8]}" == 'East Zone' ]] || fail "wrapper region was not consumed and reconstructed correctly"

echo "run.sh behavior tests passed"

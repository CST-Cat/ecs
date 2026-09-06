#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-common-download-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "common download tests: $*" >&2
  exit 1
}

source "$repo_root/scripts/lib/common.sh"

fixture_bin="$test_root/bin"
mkdir -p "$fixture_bin"
cat >"$fixture_bin/curl" <<'EOF'
#!/bin/sh
set -eu

printf '%s\0' "$@" >>"$ECS_COMMON_TEST_ARGS"
attempt=$(cat "$ECS_COMMON_TEST_COUNT" 2>/dev/null || printf '0')
attempt=$((attempt + 1))
printf '%s\n' "$attempt" >"$ECS_COMMON_TEST_COUNT"
destination=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then
    shift
    destination=$1
  fi
  shift
done
[ -n "$destination" ] || exit 64
if [ "$attempt" -eq 1 ]; then
  printf '%s\n' wrong >"$destination"
elif [ "${ECS_COMMON_TEST_MODE:-success}" = fail ]; then
  printf '%s\n' wrong >"$destination"
else
  printf '%s\n' 'common download fixture' >"$destination"
fi
EOF
chmod 0755 "$fixture_bin/curl"

fixture_path="$fixture_bin:$PATH"
args_log="$test_root/curl.args"
count_file="$test_root/attempts"
output="$test_root/source.bin"
expected_sha=$(printf '%s\n' 'common download fixture' | sha256sum | awk '{print $1}')
if ! ECS_COMMON_TEST_ARGS="$args_log" ECS_COMMON_TEST_COUNT="$count_file" PATH="$fixture_path" \
    ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$output" fixture; then
  fail "checksum retry fixture failed"
fi

[[ "$(<"$count_file")" -eq 2 ]] || fail "checksum mismatch did not retry exactly once"
[[ "$(<"$output")" == 'common download fixture' ]] || fail "retry did not retain the matching download"
args=$(tr '\0' '\n' <"$args_log")
grep -F -x -- '--connect-timeout' <<<"$args" >/dev/null || fail "curl args omitted connect timeout"
grep -F -x -- '30' <<<"$args" >/dev/null || fail "curl args omitted the configured timeout value"
grep -F -x -- '--speed-limit' <<<"$args" >/dev/null || fail "curl args omitted speed limit"
grep -F -x -- '--speed-time' <<<"$args" >/dev/null || fail "curl args omitted speed time"
grep -F -x -- '--max-time' <<<"$args" >/dev/null || fail "curl args omitted total transfer bound"
grep -F -x -- '900' <<<"$args" >/dev/null || fail "curl args omitted the source download budget"
if grep -F -x -- '--retry' <<<"$args" >/dev/null; then
  fail "common downloader nested curl retries"
fi

failure_args_log="$test_root/failure-curl.args"
failure_count_file="$test_root/failure-attempts"
failure_output="$test_root/failure.bin"
set +e
ECS_COMMON_TEST_ARGS="$failure_args_log" ECS_COMMON_TEST_COUNT="$failure_count_file" \
  ECS_COMMON_TEST_MODE=fail PATH="$fixture_path" \
  ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$failure_output" fixture
failure_status=$?
set -e
[[ "$failure_status" -ne 0 ]] || fail "continuous checksum failure unexpectedly succeeded"
[[ "$(<"$failure_count_file")" -eq 3 ]] || fail "continuous checksum failure did not stop after three attempts"
[[ ! -e "$failure_output" ]] || fail "continuous checksum failure retained the output"

echo "common download behavior tests passed"

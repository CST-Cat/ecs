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

# The three public wrappers are standalone because they must also work when
# streamed through `curl | sh`. Extract only their production download/hash
# helpers and exercise the same deterministic fixture against each one, so
# future helper drift is visible without sourcing a wrapper prelude.
wrapper_names=(run.sh install.sh compare.sh)
for wrapper_name in "${wrapper_names[@]}"; do
  wrapper_fetch="$test_root/${wrapper_name}.fetch"
  wrapper_harness="$test_root/${wrapper_name}.harness"
  sed -n '/^fetch()/,/^file_sha256()/p' "$repo_root/$wrapper_name" | sed '$d' >"$wrapper_fetch"
  {
    printf '%s\n' 'die() { printf "%s\\n" "$1" >&2; exit 1; }'
    printf '%s\n' 'UI=en'
    printf '%s\n' 'OS=linux'
    printf '%s\n' 'os_name=linux'
    cat "$wrapper_fetch"
    sed -n '/^file_sha256()/,/^}/p' "$repo_root/$wrapper_name"
  } >"$wrapper_harness"

  curl_fixture="$test_root/${wrapper_name}.curl-bin"
  mkdir -p "$curl_fixture"
  cat >"$curl_fixture/curl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\0' "$@" >>"$ECS_WRAPPER_CONTRACT_ARGS"
destination=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then
    shift
    destination=$1
  fi
  shift
done
[ -n "$destination" ] || exit 64
printf '%s\n' fixture >"$destination"
EOF
  chmod 0755 "$curl_fixture/curl"

  curl_args="$test_root/${wrapper_name}.curl.args"
  curl_output="$test_root/${wrapper_name}.curl.output"
  ECS_WRAPPER_CONTRACT_ARGS="$curl_args" PATH="$curl_fixture" \
    /bin/sh -c '. "$1"; fetch https://fixture.invalid/source "$2" 900' \
    sh "$wrapper_harness" "$curl_output" || fail "$wrapper_name curl helper failed"
  [[ "$(<"$curl_output")" == fixture ]] || fail "$wrapper_name curl helper did not write its output"
  curl_args_text=$(tr '\0' '\n' <"$curl_args")
  grep -F -x -- '--proto' <<<"$curl_args_text" >/dev/null || fail "$wrapper_name curl helper omitted HTTPS protocol enforcement"
  grep -F -x -- '=https' <<<"$curl_args_text" >/dev/null || fail "$wrapper_name curl helper weakened HTTPS protocol enforcement"
  grep -F -x -- '--tlsv1.2' <<<"$curl_args_text" >/dev/null || fail "$wrapper_name curl helper omitted TLS minimum"
  awk '$0 == "--retry-max-time" { getline; if ($0 == "900") found=1 } END { exit !found }' <<<"$curl_args_text" || fail "$wrapper_name curl helper lost its download budget"
  awk '$0 == "--max-time" { getline; if ($0 == "900") found=1 } END { exit !found }' <<<"$curl_args_text" || fail "$wrapper_name curl helper lost its total timeout"

  http_output="$test_root/${wrapper_name}.http.output"
  set +e
  PATH="$curl_fixture" /bin/sh -c '. "$1"; fetch http://fixture.invalid/source "$2" 900' \
    sh "$wrapper_harness" "$http_output" >"$test_root/${wrapper_name}.http.stdout" \
    2>"$test_root/${wrapper_name}.http.stderr"
  http_status=$?
  set -e
  [[ "$http_status" -eq 1 ]] || fail "$wrapper_name accepted a non-HTTPS URL"
  [[ ! -e "$http_output" ]] || fail "$wrapper_name created output for a non-HTTPS URL"

  wget_fixture="$test_root/${wrapper_name}.wget-bin"
  mkdir -p "$wget_fixture"
  cat >"$wget_fixture/wget" <<'EOF'
#!/bin/sh
set -eu
printf '%s\0' "$@" >>"$ECS_WRAPPER_CONTRACT_ARGS"
destination=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = -O ]; then
    shift
    destination=$1
  fi
  shift
done
[ -n "$destination" ] || exit 64
printf '%s\n' fixture >"$destination"
EOF
  cat >"$wget_fixture/timeout" <<'EOF'
#!/bin/sh
set -eu
printf '%s\0' "$@" >>"$ECS_WRAPPER_CONTRACT_TIMEOUT"
shift
exec "$@"
EOF
  chmod 0755 "$wget_fixture/wget" "$wget_fixture/timeout"

  wget_args="$test_root/${wrapper_name}.wget.args"
  wget_timeout="$test_root/${wrapper_name}.wget.timeout"
  wget_output="$test_root/${wrapper_name}.wget.output"
  ECS_WRAPPER_CONTRACT_ARGS="$wget_args" ECS_WRAPPER_CONTRACT_TIMEOUT="$wget_timeout" \
    PATH="$wget_fixture" /bin/sh -c '. "$1"; fetch https://fixture.invalid/source "$2" 900' \
    sh "$wrapper_harness" "$wget_output" || fail "$wrapper_name wget helper failed"
  [[ "$(<"$wget_output")" == fixture ]] || fail "$wrapper_name wget helper did not write its output"
  wget_args_text=$(tr '\0' '\n' <"$wget_args")
  grep -F -x -- '--https-only' <<<"$wget_args_text" >/dev/null || fail "$wrapper_name wget helper omitted HTTPS-only mode"
  grep -F -x -- '--tries=3' <<<"$wget_args_text" >/dev/null || fail "$wrapper_name wget helper lost bounded retries"
  grep -F -x -- '--timeout=20' <<<"$wget_args_text" >/dev/null || fail "$wrapper_name wget helper lost per-operation timeout"
  wget_timeout_text=$(tr '\0' '\n' <"$wget_timeout")
  [[ "${wget_timeout_text%%$'\n'*}" == 900 ]] || fail "$wrapper_name wget helper lost its total timeout"

  hash_fixture="$test_root/${wrapper_name}.hash-bin"
  mkdir -p "$hash_fixture"
  cat >"$hash_fixture/openssl" <<'EOF'
#!/bin/sh
printf '%s\n' 'SHA2-256(fixture)= ABCDEF0123456789'
EOF
  chmod 0755 "$hash_fixture/openssl"
  ln -s "$(command -v awk)" "$hash_fixture/awk"
  ln -s "$(command -v tr)" "$hash_fixture/tr"
  hash_value=$(PATH="$hash_fixture" /bin/sh -c '. "$1"; file_sha256 fixture' sh "$wrapper_harness")
  [[ "$hash_value" == abcdef0123456789 ]] || fail "$wrapper_name did not retain the openssl SHA-256 fallback"
done

# Release tags are path components, not arbitrary URL fragments. All wrappers
# must reject an invalid version before platform probing, temporary work, or a
# downloader can run.
validation_bin="$test_root/version-validation-bin"
mkdir -p "$validation_bin"
cat >"$validation_bin/curl" <<'EOF'
#!/bin/sh
set -eu
: >"$ECS_WRAPPER_CONTRACT_UNEXPECTED_DOWNLOAD"
exit 90
EOF
chmod 0755 "$validation_bin/curl"
for wrapper_name in "${wrapper_names[@]}"; do
  validation_marker="$test_root/${wrapper_name}.unexpected-download"
  set +e
  ECS_LANG=en ECS_REPOSITORY=example/ecs ECS_VERSION='bad/tag' \
    ECS_RELEASE_BASE= ECS_INSTALL_DIR="$test_root/${wrapper_name}.install" \
    ECS_WRAPPER_CONTRACT_UNEXPECTED_DOWNLOAD="$validation_marker" PATH="$validation_bin:$PATH" \
    sh "$repo_root/$wrapper_name" >"$test_root/${wrapper_name}.version.stdout" \
    2>"$test_root/${wrapper_name}.version.stderr"
  validation_status=$?
  set -e
  [[ "$validation_status" -eq 1 ]] || fail "$wrapper_name accepted an invalid release version"
  [[ ! -e "$validation_marker" ]] || fail "$wrapper_name downloaded before rejecting an invalid release version"
done

echo "common download behavior tests passed"

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
attempt=0
if [ -f "$ECS_COMMON_TEST_COUNT" ]; then
  IFS= read -r attempt <"$ECS_COMMON_TEST_COUNT"
fi
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
case "${ECS_COMMON_TEST_MODE:-network-retry}" in
network-retry)
  if [ "$attempt" -eq 1 ]; then exit 28; fi
  printf '%s\n' "${ECS_COMMON_TEST_CONTENT:-common download fixture}" >"$destination"
  ;;
network-fail)
  exit 28
  ;;
checksum-mismatch)
  printf '%s\n' wrong >"$destination"
  ;;
missing-output)
  ;;
*) exit 64 ;;
esac
EOF
chmod 0755 "$fixture_bin/curl"

fixture_path="$fixture_bin:$PATH"
args_log="$test_root/curl.args"
count_file="$test_root/attempts"
output="$test_root/source.bin"
expected_sha=$(printf '%s\n' 'common download fixture' | sha256sum | awk '{print $1}')
if ! ECS_COMMON_TEST_ARGS="$args_log" ECS_COMMON_TEST_COUNT="$count_file" PATH="$fixture_path" \
    ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$output" fixture; then
  fail "network retry fixture failed"
fi

[[ "$(<"$count_file")" -eq 2 ]] || fail "network failure did not retry exactly once"
[[ "$(<"$output")" == 'common download fixture' ]] || fail "network retry did not retain the matching download"
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
  ECS_COMMON_TEST_MODE=network-fail PATH="$fixture_path" \
  ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$failure_output" fixture
failure_status=$?
set -e
[[ "$failure_status" -ne 0 ]] || fail "continuous network failure unexpectedly succeeded"
[[ "$(<"$failure_count_file")" -eq 3 ]] || fail "continuous network failure did not stop after three attempts"
[[ ! -e "$failure_output" ]] || fail "continuous network failure retained the output"

mismatch_count_file="$test_root/mismatch-attempts"
mismatch_output="$test_root/mismatch.bin"
set +e
ECS_COMMON_TEST_ARGS="$test_root/mismatch-curl.args" ECS_COMMON_TEST_COUNT="$mismatch_count_file" \
  ECS_COMMON_TEST_MODE=checksum-mismatch PATH="$fixture_path" \
  ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$mismatch_output" fixture \
  >"$test_root/mismatch.stdout" 2>"$test_root/mismatch.stderr"
mismatch_status=$?
set -e
[[ "$mismatch_status" -eq 1 ]] || fail "successful wrong-checksum download returned $mismatch_status instead of 1"
[[ "$(<"$mismatch_count_file")" -eq 1 ]] || fail "successful wrong-checksum download was retried"
[[ ! -e "$mismatch_output" ]] || fail "successful wrong-checksum download retained its output"
grep -F 'SHA-256 mismatch' "$test_root/mismatch.stderr" >/dev/null ||
  fail "successful wrong-checksum download did not report the mismatch"

hash_error_count_file="$test_root/hash-error-attempts"
hash_error_output="$test_root/hash-error.bin"
set +e
ECS_COMMON_TEST_ARGS="$test_root/hash-error-curl.args" ECS_COMMON_TEST_COUNT="$hash_error_count_file" \
  ECS_COMMON_TEST_MODE=missing-output PATH="$fixture_path" \
  ecs_download_sha256 https://fixture.invalid/source "$expected_sha" "$hash_error_output" fixture \
  >"$test_root/hash-error.stdout" 2>"$test_root/hash-error.stderr"
hash_error_status=$?
set -e
[[ "$hash_error_status" -eq 1 ]] || fail "real SHA-256 input error returned $hash_error_status instead of 1"
[[ "$(<"$hash_error_count_file")" -eq 1 ]] || fail "real SHA-256 input error retried the download"
[[ ! -e "$hash_error_output" ]] || fail "real SHA-256 input error retained its output"

# STREAM keeps its bounded network retry path, but verifies each successful
# response once and fails immediately on a wrong digest or hash I/O error.
source "$repo_root/scripts/lib/stream.sh"
ECS_STREAM_URL=https://fixture.invalid/stream
stream_body="$test_root/stream-source.c"
printf '%s\n' 'official stream fixture' >"$stream_body"
ECS_STREAM_SOURCE_SHA256=$(sha256sum "$stream_body" | awk '{print $1}')
stream_count_file="$test_root/stream-network-attempts"
stream_output="$test_root/stream-source-output.c"
if ! ECS_COMMON_TEST_ARGS="$test_root/stream-network.args" ECS_COMMON_TEST_COUNT="$stream_count_file" \
    ECS_COMMON_TEST_CONTENT='official stream fixture' \
    ECS_COMMON_TEST_MODE=network-retry PATH="$fixture_path" \
    ecs_stream_download "$stream_output"; then
  fail "STREAM download did not recover from a network failure"
fi
[[ "$(<"$stream_count_file")" -eq 2 ]] || fail "STREAM network failure did not retry exactly once"
cmp -s "$stream_body" "$stream_output" || fail "STREAM retry did not retain the matching source"

stream_mismatch_count="$test_root/stream-mismatch-attempts"
stream_mismatch_output="$test_root/stream-mismatch.c"
set +e
ECS_COMMON_TEST_ARGS="$test_root/stream-mismatch.args" ECS_COMMON_TEST_COUNT="$stream_mismatch_count" \
  ECS_COMMON_TEST_MODE=checksum-mismatch PATH="$fixture_path" \
  ecs_stream_download "$stream_mismatch_output" \
  >"$test_root/stream-mismatch.stdout" 2>"$test_root/stream-mismatch.stderr"
stream_mismatch_status=$?
set -e
[[ "$stream_mismatch_status" -eq 1 ]] || fail "STREAM wrong-checksum download returned $stream_mismatch_status instead of 1"
[[ "$(<"$stream_mismatch_count")" -eq 1 ]] || fail "STREAM wrong-checksum download was retried"
[[ ! -e "$stream_mismatch_output" ]] || fail "STREAM wrong-checksum download retained its output"

stream_hash_error_count="$test_root/stream-hash-error-attempts"
stream_hash_error_output="$test_root/stream-hash-error.c"
set +e
ECS_COMMON_TEST_ARGS="$test_root/stream-hash-error.args" ECS_COMMON_TEST_COUNT="$stream_hash_error_count" \
  ECS_COMMON_TEST_MODE=missing-output PATH="$fixture_path" \
  ecs_stream_download "$stream_hash_error_output" \
  >"$test_root/stream-hash-error.stdout" 2>"$test_root/stream-hash-error.stderr"
stream_hash_error_status=$?
set -e
[[ "$stream_hash_error_status" -eq 1 ]] || fail "STREAM real SHA-256 input error returned $stream_hash_error_status instead of 1"
[[ "$(<"$stream_hash_error_count")" -eq 1 ]] || fail "STREAM real SHA-256 input error retried the download"
[[ ! -e "$stream_hash_error_output" ]] || fail "STREAM real SHA-256 input error retained its output"

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
    printf '%s\n' 'die() { if [ "${UI:-}" = en ]; then printf "%s\\n" "$2" >&2; else printf "%s\\n" "$1" >&2; fi; exit 1; }'
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
: >"$ECS_WRAPPER_CONTRACT_WGET_USED"
exit 90
EOF
  chmod 0755 "$wget_fixture/wget"

  wget_output="$test_root/${wrapper_name}.wget.output"
  wget_used="$test_root/${wrapper_name}.wget.used"
  set +e
  ECS_WRAPPER_CONTRACT_WGET_USED="$wget_used" \
    PATH="$wget_fixture" /bin/sh -c '. "$1"; fetch https://fixture.invalid/source "$2" 900' \
    sh "$wrapper_harness" "$wget_output" >"$test_root/${wrapper_name}.wget.stdout" \
    2>"$test_root/${wrapper_name}.wget.stderr"
  wget_status=$?
  set -e
  [[ "$wget_status" -eq 1 ]] || fail "$wrapper_name accepted wget without Linux curl"
  grep -F 'curl is required for downloads on Linux' "$test_root/${wrapper_name}.wget.stderr" >/dev/null ||
    fail "$wrapper_name did not identify missing Linux curl"
  [[ ! -e "$wget_used" ]] || fail "$wrapper_name invoked wget as a fallback"
  [[ ! -e "$wget_output" ]] || fail "$wrapper_name created output after rejecting missing curl"

  hash_fixture="$test_root/${wrapper_name}.hash-bin"
  mkdir -p "$hash_fixture"
  cat >"$hash_fixture/openssl" <<'EOF'
#!/bin/sh
: >"$ECS_WRAPPER_CONTRACT_OPENSSL_USED"
exit 90
EOF
  chmod 0755 "$hash_fixture/openssl"
  openssl_used="$test_root/${wrapper_name}.openssl.used"
  set +e
  ECS_WRAPPER_CONTRACT_OPENSSL_USED="$openssl_used" \
    PATH="$hash_fixture" /bin/sh -c '. "$1"; file_sha256 fixture' sh "$wrapper_harness" \
    >"$test_root/${wrapper_name}.hash.stdout" 2>"$test_root/${wrapper_name}.hash.stderr"
  hash_status=$?
  set -e
  [[ "$hash_status" -eq 127 ]] || fail "$wrapper_name accepted OpenSSL without Linux sha256sum"
  grep -F 'sha256sum is required for verification on Linux' "$test_root/${wrapper_name}.hash.stderr" >/dev/null ||
    fail "$wrapper_name did not report the missing canonical Linux SHA-256 tool from file_sha256"
  [[ ! -e "$openssl_used" ]] || fail "$wrapper_name used OpenSSL as a SHA-256 fallback"

  hash_failure_fixture="$test_root/${wrapper_name}.hash-failure-bin"
  mkdir -p "$hash_failure_fixture"
  ln -s "$(command -v sha256sum)" "$hash_failure_fixture/sha256sum"
  set +e
  PATH="$hash_failure_fixture" /bin/sh -c '. "$1"; file_sha256 /missing/hash-fixture' \
    sh "$wrapper_harness" >"$test_root/${wrapper_name}.hash-failure.stdout" \
    2>"$test_root/${wrapper_name}.hash-failure.stderr"
  hash_failure_status=$?
  set -e
  [[ "$hash_failure_status" -ne 0 ]] || fail "$wrapper_name hid the canonical hash command failure"
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

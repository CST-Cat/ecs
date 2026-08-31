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
export ECS_TEST_PLAN_TOOLS=""

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

record_argv "$ECS_TEST_LOG_ROOT/ecs.argv" "$@"

if [ "${1:-}" = plan ]; then
  [ "${2:-}" != --json ] || exit 64
  plan_mode=${ECS_TEST_PLAN_MODE:-valid}
  plan_extra_exposure_line=""
  plan_extra_reveal_line=""
  case "$plan_mode" in
    missing-exposure)
      plan_exposure_line=""
      plan_reveal_line='  "reveal": true,'
      ;;
    invalid-exposure)
      plan_exposure_line='  "exposure": "invalid",'
      plan_reveal_line='  "reveal": true,'
      ;;
    duplicate-exposure)
      plan_exposure_line='  "exposure": "public",'
      plan_extra_exposure_line='  "exposure": "local",'
      plan_reveal_line='  "reveal": true,'
      ;;
    missing-reveal)
      plan_exposure_line='  "exposure": "public",'
      plan_reveal_line=""
      ;;
    invalid-reveal)
      plan_exposure_line='  "exposure": "public",'
      plan_reveal_line='  "reveal": "yes",'
      ;;
    duplicate-reveal)
      plan_exposure_line='  "exposure": "public",'
      plan_reveal_line='  "reveal": true,'
      plan_extra_reveal_line='  "reveal": false,'
      ;;
    valid)
      plan_exposure_line="  \"exposure\": \"${ECS_TEST_PLAN_EXPOSURE:-public}\","
      plan_reveal_line="  \"reveal\": ${ECS_TEST_PLAN_REVEAL:-true},"
      ;;
    *)
      exit 91
      ;;
  esac
  plan_tools_json=""
  for plan_tool in $ECS_TEST_PLAN_TOOLS; do
    [ -n "$plan_tools_json" ] && plan_tools_json="$plan_tools_json, "
    plan_tools_json="$plan_tools_json\"$plan_tool\""
  done
  cat <<JSON
{
  "schema_version": "ecs.plan/v1",
  "tool": {"name": "ecs", "version": "fixture"},
  "profile": "standard",
${plan_exposure_line}
${plan_extra_exposure_line}
${plan_reveal_line}
${plan_extra_reveal_line}
  "ip_version": "auto",
  "modules": [
    {"id": "noop"}
  ],
  "required_tools": [
    $plan_tools_json
  ],
  "needs_egress_ip": false,
  "external_services": [],
  "staging": {
    "mode": "temporary-prefix",
    "tool_archive_required": false,
    "nexttrace_tiny_required": false,
    "ookla_package_required": false,
    "zstd_corpus_required": false
  }
}
JSON
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
report_dir=${ECS_TEST_REPORT_DIR:-}
if [ -z "$report_dir" ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --output ]; then
      shift
      [ "$#" -gt 0 ] || exit 64
      report_dir=$1
    fi
    shift
  done
fi
[ -n "$report_dir" ] || exit 65
mkdir -p "$report_dir"
printf '{}\n' >"$report_dir/fixture.json"
EOF
chmod 0755 "$fixture_release/archive/ecs"
tar -czf "$fixture_release/$fixture_asset" -C "$fixture_release/archive" ecs
fixture_digest=$(sha256sum "$fixture_release/$fixture_asset" | awk '{print $1}')
printf '%s  %s\n' "$fixture_digest" "$fixture_asset" >"$fixture_release/checksums.txt"
fixture_tools_asset="ecs-tools_linux_"$fixture_arch".tar.gz"
mkdir -p "$fixture_release/tools/bin"
cat >"$fixture_release/tools/bin/zstd" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "$fixture_release/tools/bin/zstd"
tar -czf "$fixture_release/$fixture_tools_asset" -C "$fixture_release/tools" bin
fixture_tools_digest=$(sha256sum "$fixture_release/$fixture_tools_asset" | awk '{print $1}')
printf '%s  %s\n' "$fixture_tools_digest" "$fixture_tools_asset" >>"$fixture_release/checksums.txt"
export ECS_TEST_TOOLS_ASSET="$fixture_tools_asset"

cat >"$fixture_bin/curl" <<'EOF'
#!/bin/sh
set -eu

url=""
destination=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o|-O)
      shift
      [ "$#" -gt 0 ] || exit 64
      destination=$1
      ;;
    http://*|https://*) url=$1 ;;
  esac
  shift
done

case "$url" in
  "$ECS_TEST_RELEASE_URL/$ECS_TEST_ASSET") source_path="$ECS_TEST_RELEASE_ROOT/$ECS_TEST_ASSET" ;;
  "$ECS_TEST_RELEASE_URL/checksums.txt") source_path="$ECS_TEST_RELEASE_ROOT/checksums.txt" ;;
  "$ECS_TEST_RELEASE_URL/$ECS_TEST_TOOLS_ASSET") source_path="$ECS_TEST_RELEASE_ROOT/$ECS_TEST_TOOLS_ASSET" ;;
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

cp "$fixture_bin/curl" "$fixture_bin/wget"
chmod 0755 "$fixture_bin/wget"

release_url="https://github.com/example/ecs/releases/download/v-test"
test_path="$fixture_bin:$PATH"

make_wget_only_path() {
  local path=$1 command_name target
  mkdir -p "$path"
  for command_name in sh uname tr mkdir mktemp cp chmod mv id awk sha256sum tar gzip rm sed sort grep wc cat; do
    target=$(command -v "$command_name") || fail "required command is missing: $command_name"
    ln -s "$target" "$path/$command_name"
  done
  ln -s "$fixture_bin/wget" "$path/wget"
}
wget_only_path="$test_root/wget-only-path"
make_wget_only_path "$wget_only_path"

# Wget-only HTTPS success proves fetch selects wget when curl is absent.
wget_success_tmp="$test_root/wget-success-tmp"
wget_success_logs="$fixture_logs/wget-success"
wget_success_output="$test_root/wget-success-output"
mkdir -p "$wget_success_tmp" "$wget_success_logs"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$wget_success_tmp" PATH="$wget_only_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$wget_success_logs" ECS_TEST_REPORT_DIR="$wget_success_output" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --only noop --output "$wget_success_output" \
    >"$test_root/wget-success.stdout" 2>"$test_root/wget-success.stderr"; then
  fail "wget-only HTTPS fixture returned a failure: $(<"$test_root/wget-success.stderr")"
fi
[[ ! -e "$wget_success_logs/unexpected-network" ]] || fail "wget-only HTTPS fixture reached unexpected network"
[[ "$(wc -l <"$wget_success_logs/fetch.log")" -eq 2 ]] || fail "wget-only HTTPS fixture did not download twice"
[[ -f "$wget_success_output/fixture.json" ]] || fail "wget-only HTTPS fixture produced no report"

run_http_checksum_rejection() {
  local case_name=$1 command_path=$2
  local case_logs="$fixture_logs/http-checksum-$case_name"
  local case_output="$test_root/http-checksum-$case_name-output"
  mkdir -p "$case_logs" "$case_output"
  mkdir -p "$test_root/http-checksum-$case_name-tmp"
  set +e
  ECS_LANG=en ECS_AUTO_DEPS=1 TMPDIR="$test_root/http-checksum-$case_name-tmp" PATH="$command_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_CORPUS_BASE_URL=http://fixture.invalid/corpus \
    ECS_TEST_LOG_ROOT="$case_logs" ECS_TEST_REPORT_DIR="$case_output" \
    ECS_TEST_PLAN_TOOLS=zstd ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --only noop --output "$case_output" \
    >"$test_root/http-checksum-$case_name.stdout" 2>"$test_root/http-checksum-$case_name.stderr"
  local case_status=$?
  set -e
  [[ ! -e "$case_logs/unexpected-network" ]] || fail "$case_name invoked a downloader for an HTTP checksum URL"
  [[ "$(grep -c 'https://github.com/example/ecs/releases/download/v-test' "$case_logs/fetch.log")" -eq 3 ]] || \
    fail "$case_name did not complete the HTTPS artifact downloads before rejecting the HTTP checksum URL"
  [[ "$case_status" -eq 1 ]] || fail "$case_name returned $case_status instead of rejecting the HTTP checksum URL"
}

run_http_checksum_rejection curl "$test_path"
run_http_checksum_rejection wget "$wget_only_path"

# Wrapper help must be local for every supported global-language spelling. The
# first field is the conflicting ECS_LANG fallback: lang=en cases use zh so
# their English output proves that the argv spelling was actually consumed.
# Each case gets its own TMPDIR and fixture log so a download, fake ecs
# invocation, or temporary work directory cannot be hidden by another case.
help_cases=(
  'en|--help'
  'en|-h'
  'zh|--lang=en --help'
  'zh|-lang=en --help'
  'zh|--lang en --help'
  'zh|-lang en --help'
)
for help_case in "${help_cases[@]}"; do
  help_env=${help_case%%|*}
  help_args=${help_case#*|}
  help_name=${help_args// /_}
  help_tmp="$test_root/help-tmp-$help_name"
  help_logs="$fixture_logs/help-$help_name"
  mkdir -p "$help_tmp" "$help_logs"
  read -r -a help_argv <<<"$help_args"
  if ! ECS_LANG="$help_env" TMPDIR="$help_tmp" PATH="$test_path" \
      ECS_TEST_LOG_ROOT="$help_logs" \
      ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
      ECS_TEST_ASSET="$fixture_asset" \
      sh "$repo_root/run.sh" "${help_argv[@]}" >"$test_root/help-$help_name.stdout" 2>"$test_root/help-$help_name.stderr"; then
    fail "$help_args returned a failure"
  fi
  grep -F 'Usage: run.sh' "$test_root/help-$help_name.stdout" >/dev/null ||
    fail "$help_args output lost its English usage header"
  [[ ! -e "$help_logs/fetch.log" ]] || fail "$help_args attempted a download"
  [[ ! -e "$help_logs/unexpected-network" ]] || fail "$help_args attempted unexpected network access"
  [[ ! -e "$help_logs/ecs.argv" ]] || fail "$help_args executed the ecs fixture"
  assert_empty_dir "$help_tmp" "$help_args"
done

# A second --help after the explicit boundary belongs to ecs and must not
# trigger wrapper help.  The local fixture proves that the binary path ran.
boundary_help_tmp="$test_root/boundary-help-tmp"
boundary_help_logs="$fixture_logs/boundary-help"
boundary_help_output="$test_root/boundary-help-output"
mkdir -p "$boundary_help_tmp" "$boundary_help_logs"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$boundary_help_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$boundary_help_logs" \
    ECS_TEST_REPORT_DIR="$boundary_help_output" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" -- --help \
    >"$test_root/boundary-help.stdout" 2>"$test_root/boundary-help.stderr"; then
  fail "boundary --help fixture returned a failure: $(<"$test_root/boundary-help.stderr")"
fi
[[ -e "$boundary_help_logs/ecs.argv" ]] || fail "boundary --help did not execute the ecs fixture"
mapfile -d '' -t boundary_help_argv <"$boundary_help_logs/run.argv"
[[ "${#boundary_help_argv[@]}" -eq 13 && "${boundary_help_argv[4]}" == --help ]] ||
  fail "boundary --help was not passed verbatim to the ecs fixture"
if grep -F 'Usage: run.sh' "$test_root/boundary-help.stdout" >/dev/null; then
  fail "boundary --help unexpectedly printed wrapper help"
fi
[[ ! -e "$boundary_help_logs/unexpected-network" ]] || fail "boundary --help attempted unexpected network access"
[[ "$(wc -l <"$boundary_help_logs/fetch.log")" -eq 2 ]] || fail "boundary --help did not use the local fixture downloads"
[[ -f "$boundary_help_output/fixture.json" ]] || fail "boundary --help fixture did not produce output"
assert_empty_dir "$boundary_help_tmp" "boundary --help"

# Once a non-early-global token appears, a later --help belongs to ecs.  Keep
# a second --output in the fixture invocation so the first case tests the
# exact value-shaped boundary without creating a path named --help.
for non_early_option in --output --name --profile --config --only --disk-path; do
  non_early_name=${non_early_option#--}
  non_early_tmp="$test_root/non-early-$non_early_name-tmp"
  non_early_logs="$fixture_logs/non-early-$non_early_name"
  non_early_output="$test_root/non-early-$non_early_name-output"
  mkdir -p "$non_early_tmp" "$non_early_logs"
  if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$non_early_tmp" PATH="$test_path" \
      ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
      ECS_TEST_LOG_ROOT="$non_early_logs" \
      ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
      ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
      ECS_TEST_ASSET="$fixture_asset" \
      sh "$repo_root/run.sh" "$non_early_option" --help --output "$non_early_output" \
      >"$test_root/non-early-$non_early_name.stdout" \
      2>"$test_root/non-early-$non_early_name.stderr"; then
    fail "$non_early_option --help fixture returned a failure: $(<"$test_root/non-early-$non_early_name.stderr")"
  fi
  if grep -F 'Usage: run.sh' \
      "$test_root/non-early-$non_early_name.stdout" \
      "$test_root/non-early-$non_early_name.stderr" >/dev/null; then
    fail "$non_early_option --help unexpectedly printed wrapper help"
  fi
  [[ ! -e "$non_early_logs/unexpected-network" ]] ||
    fail "$non_early_option --help attempted unexpected network access"
  [[ "$(wc -l <"$non_early_logs/fetch.log")" -eq 2 ]] ||
    fail "$non_early_option --help did not use the local fixture downloads"
  [[ -e "$non_early_logs/ecs.argv" ]] ||
    fail "$non_early_option --help did not execute the ecs fixture"
  mapfile -d '' -t non_early_argv <"$non_early_logs/ecs.argv"
  [[ "${#non_early_argv[@]}" -eq 16 ]] ||
    fail "$non_early_option --help reached ecs with ${#non_early_argv[@]} arguments"
  [[ "${non_early_argv[0]}" == --output && "${non_early_argv[1]}" == "$non_early_tmp" &&
     "${non_early_argv[2]}" == --name && "${non_early_argv[3]}" == ecs-report-ecs-run.* ]] ||
    fail "$non_early_option --help lost wrapper defaults"
  [[ "${non_early_argv[4]}" == "$non_early_option" &&
     "${non_early_argv[5]}" == --help ]] ||
    fail "$non_early_option --help was not passed to the ecs fixture"
  [[ -f "$non_early_output/fixture.json" ]] ||
    fail "$non_early_option --help fixture did not produce output"
  assert_empty_dir "$non_early_tmp" "$non_early_option --help"
done

# Wrapper submit options stop at the exact boundary. Assert every token seen
# by the run and submit commands for the documented happy path.
canonical_submit_tmp="$test_root/canonical-submit-tmp"
canonical_submit_logs="$fixture_logs/canonical-submit"
canonical_submission_output="$test_root/canonical-submission.json"
mkdir -p "$canonical_submit_tmp" "$canonical_submit_logs"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$canonical_submit_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$canonical_submit_logs" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=false \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --submit --provider vultr --region jp \
      --output "$canonical_submission_output" -- --profile full --yes \
      >"$test_root/canonical-submit.stdout" 2>"$test_root/canonical-submit.stderr"; then
  fail "canonical submit fixture returned a failure: $(<"$test_root/canonical-submit.stderr")"
fi
mapfile -d '' -t canonical_run_argv <"$canonical_submit_logs/run.argv"
[[ "${#canonical_run_argv[@]}" -eq 7 ]] ||
  fail "canonical submit run received ${#canonical_run_argv[@]} arguments instead of 7"
[[ "${canonical_run_argv[0]}" == --profile && "${canonical_run_argv[1]}" == full &&
   "${canonical_run_argv[2]}" == --yes && "${canonical_run_argv[3]}" == --format &&
   "${canonical_run_argv[4]}" == json && "${canonical_run_argv[5]}" == --output &&
   "${canonical_run_argv[6]}" == "$canonical_submit_tmp"/ecs-run.*/report ]] ||
  fail "canonical submit run argv changed"
mapfile -d '' -t canonical_submit_argv <"$canonical_submit_logs/submit.argv"
[[ "${#canonical_submit_argv[@]}" -eq 9 ]] ||
  fail "canonical ecs submit received ${#canonical_submit_argv[@]} arguments instead of 9"
[[ "${canonical_submit_argv[0]}" == submit && "${canonical_submit_argv[1]}" == --input &&
   "${canonical_submit_argv[2]}" == "${canonical_run_argv[6]}/fixture.json" &&
   "${canonical_submit_argv[3]}" == --output &&
   "${canonical_submit_argv[4]}" == "$canonical_submission_output" &&
   "${canonical_submit_argv[5]}" == --provider && "${canonical_submit_argv[6]}" == vultr &&
   "${canonical_submit_argv[7]}" == --region && "${canonical_submit_argv[8]}" == jp ]] ||
  fail "canonical ecs submit argv changed"
[[ ! -e "$canonical_submit_logs/unexpected-network" ]] ||
  fail "canonical submit attempted unexpected network access"
[[ "$(wc -l <"$canonical_submit_logs/fetch.log")" -eq 2 ]] ||
  fail "canonical submit did not perform exactly two fixture fetches"
assert_empty_dir "$canonical_submit_tmp" "canonical submit"

# submit/provider/region/用户 output 由 wrapper 消费；普通参数和含空格值必须仍是独立 argv。
run_tmp="$test_root/run-tmp"
submission_output="$test_root/submission file.json"
submit_logs="$fixture_logs/submit"
mkdir -p "$submit_logs"
mkdir -p "$run_tmp"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$submit_logs" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=false \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --submit --provider='Cloud Alpha' --region='East Zone' \
      --output="$submission_output" -- --profile standard --name 'node alpha' --yes \
      >"$test_root/run.stdout" 2>"$test_root/run.stderr"; then
  fail "local submit fixture returned a failure: $(<"$test_root/run.stderr")"
fi

[[ ! -e "$submit_logs/unexpected-network" ]] || fail "submit path attempted unexpected network access"
[[ "$(wc -l <"$submit_logs/fetch.log")" -eq 2 ]] || fail "submit path did not perform exactly two local fixture fetches"
mapfile -d '' -t run_argv <"$submit_logs/run.argv"
[[ "${#run_argv[@]}" -eq 9 ]] || fail "ecs run received ${#run_argv[@]} arguments instead of 9"
[[ "${run_argv[0]}" == --profile && "${run_argv[1]}" == standard ]] || fail "ordinary profile arguments changed"
[[ "${run_argv[2]}" == --name && "${run_argv[3]}" == 'node alpha' ]] || fail "space-containing --name value lost its argv boundary"
[[ "${run_argv[4]}" == --yes && "${run_argv[5]}" == --format && "${run_argv[6]}" == json ]] || fail "ordinary/forced run arguments changed"
[[ "${run_argv[7]}" == --output ]] || fail "temporary run output flag is missing"
[[ "${run_argv[8]}" == "$run_tmp"/ecs-run.*/report ]] || fail "temporary run output escaped WORK: ${run_argv[8]}"
[[ "${run_argv[8]}" != "$submission_output" ]] || fail "user submission output leaked into ecs run"

mapfile -d '' -t submit_argv <"$submit_logs/submit.argv"
[[ "${#submit_argv[@]}" -eq 9 ]] || fail "ecs submit received ${#submit_argv[@]} arguments instead of 9"
[[ "${submit_argv[0]}" == submit && "${submit_argv[1]}" == --input ]] || fail "submit prefix changed"
[[ "${submit_argv[2]}" == "${run_argv[8]}/fixture.json" ]] || fail "submit input does not match the temporary report"
[[ "${submit_argv[3]}" == --output && "${submit_argv[4]}" == "$submission_output" ]] || fail "space-containing submit output lost its argv boundary"
[[ "${submit_argv[5]}" == --provider && "${submit_argv[6]}" == 'Cloud Alpha' ]] || fail "wrapper provider was not consumed and reconstructed correctly"
[[ "${submit_argv[7]}" == --region && "${submit_argv[8]}" == 'East Zone' ]] || fail "wrapper region was not consumed and reconstructed correctly"

# Option-looking values after the boundary belong wholly to Go.  In
# particular, --submit must not activate wrapper mode when it is --name's
# value, and no wrapper option may disappear from the recorded run argv.
for boundary_value in submit output provider region; do
  boundary_value_tmp="$test_root/boundary-value-$boundary_value-tmp"
  boundary_value_logs="$fixture_logs/boundary-value-$boundary_value"
  boundary_value_report="$test_root/boundary-value-$boundary_value-report"
  mkdir -p "$boundary_value_tmp" "$boundary_value_logs" "$boundary_value_report"
  if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$boundary_value_tmp" PATH="$test_path" \
      ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
      ECS_TEST_LOG_ROOT="$boundary_value_logs" \
      ECS_TEST_REPORT_DIR="$boundary_value_report" \
      ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
      ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
      ECS_TEST_ASSET="$fixture_asset" \
      sh "$repo_root/run.sh" -- --name "--$boundary_value" \
      >"$test_root/boundary-value-$boundary_value.stdout" \
      2>"$test_root/boundary-value-$boundary_value.stderr"; then
    fail "boundary --name --$boundary_value returned a failure: $(<"$test_root/boundary-value-$boundary_value.stderr")"
  fi
  [[ ! -e "$boundary_value_logs/submit.argv" ]] ||
    fail "boundary --name --$boundary_value activated submit mode"
  mapfile -d '' -t boundary_value_argv <"$boundary_value_logs/run.argv"
  [[ "${#boundary_value_argv[@]}" -eq 14 ]] ||
    fail "boundary --name --$boundary_value reached ecs with ${#boundary_value_argv[@]} arguments"
  [[ "${boundary_value_argv[0]}" == --output &&
     "${boundary_value_argv[1]}" == "$boundary_value_tmp" &&
     "${boundary_value_argv[2]}" == --name &&
     "${boundary_value_argv[3]}" == ecs-report-ecs-run.* &&
     "${boundary_value_argv[4]}" == --name &&
     "${boundary_value_argv[5]}" == "--$boundary_value" &&
     "${boundary_value_argv[6]}" == --profile &&
     "${boundary_value_argv[7]}" == standard &&
     "${boundary_value_argv[8]}" == --only &&
     "${boundary_value_argv[9]}" == noop &&
     "${boundary_value_argv[10]}" == --yes &&
     "${boundary_value_argv[11]}" == --exposure &&
     "${boundary_value_argv[12]}" == public &&
     "${boundary_value_argv[13]}" == --reveal=true ]] ||
    fail "boundary --name --$boundary_value argv changed"
  assert_empty_dir "$boundary_value_tmp" "boundary --name --$boundary_value"
done

# Wrapper --output names the submission artifact. A Go --output after the
# boundary remains in run argv, while the wrapper's forced final output keeps
# the private intermediate report under WORK.
dual_output_tmp="$test_root/dual-output-tmp"
dual_output_logs="$fixture_logs/dual-output"
dual_submission_output="$test_root/dual-submission.json"
dual_run_output="$test_root/dual-run-output"
mkdir -p "$dual_output_tmp" "$dual_output_logs"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$dual_output_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$dual_output_logs" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=false \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --submit --output "$dual_submission_output" -- \
      --output "$dual_run_output" \
      >"$test_root/dual-output.stdout" 2>"$test_root/dual-output.stderr"; then
  fail "dual output fixture returned a failure: $(<"$test_root/dual-output.stderr")"
fi
mapfile -d '' -t dual_run_argv <"$dual_output_logs/run.argv"
[[ "${#dual_run_argv[@]}" -eq 6 &&
   "${dual_run_argv[0]}" == --output && "${dual_run_argv[1]}" == "$dual_run_output" &&
   "${dual_run_argv[2]}" == --format && "${dual_run_argv[3]}" == json &&
   "${dual_run_argv[4]}" == --output &&
   "${dual_run_argv[5]}" == "$dual_output_tmp"/ecs-run.*/report ]] ||
  fail "Go and wrapper output argv were not kept distinct"
mapfile -d '' -t dual_submit_argv <"$dual_output_logs/submit.argv"
[[ "${#dual_submit_argv[@]}" -eq 5 &&
   "${dual_submit_argv[0]}" == submit && "${dual_submit_argv[1]}" == --input &&
   "${dual_submit_argv[2]}" == "${dual_run_argv[5]}/fixture.json" &&
   "${dual_submit_argv[3]}" == --output &&
   "${dual_submit_argv[4]}" == "$dual_submission_output" ]] ||
  fail "wrapper submission output was not isolated from Go run output"
assert_empty_dir "$dual_output_tmp" "dual output submit"

# 普通 run 必须把 plan 的 exposure/reveal 放在原始冲突参数之后，确保向导/plan
# 的最终隐私选择由 Go flag 的 last-wins 规则生效。
ordinary_false_logs="$fixture_logs/ordinary-false"
ordinary_false_output="$test_root/ordinary-false-output"
mkdir -p "$ordinary_false_logs" "$ordinary_false_output"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$ordinary_false_logs" \
    ECS_TEST_PLAN_EXPOSURE=local ECS_TEST_PLAN_REVEAL=false \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --only noop --exposure any --reveal=true \
      --output "$ordinary_false_output" >"$test_root/ordinary-false.stdout" 2>"$test_root/ordinary-false.stderr"; then
  fail "local ordinary false-plan fixture returned a failure: $(<"$test_root/ordinary-false.stderr")"
fi
mapfile -d '' -t ordinary_false_argv <"$ordinary_false_logs/run.argv"
[[ "${#ordinary_false_argv[@]}" -eq 21 ]] || fail "ordinary false-plan run received ${#ordinary_false_argv[@]} arguments"
[[ "${ordinary_false_argv[8]}" == --exposure && "${ordinary_false_argv[9]}" == any ]] || fail "ordinary conflict exposure was not preserved in original argv"
[[ "${ordinary_false_argv[10]}" == --reveal=true ]] || fail "ordinary conflict reveal was not preserved in original argv"
ordinary_false_last=$(( ${#ordinary_false_argv[@]} - 3 ))
[[ "${ordinary_false_argv[$ordinary_false_last]}" == --exposure && "${ordinary_false_argv[$((ordinary_false_last + 1))]}" == local ]] || fail "plan exposure local was not appended last"
[[ "${ordinary_false_argv[$((ordinary_false_last + 2))]}" == --reveal=false ]] || fail "plan reveal=false was not appended last"
[[ "${ordinary_false_argv[$((ordinary_false_last + 1))]}" != any ]] || fail "plan exposure did not override the original value"

ordinary_true_logs="$fixture_logs/ordinary-true"
ordinary_true_output="$test_root/ordinary-true-output"
mkdir -p "$ordinary_true_logs" "$ordinary_true_output"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$ordinary_true_logs" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --only noop --exposure local --reveal=false \
      --output "$ordinary_true_output" >"$test_root/ordinary-true.stdout" 2>"$test_root/ordinary-true.stderr"; then
  fail "local ordinary true-plan fixture returned a failure: $(<"$test_root/ordinary-true.stderr")"
fi
mapfile -d '' -t ordinary_true_argv <"$ordinary_true_logs/run.argv"
[[ "${#ordinary_true_argv[@]}" -eq 21 ]] || fail "ordinary true-plan run received ${#ordinary_true_argv[@]} arguments"
[[ "${ordinary_true_argv[8]}" == --exposure && "${ordinary_true_argv[9]}" == local ]] || fail "ordinary true-plan conflict exposure was not preserved"
[[ "${ordinary_true_argv[10]}" == --reveal=false ]] || fail "ordinary true-plan conflict reveal was not preserved"
ordinary_true_last=$(( ${#ordinary_true_argv[@]} - 3 ))
[[ "${ordinary_true_argv[$ordinary_true_last]}" == --exposure && "${ordinary_true_argv[$((ordinary_true_last + 1))]}" == public ]] || fail "plan exposure public was not appended last"
[[ "${ordinary_true_argv[$((ordinary_true_last + 2))]}" == --reveal=true ]] || fail "plan reveal=true was not appended last"

# 其余两个合法 exposure 也必须通过同一 parser/final-run 路径；这里保留
# 精确 argv 长度与末尾三个计划参数断言，避免未来在计划值后追加冲突参数。
for legal_exposure in thirdparty any; do
  legal_reveal=false
  [ "$legal_exposure" = any ] && legal_reveal=true
  legal_logs="$fixture_logs/ordinary-$legal_exposure"
  legal_output="$test_root/ordinary-$legal_exposure-output"
  mkdir -p "$legal_logs" "$legal_output"
  if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
      ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
      ECS_TEST_LOG_ROOT="$legal_logs" \
      ECS_TEST_PLAN_EXPOSURE="$legal_exposure" ECS_TEST_PLAN_REVEAL="$legal_reveal" \
      ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
      ECS_TEST_ASSET="$fixture_asset" \
      sh "$repo_root/run.sh" --profile standard --only noop --output "$legal_output" \
      >"$test_root/ordinary-$legal_exposure.stdout" 2>"$test_root/ordinary-$legal_exposure.stderr"; then
    fail "local ordinary $legal_exposure-plan fixture returned a failure: $(<"$test_root/ordinary-$legal_exposure.stderr")"
  fi
  mapfile -d '' -t legal_argv <"$legal_logs/run.argv"
  [[ "${#legal_argv[@]}" -eq 18 ]] || fail "ordinary $legal_exposure-plan run received ${#legal_argv[@]} arguments"
  legal_last=$(( ${#legal_argv[@]} - 3 ))
  [[ "${legal_argv[$legal_last]}" == --exposure && "${legal_argv[$((legal_last + 1))]}" == "$legal_exposure" ]] || fail "plan exposure $legal_exposure was not the final exposure value"
  [[ "${legal_argv[$((legal_last + 2))]}" == "--reveal=$legal_reveal" ]] || fail "plan reveal=$legal_reveal was not the final reveal value"
done

run_invalid_plan_case() {
  case_name=$1
  plan_mode=$2
  for ui_lang in en zh; do
    case_logs="$fixture_logs/failure-$case_name-$ui_lang"
    case_output="$test_root/failure-$case_name-$ui_lang-output"
    mkdir -p "$case_logs" "$case_output"
    if ECS_LANG="$ui_lang" ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
        ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
        ECS_TEST_LOG_ROOT="$case_logs" ECS_TEST_PLAN_MODE="$plan_mode" \
        ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
        ECS_TEST_ASSET="$fixture_asset" \
        sh "$repo_root/run.sh" --profile standard --only noop --output "$case_output" \
        >"$test_root/failure-$case_name-$ui_lang.stdout" 2>"$test_root/failure-$case_name-$ui_lang.stderr"; then
      fail "$case_name $ui_lang plan unexpectedly succeeded"
    fi
    if [ "$ui_lang" = en ]; then
      grep -F 'execution plan' "$test_root/failure-$case_name-$ui_lang.stderr" >/dev/null ||
        fail "$case_name English failure did not report an execution-plan error"
    else
      grep -F '执行计划' "$test_root/failure-$case_name-$ui_lang.stderr" >/dev/null ||
        fail "$case_name Chinese failure did not report an execution-plan error"
    fi
    [[ ! -e "$case_logs/run.argv" ]] || fail "$case_name $ui_lang plan executed the final run"
  done
}

run_invalid_plan_case missing-exposure missing-exposure
run_invalid_plan_case invalid-exposure invalid-exposure
run_invalid_plan_case duplicate-exposure duplicate-exposure
run_invalid_plan_case missing-reveal missing-reveal
run_invalid_plan_case invalid-reveal invalid-reveal
run_invalid_plan_case duplicate-reveal duplicate-reveal

if find "$fixture_logs" -name unexpected-network -print -quit | grep -q .; then
  fail "a fixture path attempted unexpected network access"
fi

echo "run.sh behavior tests passed"

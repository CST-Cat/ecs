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
  "required_tools": [],
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

# submit/provider/region/用户 output 由 wrapper 消费；普通参数和含空格值必须仍是独立 argv。
run_tmp="$test_root/run-tmp"
submission_output="$test_root/submission file.json"
submit_logs="$fixture_logs/submit"
mkdir -p "$submit_logs"
mkdir -p "$run_tmp"
if ! ECS_LANG=en ECS_AUTO_DEPS=0 TMPDIR="$run_tmp" PATH="$test_path" \
    ECS_REPOSITORY=example/ecs ECS_VERSION=v-test \
    ECS_TEST_LOG_ROOT="$submit_logs" \
    ECS_TEST_PLAN_EXPOSURE=public ECS_TEST_PLAN_REVEAL=true \
    ECS_TEST_RELEASE_URL="$release_url" ECS_TEST_RELEASE_ROOT="$fixture_release" \
    ECS_TEST_ASSET="$fixture_asset" \
    sh "$repo_root/run.sh" --profile standard --submit --name 'node alpha' \
      --provider 'Cloud Alpha' --yes --region='East Zone' --output "$submission_output" \
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
[[ "${#ordinary_false_argv[@]}" -eq 17 ]] || fail "ordinary false-plan run received ${#ordinary_false_argv[@]} arguments"
[[ "${ordinary_false_argv[4]}" == --exposure && "${ordinary_false_argv[5]}" == any ]] || fail "ordinary conflict exposure was not preserved in original argv"
[[ "${ordinary_false_argv[6]}" == --reveal=true ]] || fail "ordinary conflict reveal was not preserved in original argv"
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
[[ "${#ordinary_true_argv[@]}" -eq 17 ]] || fail "ordinary true-plan run received ${#ordinary_true_argv[@]} arguments"
[[ "${ordinary_true_argv[4]}" == --exposure && "${ordinary_true_argv[5]}" == local ]] || fail "ordinary true-plan conflict exposure was not preserved"
[[ "${ordinary_true_argv[6]}" == --reveal=false ]] || fail "ordinary true-plan conflict reveal was not preserved"
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
  [[ "${#legal_argv[@]}" -eq 14 ]] || fail "ordinary $legal_exposure-plan run received ${#legal_argv[@]} arguments"
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

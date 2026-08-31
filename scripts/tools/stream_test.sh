#!/usr/bin/env bash
set -euo pipefail

# This is a tiny local output fixture, not a benchmark. It exercises the smoke
# runner's thread selection and file reading without executing STREAM itself.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-stream-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

fail() {
  echo "STREAM smoke tests: $*" >&2
  exit 1
}

die() {
  fail "$*"
}

source "$repo_root/scripts/tools/stream.sh"

# run_target is the local fixture: it records the requested allowance and
# emits the smallest output that satisfies the existing STREAM smoke contract.
stream_bin=stream-fixture
run_target() {
  printf '%s\n' "${OMP_NUM_THREADS:-unset}" >>"$work/calls"
  printf '%s\n' \
    'Copy: 1' \
    'Scale: 1' \
    'Add: 1' \
    'Triad: 1' \
    'Solution Validates'
}

run_case() {
  local allowance=$1 expected_calls=$2 expected_runs=$3 actual_copy_rows
  work="$test_root/work-$allowance"
  mkdir -p "$work"
  : >"$work/calls"

  STREAM_NT_THREADS="$allowance" ecs_tool_smoke_stream >"$work/result"

  [[ "$(<"$work/calls")" == "$expected_calls" ]] ||
    fail "allowance $allowance calls = $(<"$work/calls"), want $expected_calls"
  actual_copy_rows=$(grep -c '^Copy:' "$work/result")
  [[ "$actual_copy_rows" -eq "$expected_runs" ]] ||
    fail "allowance $allowance read $actual_copy_rows Copy rows, want $expected_runs"
  [[ -f "$work/stream-1t.txt" ]] || fail "allowance $allowance omitted 1t output"
  if [[ "$allowance" -eq 1 ]]; then
    [[ ! -e "$work/stream-nt.txt" ]] || fail "single-CPU allowance created an nt output"
  else
    [[ -f "$work/stream-nt.txt" ]] || fail "allowance $allowance omitted nt output"
  fi
}

run_case 1 '1' 1
run_case 2 $'1\n2' 2

echo "STREAM smoke tests passed"

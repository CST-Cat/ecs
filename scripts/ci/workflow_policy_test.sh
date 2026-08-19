#!/usr/bin/env bash
set -euo pipefail

# release.yml 与 ci.yml 的版本/工具链边界回归。
#
# 这是 Bash 脚本，故意无 Python/YAML 依赖：即使分析工具环境尚未准备好，
# workflow policy 也必须被独立、确定性地验证，不能静默回退。

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
release_workflow="$repo_root/.github/workflows/release.yml"
ci_workflow="$repo_root/.github/workflows/ci.yml"

fail() {
  echo "workflow policy: $*" >&2
  exit 1
}

[[ -f "$release_workflow" ]] || fail "missing $release_workflow"
[[ -f "$ci_workflow" ]] || fail "missing $ci_workflow"

assert_fixed_line() {
  local file=$1
  local line=$2
  grep -F -x -- "$line" "$file" >/dev/null ||
    fail "$file is missing: $line"
}

assert_absent() {
  local file=$1
  local text=$2
  if grep -F -- "$text" "$file" >/dev/null; then
    fail "$file contains forbidden policy: $text"
  fi
}

job_body() {
  local file=$1
  local job=$2
  awk -v wanted="  $job:" '
    function job_header(line) {
      return line ~ /^  [[:alnum:]_][[:alnum:]_-]*:/
    }
    $0 == wanted {
      found = 1
      print
      next
    }
    found && job_header($0) { exit }
    found { print }
    END {
      if (!found) exit 2
    }
  ' "$file"
}

# Release compiler selection has one source of truth, and every setup-go entry
# consumes it. GOTOOLCHAIN is inherited by nested release/build/verify commands.
release_pin_lines=$(grep -E '^[[:space:]]+ECS_RELEASE_GO:' "$release_workflow" || true)
release_pin_count=$(printf '%s\n' "$release_pin_lines" | sed '/^$/d' | wc -l)
[[ "$release_pin_count" -eq 1 ]] ||
  fail "release.yml has $release_pin_count ECS_RELEASE_GO assignments; want one"
release_pin_line=$release_pin_lines
[[ "$release_pin_line" == "  ECS_RELEASE_GO:"* ]] ||
  fail "ECS_RELEASE_GO must be the workflow-level release pin"
release_pin=${release_pin_line#*:}
release_pin=${release_pin//[[:space:]]/}
release_pin=${release_pin#\"}
release_pin=${release_pin%\"}
[[ "$release_pin" =~ ^1\.[0-9]+\.[0-9]+$ ]] ||
  fail "ECS_RELEASE_GO is not an exact Go semver pin: $release_pin"
assert_fixed_line "$release_workflow" '  GOTOOLCHAIN: local'
assert_absent "$release_workflow" 'go-version-file:'

release_go_versions=$(grep -E '^[[:space:]]+go-version:' "$release_workflow" || true)
[[ -n "$release_go_versions" ]] || fail "release.yml has no setup-go version"
if grep -vF 'go-version: ${{ env.ECS_RELEASE_GO }}' <<<"$release_go_versions" >/dev/null; then
  fail "a release setup-go entry bypasses ECS_RELEASE_GO"
fi

# The minimum-compatibility job is intentionally a single 1.22.x check. Its
# local toolchain mode must be inside that job, while stable belongs to unit.
compat=$(job_body "$ci_workflow" compat) || fail "ci.yml has no compat job"
grep -F 'go-version: ["1.22.x"]' <<<"$compat" >/dev/null || fail "compat matrix is not exactly 1.22.x"
if grep -F 'stable' <<<"$compat" >/dev/null; then
  fail "stable is duplicated in compat"
fi
grep -F '      GOTOOLCHAIN: local' <<<"$compat" >/dev/null || fail "compat does not force GOTOOLCHAIN=local"

unit=$(job_body "$ci_workflow" unit) || fail "ci.yml has no unit job"
grep -F 'go-version: stable' <<<"$unit" >/dev/null || fail "unit no longer runs on stable"
if grep -F 'GOTOOLCHAIN:' <<<"$unit" >/dev/null; then
  fail "unit is pretending to be the minimum-compatibility job"
fi

# Keep the compat matrix and its policy wiring singular: a second assignment or
# a second stable matrix entry would make the required check ambiguous.
toolchain_assignments=$(grep -c -E '^[[:space:]]+GOTOOLCHAIN:' "$ci_workflow" || true)
[[ "$toolchain_assignments" -eq 1 ]] || fail "ci.yml has $toolchain_assignments GOTOOLCHAIN assignments; want one in compat"

echo "workflow policy tests passed"

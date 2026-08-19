#!/usr/bin/env bash
set -euo pipefail

# Release 编译器 pin 与最低兼容版本 job 的最小 wiring 契约。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
release_workflow="$repo_root/.github/workflows/release.yml"
ci_workflow="$repo_root/.github/workflows/ci.yml"

fail() {
  echo "workflow policy: $*" >&2
  exit 1
}

[[ -f "$release_workflow" && -f "$ci_workflow" ]] || fail "workflow file is missing"

release_pin=$(grep -E '^[[:space:]]+ECS_RELEASE_GO:' "$release_workflow" |
  awk -F: '{ gsub(/[[:space:]\"]/, "", $2); print $2 }')
[[ "$release_pin" =~ ^1\.[0-9]+\.[0-9]+$ ]] || fail "invalid ECS_RELEASE_GO pin: $release_pin"
[[ "$(grep -Ec '^[[:space:]]+ECS_RELEASE_GO:' "$release_workflow")" -eq 1 ]] || fail "release pin is not unique"
grep -Fqx '  GOTOOLCHAIN: local' "$release_workflow" || fail "release does not force local toolchain"
grep -F 'go-version: ${{ env.ECS_RELEASE_GO }}' "$release_workflow" >/dev/null || fail "release setup-go bypasses release pin"

compat=$(awk '
  $0 == "  compat:" { found=1; print; next }
  found && /^  [[:alnum:]_-]+:/ { exit }
  found { print }
  END { if (!found) exit 2 }
' "$ci_workflow") || fail "compat job is missing"
grep -F 'go-version: ["1.22.x"]' <<<"$compat" >/dev/null || fail "compat is not pinned to 1.22.x"
grep -F 'GOTOOLCHAIN: local' <<<"$compat" >/dev/null || fail "compat does not force local toolchain"
if grep -F 'stable' <<<"$compat" >/dev/null; then
  fail "stable is duplicated in compat"
fi

unit=$(awk '
  $0 == "  unit:" { found=1; print; next }
  found && /^  [[:alnum:]_-]+:/ { exit }
  found { print }
  END { if (!found) exit 2 }
' "$ci_workflow") || fail "unit job is missing"
grep -F 'go-version: stable' <<<"$unit" >/dev/null || fail "unit no longer runs stable Go"

echo "workflow policy tests passed"

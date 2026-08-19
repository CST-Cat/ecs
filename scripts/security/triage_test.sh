#!/usr/bin/env bash
set -euo pipefail

# 发布制品版本与 security runner 版本不同、漏洞 fixed=1.26.6 时，
# triage 必须根据制品的 1.26.5 推荐 1.26.6。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
triage="$repo_root/scripts/security/triage.py"
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-triage-test.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

cat >"$work/finding.json" <<'JSON'
{"osv":{"id":"GO-2026-WIRING","affected":[{"package":{"name":"stdlib","ecosystem":"Go"},"ranges":[{"type":"SEMVER","events":[{"introduced":"1.26.0-0"},{"fixed":"1.26.6"}]}]}]}}
{"finding":{"osv":"GO-2026-WIRING","trace":[{"module":"stdlib"}]}}
JSON

artifact_current=go1.26.5
runner_current=go1.26.6
[[ "$artifact_current" != "$runner_current" ]] || { echo "fixture versions must differ" >&2; exit 1; }
upgrade=$(
  "$triage" --current "$artifact_current" "$work/finding.json" |
    awk -F= '$1 == "upgrade_to" { print $2; exit }'
)
[[ "$upgrade" == 1.26.6 ]] || { echo "upgrade_to=$upgrade, want 1.26.6" >&2; exit 1; }

if ! no_upgrade=$("$triage" --current 1.26.6 "$work/finding.json" 2>"$work/no-upgrade.err"); then
  echo "already-fixed triage unexpectedly failed" >&2
  exit 1
fi
grep -Fx 'upgrade_to=' <<<"$no_upgrade" >/dev/null || {
  echo "already-fixed finding requested an upgrade: $no_upgrade" >&2
  exit 1
}
grep -F "no fixed Go release" "$work/no-upgrade.err" >/dev/null || {
  echo "already-fixed diagnostic was lost" >&2
  exit 1
}

security_workflow="$repo_root/.github/workflows/security.yml"
grep -F 'release_build_go: ${{ steps.scan.outputs.release_build_go }}' "$security_workflow" >/dev/null ||
  { echo "security workflow does not expose release build Go" >&2; exit 1; }
grep -F -- '--current "$RELEASE_BUILD_GO"' "$security_workflow" >/dev/null ||
  { echo "security workflow does not pass artifact Go to triage" >&2; exit 1; }
if grep -F -- '--current "$(go env GOVERSION)"' "$security_workflow" >/dev/null; then
  echo "security runner Go was wired as triage current" >&2
  exit 1
fi

echo "triage tests passed"

#!/usr/bin/env bash
set -euo pipefail

# 只验证从一个 Release-like Go binary 读取真实 build metadata。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source "$repo_root/scripts/security/scan_released.sh"

command -v go >/dev/null 2>&1 || { echo "go is required" >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-scan-released-test.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
cat >"$work/main.go" <<'GO'
package main

func main() {}
GO

GO111MODULE=off go build -trimpath -o "$work/ecs" "$work/main.go"
want=$(go version | awk '{print $3}')
got=$(read_release_build_go "$work/ecs")
[[ "$got" == "$want" ]] || {
  echo "build Go metadata = $got, want $want" >&2
  exit 1
}

printf '%s\n' 'not a Go binary' >"$work/not-go"
chmod 0755 "$work/not-go"
if failure=$(read_release_build_go "$work/not-go" 2>&1); then
  echo "non-Go binary unexpectedly passed" >&2
  exit 1
fi
grep -E "no valid Go build metadata|go version -m failed" <<<"$failure" >/dev/null || {
  echo "invalid metadata diagnostic was lost: $failure" >&2
  exit 1
}

echo "scan-released tests passed"

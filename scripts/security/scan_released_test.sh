#!/usr/bin/env bash
set -euo pipefail

# Offline regression tests for scan_released.sh's Release metadata gate.
#
# The fixtures are actual Go cross-builds.  The test invokes the real `go
# version -m` through verify_release_build_go; it does not replace the parser
# with a shell stub.  One fixture uses the linker's runtime.buildVersion value
# solely to create a second, still-readable Go metadata version without a
# network download.

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source "$repo_root/scripts/lib/common.sh"
source "$repo_root/scripts/security/scan_released.sh"

command -v go >/dev/null 2>&1 || {
  echo "scan-released tests: go is required" >&2
  exit 1
}
command -v tar >/dev/null 2>&1 || {
  echo "scan-released tests: tar is required" >&2
  exit 1
}

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/src"
cat >"$work/src/main.go" <<'GO'
package main

func main() {}
GO

go_build_version=$(go version | awk '{print $3}')
[[ "$go_build_version" =~ ^go[0-9]+\.[0-9]+(\.[0-9]+)?([[:alnum:]_.+-]*)?$ ]] || {
  echo "scan-released tests: unable to parse local Go version: $go_build_version" >&2
  exit 1
}

build_archive() {
  local dist=$1 arch=$2 override=${3:-}
  local target goos goarch package_arch binary stage
  for target in "${ECS_TARGETS[@]}"; do
    read -r goos goarch package_arch <<<"$target"
    [[ "$package_arch" == "$arch" ]] || continue

    binary="$work/$arch-ecs"
    stage="$work/stage-$arch"
    mkdir -p "$stage" "$dist"
    (
      cd "$work/src"
      export GO111MODULE=off GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0
      if [[ "$goarch" == "arm" ]]; then
        export GOARM=7
      fi
      if [[ -n "$override" ]]; then
        go build -trimpath -ldflags "-X=runtime.buildVersion=$override" -o "$binary"
      else
        go build -trimpath -o "$binary"
      fi
    )
    cp -- "$binary" "$stage/ecs"
    tar -czf "$dist/ecs_linux_${arch}.tar.gz" -C "$stage" ecs
    return 0
  done
  echo "scan-released tests: unknown architecture: $arch" >&2
  return 1
}

copy_dist() {
  local from=$1 to=$2
  mkdir -p "$to"
  cp -- "$from"/ecs_linux_*.tar.gz "$to/"
}

run_verify() {
  local dist=$1 extracted=$2
  bash -c 'source "$1"; verify_release_build_go "$2" "$3"' \
    bash "$repo_root/scripts/security/scan_released.sh" "$dist" "$extracted"
}

consistent_dist="$work/consistent-dist"
for arch in "${ECS_ARCHES[@]}"; do
  build_archive "$consistent_dist" "$arch"
done

consistent_output=$(run_verify "$consistent_dist" "$work/consistent-extracted" 2>"$work/consistent.log")
[[ "$consistent_output" == "$go_build_version" ]] || {
  echo "FAIL seven matching architectures: got '$consistent_output', want '$go_build_version'" >&2
  cat "$work/consistent.log" >&2
  exit 1
}
echo "ok   seven architectures with identical real Go metadata"

mismatch_dist="$work/mismatch-dist"
copy_dist "$consistent_dist" "$mismatch_dist"
build_archive "$mismatch_dist" "${ECS_ARCHES[0]}" go99.99.99
if run_verify "$mismatch_dist" "$work/mismatch-extracted" >"$work/mismatch.out" 2>&1; then
  echo "FAIL mismatched Go metadata was accepted" >&2
  exit 1
fi
grep -F 'Go 工具链不一致' "$work/mismatch.out" >/dev/null || {
  echo "FAIL mismatch did not report the architecture/version discrepancy" >&2
  cat "$work/mismatch.out" >&2
  exit 1
}
echo "ok   one architecture with a different real Go metadata version is rejected"

missing_dist="$work/missing-dist"
copy_dist "$consistent_dist" "$missing_dist"
missing_arch="${ECS_ARCHES[1]}"
missing_stage="$work/missing-stage"
mkdir -p "$missing_stage"
cp -- /bin/true "$missing_stage/ecs"
tar -czf "$missing_dist/ecs_linux_${missing_arch}.tar.gz" -C "$missing_stage" ecs
if run_verify "$missing_dist" "$work/missing-extracted" >"$work/missing.out" 2>&1; then
  echo "FAIL missing Go metadata was accepted" >&2
  exit 1
fi
grep -F 'no valid Go build metadata' "$work/missing.out" >/dev/null || {
  echo "FAIL missing metadata did not fail at the go version -m gate" >&2
  cat "$work/missing.out" >&2
  exit 1
}
echo "ok   missing Go metadata is rejected"

workflow="$repo_root/.github/workflows/security.yml"
if grep -F -- '--current "$(go env GOVERSION)"' "$workflow" >/dev/null; then
  echo "FAIL security workflow still uses the runner Go version for triage" >&2
  exit 1
fi
grep -F -- '--current "$RELEASE_BUILD_GO"' "$workflow" >/dev/null || {
  echo "FAIL security workflow does not pass Release metadata to triage" >&2
  exit 1
}
grep -F -- 'release_build_go: ${{ steps.scan.outputs.release_build_go }}' "$workflow" >/dev/null || {
  echo "FAIL security workflow does not expose the Release build Go output" >&2
  exit 1
}
echo "ok   workflow triage uses the Release artifact build Go output"

echo "scan-released tests passed"

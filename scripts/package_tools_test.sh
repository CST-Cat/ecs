#!/usr/bin/env bash
set -euo pipefail

# Build a small, entirely local stage for every locked architecture. The
# binaries are shell fixtures; package.sh only needs to copy them, while the
# concrete manifest hashes let this test detect a staged-binary mutation.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repo_root/scripts/lib/common.sh"

test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-package-test.XXXXXX")
package_repo="$test_root/repo"
stage_root="$test_root/tools-stage"
dist_root="$package_repo/dist"

fail() {
  echo "package tools stage tests: $*" >&2
  exit 1
}

trap 'rm -rf -- "$test_root"' EXIT

# package.sh owns its repository dist directory, so run it from an isolated
# repository copy instead of risking pre-existing release artifacts.
mkdir -p "$package_repo" "$stage_root"
cp -a \
  "$repo_root/cmd" \
  "$repo_root/internal" \
  "$repo_root/scripts" \
  "$repo_root/tools" \
  "$repo_root/go.mod" \
  "$repo_root/LICENSE" \
  "$repo_root/NOTICE" \
  "$repo_root/README.md" \
  "$repo_root/README_EN.md" \
  "$repo_root/SECURITY.md" \
  "$repo_root/THIRD_PARTY.md" \
  "$package_repo/"

for arch in "${ECS_ARCHES[@]}"; do
  stage_dir="$stage_root/linux_$arch"
  manifest="$stage_dir/manifest.json"
  mkdir -p "$stage_dir/bin" "$stage_dir/LICENSES"
  printf '%s\n' "local package fixture for $arch" >"$stage_dir/LICENSES/fixture.txt"

  build_params=$("$package_repo/scripts/build_tools_container.sh" \
    --arch "$arch" --print-params)
  toolchain_mode=$(awk -F= '$1 == "toolchain_mode" { print $2 }' <<<"$build_params")
  smoke_runner=$(awk -F= '$1 == "target_runner" { print $2 }' <<<"$build_params")
  npb_smoke_class=$(awk -F= '$1 == "npb_ci_smoke_class" { print $2 }' <<<"$build_params")
  [[ -n "$toolchain_mode" && -n "$smoke_runner" && -n "$npb_smoke_class" ]] ||
    fail "could not resolve build parameters for $arch"

  jq --arg architecture "$arch" \
    --arg toolchain_mode "$toolchain_mode" \
    --arg smoke_runner "$smoke_runner" \
    --arg npb_smoke_class "$npb_smoke_class" \
    '
      .architecture = $architecture
      | .build.toolchain_mode = $toolchain_mode
      | .build.smoke_runner = $smoke_runner
      | .tools |= map(
          .architecture = $architecture
          | if .name == "npb-ep" or .name == "npb-ft"
            then .parameters.ci_smoke_class = $npb_smoke_class
            else .
            end
        )
    ' "$package_repo/tools/manifest.example.json" >"$manifest"

  for tool in "${ECS_TOOL_NAMES[@]}"; do
    printf '%s\n' '#!/bin/sh' "# package fixture: $arch/$tool" 'exit 0' >"$stage_dir/bin/$tool"
    chmod 0755 "$stage_dir/bin/$tool"
  done
done

package_args=(
  COMMIT=phase6-package-fixture
  SOURCE_DATE_EPOCH=946684800
  BUILD_DATE=2000-01-01T00:00:00Z
)
if ! package_output=$(env "${package_args[@]}" bash "$package_repo/scripts/package.sh" \
  phase6-test --tools-stage "$stage_root" 2>&1); then
  fail "package.sh happy path failed:\n$package_output"
fi

archive="$dist_root/ecs-tools_linux_amd64.tar.gz"
[[ -s "$archive" ]] || fail "package.sh did not write the amd64 tools archive"
archive_listing=$(tar -tzf "$archive") || fail "could not inspect the tools archive"
for member in bin LICENSES LICENSE NOTICE manifest.json; do
  grep -F -x "$member/" <<<"$archive_listing" >/dev/null 2>&1 ||
    grep -F -x "$member" <<<"$archive_listing" >/dev/null 2>&1 ||
    fail "tools archive omitted $member"
done
for tool in "${ECS_TOOL_NAMES[@]}"; do
  grep -F -x "bin/$tool" <<<"$archive_listing" >/dev/null ||
    fail "tools archive omitted bin/$tool"
done
if grep -E '(^|/)share(/|$)|ecs-silesia-v1[.]corpus' <<<"$archive_listing" >/dev/null; then
  fail "tools archive unexpectedly contains corpus or share data"
fi

echo "package tools stage tests passed"

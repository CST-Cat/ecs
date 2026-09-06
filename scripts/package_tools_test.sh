#!/usr/bin/env bash
set -euo pipefail

# Build a small, entirely local stage for every locked architecture. The
# Binaries are shell fixtures; package.sh only needs to copy them. The manifest
# fields are generated through the same build-parameter path used by release.
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
mkdir -p \
  "$package_repo/cmd" \
  "$package_repo/internal" \
  "$package_repo/scripts/lib" \
  "$package_repo/tools" \
  "$stage_root" \
  "$dist_root"
cp -a \
  "$repo_root/cmd/tools-manifest-check" \
  "$package_repo/cmd/"
cp -a \
  "$repo_root/internal/toolsmanifest" \
  "$package_repo/internal/"
cp -a \
  "$repo_root/scripts/package.sh" \
  "$repo_root/scripts/verify_tools_stage.sh" \
  "$repo_root/scripts/build_tools_container.sh" \
  "$package_repo/scripts/"
cp -a \
  "$repo_root/scripts/lib/common.sh" \
  "$package_repo/scripts/lib/"
cp -a \
  "$repo_root/tools/manifest.example.json" \
  "$repo_root/tools/lock.json" \
  "$package_repo/tools/"
cp -a \
  "$repo_root/go.mod" \
  "$repo_root/LICENSE" \
  "$repo_root/NOTICE" \
  "$repo_root/README.md" \
  "$repo_root/README_EN.md" \
  "$repo_root/SECURITY.md" \
  "$repo_root/THIRD_PARTY.md" \
  "$package_repo/"

# These are prebuilt main-program fixtures. They are deliberately labeled shell
# files: this regression checks packaging layout and ownership, never adapter
# execution or measurement validity.
for arch in "${ECS_ARCHES[@]}"; do
  binary="$dist_root/ecs_linux_$arch"
  printf '%s\n' '#!/bin/sh' "# prebuilt package fixture: linux/$arch" 'exit 0' >"$binary"
  chmod 0755 "$binary"
done

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
  SOURCE_DATE_EPOCH=946684800
)

# All inputs must be checked before package.sh removes old archive outputs.
stale_archive="$dist_root/ecs_linux_stale.tar.gz"
printf '%s\n' stale >"$stale_archive"
missing_arch=${ECS_ARCHES[0]}
missing_binary="$dist_root/ecs_linux_$missing_arch"
rm "$missing_binary"
if env "${package_args[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$dist_root" --tools-stage "$stage_root" \
  >"$test_root/missing.stdout" 2>"$test_root/missing.stderr"; then
  fail "package.sh accepted a missing prebuilt binary"
fi
[[ -s "$stale_archive" ]] || fail "input validation removed an old archive before failing"
printf '%s\n' '#!/bin/sh' "# prebuilt package fixture: linux/$missing_arch" 'exit 0' >"$missing_binary"
chmod 0755 "$missing_binary"

if ! package_output=$(env "${package_args[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$dist_root" --tools-stage "$stage_root" 2>&1); then
  fail "package.sh happy path failed:\n$package_output"
fi

for arch in "${ECS_ARCHES[@]}"; do
  main_archive="$dist_root/ecs_linux_$arch.tar.gz"
  tools_archive="$dist_root/ecs-tools_linux_$arch.tar.gz"
  [[ -s "$main_archive" ]] || fail "package.sh did not write the $arch main archive"
  [[ -s "$tools_archive" ]] || fail "package.sh did not write the $arch tools archive"
  main_listing=$(tar -tzf "$main_archive") || fail "could not inspect the $arch main archive"
  grep -F -x 'ecs' <<<"$main_listing" >/dev/null || fail "$arch main archive omitted ecs"
  for member in LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md; do
    grep -F -x "$member" <<<"$main_listing" >/dev/null ||
      fail "$arch main archive omitted $member"
  done
  marker=$(tar -xOf "$main_archive" ecs) || fail "could not read the $arch main fixture"
  grep -F -x "# prebuilt package fixture: linux/$arch" <<<"$marker" >/dev/null ||
    fail "$arch main archive did not preserve its labeled fixture"

  tools_listing=$(tar -tzf "$tools_archive") || fail "could not inspect the $arch tools archive"
  for member in bin LICENSES LICENSE NOTICE manifest.json; do
    grep -F -x "$member/" <<<"$tools_listing" >/dev/null 2>&1 ||
      grep -F -x "$member" <<<"$tools_listing" >/dev/null 2>&1 ||
      fail "$arch tools archive omitted $member"
  done
  for tool in "${ECS_TOOL_NAMES[@]}"; do
    grep -F -x "bin/$tool" <<<"$tools_listing" >/dev/null ||
      fail "$arch tools archive omitted bin/$tool"
  done
  if grep -E '(^|/)share(/|$)|ecs-silesia-v1[.]corpus' <<<"$tools_listing" >/dev/null; then
    fail "$arch tools archive unexpectedly contains corpus or share data"
  fi

  [[ -x "$dist_root/ecs_linux_$arch" ]] ||
    fail "$arch prebuilt input was removed while packaging"
done

asset_count=$(( ${#ECS_ARCHES[@]} * 2 ))
mapfile -t assets < <(find "$dist_root" -mindepth 1 -maxdepth 1 -type f \
  \( -name 'ecs_*.tar.gz' -o -name 'ecs-tools_*.tar.gz' \) -printf '%f\n' | sort)
[[ "${#assets[@]}" -eq "$asset_count" ]] ||
  fail "dist contains ${#assets[@]} archives, want $asset_count"
[[ "$(wc -l <"$dist_root/checksums.txt")" -eq "$asset_count" ]] ||
  fail "checksums.txt does not cover all $asset_count archives"
for asset in "${assets[@]}"; do
  grep -F -x "${asset}" <(sed -E 's/^[0-9a-f]{64}  //' "$dist_root/checksums.txt") >/dev/null ||
    fail "checksums.txt omitted $asset"
done
(cd "$dist_root" && sha256sum -c checksums.txt >/dev/null) ||
  fail "checksums.txt does not validate all archives"

echo "package tools stage tests passed"

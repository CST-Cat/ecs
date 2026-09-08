#!/usr/bin/env bash
set -euo pipefail

# Build local fixtures for the two package layouts. The shell files are never
# run as benchmark adapters; this regression only inspects archive contents.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repo_root/scripts/lib/common.sh"

test_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-package-test.XXXXXX")
package_repo="$test_root/repo"
binary_root="$package_repo/ecs-binaries"
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
  "$package_repo/scripts/lib" \
  "$package_repo/tools" \
  "$binary_root" \
  "$stage_root" \
  "$dist_root"
cp -a \
  "$repo_root/scripts/package.sh" \
  "$package_repo/scripts/"
cp -a \
  "$repo_root/scripts/lib/common.sh" \
  "$package_repo/scripts/lib/"
cp -a \
  "$repo_root/tools/lock.json" \
  "$package_repo/tools/"
cp -a \
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
  binary="$binary_root/ecs_linux_$arch"
  printf '%s\n' '#!/bin/sh' "# prebuilt package fixture: linux/$arch" 'exit 0' >"$binary"
  chmod 0755 "$binary"
done

for arch in "${ECS_ARCHES[@]}"; do
  stage_dir="$stage_root/linux_$arch"
  mkdir -p \
    "$stage_dir/bin" \
    "$stage_dir/LICENSES" \
    "$stage_dir/share/ecs/corpus"
  printf '%s\n' "local package fixture for $arch" >"$stage_dir/LICENSES/fixture.txt"
  printf '%s\n' '{}' >"$stage_dir/manifest.json"
  printf '%s\n' "package corpus fixture" >"$stage_dir/share/ecs/corpus/$ECS_CORPUS_NAME"

  for tool in "${ECS_TOOL_NAMES[@]}"; do
    printf '%s\n' '#!/bin/sh' "# package fixture: $arch/$tool" 'exit 0' >"$stage_dir/bin/$tool"
    chmod 0755 "$stage_dir/bin/$tool"
  done
done

package_env=(
  SOURCE_DATE_EPOCH=946684800
)

assert_archives() {
  local prefix=$1 expected_count=$2
  local -a assets=()
  mapfile -t assets < <(find "$dist_root" -mindepth 1 -maxdepth 1 -type f \
    -name "${prefix}*.tar.gz" -printf '%f\n' | sort)
  [[ "${#assets[@]}" -eq "$expected_count" ]] ||
    fail "dist contains ${#assets[@]} ${prefix} archives, want $expected_count"
}

assert_checksums() {
  local expected_count=$1
  [[ -s "$dist_root/checksums.txt" ]] || fail "package.sh did not write checksums.txt"
  [[ "$(wc -l <"$dist_root/checksums.txt")" -eq "$expected_count" ]] ||
    fail "checksums.txt has $(wc -l <"$dist_root/checksums.txt") lines, want $expected_count"
}

# Case A: ECS release package owns only the seven ECS archives.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$binary_root" 2>&1); then
  fail "ECS package invocation failed:\n$package_output"
fi
assert_archives ecs_linux_ "${#ECS_ARCHES[@]}"
assert_checksums "${#ECS_ARCHES[@]}"
[[ -z "$(find "$dist_root" -mindepth 1 -maxdepth 1 -type f -name 'ecs-tools_*.tar.gz' -print -quit)" ]] ||
  fail "ECS package unexpectedly wrote tools archives"
[[ ! -e "$dist_root/$ECS_CORPUS_ARCHIVE" ]] ||
  fail "ECS package unexpectedly wrote a corpus archive"

for arch in "${ECS_ARCHES[@]}"; do
  archive="$dist_root/ecs_linux_$arch.tar.gz"
  [[ -s "$archive" ]] || fail "package.sh did not write the $arch ECS archive"
  listing=$(tar -tzf "$archive") || fail "could not inspect the $arch ECS archive"
  for member in ecs LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md; do
    grep -F -x "$member" <<<"$listing" >/dev/null ||
      fail "$arch ECS archive omitted $member"
  done
  marker=$(tar -xOf "$archive" ecs) || fail "could not read the $arch ECS fixture"
  grep -F -x "# prebuilt package fixture: linux/$arch" <<<"$marker" >/dev/null ||
    fail "$arch ECS archive did not preserve its labeled fixture"
done

# Case B: Bundle package owns only the seven tools archives and one corpus.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --tools-stage "$stage_root" 2>&1); then
  fail "Bundle package invocation failed:\n$package_output"
fi
assert_archives ecs-tools_linux_ "${#ECS_ARCHES[@]}"
assert_checksums "$((${#ECS_ARCHES[@]} + 1))"
[[ -z "$(find "$dist_root" -mindepth 1 -maxdepth 1 -type f -name 'ecs_linux_*.tar.gz' -print -quit)" ]] ||
  fail "Bundle package unexpectedly wrote ECS archives"
corpus_archive="$dist_root/$ECS_CORPUS_ARCHIVE"
[[ -s "$corpus_archive" ]] || fail "Bundle package did not write the corpus archive"
corpus_listing=$(tar -tzf "$corpus_archive") || fail "could not inspect the corpus archive"
[[ "$corpus_listing" == "$ECS_CORPUS_NAME" ]] ||
  fail "corpus archive contents = $corpus_listing, want $ECS_CORPUS_NAME"

for arch in "${ECS_ARCHES[@]}"; do
  archive="$dist_root/ecs-tools_linux_$arch.tar.gz"
  [[ -s "$archive" ]] || fail "package.sh did not write the $arch tools archive"
  listing=$(tar -tzf "$archive") || fail "could not inspect the $arch tools archive"
  for member in bin LICENSES LICENSE NOTICE manifest.json; do
    grep -F -x "$member/" <<<"$listing" >/dev/null 2>&1 ||
      grep -F -x "$member" <<<"$listing" >/dev/null 2>&1 ||
      fail "$arch tools archive omitted $member"
  done
  for tool in "${ECS_TOOL_NAMES[@]}"; do
    grep -F -x "bin/$tool" <<<"$listing" >/dev/null ||
      fail "$arch tools archive omitted bin/$tool"
  done
  if grep -E '(^|/)share(/|$)|ecs-silesia-v1[.]corpus' <<<"$listing" >/dev/null; then
    fail "$arch tools archive unexpectedly contains corpus or share data"
  fi
done

echo "package tools stage tests passed"

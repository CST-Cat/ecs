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
for target_record in "${ECS_TARGETS[@]}"; do
  read -r target goos _goarch arch <<<"$target_record"
  binary="$binary_root/ecs_$target"
  printf '%s\n' '#!/bin/sh' "# prebuilt package fixture: $target" 'exit 0' >"$binary"
  chmod 0755 "$binary"
done

for target_record in "${ECS_TARGETS[@]}"; do
  read -r target goos _goarch arch <<<"$target_record"
  stage_dir="$stage_root/$target"
  mkdir -p \
    "$stage_dir/bin" \
    "$stage_dir/LICENSES" \
    "$stage_dir/share/ecs/corpus"
  printf '%s\n' "local package fixture for $target" >"$stage_dir/LICENSES/fixture.txt"
  printf '%s\n' '{}' >"$stage_dir/manifest.json"
  if [[ "$goos" == linux ]]; then
    printf '%s\n' "package corpus fixture" >"$stage_dir/share/ecs/corpus/$ECS_CORPUS_NAME"
  fi

  mapfile -t target_tools < <(ecs_target_tool_names "$target")
  for tool in "${target_tools[@]}"; do
    printf '%s\n' '#!/bin/sh' "# package fixture: $target/$tool" 'exit 0' >"$stage_dir/bin/$tool"
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

# Case A: the existing default release set remains the seven Linux archives.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$binary_root" 2>&1); then
  fail "ECS package invocation failed:\n$package_output"
fi
assert_archives ecs_ "${#ECS_LINUX_TARGETS[@]}"
assert_checksums "${#ECS_LINUX_TARGETS[@]}"
[[ -z "$(find "$dist_root" -mindepth 1 -maxdepth 1 -type f -name 'ecs-tools_*.tar.gz' -print -quit)" ]] ||
  fail "ECS package unexpectedly wrote tools archives"
[[ ! -e "$dist_root/$ECS_CORPUS_ARCHIVE" ]] ||
  fail "ECS package unexpectedly wrote a corpus archive"

for target_record in "${ECS_LINUX_TARGETS[@]}"; do
  read -r target goos _goarch arch <<<"$target_record"
  archive="$dist_root/ecs_$target.tar.gz"
  [[ -s "$archive" ]] || fail "package.sh did not write the $target ECS archive"
  listing=$(tar -tzf "$archive") || fail "could not inspect the $target ECS archive"
  for member in ecs LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md; do
    grep -F -x "$member" <<<"$listing" >/dev/null ||
      fail "$arch ECS archive omitted $member"
  done
  marker=$(tar -xOf "$archive" ecs) || fail "could not read the $target ECS fixture"
  grep -F -x "# prebuilt package fixture: $target" <<<"$marker" >/dev/null ||
    fail "$target ECS archive did not preserve its labeled fixture"
done

# Case B: the existing default bundle owns seven Linux tools archives and one
# corpus until the bundle-release phase promotes the validated FreeBSD stages.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --tools-stage "$stage_root" 2>&1); then
  fail "Bundle package invocation failed:\n$package_output"
fi
assert_archives ecs-tools_ "${#ECS_LINUX_TARGETS[@]}"
assert_checksums "$((${#ECS_LINUX_TARGETS[@]} + 1))"
[[ -z "$(find "$dist_root" -mindepth 1 -maxdepth 1 -type f -name 'ecs_linux_*.tar.gz' -print -quit)" ]] ||
  fail "Bundle package unexpectedly wrote ECS archives"
corpus_archive="$dist_root/$ECS_CORPUS_ARCHIVE"
[[ -s "$corpus_archive" ]] || fail "Bundle package did not write the corpus archive"
corpus_listing=$(tar -tzf "$corpus_archive") || fail "could not inspect the corpus archive"
[[ "$corpus_listing" == "$ECS_CORPUS_NAME" ]] ||
  fail "corpus archive contents = $corpus_listing, want $ECS_CORPUS_NAME"

for target_record in "${ECS_LINUX_TARGETS[@]}"; do
  read -r target goos _goarch arch <<<"$target_record"
  archive="$dist_root/ecs-tools_$target.tar.gz"
  [[ -s "$archive" ]] || fail "package.sh did not write the $target tools archive"
  listing=$(tar -tzf "$archive") || fail "could not inspect the $target tools archive"
  for member in bin LICENSES LICENSE NOTICE manifest.json; do
    grep -F -x "$member/" <<<"$listing" >/dev/null 2>&1 ||
      grep -F -x "$member" <<<"$listing" >/dev/null 2>&1 ||
      fail "$target tools archive omitted $member"
  done
  mapfile -t target_tools < <(ecs_target_tool_names "$target")
  for tool in "${target_tools[@]}"; do
    grep -F -x "bin/$tool" <<<"$listing" >/dev/null ||
      fail "$target tools archive omitted bin/$tool"
  done
  if [[ "$goos" == freebsd ]]; then
    if grep -E -x 'bin/(ping|nexttrace-tiny)' <<<"$listing" >/dev/null; then
      fail "$target tools archive unexpectedly contains a base-system tool"
    fi
  fi
  if grep -E '(^|/)share(/|$)|ecs-silesia-v1[.]corpus' <<<"$listing" >/dev/null; then
    fail "$target tools archive unexpectedly contains corpus or share data"
  fi
done

# Case C: a native FreeBSD build can package one unambiguous target without
# fabricating stages for the other platform targets.  The selector also keeps
# the shared corpus (which has its own platform-independent archive) out of a
# FreeBSD-only tools packaging invocation.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --tools-stage "$stage_root" --target freebsd_amd64 2>&1); then
  fail "target-selected bundle invocation failed:\n$package_output"
fi
assert_archives ecs-tools_ 1
assert_checksums 1
[[ ! -e "$dist_root/$ECS_CORPUS_ARCHIVE" ]] ||
  fail "FreeBSD target-selected bundle unexpectedly wrote a corpus archive"
[[ -s "$dist_root/ecs-tools_freebsd_amd64.tar.gz" ]] ||
  fail "target-selected bundle did not write the FreeBSD archive"
freebsd_listing=$(tar -tzf "$dist_root/ecs-tools_freebsd_amd64.tar.gz") ||
  fail "could not inspect the target-selected FreeBSD tools archive"
mapfile -t freebsd_tools < <(ecs_target_tool_names freebsd_amd64)
for tool in "${freebsd_tools[@]}"; do
  grep -F -x "bin/$tool" <<<"$freebsd_listing" >/dev/null ||
    fail "target-selected FreeBSD tools archive omitted bin/$tool"
done
if grep -E -x 'bin/(ping|nexttrace-tiny)' <<<"$freebsd_listing" >/dev/null; then
  fail "target-selected FreeBSD tools archive contains a base-system tool"
fi

# The same selector applies to main-program packaging, preserving the
# target-identity distinction for the two platforms that share GOARCH=amd64.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$binary_root" --target freebsd_arm64 2>&1); then
  fail "target-selected ECS invocation failed:\n$package_output"
fi
assert_archives ecs_ 1
assert_checksums 1
[[ -s "$dist_root/ecs_freebsd_arm64.tar.gz" ]] ||
  fail "target-selected ECS package did not write the FreeBSD archive"

# Case D: --all-targets is how release and bundle wiring promote every platform
# target, including the two FreeBSD stages, in one invocation. The default stays
# Linux-only, so this is the path that emits the FreeBSD tool archives.
if ! package_output=$(env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --tools-stage "$stage_root" --all-targets 2>&1); then
  fail "all-targets bundle invocation failed:\n$package_output"
fi
assert_archives ecs-tools_ "${#ECS_TARGETS[@]}"
assert_checksums "$((${#ECS_TARGETS[@]} + 1))"
[[ -z "$(find "$dist_root" -mindepth 1 -maxdepth 1 -type f -name 'ecs_linux_*.tar.gz' -print -quit)" ]] ||
  fail "all-targets bundle unexpectedly wrote ECS archives"
[[ -s "$dist_root/$ECS_CORPUS_ARCHIVE" ]] ||
  fail "all-targets bundle did not write the corpus archive"
for target_record in "${ECS_FREEBSD_TARGETS[@]}"; do
  read -r target goos _goarch arch <<<"$target_record"
  archive="$dist_root/ecs-tools_$target.tar.gz"
  [[ -s "$archive" ]] || fail "all-targets bundle did not write the $target tools archive"
  listing=$(tar -tzf "$archive") || fail "could not inspect the $target tools archive"
  if grep -E -x 'bin/(ping|nexttrace-tiny)' <<<"$listing" >/dev/null; then
    fail "$target tools archive unexpectedly contains a base-system tool"
  fi
  mapfile -t target_tools < <(ecs_target_tool_names "$target")
  for tool in "${target_tools[@]}"; do
    grep -F -x "bin/$tool" <<<"$listing" >/dev/null ||
      fail "$target tools archive omitted bin/$tool"
  done
done

# --all-targets and --target answer the same question; asking both is ambiguous
# input rather than a union, and must not silently pick one interpretation.
if env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$binary_root" --all-targets --target freebsd_amd64 \
  >"$test_root/all-targets-conflict.out" 2>&1; then
  fail "package.sh accepted --all-targets together with --target"
fi
grep -F -- '--all-targets cannot be combined with --target' "$test_root/all-targets-conflict.out" >/dev/null ||
  fail "package.sh did not diagnose --all-targets together with --target"

# Duplicate selectors are ambiguous input and must not silently duplicate a
# checksum entry for one overwritten archive.
if env "${package_env[@]}" bash "$package_repo/scripts/package.sh" \
  --binaries-dir "$binary_root" --target freebsd_amd64 --target freebsd_amd64 \
  >"$test_root/duplicate-selector.out" 2>&1; then
  fail "package.sh accepted a duplicate target selector"
fi
grep -F 'target may only be supplied once: freebsd_amd64' "$test_root/duplicate-selector.out" >/dev/null ||
  fail "package.sh did not diagnose the duplicate target selector"

echo "package tools stage tests passed"

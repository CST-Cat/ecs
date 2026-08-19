#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
package_script="$repo_root/scripts/package.sh"
go_command="${GO:-go}"
stage_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-test.XXXXXX")
log_file="$stage_root/package.log"
package_tmp="$stage_root/package-tmp"
mkdir -p "$package_tmp"
trap 'rm -rf -- "$stage_root"' EXIT

source "$repo_root/scripts/lib/common.sh"
architectures=("${ECS_ARCHES[@]}")
tools=("${ECS_TOOL_NAMES[@]}")

write_fixture() {
  local path=$1
  printf '%s\n' '#!/bin/sh' 'printf "%s\\n" fixture' >"$path"
  chmod 0755 "$path"
}

write_manifest_fixture() {
  local arch=$1
  local path=$2
  sed "s/\"architecture\": \"amd64\"/\"architecture\": \"$arch\"/g" \
    "$repo_root/tools/manifest.example.json" >"$path"
}

for arch in "${architectures[@]}"; do
  stage_dir="$stage_root/linux_${arch}"
  mkdir -p "$stage_dir/bin" "$stage_dir/LICENSES"
  cp "$repo_root/LICENSE" "$stage_dir/LICENSES/project-license.txt"
  write_manifest_fixture "$arch" "$stage_dir/manifest.json"
  for tool in "${tools[@]}"; do
    write_fixture "$stage_dir/bin/$tool"
  done
  # This is an executable sentinel; package_tools must copy only the ten
  # declared tools and must never include an Ookla/speedtest path.
  write_fixture "$stage_dir/bin/ookla"
done

if ! GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  cat "$log_file" >&2
  exit 1
fi

for arch in "${architectures[@]}"; do
  archive="$repo_root/dist/ecs-tools_linux_${arch}.tar.gz"
  [[ -s "$archive" ]] || {
    echo "missing or empty archive: $archive" >&2
    exit 1
  }
  listing=$(tar -tzf "$archive")
  [[ "$(printf '%s\n' "$listing" | wc -l)" -eq $(( ${#tools[@]} + 6 )) ]] || {
    echo "archive $archive has an unexpected number of entries" >&2
    exit 1
  }
  for entry in bin/ "${tools[@]/#/bin/}" LICENSES/ LICENSES/project-license.txt LICENSE NOTICE manifest.json; do
    printf '%s\n' "$listing" | grep -F -x "$entry" >/dev/null || {
      echo "archive $archive is missing $entry" >&2
      exit 1
    }
  done
  if printf '%s\n' "$listing" | grep -E '(^|/)share/|ecs-silesia-v1\.corpus$' >/dev/null; then
    echo "archive $archive contains the Silesia corpus or a share directory" >&2
    exit 1
  fi
  if printf '%s\n' "$listing" | grep -E -i '(^|/)(ookla|speedtest)(/|$)' >/dev/null; then
    echo "archive $archive contains an Ookla path" >&2
    exit 1
  fi
done

if find "$package_tmp" -mindepth 1 -print -quit | grep -q .; then
  echo "package.sh left temporary staging files behind" >&2
  find "$package_tmp" -mindepth 1 -print >&2
  exit 1
fi

archive_count=$(find "$repo_root/dist" -maxdepth 1 -type f -name 'ecs-tools_linux_*.tar.gz' | wc -l)
[[ "$archive_count" -eq "${#architectures[@]}" ]] || {
  echo "expected ${#architectures[@]} tool archives, found $archive_count" >&2
  exit 1
}

rm "$stage_root/linux_arm64/manifest.json"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "missing manifest unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'missing or empty manifest for arm64' "$log_file" >/dev/null || {
  echo "missing-manifest error did not identify the architecture" >&2
  cat "$log_file" >&2
  exit 1
}

write_manifest_fixture arm64 "$stage_root/linux_arm64/manifest.json"
sed -i '0,/"architecture": "arm64"/s//"architecture": "bad"/' \
  "$stage_root/linux_arm64/manifest.json"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "bad manifest unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'invalid manifest for arm64' "$log_file" >/dev/null || {
  echo "bad-manifest error did not identify the architecture" >&2
  cat "$log_file" >&2
  exit 1
}

write_manifest_fixture arm64 "$stage_root/linux_arm64/manifest.json"
rm "$stage_root/linux_arm64/bin/fio"
if GO="$go_command" TMPDIR="$package_tmp" "$package_script" test --tools-stage "$stage_root" >"$log_file" 2>&1; then
  echo "missing tool unexpectedly succeeded" >&2
  exit 1
fi
grep -F 'missing fio for arm64' "$log_file" >/dev/null || {
  echo "missing-tool error did not identify the architecture and tool" >&2
  cat "$log_file" >&2
  exit 1
}

echo "package tools layout tests passed"

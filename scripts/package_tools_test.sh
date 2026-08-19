#!/usr/bin/env bash
set -euo pipefail

# 一个 amd64 stage 的最小 happy path；工具本身只是假可执行文件。
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
stage_root=$(mktemp -d "${TMPDIR:-/tmp}/ecs-tools-stage-test.XXXXXX")
trap 'rm -rf -- "$stage_root"' EXIT

source "$repo_root/scripts/lib/common.sh"
stage_dir="$stage_root/linux_amd64"
mkdir -p "$stage_dir/bin" "$stage_dir/LICENSES"
cp "$repo_root/tools/manifest.example.json" "$stage_dir/manifest.json"

for tool in "${ECS_TOOL_NAMES[@]}"; do
  printf '%s\n' '#!/bin/sh' 'exit 0' >"$stage_dir/bin/$tool"
  chmod 0755 "$stage_dir/bin/$tool"
done

bash "$repo_root/scripts/verify_tools_stage.sh" \
  --arch amd64 --stage-root "$stage_root" --keep-corpus >/dev/null

rm -f "$stage_dir/bin/ping"
if failure=$(bash "$repo_root/scripts/verify_tools_stage.sh" \
  --arch amd64 --stage-root "$stage_root" --keep-corpus 2>&1); then
  echo "missing executable unexpectedly passed" >&2
  exit 1
fi
grep -F "missing an executable ping" <<<"$failure" >/dev/null || {
  echo "missing executable diagnostic was lost: $failure" >&2
  exit 1
}

echo "package tools stage tests passed"

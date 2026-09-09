#!/usr/bin/env bash
set -euo pipefail

if [[ "$(id -u)" -eq 0 ]]; then
  echo "freebsd-tools: build and smoke tests must run as an ordinary user" >&2
  exit 1
fi
if [[ "$(uname -s)" != FreeBSD ]]; then
  echo "freebsd-tools: expected FreeBSD" >&2
  exit 1
fi
if [[ -z "${ECS_FREEBSD_TARGET:-}" ]]; then
  echo "freebsd-tools: ECS_FREEBSD_TARGET is required" >&2
  exit 1
fi

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
stage_root=/tmp/ecs-freebsd-tools-stage
work_root=/tmp/ecs-freebsd-tools-work
rm -rf -- "$stage_root" "$work_root"

cd "$repo_root"
ECS_TOOLS_WORK="$work_root" JOBS=2 \
  bash scripts/build_tools_freebsd.sh \
    --target "$ECS_FREEBSD_TARGET" \
    --stage-root "$stage_root"
bash scripts/verify_tools_stage.sh \
  --target "$ECS_FREEBSD_TARGET" \
  --stage-root "$stage_root"

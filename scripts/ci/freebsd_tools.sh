#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_tools.sh [all|sources|sysbench|zstd|npb|openssl|stream|fio|iperf3|manifest|verify]
USAGE
}

die() {
  echo "freebsd-tools: $*" >&2
  exit 1
}

if [[ "$(id -u)" -eq 0 ]]; then
  die 'build and smoke tests must run as an ordinary user'
fi
[[ "$(uname -s)" == FreeBSD ]] || die 'expected FreeBSD'

if [[ -z "${ECS_FREEBSD_TARGET:-}" ]]; then
  case "$(uname -m)" in
    amd64 | x86_64) ECS_FREEBSD_TARGET=freebsd_amd64 ;;
    arm64 | aarch64) ECS_FREEBSD_TARGET=freebsd_arm64 ;;
    *) die "unsupported FreeBSD architecture: $(uname -m)" ;;
  esac
fi
export ECS_FREEBSD_TARGET

phase=${1:-all}
case "$phase" in
  all | sources | sysbench | zstd | npb | openssl | stream | fio | iperf3 | manifest | verify) ;;
  *) usage; die "unsupported phase: $phase" ;;
esac

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
stage_root=/tmp/ecs-freebsd-tools-stage
work_root=/tmp/ecs-freebsd-tools-work
cd "$repo_root"

run_builder() {
  ECS_TOOLS_WORK="$work_root" JOBS=2 \
    bash scripts/build_tools_freebsd.sh \
      --target "$ECS_FREEBSD_TARGET" \
      --stage-root "$stage_root" \
      --phase "$1"
}

case "$phase" in
  all)
    rm -rf -- "$stage_root" "$work_root"
    ECS_TOOLS_WORK="$work_root" JOBS=2 \
      bash scripts/build_tools_freebsd.sh \
        --target "$ECS_FREEBSD_TARGET" \
        --stage-root "$stage_root"
    bash scripts/verify_tools_stage.sh \
      --target "$ECS_FREEBSD_TARGET" \
      --stage-root "$stage_root"
    ;;
  sources)
    rm -rf -- "$stage_root" "$work_root"
    run_builder sources
    ;;
  verify)
    bash scripts/verify_tools_stage.sh \
      --target "$ECS_FREEBSD_TARGET" \
      --stage-root "$stage_root"
    ;;
  *)
    run_builder "$phase"
    ;;
esac

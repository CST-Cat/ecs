#!/usr/bin/env bash
set -Eeuo pipefail

# Real FreeBSD/arm64 guest gate for the cross-built freebsd_arm64 tools stage.
# The amd64 cross builder only ELF-validates fio (qemu-user cannot set up the
# shm segment fio needs). This script is the missing functional acceptance:
# it runs on a genuine FreeBSD/aarch64 VM against the staged static binary.

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_arm64_gate.sh --stage-root DIR [--target freebsd_arm64]
USAGE
}

die() {
  echo "freebsd-arm64-gate: $*" >&2
  exit 1
}

stage_root=""
target=freebsd_arm64
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$stage_root" ]] || {
  usage
  exit 2
}
[[ "$stage_root" = /* ]] || die "stage root must be an absolute path: $stage_root"

[[ "$(uname -s)" == FreeBSD ]] || die "gate must run on FreeBSD, got $(uname -s)"
guest_arch=$(uname -m)
case "$guest_arch" in
  arm64 | aarch64) ;;
  *) die "gate must run on FreeBSD/arm64, got $guest_arch" ;;
esac
[[ "$target" == freebsd_arm64 ]] || die "gate only accepts freebsd_arm64, got $target"

stage="$stage_root/$target"
fio="$stage/bin/fio"
[[ -x "$fio" ]] || die "staged fio is missing or not executable: $fio"

for command_name in file jq dd; do
  command -v "$command_name" >/dev/null 2>&1 || die "missing gate command: $command_name"
done

echo "freebsd-arm64-gate: guest=$(uname -s)/$guest_arch target=$target"

# Identity: the staged binary must be a FreeBSD ELF, not a Linux artifact.
file "$fio"
file "$fio" | grep -Eq 'for FreeBSD|FreeBSD-style' ||
  die "staged fio is not a FreeBSD ELF"

# Version contract (same pin the native amd64 path asserts).
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
lock_file="$repo_root/tools/lock.json"
[[ -s "$lock_file" ]] || die "missing tools lock: $lock_file"
fio_version=$(jq -er '.tools[] | select(.name == "fio") | .version' "$lock_file")
[[ -n "$fio_version" ]] || die "tools lock has no fio version"

version_out=$("$fio" --version 2>&1) || die "fio --version failed"
printf '%s\n' "$version_out"
grep -Eq "^fio-${fio_version//./\\.}([[:space:]]|$)" <<<"$version_out" ||
  die "fio --version did not report $fio_version"

engines_out=$("$fio" --enghelp 2>&1) || die "fio --enghelp failed"
for required_engine in posixaio psync; do
  grep -Eiq "(^|[^[:alnum:]_])${required_engine}([^[:alnum:]_]|$)" <<<"$engines_out" ||
    die "staged fio omitted required engine: $required_engine"
done

work=/tmp/ecs-freebsd-arm64-gate
rm -rf -- "$work"
mkdir -p "$work"
dd if=/dev/zero of="$work/fio-smoke.data" bs=4096 count=2048 >/dev/null 2>&1

for requested_depth in 32 64; do
  fio_json="$work/fio-qd${requested_depth}.json"
  "$fio" --name="ecs-qd${requested_depth}" --filename="$work/fio-smoke.data" \
    --rw=read --bs=4k --size=4m --runtime=1 --time_based=1 \
    --ioengine=posixaio --iodepth="$requested_depth" --numjobs=1 --direct=1 \
    --output-format=json --output="$fio_json" ||
    die "fio posixaio QD${requested_depth} failed on the real arm64 guest"
  jq -e --argjson depth "$requested_depth" \
    '(.jobs | length == 1) and
     (.jobs[0].error == 0) and
     (.jobs[0]["job options"].ioengine == "posixaio") and
     ((.jobs[0]["job options"].iodepth | tonumber) == $depth) and
     ([.jobs[0].iodepth_level | to_entries[] | select(.key != "1") | .value] | any(. > 0))' \
    "$fio_json" >/dev/null || {
    cat "$fio_json" >&2
    die "fio posixaio QD${requested_depth} did not show effective depth > 1"
  }
  echo "freebsd-arm64-gate: posixaio QD${requested_depth} effective depth OK"
done

echo "freebsd-arm64-gate: fio functional smoke passed on real FreeBSD/arm64"

#!/usr/bin/env bash
set -euo pipefail

# Linux-hosted FreeBSD sysroot installer and static-ELF probe.
#
# Downloads a fixed FreeBSD 15.1-RELEASE base.txz, verifies SHA256, extracts
# only the headers/libraries needed for --sysroot cross compilation, then
# proves Clang+LLD can emit a static FreeBSD ELF for the requested target.
#
# This is not a tools builder. Stage 2 only proves the sysroot contract.

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

LOCK_FILE="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_sysroot.sh --target freebsd_amd64|freebsd_arm64
                                     --sysroot-dir DIR [--work-dir DIR]
       scripts/ci/freebsd_sysroot.sh --print-lock

  --target TARGET       freebsd_amd64 | freebsd_arm64
  --sysroot-dir DIR     destination sysroot root
  --work-dir DIR        download/cache scratch (default: .ci/sysroot-work)
  --print-lock          dump the resolved lock facts for TARGET
USAGE
}

die() {
  echo "freebsd-sysroot: $*" >&2
  exit 1
}

target=""
sysroot_dir=""
work_dir=""
print_lock=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --sysroot-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--sysroot-dir requires a value"
      sysroot_dir=$2
      shift 2
      ;;
    --work-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--work-dir requires a value"
      work_dir=$2
      shift 2
      ;;
    --print-lock)
      print_lock=1
      shift
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

[[ -s "$LOCK_FILE" ]] || die "missing sysroot lock: $LOCK_FILE"
[[ "$(jq -er '.schema_version' "$LOCK_FILE")" == "ecs.freebsd-sysroot.lock/v1" ]] ||
  die "unsupported sysroot lock schema"

release=$(jq -er '.freebsd_release' "$LOCK_FILE")
revision=$(jq -er '.freebsd_revision' "$LOCK_FILE")

lock_target_field() {
  local field=$1
  jq -er --arg target "$target" --arg field "$field" \
    ".targets[\$target][\$field] // empty" "$LOCK_FILE"
}

if [[ "$print_lock" -eq 1 ]]; then
  [[ -n "$target" ]] || die "--print-lock requires --target"
  case "$target" in
    freebsd_amd64 | freebsd_arm64) ;;
    *) die "unsupported target: $target" ;;
  esac
  triple=$(lock_target_field clang_target_triple)
  base_url=$(lock_target_field base_txz_url)
  base_sha=$(lock_target_field base_txz_sha256)
  elf_machine=$(lock_target_field elf_machine)
  cat <<EOF
target=$target
freebsd_release=$release
freebsd_revision=$revision
clang_target_triple=$triple
elf_machine=$elf_machine
base_txz_url=$base_url
base_txz_sha256=$base_sha
EOF
  exit 0
fi

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *)
    usage
    die "--target is required and must be freebsd_amd64 or freebsd_arm64"
    ;;
esac
[[ -n "$sysroot_dir" ]] || {
  usage
  die "--sysroot-dir is required"
}
if [[ -z "$work_dir" ]]; then
  work_dir="$ECS_REPO_ROOT/.ci/sysroot-work"
fi

triple=$(lock_target_field clang_target_triple)
base_url=$(lock_target_field base_txz_url)
base_sha=$(lock_target_field base_txz_sha256)
elf_machine=$(lock_target_field elf_machine)
[[ -n "$triple" && -n "$base_url" && -n "$base_sha" && -n "$elf_machine" ]] ||
  die "incomplete lock entry for $target"

# Never accept rolling aliases.
case "$base_url" in
  *latest* | *stable* | *current*)
    die "sysroot lock must pin a fixed release URL, got: $base_url"
    ;;
  *"$release"*) ;;
  *) die "sysroot URL does not mention pinned release $release: $base_url" ;;
esac

command -v clang >/dev/null 2>&1 || die "clang is required"
command -v llvm-readelf >/dev/null 2>&1 || command -v readelf >/dev/null 2>&1 ||
  die "llvm-readelf or readelf is required"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"
command -v jq >/dev/null 2>&1 || die "jq is required"

readelf_bin=$(command -v llvm-readelf || command -v readelf)

# Cache identity of this sysroot tree. A restored tree is only reused when
# every production input matches byte-for-byte; the required-file, forbidden
# path and static-ELF probe checks below still run on every invocation, hit
# or miss. Cache never participates in correctness: a miss must take the
# full download/extract path.
sysroot_lock_sha256=$(sha256sum "$LOCK_FILE" | awk '{print $1}')
sysroot_script_sha256=$(sha256sum "$ECS_REPO_ROOT/scripts/ci/freebsd_sysroot.sh" | awk '{print $1}')
cache_id=$(cat <<EOF
ecs-sysroot-cache-v1
target=$target
release=$release
base_txz_sha256=$base_sha
lock_sha256=$sysroot_lock_sha256
script_sha256=$sysroot_script_sha256
EOF
)
cache_marker="$sysroot_dir/.ecs-sysroot-cache.id"

echo "freebsd-sysroot: target=$target release=$release revision=$revision" >&2
echo "freebsd-sysroot: triple=$triple" >&2
echo "freebsd-sysroot: url=$base_url" >&2

if [[ -f "$cache_marker" && "$(cat "$cache_marker")" == "$cache_id" ]]; then
  echo "freebsd-sysroot: cache identity matched; reusing extracted sysroot for $target" >&2
else
  rm -rf "$sysroot_dir"
  mkdir -p "$work_dir" "$sysroot_dir"
  archive="$work_dir/base-$target.txz"
  marker="$work_dir/base-$target.sha256"

  if [[ -s "$archive" && -f "$marker" && "$(cat "$marker")" == "$base_sha" ]]; then
    echo "freebsd-sysroot: reusing verified base.txz for $target" >&2
  else
    rm -f "$archive" "$marker"
    curl -fsSL --retry 3 --retry-delay 2 -o "$archive" "$base_url"
    actual_sha=$(sha256sum "$archive" | awk '{print $1}')
    [[ "$actual_sha" == "$base_sha" ]] ||
      die "base.txz SHA256 mismatch for $target: expected=$base_sha actual=$actual_sha"
    printf '%s\n' "$base_sha" >"$marker"
  fi

  echo "freebsd-sysroot: extracting allowed paths into $sysroot_dir" >&2
  mapfile -t extract_paths < <(jq -er '.extract_paths[]' "$LOCK_FILE")
  [[ "${#extract_paths[@]}" -gt 0 ]] || die "lock has no extract_paths"
  extract_args=()
  for path in "${extract_paths[@]}"; do
    extract_args+=("./$path")
  done
  # Only the locked header/library trees. Never pull /usr/bin toolchains.
  tar -xJf "$archive" -C "$sysroot_dir" "${extract_args[@]}"
fi

for required in \
  usr/include/sys/param.h \
  usr/lib/crt1.o \
  usr/lib/libc.a; do
  [[ -s "$sysroot_dir/$required" ]] || die "sysroot is missing $required"
done

mapfile -t forbidden < <(jq -er '.forbidden_sysroot_contents[]' "$LOCK_FILE")
for path in "${forbidden[@]}"; do
  if [[ -e "$sysroot_dir/$path" ]]; then
    die "sysroot unexpectedly contains forbidden path: $path"
  fi
done

probe_dir="$work_dir/probe-$target"
rm -rf "$probe_dir"
mkdir -p "$probe_dir"
cat >"$probe_dir/hello.c" <<'EOF'
#include <stdio.h>

int main(void) {
    puts("ok");
    return 0;
}
EOF

echo "freebsd-sysroot: probing clang static link for $triple" >&2
clang \
  --target="$triple" \
  --sysroot="$sysroot_dir" \
  -fuse-ld=lld \
  -static \
  -o "$probe_dir/hello" \
  "$probe_dir/hello.c"

[[ -x "$probe_dir/hello" ]] || die "probe binary was not produced"

file_out=$(file "$probe_dir/hello" || true)
echo "freebsd-sysroot: file=$file_out" >&2
case "$file_out" in
  *"$elf_machine"*) ;;
  *) die "probe binary architecture is not $elf_machine: $file_out" ;;
esac
case "$file_out" in
  *FreeBSD*) ;;
  *) die "probe binary is not identified as FreeBSD: $file_out" ;;
esac
case "$file_out" in
  *static* | *statically*) ;;
  *) die "probe binary is not static: $file_out" ;;
esac

readelf_out=$("$readelf_bin" -h "$probe_dir/hello" || true)
echo "freebsd-sysroot: readelf header follows" >&2
echo "$readelf_out" >&2
case "$readelf_out" in
  *"FreeBSD"*) ;;
  *) die "ELF OS/ABI is not FreeBSD" ;;
esac

needed=$("$readelf_bin" -d "$probe_dir/hello" 2>/dev/null || true)
if [[ -n "$needed" ]] && grep -Eq 'NEEDED|Dynamic section' <<<"$needed"; then
  if grep -q 'NEEDED' <<<"$needed"; then
    die "static probe binary still has dynamic NEEDED entries"
  fi
fi

# Prove the probe is a real static FreeBSD ELF, not a host Linux binary.
if command -v readelf >/dev/null 2>&1; then
  if readelf -d "$probe_dir/hello" 2>/dev/null | grep -q 'NEEDED'; then
    die "probe binary has dynamic dependencies"
  fi
fi

# Record the cache identity only after every validation above has passed, so
# a partial or rejected tree can never be cached as reusable.
printf '%s\n' "$cache_id" >"$cache_marker"

echo "freebsd-sysroot: $target sysroot ready at $sysroot_dir" >&2
echo "freebsd-sysroot: static FreeBSD $elf_machine ELF probe OK" >&2

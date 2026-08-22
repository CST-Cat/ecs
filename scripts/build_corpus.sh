#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'EOF'
usage: scripts/build_corpus.sh --output PATH

Download, verify, construct, and package the fixed ECS Silesia corpus.
The output archive contains exactly ecs-silesia-v1.corpus at its root.
EOF
}

die() {
  echo "build-corpus: $*" >&2
  exit 1
}

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

output=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --output)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      output=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

[[ -n "$output" ]] || { usage; exit 2; }
case "$output" in
  /*) ;;
  *) output="$PWD/$output" ;;
esac

for command_name in curl sha256sum stat tar unzip; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

source_url=$(ecs_lock_corpus_field source_url)
source_sha=$(ecs_lock_corpus_field source_sha256)
corpus_sha=$(ecs_lock_corpus_field sha256)
corpus_bytes=$(ecs_lock_corpus_field bytes)
corpus_name=$(ecs_lock_corpus_field name)
mapfile -t corpus_order < <(ecs_lock_corpus_order) || die 'could not read corpus order from tools lock'

work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-corpus.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT

zip_path="$work/silesia.zip"
source_dir="$work/silesia"
corpus_path="$work/$corpus_name"
verify_dir="$work/verify"
mkdir -p "$source_dir" "$verify_dir" "$(dirname "$output")"

curl -fsSL --retry 4 --retry-delay 2 --connect-timeout 30 \
  "$source_url" -o "$zip_path"
unzip -tq "$zip_path" >/dev/null ||
  die 'Silesia download is not a valid ZIP archive'
actual_source_sha=$(sha256sum "$zip_path" | awk '{print $1}')
[[ "$actual_source_sha" == "$source_sha" ]] ||
  die "Silesia source ZIP SHA-256 mismatch: expected $source_sha, got $actual_source_sha"
unzip -q "$zip_path" -d "$source_dir"

: >"$corpus_path"
for corpus_member in "${corpus_order[@]}"; do
  [[ -f "$source_dir/$corpus_member" ]] || die "Silesia ZIP omitted $corpus_member"
  cat "$source_dir/$corpus_member" >>"$corpus_path"
done
[[ "$(stat -c %s "$corpus_path")" -eq "$corpus_bytes" ]] ||
  die 'fixed Silesia corpus byte length mismatch'
actual_corpus_sha=$(sha256sum "$corpus_path" | awk '{print $1}')
[[ "$actual_corpus_sha" == "$corpus_sha" ]] ||
  die "fixed Silesia corpus SHA-256 mismatch: expected $corpus_sha, got $actual_corpus_sha"
chmod 0644 "$corpus_path"

source_date_epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD 2>/dev/null || date -u +%s)}
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || die 'SOURCE_DATE_EPOCH must be an integer'
tar -C "$work" --sort=name --mtime="@$source_date_epoch" \
  --owner=0 --group=0 --numeric-owner -czf "$output" \
  "$corpus_name"

archive_listing=$(tar -tzf "$output")
[[ "$archive_listing" == "$corpus_name" ]] ||
  die 'corpus archive must contain exactly ecs-silesia-v1.corpus at its root'
tar -xzf "$output" -C "$verify_dir" "$corpus_name"
[[ "$(stat -c %s "$verify_dir/$corpus_name")" -eq "$corpus_bytes" ]] ||
  die 'corpus archive extracted byte length mismatch'
actual_archive_sha=$(sha256sum "$verify_dir/$corpus_name" | awk '{print $1}')
[[ "$actual_archive_sha" == "$corpus_sha" ]] ||
  die 'corpus archive extracted SHA-256 mismatch'

echo "created verified corpus archive: $output"

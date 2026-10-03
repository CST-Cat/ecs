#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/windows_tools_package.sh --output-dir DIRECTORY --corpus-path FILE

Package the verified Silesia corpus built by the Windows stage and append the
archive checksum required by the Windows run/install download contract.
USAGE
}

die() {
  echo "windows-tools-package: $*" >&2
  exit 1
}

output_dir=""
corpus_path=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --output-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      [[ -z "$output_dir" ]] || die '--output-dir may only be supplied once'
      output_dir=$2
      shift 2
      ;;
    --corpus-path)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      [[ -z "$corpus_path" ]] || die '--corpus-path may only be supplied once'
      corpus_path=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit "$?"
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

[[ -n "$output_dir" ]] || { usage; exit 2; }
[[ -n "$corpus_path" ]] || { usage; exit 2; }

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
for command_name in sha256sum tar; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command is missing: $command_name"
done

output_dir=$(ecs_absolute_path "$output_dir")
corpus_path=$(ecs_absolute_path "$corpus_path")
corpus_name=$(ecs_lock_corpus_field name)
[[ -f "$corpus_path" && -s "$corpus_path" ]] || die "verified build corpus is missing or empty: $corpus_path"
[[ "$(basename "$corpus_path")" == "$corpus_name" ]] || die "corpus input must be named $corpus_name"

if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  source_date_epoch=$SOURCE_DATE_EPOCH
else
  source_date_epoch=$(git -C "$ECS_REPO_ROOT" show -s --format=%ct HEAD) ||
    die 'could not determine Git commit timestamp for reproducible corpus archive'
fi
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || die 'SOURCE_DATE_EPOCH must be an integer'
mkdir -p "$output_dir"
archive="$output_dir/$ECS_CORPUS_ARCHIVE"
tar -C "$(dirname "$corpus_path")" --sort=name --mtime="@$source_date_epoch" \
  --owner=0 --group=0 --numeric-owner -czf "$archive" \
  "$corpus_name"
[[ -s "$archive" ]] || die "corpus archive was not created: $archive"

(
  cd "$output_dir"
  sha256sum "$ECS_CORPUS_ARCHIVE" >> checksums.txt
)

echo "packaged Windows tools corpus archive and checksum: $archive"

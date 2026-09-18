#!/usr/bin/env bash
set -Eeuo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/windows_tools_package.sh --output-dir DIRECTORY

Build the locked Silesia corpus archive and append its checksum to the
Windows tools bundle checksum file.
USAGE
}

die() {
  echo "windows-tools-package: $*" >&2
  exit 1
}

output_dir=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --output-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || { usage; exit 2; }
      [[ -z "$output_dir" ]] || die '--output-dir may only be supplied once'
      output_dir=$2
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

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
command -v sha256sum >/dev/null 2>&1 || die 'required command is missing: sha256sum'
corpus_url=$(ecs_lock_corpus_field source_url)
[[ "$corpus_url" == https://* ]] || die 'locked Silesia source URL must use HTTPS'

output_dir=$(ecs_absolute_path "$output_dir")
mkdir -p "$output_dir"
archive="$output_dir/$ECS_CORPUS_ARCHIVE"

"$ECS_REPO_ROOT/scripts/build_corpus.sh" --output "$archive"
[[ -s "$archive" ]] || die "corpus archive was not created: $archive"

(
  cd "$output_dir"
  sha256sum "$ECS_CORPUS_ARCHIVE" >> checksums.txt
)

echo "created Windows tools corpus archive and checksum: $archive"

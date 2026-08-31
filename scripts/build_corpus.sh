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
source "$(dirname "${BASH_SOURCE[0]}")/lib/corpus.sh"

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

corpus_name=$(ecs_lock_corpus_field name)

work=$(mktemp -d "${TMPDIR:-/tmp}/ecs-corpus.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT
  rm -rf -- "$work"
  exit "$status"
}
trap cleanup EXIT

corpus_path="$work/$corpus_name"
mkdir -p "$(dirname "$output")"
ecs_build_silesia_corpus "$work" "$corpus_path" || die 'could not build the fixed Silesia corpus'

source_date_epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD 2>/dev/null || date -u +%s)}
[[ "$source_date_epoch" =~ ^[0-9]+$ ]] || die 'SOURCE_DATE_EPOCH must be an integer'
tar -C "$work" --sort=name --mtime="@$source_date_epoch" \
  --owner=0 --group=0 --numeric-owner -czf "$output" \
  "$corpus_name"

echo "created corpus archive: $output"

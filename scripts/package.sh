#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: scripts/package.sh --binaries-dir BINARY_DIR
       scripts/package.sh --tools-stage STAGE_ROOT
       [--target TARGET] ...
USAGE
}

tools_stage_root=""
binaries_dir=""
target_selectors=()
usage_error() {
  echo "$*" >&2
  usage
  exit 1
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --binaries-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || usage_error "--binaries-dir requires BINARY_DIR"
      [[ -z "$binaries_dir" ]] || usage_error "--binaries-dir may only be supplied once"
      [[ -z "$tools_stage_root" ]] || usage_error "--binaries-dir and --tools-stage are mutually exclusive"
      binaries_dir=$2
      shift 2
      ;;
    --tools-stage)
      [[ "$#" -ge 2 && -n "$2" ]] || usage_error "--tools-stage requires STAGE_ROOT"
      [[ -z "$tools_stage_root" ]] || usage_error "--tools-stage may only be supplied once"
      [[ -z "$binaries_dir" ]] || usage_error "--binaries-dir and --tools-stage are mutually exclusive"
      tools_stage_root=$2
      shift 2
      ;;
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || usage_error "--target requires TARGET"
      target_selectors+=("$2")
      shift 2
      ;;
    *)
      usage_error "unknown option: $1"
      ;;
  esac
done

[[ -n "$binaries_dir" || -n "$tools_stage_root" ]] ||
  usage_error "one package input is required: --binaries-dir or --tools-stage"

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
repo_root=$ECS_REPO_ROOT
dist_dir="$repo_root/dist"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git -C "$repo_root" show -s --format=%ct HEAD 2>/dev/null || date -u +%s)}"
if [[ ! "$source_date_epoch" =~ ^[0-9]+$ ]]; then
  echo "SOURCE_DATE_EPOCH must be an integer" >&2
  exit 1
fi
if [[ -n "$tools_stage_root" && "$tools_stage_root" != /* ]]; then
  tools_stage_root="$repo_root/$tools_stage_root"
fi
if [[ -n "$binaries_dir" && "$binaries_dir" != /* ]]; then
  binaries_dir="$repo_root/$binaries_dir"
fi

die() {
  echo "ecs-tools: $*" >&2
  exit 1
}

# Preserve the existing Linux release set by default while allowing callers to
# select an unambiguous Linux or FreeBSD platform target explicitly.
targets=("${ECS_LINUX_TARGETS[@]}")
if [[ "${#target_selectors[@]}" -gt 0 ]]; then
  targets=()
  for selected_target in "${target_selectors[@]}"; do
    for existing_target in "${targets[@]}"; do
      read -r existing_target_id _ <<<"$existing_target"
      [[ "$existing_target_id" != "$selected_target" ]] ||
        usage_error "target may only be supplied once: $selected_target"
    done
    selected=0
    for target_record in "${ECS_TARGETS[@]}"; do
      read -r target_id _goos _goarch _arch <<<"$target_record"
      if [[ "$target_id" == "$selected_target" ]]; then
        targets+=("$target_record")
        selected=1
        break
      fi
    done
    [[ "$selected" -eq 1 ]] || usage_error "unsupported target: $selected_target"
  done
fi

temp_stages=()
new_temp_stage_path=""
cleanup_temp_stages() {
  local status=$?
  trap - EXIT
  for temp_stage in "${temp_stages[@]}"; do
    [[ -n "$temp_stage" ]] || continue
    rm -rf -- "$temp_stage"
  done
  exit "$status"
}
trap cleanup_temp_stages EXIT

assert_temp_stage_path() {
  local path=$1
  local temp_root=${TMPDIR:-/tmp}

  [[ -n "$path" ]] || die "internal error: temporary package staging path is empty"
  [[ "$temp_root" != "/" ]] || die "TMPDIR must not be / for package staging"
  case "$path" in
    "$temp_root"/*|/tmp/*) ;;
    *) die "internal error: package staging path is outside the temporary directory: $path" ;;
  esac
  [[ -d "$path" ]] || die "internal error: temporary package staging path does not exist: $path"
}

new_temp_stage() {
  new_temp_stage_path=$(mktemp -d "${TMPDIR:-/tmp}/ecs-package.XXXXXX")
  assert_temp_stage_path "$new_temp_stage_path"
  temp_stages+=("$new_temp_stage_path")
}

tool_stage_dir() {
  local target=$1
  printf '%s\n' "$tools_stage_root/$target"
}

package_tools() {
  local target=$1
  local stage_dir
  local source_dir
  local package_stage
  local archive
  local -a target_tools

  stage_dir=$(tool_stage_dir "$target")
  source_dir="$stage_dir/bin"
  mapfile -t target_tools < <(ecs_target_tool_names "$target")
  [[ "${#target_tools[@]}" -gt 0 ]] || die "no tools defined for target $target"
  [[ -d "$stage_dir" ]] || die "missing tools stage: $stage_dir"
  new_temp_stage
  package_stage=$new_temp_stage_path
  assert_temp_stage_path "$package_stage"
  mkdir -p "$package_stage/bin" "$package_stage/LICENSES"

  cp "$repo_root/LICENSE" "$package_stage/LICENSE"
  cp "$repo_root/NOTICE" "$package_stage/NOTICE"
  cp "$stage_dir/manifest.json" "$package_stage/manifest.json"
  cp -a "$stage_dir/LICENSES/." "$package_stage/LICENSES/"
  for tool in "${target_tools[@]}"; do
    cp "$source_dir/$tool" "$package_stage/bin/$tool"
    chmod 0755 "$package_stage/bin/$tool"
  done
  archive="$dist_dir/ecs-tools_${target}.tar.gz"
  echo "packaging ecs-tools_${target}"
  tar -C "$package_stage" --sort=name --mtime="@$source_date_epoch" \
    --owner=0 --group=0 --numeric-owner -czf "$archive" \
    bin LICENSES LICENSE NOTICE manifest.json
}

package_corpus() {
  local corpus_target=$1 corpus_stage

  corpus_stage="$(tool_stage_dir "$corpus_target")/share/ecs/corpus"
  echo "packaging $ECS_CORPUS_ARCHIVE"
  tar -C "$corpus_stage" --sort=name --mtime="@$source_date_epoch" \
    --owner=0 --group=0 --numeric-owner -czf "$dist_dir/$ECS_CORPUS_ARCHIVE" \
    "$ECS_CORPUS_NAME"
}

mkdir -p "$dist_dir"
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f \
  \( -name 'ecs_*.tar.*' -o -name 'ecs-tools_*' -o -name 'ecs-corpus_*' \) -delete
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -name 'checksums.txt' -delete

assets=()
if [[ -n "$binaries_dir" ]]; then
  for target in "${targets[@]}"; do
    read -r target_id goos _goarch arch <<<"$target"
    suffix="$target_id"
    new_temp_stage
    stage=$new_temp_stage_path
    binary="$stage/ecs"
    echo "packaging $suffix"
    cp "$binaries_dir/ecs_${suffix}" "$binary"
    chmod 0755 "$binary"
    cp "$repo_root/LICENSE" "$repo_root/NOTICE" "$repo_root/README.md" "$repo_root/README_EN.md" "$repo_root/SECURITY.md" "$repo_root/THIRD_PARTY.md" "$stage/"
    tar -C "$stage" --sort=name --mtime="@$source_date_epoch" \
      --owner=0 --group=0 --numeric-owner -czf "$dist_dir/ecs_${suffix}.tar.gz" \
      ecs LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md
    assets+=("ecs_${suffix}.tar.gz")
  done
else
  corpus_target=""
  for target in "${targets[@]}"; do
    read -r target_id goos _goarch arch <<<"$target"
    package_tools "$target_id"
    assets+=("ecs-tools_${target_id}.tar.gz")
    if [[ "$goos" == linux && -z "$corpus_target" ]]; then
      corpus_target=$target_id
    fi
  done
  if [[ -n "$corpus_target" ]]; then
    package_corpus "$corpus_target"
    assets+=("$ECS_CORPUS_ARCHIVE")
  fi
fi

(
  cd "$dist_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${assets[@]}" > checksums.txt
  else
    shasum -a 256 "${assets[@]}" > checksums.txt
  fi
)

echo "release assets written to $dist_dir"

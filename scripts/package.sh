#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scripts/package.sh VERSION [--tools-stage STAGE_ROOT]" >&2
}

version="${1:-}"
if [[ -z "$version" ]]; then
  usage
  exit 1
fi
shift
if [[ ! "$version" =~ ^[0-9A-Za-z._+-]+$ ]]; then
  echo "VERSION may only contain letters, digits, dot, underscore, plus, and hyphen" >&2
  exit 1
fi

tools_enabled=0
tools_stage_root=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --tools-stage)
      [[ "$#" -ge 2 && -n "$2" ]] || {
        echo "--tools-stage requires STAGE_ROOT" >&2
        usage
        exit 1
      }
      [[ "$tools_enabled" -eq 0 ]] || {
        echo "--tools-stage may only be supplied once" >&2
        exit 1
      }
      tools_enabled=1
      tools_stage_root=$2
      shift 2
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
repo_root=$ECS_REPO_ROOT
dist_dir="$repo_root/dist"
go_command="${GO:-go}"
commit="${COMMIT:-$(git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git -C "$repo_root" show -s --format=%ct HEAD 2>/dev/null || date -u +%s)}"
if [[ ! "$source_date_epoch" =~ ^[0-9]+$ ]]; then
  echo "SOURCE_DATE_EPOCH must be an integer" >&2
  exit 1
fi
build_date="${BUILD_DATE:-$(date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ)}"
ldflags="-s -w -X ecs/internal/buildinfo.Version=${version} -X ecs/internal/buildinfo.Commit=${commit} -X ecs/internal/buildinfo.BuildDate=${build_date}"

if [[ "$tools_enabled" -eq 1 && "$tools_stage_root" != /* ]]; then
  tools_stage_root="$repo_root/$tools_stage_root"
fi

die() {
  echo "ecs-tools: $*" >&2
  exit 1
}

# 发布目标与工具清单来自 scripts/lib/common.sh，与交叉构建、发布校验共用同一张表。
targets=("${ECS_TARGETS[@]}")

tool_names=("${ECS_TOOL_NAMES[@]}")

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

require_tool_artifact() {
  local path=$1
  local arch=$2
  local tool=$3

  if [[ ! -e "$path" ]]; then
    die "missing $tool for $arch: $path"
  fi
  if [[ -L "$path" || ! -f "$path" ]]; then
    die "tool is not a regular file for $arch: $path"
  fi
  if [[ ! -s "$path" ]]; then
    die "tool is empty for $arch: $path"
  fi
  if [[ ! -x "$path" ]]; then
    die "tool is not executable for $arch: $path"
  fi
}

tool_stage_dir() {
  local arch=$1
  printf '%s\n' "$tools_stage_root/linux_${arch}"
}

validate_tool_stage() {
  local arch=$1
  local stage_dir
  local licenses_dir
  local license_file

  stage_dir=$(tool_stage_dir "$arch")
  [[ -d "$stage_dir" ]] || die "missing architecture staging directory: $stage_dir"

  "$repo_root/scripts/verify_tools_stage.sh" \
    --arch "$arch" --stage-root "$tools_stage_root" --keep-corpus ||
    die "invalid tools stage for $arch"

  licenses_dir="$stage_dir/LICENSES"
  [[ -d "$licenses_dir" ]] || die "missing LICENSES directory for $arch: $licenses_dir"
  license_file=$(find "$licenses_dir" -mindepth 1 -maxdepth 1 -type f -size +0c -print -quit)
  [[ -n "$license_file" ]] || die "LICENSES is empty for $arch: $licenses_dir"
}

preflight_tools() {
  [[ -d "$tools_stage_root" ]] || die "staging root does not exist: $tools_stage_root"
  command -v tar >/dev/null 2>&1 || die "tar is required for --tools-stage"

  for target in "${targets[@]}"; do
    read -r _goos _goarch arch <<<"$target"
    validate_tool_stage "$arch"
  done
}

package_tools() {
  local arch=$1
  local stage_dir
  local source_dir
  local package_stage
  local archive

  stage_dir=$(tool_stage_dir "$arch")
  source_dir="$stage_dir/bin"
  new_temp_stage
  package_stage=$new_temp_stage_path
  assert_temp_stage_path "$package_stage"
  mkdir -p "$package_stage/bin" "$package_stage/LICENSES"

  cp "$repo_root/LICENSE" "$package_stage/LICENSE"
  cp "$repo_root/NOTICE" "$package_stage/NOTICE"
  cp "$stage_dir/manifest.json" "$package_stage/manifest.json"
  cp -a "$stage_dir/LICENSES/." "$package_stage/LICENSES/"
  for tool in "${tool_names[@]}"; do
    require_tool_artifact "$source_dir/$tool" "$arch" "$tool"
    cp "$source_dir/$tool" "$package_stage/bin/$tool"
    chmod 0755 "$package_stage/bin/$tool"
  done
  archive="$dist_dir/ecs-tools_linux_${arch}.tar.gz"
  echo "packaging ecs-tools_linux_${arch}"
  tar -C "$package_stage" --sort=name --mtime="@$source_date_epoch" \
    --owner=0 --group=0 --numeric-owner -czf "$archive" \
    bin LICENSES LICENSE NOTICE manifest.json
}

if [[ "$tools_enabled" -eq 1 ]]; then
  preflight_tools
fi

mkdir -p "$dist_dir"
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -name 'ecs_*' -delete
find "$dist_dir" -mindepth 1 -maxdepth 1 -type f -name 'checksums.txt' -delete

for target in "${targets[@]}"; do
  read -r goos goarch arch <<<"$target"
  goarm=""
  [[ "$goarch" == "arm" ]] && goarm=7
  suffix="${goos}_${arch}"
  new_temp_stage
  stage=$new_temp_stage_path
  binary="$stage/ecs"
  echo "building $suffix"
  (
    cd "$repo_root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="${goarm:-}" \
      "$go_command" build -trimpath -ldflags "$ldflags" -o "$binary" ./cmd/ecs
  )
  cp "$repo_root/LICENSE" "$repo_root/NOTICE" "$repo_root/README.md" "$repo_root/README_EN.md" "$repo_root/SECURITY.md" "$repo_root/THIRD_PARTY.md" "$stage/"
  tar -C "$stage" --sort=name --mtime="@$source_date_epoch" \
    --owner=0 --group=0 --numeric-owner -czf "$dist_dir/ecs_${suffix}.tar.gz" \
    ecs LICENSE NOTICE README.md README_EN.md SECURITY.md THIRD_PARTY.md
  if [[ "$tools_enabled" -eq 1 ]]; then
    package_tools "$arch"
  fi
done

(
  cd "$dist_dir"
  assets=(ecs_*.tar.gz)
  if [[ "$tools_enabled" -eq 1 ]]; then
    assets+=(ecs-tools_*.tar.gz)
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${assets[@]}" > checksums.txt
  else
    shasum -a 256 "${assets[@]}" > checksums.txt
  fi
)

echo "release assets written to $dist_dir"

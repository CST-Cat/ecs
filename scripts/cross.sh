#!/usr/bin/env bash
set -euo pipefail

# 交叉编译全部发布目标的 ecs 主程序，确认七个 Linux 与两个 FreeBSD
# target 都还能构建。
#
# 只构建、不打包：打包是 scripts/package.sh 的职责，这里只回答九个目标
# 现在是否都能编译。目标列表来自 scripts/lib/common.sh，与打包和发布共用同一张表。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

target=""
if [[ "$#" -eq 2 && "$1" == "--target" && -n "$2" ]]; then
  target=$2
elif [[ "$#" -ne 0 ]]; then
  echo "usage: $0 [--target GOOS_GOARCH]" >&2
  exit 2
fi

go_command="${GO:-go}"
version="${VERSION:-dev}"
if [[ ! "$version" =~ ^[0-9A-Za-z._+-]+$ ]]; then
  echo "VERSION may only contain letters, digits, dot, underscore, plus, and hyphen" >&2
  exit 1
fi
commit="${COMMIT:-$(git -C "$ECS_REPO_ROOT" rev-parse --short HEAD 2>/dev/null || printf unknown)}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git -C "$ECS_REPO_ROOT" show -s --format=%ct HEAD 2>/dev/null || date -u +%s)}"
if [[ ! "$source_date_epoch" =~ ^[0-9]+$ ]]; then
  echo "SOURCE_DATE_EPOCH must be an integer" >&2
  exit 1
fi
build_date="${BUILD_DATE:-$(date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ)}"
output_dir="${OUTPUT_DIR:-$ECS_REPO_ROOT/dist}"
tools_bundle=$(<"$ECS_REPO_ROOT/tools/BUNDLE")

ldflags="-s -w"
ldflags+=" -X ecs/internal/buildinfo.Version=$version"
ldflags+=" -X ecs/internal/buildinfo.Commit=$commit"
ldflags+=" -X ecs/internal/buildinfo.BuildDate=$build_date"
ldflags+=" -X ecs/internal/buildinfo.ToolsBundle=$tools_bundle"

mkdir -p "$output_dir"
build_count=0
for entry in "${ECS_TARGETS[@]}"; do
  read -r target_id goos goarch package_arch <<<"$entry"
  if [[ -n "$target" && "$target_id" != "$target" ]]; then
    continue
  fi
  goarm=""
  [[ "$goos" == linux && "$package_arch" == armv7 ]] && goarm=7
  printf 'cross: %s/%s -> %s\n' "$goos" "$package_arch" "$output_dir/ecs_${target_id}" >&2
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
    "$go_command" build -trimpath -ldflags "$ldflags" \
    -o "$output_dir/ecs_${target_id}" "$ECS_REPO_ROOT/cmd/ecs"
  build_count=$((build_count + 1))
done

if [[ -n "$target" && "$build_count" -eq 0 ]]; then
  echo "cross: unknown target: $target" >&2
  exit 1
fi

echo "cross: built $build_count targets into $output_dir" >&2

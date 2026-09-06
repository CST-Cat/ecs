#!/usr/bin/env bash
set -euo pipefail

# 交叉编译七架构 ecs 主程序，确认每个发布目标都还能构建。
#
# 只构建、不打包：打包是 scripts/package.sh 的职责，这里只回答"七个架构现在
# 都编得过吗"。架构列表来自 scripts/lib/common.sh，与打包和发布共用同一张表。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

arch=""
if [[ "$#" -eq 2 && "$1" == "--arch" && -n "$2" ]]; then
  arch=$2
elif [[ "$#" -ne 0 ]]; then
  echo "usage: $0 [--arch ARCH]" >&2
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

ldflags="-s -w"
ldflags+=" -X ecs/internal/buildinfo.Version=$version"
ldflags+=" -X ecs/internal/buildinfo.Commit=$commit"
ldflags+=" -X ecs/internal/buildinfo.BuildDate=$build_date"

mkdir -p "$output_dir"
build_count=0
for entry in "${ECS_TARGETS[@]}"; do
  read -r goos goarch name <<<"$entry"
  if [[ -n "$arch" && "$name" != "$arch" ]]; then
    continue
  fi
  goarm=""
  [[ "$name" == armv7 ]] && goarm=7
  printf 'cross: %s/%s -> %s\n' "$goos" "$name" "$output_dir/ecs_${goos}_${name}" >&2
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
    "$go_command" build -trimpath -ldflags "$ldflags" \
    -o "$output_dir/ecs_${goos}_${name}" "$ECS_REPO_ROOT/cmd/ecs"
  build_count=$((build_count + 1))
done

if [[ -n "$arch" && "$build_count" -eq 0 ]]; then
  echo "cross: unknown architecture: $arch" >&2
  exit 1
fi

echo "cross: built $build_count architectures into $output_dir" >&2

#!/usr/bin/env bash
set -Eeuo pipefail

# 在 Linux 发布主机上组装 FreeBSD 的发布布局。
#
# run.sh 消费两个相互独立的发行版本线：
#   ECS Release      -> ecs_<target>.tar.gz + checksums.txt
#   Bundle Release   -> ecs-tools_<target>.tar.gz + checksums.txt
# 两条线的资产名和校验清单都由 scripts/package.sh 生成，这里只负责按同一次
# 调用的顺序把它们搬进一个目录，供 scripts/ci/freebsd_artifact_e2e.sh 在真实
# FreeBSD 客户机里消费。
#
# 刻意不重新实现打包：所有归档都来自 scripts/package.sh，所以 E2E 验证的是
# 发布路径真正会产出的字节，而不是测试自己拼出来的近似物。

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_artifact_layout.sh --stage-root DIR --layout DIR [--target freebsd_amd64]

  --stage-root DIR  FreeBSD 工具 stage 根目录，包含 DIR/<target>/{bin,manifest.json,LICENSES}
  --layout DIR      输出目录；脚本会创建 DIR/ecs-release 与 DIR/bundle-release
  --target TARGET   freebsd_amd64（默认）或 freebsd_arm64

必须在能交叉编译 FreeBSD 目标的 Linux 主机上运行，需要 bash、go、sha256sum。
USAGE
}

die() {
  echo "freebsd-artifact-layout: $*" >&2
  exit 1
}

stage_root=""
layout_root=""
target=freebsd_amd64
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --layout)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--layout requires a value"
      layout_root=$2
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
[[ -n "$layout_root" ]] || {
  usage
  exit 2
}
[[ "$stage_root" = /* ]] || die "--stage-root must be an absolute path: $stage_root"
[[ "$layout_root" = /* ]] || die "--layout must be an absolute path: $layout_root"

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
repo_root=$ECS_REPO_ROOT
cd "$repo_root"

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *) die "unsupported target: $target" ;;
esac

[[ -d "$stage_root/$target/bin" ]] ||
  die "tools stage is missing $stage_root/$target/bin"
[[ -s "$stage_root/$target/manifest.json" ]] ||
  die "tools stage is missing $stage_root/$target/manifest.json"
[[ -d "$stage_root/$target/LICENSES" ]] ||
  die "tools stage is missing $stage_root/$target/LICENSES"

# package.sh 固定写入 $repo_root/dist，并且每次调用都会先清掉上一次的资产，
# 所以两条版本线必须一次调用各取一份、立刻搬走，而不是共用同一个 dist。
ecs_release_dir="$layout_root/ecs-release"
bundle_release_dir="$layout_root/bundle-release"
rm -rf -- "$layout_root"
mkdir -p "$ecs_release_dir" "$bundle_release_dir"

binaries_dir=$(mktemp -d "${TMPDIR:-/tmp}/ecs-freebsd-artifact-binaries.XXXXXX")
cleanup() {
  local status=$?
  trap - EXIT
  rm -rf -- "$binaries_dir"
  exit "$status"
}
trap cleanup EXIT

# ECS Release：先交叉编译 FreeBSD 主程序，再由 package.sh 打包。
OUTPUT_DIR="$binaries_dir" bash scripts/cross.sh --target "$target"
[[ -s "$binaries_dir/ecs_${target}" ]] ||
  die "cross.sh did not produce $binaries_dir/ecs_${target}"

bash scripts/package.sh --binaries-dir "$binaries_dir" --target "$target"
cp -a "$repo_root/dist/." "$ecs_release_dir/"

# Bundle Release：由真实工具 stage 打包。package.sh 会重新清空 dist，所以这一步
# 必须在上一份资产已经搬走之后执行。
bash scripts/package.sh --tools-stage "$stage_root" --target "$target"
cp -a "$repo_root/dist/." "$bundle_release_dir/"

for required_file in \
  "$ecs_release_dir/ecs_${target}.tar.gz" \
  "$ecs_release_dir/checksums.txt" \
  "$bundle_release_dir/ecs-tools_${target}.tar.gz" \
  "$bundle_release_dir/checksums.txt"; do
  [[ -s "$required_file" ]] || die "packaging did not produce $required_file"
done

# 两条版本线的 checksums.txt 必须各自覆盖自己的资产；一个只写了另一个的
# 校验清单会让 run.sh 在下载之后才失败，问题会离现场很远。
grep -F "ecs_${target}.tar.gz" "$ecs_release_dir/checksums.txt" >/dev/null ||
  die "ECS release checksums do not cover ecs_${target}.tar.gz"
grep -F "ecs-tools_${target}.tar.gz" "$bundle_release_dir/checksums.txt" >/dev/null ||
  die "Bundle release checksums do not cover ecs-tools_${target}.tar.gz"

# FreeBSD 工具包不得携带 ping 或 nexttrace-tiny：这两个由 base 系统提供，
# 一旦被打进归档就等于发布了一个不该存在的下载。
archive_tools=$(tar -tzf "$bundle_release_dir/ecs-tools_${target}.tar.gz" | sed -n 's#^bin/##p' | sort)
for forbidden in ping nexttrace-tiny; do
  if printf '%s\n' "$archive_tools" | grep -Fx "$forbidden" >/dev/null; then
    die "FreeBSD tools archive contains $forbidden, which the base system provides"
  fi
done

echo "freebsd-artifact-layout: target=$target"
echo "freebsd-artifact-layout: ecs release    -> $ecs_release_dir"
echo "freebsd-artifact-layout: bundle release -> $bundle_release_dir"
printf 'freebsd-artifact-layout: tools archive members: %s\n' "$(printf '%s ' $archive_tools)"

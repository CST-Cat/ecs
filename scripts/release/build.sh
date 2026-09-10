#!/usr/bin/env bash
set -euo pipefail

# 组装 ECS Release。
#
#   dist/ecs_<target>.tar.gz              九个平台目标主程序
#                                         （七个 Linux 加 FreeBSD amd64/arm64）
#   dist/checksums.txt                    以上全部的 SHA-256
#
# 本脚本被 release workflow 的 assemble job 和本地 `make release-dry-run` 共用。
# 发布路径与本地演练路径一旦是两份实现，本地演练就失去意义。
#
# checksums.txt 只有一个消费者：下载方。install.sh、compare.sh 和 run.sh 从
# Release 取回它来校验刚下载的资产。发布链内部不再重算这些摘要。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/release/build.sh VERSION [--binaries-dir BINARY_DIR] [--dry-run]

  --binaries-dir  已构建的九目标主程序目录，省略时由本脚本编译
  --dry-run      本地演练：允许脏工作区。发布路径绝不能传——洁净检查正是
                 用来挡住会带上 vcs.modified=true 的构建的。
USAGE
}

die() {
  echo "release-build: $*" >&2
  exit 1
}

version="${1:-}"
[[ -n "$version" ]] || {
  usage
  exit 1
}
shift

binaries_dir=""
dry_run=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --binaries-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--binaries-dir requires BINARY_DIR"
      [[ -z "$binaries_dir" ]] || die "--binaries-dir may only be supplied once"
      binaries_dir=$2
      shift 2
      ;;
    --dry-run)
      dry_run=1
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

dist="$ECS_REPO_ROOT/dist"

# 工作区必须洁净：go build 会把 vcs.modified 写进二进制，脏工作区产出的制品
# 会在 verify 阶段被打回。在这里失败比在七架构都构建完之后失败便宜得多。
# 本地演练除外——开发机上的工作区本来就是脏的，那正是演练要覆盖的场景。
if [[ "$dry_run" -eq 0 ]]; then
  status=$(git -C "$ECS_REPO_ROOT" status --porcelain=v1 --untracked-files=all)
  if [[ -n "$status" ]]; then
    echo "release-build: 工作区不洁净，无法产出可信制品：" >&2
    printf '%s\n' "$status" >&2
    exit 1
  fi
fi

# 可复现构建：时间戳取自提交；同一提交、工具链和输入可得到同样的包。
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ECS_REPO_ROOT" show -s --format=%ct HEAD)}"

prebuilt_dir=""
cleanup_prebuilt() {
  [[ -n "$prebuilt_dir" ]] || return 0
  rm -rf -- "$prebuilt_dir"
}
trap cleanup_prebuilt EXIT

if [[ -n "$binaries_dir" ]]; then
  echo "release-build: 使用预构建主程序目录 $binaries_dir" >&2
else
  prebuilt_dir=$(mktemp -d "${TMPDIR:-/tmp}/ecs-release-binaries.XXXXXX")
  echo "release-build: 编译九目标主程序" >&2
  OUTPUT_DIR="$prebuilt_dir" VERSION="$version" scripts/cross.sh
  binaries_dir="$prebuilt_dir"
fi

scripts/package.sh --binaries-dir "$binaries_dir" --all-targets

echo "release-build: $version 组装完成，共 $(wc -l <"$dist/checksums.txt") 个发布物" >&2

#!/usr/bin/env bash
set -euo pipefail

# 组装完整的一组发布物。
#
#   dist/ecs_linux_<arch>.tar.gz          七架构主程序
#   dist/ecs-tools_linux_<arch>.tar.gz    七架构工具包（有 --tools-stage 时）
#   dist/ecs-corpus_silesia-v1.tar.gz     独立语料
#   dist/checksums.txt                    以上全部的 SHA-256
#
# 本脚本被 release workflow 的 assemble job 和本地 `make release-dry-run` 共用。
# 发布路径与本地演练路径一旦是两份实现，本地演练就失去意义。
#
# 输出的 checksums.txt 同时是 artifact attestation 的 subject 清单：
# attest 按摘要绑定，因此这一份文件既给用户校验，也给 provenance 用。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/release/build.sh VERSION [--tools-stage STAGE_ROOT] [--dry-run]

  --tools-stage  七架构工具 stage 的根目录，省略时只打包主程序
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

tools_stage=""
dry_run=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --tools-stage)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--tools-stage requires STAGE_ROOT"
      tools_stage=$2
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

# 可复现构建：时间戳取自提交而不是墙上时钟，同一个提交重复构建得到同样的包。
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ECS_REPO_ROOT" show -s --format=%ct HEAD)}"

if [[ -n "$tools_stage" ]]; then
  [[ "$tools_stage" == /* ]] || tools_stage="$ECS_REPO_ROOT/$tools_stage"
  echo "release-build: 使用工具 stage $tools_stage" >&2

  # 这里只做 package.sh 做不了的两件事。stage 的完整校验（manifest 能被 ecs
  # 自己的解析器读下来、十个工具都是非空可执行的普通文件、LICENSES 非空）
  # 由 package.sh 的 preflight_tools 负责，不在这里抄一份更弱的版本。
  for arch in "${ECS_ARCHES[@]}"; do
    stage_dir="$tools_stage/linux_$arch"
    [[ -d "$stage_dir" ]] || die "缺少 $arch 的 stage 目录：$stage_dir"

    # upload-artifact 不保留可执行位，下载回来必须先补上——否则 package.sh
    # 的可执行断言会在一个其实完好的 stage 上失败。
    chmod +x "$stage_dir/bin/"*

    # 语料是独立发布物。它混在工具包里会让七个归档各带 200 MB。
    [[ ! -e "$stage_dir/share/ecs/corpus/ecs-silesia-v1.corpus" ]] ||
      die "$arch stage 仍带着语料，它应当作为独立发布物"
  done

  scripts/package.sh "$version" --tools-stage "$tools_stage"
else
  echo "release-build: 未提供工具 stage，只打包主程序" >&2
  scripts/package.sh "$version"
fi

echo "release-build: 构建独立语料发布物 $ECS_CORPUS_ARCHIVE" >&2
scripts/build_corpus.sh --output "$dist/$ECS_CORPUS_ARCHIVE"

echo "release-build: 生成 checksums.txt" >&2
(
  cd "$dist"
  if [[ -n "$tools_stage" ]]; then
    sha256sum ecs_*.tar.gz ecs-tools_*.tar.gz "$ECS_CORPUS_ARCHIVE" >checksums.txt
  else
    sha256sum ecs_*.tar.gz "$ECS_CORPUS_ARCHIVE" >checksums.txt
  fi
)

echo "release-build: $version 组装完成，共 $(wc -l <"$dist/checksums.txt") 个发布物" >&2

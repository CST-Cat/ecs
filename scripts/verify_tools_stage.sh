#!/usr/bin/env bash
set -euo pipefail

# 校验某个架构的工具 stage，并把 Silesia 语料从中摘出去。
#
# 校验分两层：
#
#   1. cmd/tools-manifest-check 用 ecs 自己的解析器读 manifest——发布物必须能被
#      将要读它的那份代码认下来，而不是只能被另一份 shell parser 认下来；
#      build mode、smoke runner 和 NPB smoke class 的 stage 口径也通过同一入口
#      与构建容器的解析结果比对；
#   2. 下面只保留 stage 特有检查：十个 executable、LICENSES 与独立语料。
#
# 构建口径（cross 还是 native、smoke 运行器是谁）不在这里第二次定义，而是向
# build_tools_container.sh --print-params 索取。manifest checker 只负责把 stage
# 中记录的值与这份解析结果比对。
#
# 语料是独立发布物：它 200 MB 出头，七个架构各带一份会让 Release 膨胀到没有
# 必要的体积。它的内容在下载入口（build_tools.sh）已按 lock.json 校验过，这里
# 只负责把它从 stage 中摘出去。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  echo "usage: scripts/verify_tools_stage.sh --arch ARCH --stage-root DIR [--keep-corpus]" >&2
}

die() {
  echo "verify-tools-stage: $*" >&2
  exit 1
}

arch=""
stage_root=""
keep_corpus=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --arch)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--arch requires a value"
      arch=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --keep-corpus)
      keep_corpus=1
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

[[ -n "$arch" ]] || {
  usage
  die "--arch is required"
}
[[ -n "$stage_root" ]] || {
  usage
  die "--stage-root is required"
}
stage_dir="$stage_root/linux_$arch"
manifest="$stage_dir/manifest.json"
[[ -s "$manifest" ]] || die "missing manifest: $manifest"

# 构建口径来自构建脚本本身。
params=$("$ECS_REPO_ROOT/scripts/build_tools_container.sh" --arch "$arch" --print-params)
toolchain_mode=$(awk -F= '$1 == "toolchain_mode" { print $2 }' <<<"$params")
target_runner=$(awk -F= '$1 == "target_runner" { print $2 }' <<<"$params")
npb_class=$(awk -F= '$1 == "npb_ci_smoke_class" { print $2 }' <<<"$params")
[[ -n "$toolchain_mode" && -n "$target_runner" && -n "$npb_class" ]] ||
  die "could not resolve build parameters for $arch"

echo "verify-tools-stage: $arch mode=$toolchain_mode runner=$target_runner npb_class=$npb_class" >&2

# Canonical parser/validator owns manifest structure, fields, tool set, and
# architecture semantics. The expected build mode, smoke runner, and NPB
# class are stage-specific values supplied by the build container, so they are
# checked by the same Go entry point rather than duplicated in jq.
go run "$ECS_REPO_ROOT/cmd/tools-manifest-check" \
  --architecture "$arch" \
  --toolchain-mode "$toolchain_mode" \
  --smoke-runner "$target_runner" \
  --npb-smoke-class "$npb_class" \
  "$manifest"

# 十个工具都必须真的在 stage 里，且可执行。
for tool in "${ECS_TOOL_NAMES[@]}"; do
  tool_path="$stage_dir/bin/$tool"
  [[ -x "$tool_path" ]] || die "$arch stage is missing an executable $tool"
done
[[ -d "$stage_dir/LICENSES" ]] || die "$arch stage is missing LICENSES"

corpus="$stage_dir/share/ecs/corpus/$ECS_CORPUS_NAME"
if [[ "$keep_corpus" -eq 1 ]]; then
  echo "verify-tools-stage: $arch verified, corpus kept" >&2
  exit 0
fi

[[ -f "$corpus" ]] || die "$arch stage is missing the Silesia corpus"

# 容器以 root 写出这些文件，宿主上的普通用户需要 sudo 才能删。
remove() {
  if rm -f -- "$1" 2>/dev/null; then
    return 0
  fi
  sudo rm -f -- "$1"
}
remove "$corpus"
rmdir "$stage_dir/share/ecs/corpus" "$stage_dir/share/ecs" "$stage_dir/share" 2>/dev/null ||
  sudo rmdir "$stage_dir/share/ecs/corpus" "$stage_dir/share/ecs" "$stage_dir/share" 2>/dev/null || true
[[ ! -e "$corpus" ]] || die "corpus is still present after removal"

echo "verify-tools-stage: $arch verified, corpus removed" >&2

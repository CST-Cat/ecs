#!/usr/bin/env bash
set -euo pipefail

# 校验某个架构的工具 stage，并把 Silesia 语料从中摘出去。
#
# 校验分两层：
#
#   1. cmd/tools-manifest-check 用 ecs 自己的解析器读 manifest——发布物必须能被
#      将要读它的那份代码认下来，而不是只能被 jq 认下来；
#   2. 下面的 jq 契约再断言构建口径本身：工具数量、toolchain_mode、smoke 运行器、
#      三元组、验证范围、NPB 的 smoke class、每个工具的架构与 SHA-256 格式。
#
# 构建口径（cross 还是 native、smoke 运行器是谁）不在这里第二次定义，而是向
# build_tools_container.sh --print-params 索取。两处各写一份迟早会分叉。
#
# 语料是独立发布物：它 200 MB 出头，七个架构各带一份会让 Release 膨胀到没有
# 必要的体积。摘除前先核对尺寸与摘要，确认摘掉的确实是那一份。

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
command -v jq >/dev/null 2>&1 || die "jq is required"

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

# 第一层：用 ecs 自己的解析器读。
go run "$ECS_REPO_ROOT/cmd/tools-manifest-check" --architecture "$arch" "$manifest"

# 第二层：构建口径契约。
jq -e \
  --arg arch "$arch" \
  --arg toolchain_mode "$toolchain_mode" \
  --arg target_runner "$target_runner" \
  --arg npb_class "$npb_class" \
  --argjson tool_count "${#ECS_TOOL_NAMES[@]}" '
  .architecture == $arch and
  (.tools | length == $tool_count) and
  (.build.toolchain_mode == $toolchain_mode) and
  (.build.smoke_runner == $target_runner) and
  (.build.build_triplet | type == "string" and length > 0) and
  (.build.target_triplet | type == "string" and length > 0) and
  (.build.validation.scope == "functional") and
  (.build.validation.performance_valid == false) and
  (all(.tools[] | select(.name == "npb-ep" or .name == "npb-ft");
    .parameters.ci_smoke_class == $npb_class)) and
  (all(.tools[]; .architecture == $arch and (.sha256 | test("^[0-9A-Fa-f]{64}$"))))
' "$manifest" >/dev/null || die "$arch manifest violates the build contract"

# 十个工具都必须真的在 stage 里，且可执行。
for tool in "${ECS_TOOL_NAMES[@]}"; do
  [[ -x "$stage_dir/bin/$tool" ]] || die "$arch stage is missing an executable $tool"
done
[[ -d "$stage_dir/LICENSES" ]] || die "$arch stage is missing LICENSES"

corpus="$stage_dir/share/ecs/corpus/ecs-silesia-v1.corpus"
if [[ "$keep_corpus" -eq 1 ]]; then
  echo "verify-tools-stage: $arch verified, corpus kept" >&2
  exit 0
fi

[[ -f "$corpus" ]] || die "$arch stage is missing the Silesia corpus"
actual_bytes=$(stat -c %s "$corpus")
[[ "$actual_bytes" -eq "$ECS_CORPUS_BYTES" ]] ||
  die "corpus size = $actual_bytes, want $ECS_CORPUS_BYTES"
actual_sha=$(sha256sum "$corpus" | awk '{print $1}')
[[ "$actual_sha" == "$ECS_CORPUS_SHA256" ]] ||
  die "corpus sha256 = $actual_sha, want $ECS_CORPUS_SHA256"

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

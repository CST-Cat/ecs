#!/usr/bin/env bash
set -euo pipefail

# 把一组已经验证过的制品发布到 GitHub Release。
#
# 这是整条流水线上唯一需要仓库写权限的动作，所以它的实现放在仓库里可审阅，
# 而不是埋在 workflow YAML 里——最该被人看懂的一段逻辑，不该是最难看到的。
#
# 先建草稿再转正：上传中途断掉时，留下的是一个草稿而不是一个只有一半资产的
# 公开 Release。全部资产在远端可见之后才 --draft=false。
#
# 重跑安全：已存在的草稿会被复用（断点续传），但绝不会去改一个已经转正的
# Release——那意味着用户下载过的东西被换掉了。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  echo "usage: scripts/release/publish.sh --tag TAG --version VERSION --revision SHA --dist DIR" >&2
}

die() {
  echo "release-publish: $*" >&2
  exit 1
}

tag=""
version=""
revision=""
dist=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --tag)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--tag requires a value"
      tag=$2
      shift 2
      ;;
    --version)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--version requires a value"
      version=$2
      shift 2
      ;;
    --revision)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--revision requires a value"
      revision=$2
      shift 2
      ;;
    --dist)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--dist requires a value"
      dist=$2
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

[[ -n "$tag" && -n "$version" && -n "$revision" && -n "$dist" ]] || {
  usage
  die "--tag / --version / --revision / --dist are all required"
}
[[ "$dist" == /* ]] || dist="$ECS_REPO_ROOT/$dist"
[[ -d "$dist" ]] || die "no such dist directory: $dist"
command -v gh >/dev/null 2>&1 || die "gh is required"

# ---- 发布说明 ----
#
# 取自 CHANGELOG.md 里对应版本那一节。取不到就失败：一个没有说明的 Release
# 对用户没有意义，而"忘了写 CHANGELOG"正是应该在这里被拦住的疏漏。
notes_file=$(mktemp)
trap 'rm -f -- "$notes_file"' EXIT

awk -v version="$version" '
  BEGIN { heading = "## " version; found = 0 }
  $0 == heading || index($0, heading " ") == 1 { found = 1; print; next }
  found && /^## / { exit }
  found { print }
  END { if (!found) exit 1 }
' CHANGELOG.md >"$notes_file" || die "CHANGELOG.md 里没有 $version 这一节"
[[ -s "$notes_file" ]] || die "CHANGELOG.md 的 $version 一节是空的"

printf '\n\n---\n完整版本历史：[CHANGELOG.md](%s)\n' \
  "https://github.com/${GITHUB_REPOSITORY:-CST-Cat/ecs}/blob/${revision}/CHANGELOG.md" \
  >>"$notes_file"
echo "release-publish: 已从 CHANGELOG.md 取出 $version 的发布说明" >&2

# ---- 资产清单 ----
assets=(checksums.txt ecs-corpus_silesia-v1.tar.gz)
for arch in "${ECS_ARCHES[@]}"; do
  assets+=("ecs_linux_${arch}.tar.gz" "ecs-tools_linux_${arch}.tar.gz")
done

uploads=()
for asset in "${assets[@]}"; do
  [[ -s "$dist/$asset" ]] || die "缺少发布资产 $asset"
  uploads+=("$dist/$asset")
done

# ---- 草稿 ----
if gh release view "$tag" >/dev/null 2>&1; then
  [[ "$(gh release view "$tag" --json isDraft --jq .isDraft)" == "true" ]] ||
    die "$tag 已经是正式 Release，拒绝改动已发布的东西"
  echo "release-publish: 复用已有草稿 $tag" >&2
  gh release edit "$tag" --notes-file "$notes_file" >&2
else
  gh release create "$tag" --verify-tag --draft --notes-file "$notes_file" >&2
fi

ecs_retry gh release upload "$tag" "${uploads[@]}" --clobber >&2

# ---- 转正前确认远端真的齐了 ----
remote=$(gh release view "$tag" --json assets --jq '.assets[].name')
for asset in "${assets[@]}"; do
  grep -F -x "$asset" <<<"$remote" >/dev/null || die "远端缺少 $asset，保持草稿状态"
done

gh release edit "$tag" --draft=false >&2
[[ "$(gh release view "$tag" --json isDraft --jq .isDraft)" == "false" ]] ||
  die "$tag 未能转为正式 Release"

echo "release-publish: $tag 已发布，共 ${#assets[@]} 个资产" >&2

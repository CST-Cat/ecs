#!/usr/bin/env bash
set -euo pipefail

# 把一组已经验证过的制品发布到 GitHub Release。
#
# 这是整条流水线上唯一需要仓库写权限的动作，所以它的实现放在仓库里可审阅，
# 而不是埋在 workflow YAML 里——最该被人看懂的一段逻辑，不该是最难看到的。
#
# --check-only 是远端零副作用的发布边界演练：走发布说明解析、依赖引用解析和
# 资产清单校验，但在任何 gh release 查询/创建/编辑/上传之前返回。ECS 的
# workflow_dispatch 使用 version=dev，没有对应的版本化 CHANGELOG 章节，因此
# 该特例只跳过版本说明抽取；正式 tag 发布仍必须完整抽取双语说明。
#
# 正式发布先建草稿再转正：上传中途断掉时，留下的是一个草稿而不是一个只有
# 一半资产的公开 Release。全部资产在远端可见之后才 --draft=false。
#
# 重跑安全：已存在的草稿会被复用（断点续传），但绝不会去改一个已经转正的
# Release——那意味着用户下载过的东西被换掉了。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/release/publish.sh [--kind ecs|bundle] --tag TAG --version VERSION --revision SHA --dist DIR [--check-only]

  --kind ecs|bundle  发布 ECS（默认）或 Bundle
  --check-only       只校验发布说明/依赖引用/资产，不访问或修改 GitHub Release
USAGE
}

die() {
  echo "release-publish: $*" >&2
  exit 1
}

tag=""
version=""
revision=""
dist=""
kind="ecs"
check_only=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --kind)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--kind requires ecs or bundle"
      case "$2" in
        ecs | bundle) kind=$2 ;;
        *) die "--kind must be ecs or bundle" ;;
      esac
      shift 2
      ;;
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
    --check-only)
      check_only=1
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

[[ -n "$tag" && -n "$version" && -n "$revision" && -n "$dist" ]] || {
  usage
  die "--tag / --version / --revision / --dist are all required"
}
[[ "$dist" == /* ]] || dist="$ECS_REPO_ROOT/$dist"
[[ -d "$dist" ]] || die "no such dist directory: $dist"

# ---- 发布说明 ----
#
# ECS（--kind ecs）取自 CHANGELOG.md 对应版本的中文与 English 小节，并在顶部
# 以中英双语简单引用当前最新的 Bundle 工具包与 GNU SDK 快照；Bundle
# （--kind bundle）取自 tools/BUNDLE_NOTES.md 的对应版本章节。取不到就失败：
# 一个没有完整双语说明的 Release 对用户没有意义，而这种疏漏应该在发布时被拦住。
notes_source=CHANGELOG.md
if [[ "$kind" == "bundle" ]]; then
  notes_source=tools/BUNDLE_NOTES.md
fi
notes_file=$(mktemp)
trap 'rm -f -- "$notes_file"' EXIT
: >"$notes_file"

append_changelog_language() {
  local language=$1 status=0
  awk -v version="$version" -v language_heading="### $language" '
    BEGIN {
      heading = "## " version
      found = 0
      selected = 0
      has_content = 0
      pending_blank = ""
      version_heading = ""
    }
    $0 == heading || index($0, heading " ") == 1 {
      found = 1
      version_heading = $0
      next
    }
    found && /^## / { exit }
    found && $0 == language_heading {
      selected = 1
      print version_heading
      print ""
      next
    }
    selected && /^### / { exit }
    selected && $0 ~ /^[[:space:]]*$/ {
      if (has_content) pending_blank = pending_blank "\n"
      next
    }
    selected {
      printf "%s%s\n", pending_blank, $0
      pending_blank = ""
      has_content = 1
    }
    END {
      if (!found || !selected || !has_content) exit 1
    }
  ' "$notes_source" >>"$notes_file" || status=$?
  [[ "$status" -eq 0 ]] ||
    die "${notes_source} 的 $version 章节缺少非空 $language 小节"
}

repository=${GITHUB_REPOSITORY:-CST-Cat/ecs}
changelog_url="https://github.com/${repository}/blob/${revision}/CHANGELOG.md"
notes_required=1
if [[ "$check_only" -eq 1 && "$kind" == "ecs" && "$version" == "dev" ]]; then
  notes_required=0
fi

if [[ "$kind" == "ecs" ]]; then
  bundle_tag=$(<tools/BUNDLE)
  [[ -n "$bundle_tag" ]] || die "tools/BUNDLE 为空"
  bundle_url="https://github.com/${repository}/releases/tag/${bundle_tag}"
  sdk_tag=$(jq -r '.targets.freebsd_amd64.prebuilt.url' tools/freebsd-gnu-openmp.lock.json)
  sdk_tag=${sdk_tag#*releases/download/}
  sdk_tag=${sdk_tag%%/*}
  [[ -n "$sdk_tag" ]] || die "无法从 FreeBSD GNU SDK lock 解析快照 tag"
  sdk_url="https://github.com/${repository}/releases/tag/${sdk_tag}"
  if [[ "$notes_required" -eq 1 ]]; then
    printf '第三方工具包：\n[%s](%s)\n' "$bundle_tag" "$bundle_url" >>"$notes_file"
    printf 'GNU SDK 快照：\n[%s](%s)\n\n' "$sdk_tag" "$sdk_url" >>"$notes_file"
  fi
fi

if [[ "$notes_required" -eq 1 ]]; then
  append_changelog_language "中文"
  printf '\n完整版本历史：[CHANGELOG.md](%s)\n' "$changelog_url" >>"$notes_file"

  printf '\n---\n\n' >>"$notes_file"

  if [[ "$kind" == "ecs" ]]; then
    printf 'Third-party Tool Package:\n[%s](%s)\n' \
      "$bundle_tag" "$bundle_url" >>"$notes_file"
    printf 'GNU SDK snapshot:\n[%s](%s)\n\n' \
      "$sdk_tag" "$sdk_url" >>"$notes_file"
  fi
  append_changelog_language "English"
  printf '\nFull version history: [CHANGELOG.md](%s)\n' \
    "$changelog_url" >>"$notes_file"
  echo "release-publish: 已从 $notes_source 取出 $version 的中英文发布说明" >&2
else
  echo "release-publish: ECS dev 彩排没有版本化 CHANGELOG 章节；已校验 Bundle/SDK 引用，跳过说明抽取" >&2
fi

# ---- 资产清单 ----
assets=(checksums.txt)
case "$kind" in
  ecs)
    for target in "${ECS_TARGET_IDS[@]}"; do
      assets+=("ecs_${target}.tar.gz")
    done
    ;;
  bundle)
    for target in "${ECS_TARGET_IDS[@]}"; do
      assets+=("ecs-tools_${target}.tar.gz")
    done
    assets+=("$ECS_CORPUS_ARCHIVE")
    ;;
esac

uploads=()
for asset in "${assets[@]}"; do
  [[ -s "$dist/$asset" ]] || die "缺少发布资产 $asset"
  uploads+=("$dist/$asset")
done

# 远端零副作用边界：到这里已经完成正式发布前能够本地确定性验证的全部内容。
# check-only 必须在 command -v gh 和任何 gh 子命令之前返回。
if [[ "$check_only" -eq 1 ]]; then
  echo "release-publish: check-only 通过；$kind / $tag 的 ${#assets[@]} 个资产已校验，未访问或修改 GitHub Release" >&2
  exit 0
fi

command -v gh >/dev/null 2>&1 || die "gh is required"

# ---- 草稿 ----
if gh release view "$tag" >/dev/null 2>&1; then
  [[ "$(gh release view "$tag" --json isDraft --jq .isDraft)" == "true" ]] ||
    die "$tag 已经是正式 Release，拒绝改动已发布的东西"
  echo "release-publish: 复用已有草稿 $tag" >&2
  if [[ "$kind" == "bundle" ]]; then
    gh release edit "$tag" --title "Third-party Tool Package · $tag" \
      --notes-file "$notes_file" >&2
  else
    gh release edit "$tag" --notes-file "$notes_file" >&2
  fi
else
  create_args=("$tag" --draft --notes-file "$notes_file")
  if [[ "$kind" == "ecs" ]]; then
    create_args+=(--verify-tag)
  else
    create_args+=(--target "$revision" --latest=false \
      --title "Third-party Tool Package · $tag")
  fi
  gh release create "${create_args[@]}" >&2
fi

gh release upload "$tag" "${uploads[@]}" --clobber >&2

gh release edit "$tag" --draft=false >&2

echo "release-publish: $tag 已发布，共 ${#assets[@]} 个资产" >&2

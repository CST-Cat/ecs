#!/usr/bin/env bash
set -euo pipefail

# 冻结发布候选：解析出这次要发的提交，并判断它有没有资格发。
#
# 正式 tag push 在这里读取 main 一次并确认候选已进入 main。Release transaction
# 一旦开始，后续所有 job 都 checkout 到这里输出的 SHA，不再比较移动中的 main。
# workflow_dispatch 是 dev 彩排，直接冻结所选 ref 的 SHA，不要求它已合入 main。
#
# workflow_dispatch 永远是演练事件：即使维护者在 Actions UI 里选中了一个 v*
# tag 作为 dispatch ref，也只能得到 version=dev，绝不能因为 ref 看起来像发布 tag
# 就越过事件边界。正式版本只允许由 push 到 refs/tags/v* 解析。
#
# 输出是 GitHub Actions 的 key=value，写到 stdout；诊断信息一律走 stderr。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  echo "usage: scripts/release/freeze.sh --event EVENT_NAME --ref REF [--sha SHA]" >&2
}

die() {
  echo "release-freeze: $*" >&2
  exit 1
}

event=""
ref=""
sha=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --event)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--event requires a value"
      event=$2
      shift 2
      ;;
    --ref)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--ref requires a value"
      ref=$2
      shift 2
      ;;
    --sha)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--sha requires a value"
      sha=$2
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

[[ -n "$event" && -n "$ref" ]] || {
  usage
  die "--event 与 --ref 必填"
}

# 事件类型拥有最高优先级，不能让一个 workflow_dispatch 的 tag ref 冒充正式
# tag push。正式发布与演练首先由 event 分流，再在正式发布分支里解析 tag。
case "$event" in
  workflow_dispatch)
    [[ -n "$sha" ]] || die "workflow_dispatch 需要 --sha"
    candidate=$sha
    version=dev
    ;;
  push)
    case "$ref" in
      refs/tags/v*)
        tag=${ref#refs/tags/}
        candidate=$(git rev-list -n 1 "$tag")
        version=${tag#v}
        ;;
      *)
        die "不支持的正式发布 ref：$event / $ref"
        ;;
    esac
    ;;
  *)
    die "不支持的发布事件：$event / $ref"
    ;;
esac

# 版本号会进 ldflags 和归档名，先卡住字符集再往下走。
[[ "$version" =~ ^[0-9A-Za-z._+-]+$ ]] ||
  die "版本号只能含字母、数字、点、下划线、加号和连字符：$version"

# 正式 tag 发布候选必须已进入 main；dispatch 是 dev 彩排，使用其输入 SHA。
if [[ "$event" == push ]]; then
  git fetch --no-tags origin main >&2
  main_commit=$(git rev-parse refs/remotes/origin/main)
  [[ "$candidate" == "$main_commit" ]] ||
    die "发布候选 $candidate 不是远端 main $main_commit"
fi

echo "release-freeze: 冻结 $candidate（版本 $version）" >&2
printf 'sha=%s\n' "$candidate"
printf 'version=%s\n' "$version"

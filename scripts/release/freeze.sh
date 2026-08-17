#!/usr/bin/env bash
set -euo pipefail

# 冻结发布候选：解析出这次要发的提交，并判断它有没有资格发。
#
# main 只在这里读一次。Release transaction 一旦开始，后续所有 job 都 checkout
# 到这里输出的 SHA——不再拿移动中的 main 和已经冻结的 tag 做比较。那种比较
# 会在发布过程中有人推 main 时莫名其妙地失败。
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

case "$ref" in
  refs/tags/v*)
    tag=${ref#refs/tags/}
    candidate=$(git rev-list -n 1 "$tag")
    version=${tag#v}
    ;;
  *)
    [[ "$event" == "workflow_dispatch" ]] ||
      die "不支持的发布事件：$event / $ref"
    [[ -n "$sha" ]] || die "workflow_dispatch 需要 --sha"
    candidate=$sha
    version=dev
    ;;
esac

# 版本号会进 ldflags 和归档名，先卡住字符集再往下走。
[[ "$version" =~ ^[0-9A-Za-z._+-]+$ ]] ||
  die "版本号只能含字母、数字、点、下划线、加号和连字符：$version"

# main 只在此处读取，之后不再参与任何判断。
git fetch --no-tags origin main >&2
main_commit=$(git rev-parse refs/remotes/origin/main)
[[ "$candidate" == "$main_commit" ]] ||
  die "发布候选 $candidate 不是远端 main $main_commit"

echo "release-freeze: 冻结 $candidate（版本 $version）" >&2
printf 'sha=%s\n' "$candidate"
printf 'version=%s\n' "$version"

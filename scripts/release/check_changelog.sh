#!/usr/bin/env bash
set -euo pipefail

# 发布前尽早确认对应版本有可用的发布说明。
#
# 章节只有标题没有正文时也算缺失：这样的 Release 虽然能构建，却没有给用户
# 任何可读的变更说明。发布 workflow 的 preflight 和最终 publish 都调用这份
# 检查，避免两处对“非空”的理解分叉。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  echo "usage: scripts/release/check_changelog.sh --version VERSION" >&2
}

die() {
  echo "release-changelog: $*" >&2
  exit 1
}

version=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --version)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--version requires a value"
      version=$2
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

[[ -n "$version" ]] || {
  usage
  die "--version is required"
}
[[ "$version" =~ ^[0-9A-Za-z._+-]+$ ]] || die "invalid version: $version"
[[ -f CHANGELOG.md ]] || die "CHANGELOG.md 不存在"

status=0
awk -v version="$version" '
  BEGIN {
    heading = "## " version
    found = 0
    has_content = 0
  }
  $0 == heading || index($0, heading " ") == 1 {
    found = 1
    next
  }
  found && /^## / { exit }
  found && $0 !~ /^[[:space:]]*$/ { has_content = 1 }
  END {
    if (!found) exit 1
    if (!has_content) exit 2
  }
' CHANGELOG.md || status=$?

case "$status" in
  0)
    echo "release-changelog: $version 章节存在且非空" >&2
    ;;
  1)
    die "CHANGELOG.md 里没有 $version 这一节"
    ;;
  2)
    die "CHANGELOG.md 的 $version 一节没有正文"
    ;;
  *)
    die "校验 CHANGELOG.md 的 $version 一节失败（awk exit $status）"
    ;;
esac

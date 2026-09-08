#!/usr/bin/env bash
set -euo pipefail

# 发布前尽早确认对应版本有中英文发布说明。
#
# 任一语言只有标题没有正文时也算缺失：这样的 Release 虽然能构建，却没有完整
# 的双语变更说明。publish.sh 随后只抽取 English 小节作为 Release notes。

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
    language = ""
    found_zh = 0
    found_en = 0
    has_zh = 0
    has_en = 0
  }
  $0 == heading || index($0, heading " ") == 1 {
    found = 1
    next
  }
  found && /^## / { exit }
  found && $0 == "### 中文" {
    language = "zh"
    found_zh = 1
    next
  }
  found && $0 == "### English" {
    language = "en"
    found_en = 1
    next
  }
  found && /^### / {
    language = ""
    next
  }
  found && language == "zh" && $0 !~ /^[[:space:]]*$/ { has_zh = 1 }
  found && language == "en" && $0 !~ /^[[:space:]]*$/ { has_en = 1 }
  END {
    if (!found) exit 1
    if (!found_zh || !has_zh) exit 2
    if (!found_en || !has_en) exit 3
  }
' CHANGELOG.md || status=$?

case "$status" in
  0)
    echo "release-changelog: $version 的中文与 English 小节均存在且非空" >&2
    ;;
  1)
    die "CHANGELOG.md 里没有 $version 这一节"
    ;;
  2)
    die "CHANGELOG.md 的 $version 章节缺少非空中文小节"
    ;;
  3)
    die "CHANGELOG.md 的 $version 章节缺少非空 English 小节"
    ;;
  *)
    die "校验 CHANGELOG.md 的 $version 一节失败（awk exit $status）"
    ;;
esac
